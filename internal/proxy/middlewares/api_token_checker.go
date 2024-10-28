package middlewares

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"golang.org/x/time/rate"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	tokenWhiteListUpdateInterval = 5 * time.Minute
	userCacheTTL                 = 10 * time.Minute
	userInfoCacheInterval        = 10 * time.Minute
)
const (
	bannedTokenCacheTTL                  = time.Minute
	bannedTokenExpireIn                  = 2 * time.Minute
	maxConsecutivelyReqsWithExpiredToken = 25
	refererHeader                        = "referer"
	domainRestrictionWildcard            = "*."
)

var (
	ErrEmptyAPIToken      = errors.New("Usage without a token is no longer available. For future use, register and receive a free API") // TODO: remove
	ErrCreditsExhausted   = errors.New("You've exhausted the credits for current subscription. Please upgrade your plan")
	ErrEmptyRefererHeader = echo.NewHTTPError(http.StatusUnauthorized, "empty 'referer' header")
	ErrWrongReferer       = echo.NewHTTPError(http.StatusUnauthorized, "request denied for current 'referer'")
)

func APITokenCheckerMiddleware(tokenChecker *TokenChecker) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cp := util.NewRuntimeCheckpoint("APITokenCheckerMiddleware")
			cc := c.(*echoUtil.CustomContext) //nolint:errcheck

			token := strings.TrimRight(c.Param(echoUtil.TokenParamName), "/")
			userUID, tokenType, isTrackedRequest, err := tokenChecker.CheckToken(c.Request().Context(), token)
			cc.GetMetrics().AddCheckpoint(cp)
			if err != nil {
				if errors.Is(err, ErrEmptyAPIToken) {
					return echo.NewHTTPError(http.StatusUnauthorized, ErrEmptyAPIToken.Error())
				}
				if errors.Is(err, ErrCreditsExhausted) {
					return echo.NewHTTPError(http.StatusUnauthorized, ErrCreditsExhausted.Error())
				}

				log.Logger.Proxy.Warnf("APITokenCheckerMiddleware: CheckToken (%s): %s", token, err)
				return util.ErrTokenInvalid
			}
			//if user != nil {
			//	if user.GetChain() != chainNameByHosts[c.Request().Host] {
			//		return util.ErrTokenInvalid
			//	}
			//	if err := checkDomainsRestriction(c, user.GetDomainsRestriction()); err != nil {
			//		return err
			//	}
			//}

			// load userUID info to custom context
			cc.SetUserUID(userUID, tokenType)
			//cc.SetUserInfo(user)

			cc.SetIsTrackedRequest(isTrackedRequest)

			return next(c)
		}
	}
}

type TokenChecker struct {
	userCache        *cache.Cache
	auraAPI          proto.AuraClient
	tokenWhiteList   map[string]models.TokenInfo
	bannedTokens     bannedTokenLimiter
	tokenWhiteListMx sync.RWMutex
}

type (
	bannedTokenLimiter struct {
		banned      *cache.Cache
		visitors    map[string]*visitor
		lastCleanup time.Time
		mutex       sync.Mutex
	}
	visitor struct {
		*rate.Limiter
		lastSeen time.Time
	}
)

func NewTokenChecker(ctx context.Context, auraAPI proto.AuraClient) (*TokenChecker, error) {
	if auraAPI == nil {
		return nil, errors.New("empty auraAPI")
	}

	t := &TokenChecker{
		userCache: cache.New(userCacheTTL, userCacheTTL),
		bannedTokens: bannedTokenLimiter{
			banned:      cache.New(bannedTokenCacheTTL, bannedTokenCacheTTL),
			visitors:    make(map[string]*visitor),
			lastCleanup: time.Now(),
		},
		tokenWhiteList:   make(map[string]models.TokenInfo),
		tokenWhiteListMx: sync.RWMutex{},
		auraAPI:          auraAPI,
	}

	err := util.AsyncRunWithInterval(ctx, nil, tokenWhiteListUpdateInterval, true, false, func(ctx context.Context) error {
		//err := t.updateTokenWhiteList(ctx)
		//if err != nil {
		//	err = fmt.Errorf("TokenChecker.updateTokenWhiteList: %s", err)
		//	log.Logger.Proxy.Error(err.Error())
		//	return err
		//}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (t *TokenChecker) CheckToken(ctx context.Context, token string) (userUID string, tokenType models.TokenType, tracked bool, err error) {
	if token == "" {
		return token, models.DefaultTokenType, tracked, ErrEmptyAPIToken
	}

	// validate token
	_, err = uuid.Parse(token)
	if err != nil {
		return token, models.DefaultTokenType, tracked, fmt.Errorf("uuid.Parse(%s): %s", token, err)
	}

	// prevent db query
	tokenData, ok := t.getWhiteListToken(token)
	if ok {
		return token, tokenData.TokenType, tokenData.Tracked, nil
	}

	//user, err = t.getUserFromAPICached(ctx, token)
	//if err != nil {
	//	return token, models.DefaultTokenType, nil, tracked, fmt.Errorf("getUserFromAPICached: %w", err)
	//}
	//
	//// in this case providerID is used instead of self-generated token to make it easier to replace a self-generated token with another
	//userUID = user.GetProviderId()
	//tokenType = models.TokenType(user.GetApiTokenType())

	if userUID == "" {
		return "", "", tracked, fmt.Errorf("empty providerID for token %s", token)
	}

	return
}

func (t *TokenChecker) getWhiteListToken(token string) (tokenType models.TokenInfo, ok bool) {
	t.tokenWhiteListMx.RLock()
	tokenType, ok = t.tokenWhiteList[token]
	t.tokenWhiteListMx.RUnlock()

	return
}

//func (t *TokenChecker) updateTokenWhiteList(ctx context.Context) error {
//	tokens, err := t.auraAPI.GetTokens(ctx, new(emptypb.Empty))
//	if err != nil {
//		return fmt.Errorf("GetTokens: %s", err)
//	}
//	convertedTokens := make(map[string]models.TokenInfo, len(tokens.GetTokens()))
//	for _, tkn := range tokens.GetTokens() {
//		convertedTokens[tkn.GetToken()] = models.TokenInfo{
//			TokenType: models.TokenType(tkn.GetTokenType()),
//			Tracked:   tkn.GetTracked(),
//		}
//	}
//
//	t.tokenWhiteListMx.Lock()
//	t.tokenWhiteList = convertedTokens
//	t.tokenWhiteListMx.Unlock()
//
//	return nil
//}
//
//func (t *TokenChecker) getUserFromAPICached(ctx context.Context, token string) (user *proto.GetUserInfoResp, err error) {
//	cachedUserInterface, ok := t.userCache.Get(token)
//	user, _ = cachedUserInterface.(*proto.GetUserInfoResp)
//
//	if !ok || time.Now().After(user.GetSubscriptionEndsOn().AsTime()) {
//		if t.bannedTokens.isBanned(token) {
//			return user, errors.New("too many requests with banned token")
//		}
//
//		user, err = t.auraAPI.GetUserInfo(ctx, &proto.GetUserInfoReq{ApiToken: token})
//		if err != nil {
//			return user, fmt.Errorf("GetUserInfo: %s", err)
//		}
//
//		if time.Now().After(user.GetSubscriptionEndsOn().AsTime()) || user.GetProjectIsDeleted() {
//			return nil, errors.New("no active subscription")
//		}
//		if user.GetIsBlocked() {
//			return nil, errors.New("blocked user")
//		}
//		if user.GetIsCreditExhausted() {
//			return nil, ErrCreditsExhausted
//		}
//		if user.GetChainProhibitedForSubscription() {
//			return nil, errors.New("chain prohibited for subscription")
//		}
//
//		t.userCache.Set(token, user, userInfoCacheInterval)
//	}
//
//	return
//}

func (b *bannedTokenLimiter) isBanned(token string) bool {
	_, ok := b.banned.Get(token)
	if ok {
		return ok
	}

	if ok = b.Allow(token); !ok {
		b.banned.SetDefault(token, struct{}{})
		return true
	}

	return false
}

func (b *bannedTokenLimiter) Allow(token string) bool {
	b.mutex.Lock()
	limiter, ok := b.visitors[token]
	if !ok {
		limiter = &visitor{Limiter: rate.NewLimiter(maxConsecutivelyReqsWithExpiredToken, maxConsecutivelyReqsWithExpiredToken)}
		b.visitors[token] = limiter
	}

	limiter.lastSeen = time.Now()
	if limiter.lastSeen.Sub(b.lastCleanup) > bannedTokenExpireIn {
		b.cleanupStaleVisitors()
	}
	b.mutex.Unlock()

	return limiter.Allow()
}

func (b *bannedTokenLimiter) cleanupStaleVisitors() {
	now := time.Now()
	for id, visitor := range b.visitors {
		if now.Sub(visitor.lastSeen) > bannedTokenExpireIn {
			delete(b.visitors, id)
		}
	}

	b.lastCleanup = time.Now()
}

func checkDomainsRestriction(c echo.Context, domainsRestriction []string) error {
	if len(domainsRestriction) == 0 {
		return nil
	}

	headerValue := echoUtil.PrepareDomainForRefererHeader(c.Request().Header.Get(refererHeader))
	if headerValue == "" {
		headerValue = echoUtil.PrepareDomainForRefererHeader(c.Request().Header.Get(echo.HeaderOrigin))
	}
	if headerValue == "" {
		return ErrEmptyRefererHeader
	}

	for _, d := range domainsRestriction {
		if d == headerValue || strings.HasPrefix(d, domainRestrictionWildcard) && strings.HasSuffix(headerValue, d[2:]) {
			return nil
		}
	}

	return ErrWrongReferer
}
