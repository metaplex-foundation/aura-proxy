package middlewares

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	auraProto "github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"google.golang.org/protobuf/types/known/emptypb"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	subscriptionsListUpdateInterval = 5 * time.Minute
	userCacheTTL                    = 10 * time.Minute
	userInfoCacheInterval           = time.Minute
)

var (
	ErrEmptyAPIToken    = errors.New("Usage without a token is no longer available. For future use, register and receive a free API") // TODO: remove
	ErrCreditsExhausted = errors.New("You've exhausted the credits for current subscription. Please upgrade your plan")
)

type ITokenChecker interface {
	CheckToken(cc *echoUtil.CustomContext, token string) (userInfo *auraProto.UserWithTokens, err error)
}

func APITokenCheckerMiddleware(tokenChecker ITokenChecker) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cp := util.NewRuntimeCheckpoint("APITokenCheckerMiddleware")
			cc := c.(*echoUtil.CustomContext) //nolint:errcheck

			token := strings.TrimRight(c.Param(echoUtil.TokenParamName), "/")
			userInfo, err := tokenChecker.CheckToken(cc, token)
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
			// load userUID info to custom context
			cc.SetUserInfo(userInfo)
			cc.SetAPIToken(token)

			return next(c)
		}
	}
}

func (t *TokenChecker) UserBalanceMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := c.(*echoUtil.CustomContext)

			token := cc.GetAPIToken()
			var userInfo *auraProto.UserWithTokens

			t.userCacheMx.RLock()
			u, ok := t.userCache[token]
			t.userCacheMx.RUnlock()
			// very low chance to get into this scenario though
			// expected that token always will be in the cache
			if !ok {
				userInfo = cc.GetUserInfo()
			} else {
				userInfo = u.GetUser()
			}

			tokens := userInfo.GetTokens()
			userInfo.MplxBalance -= cc.GetCreditsUsed()

			t.userCacheMx.Lock()
			for _, tkn := range tokens {
				t.userCache[tkn] = &auraProto.GetUserInfoResp{
					User: userInfo,
				}
			}
			t.userCacheMx.Unlock()

			return next(c)
		}
	}
}

type TokenChecker struct {
	userCache          map[string]*auraProto.GetUserInfoResp
	userCacheMx        sync.RWMutex
	auraAPI            auraProto.AuraClient
	subscriptionList   map[int64]*auraProto.SubscriptionWithPricing
	subscriptionListMx sync.RWMutex
}

func NewTokenChecker(ctx context.Context, auraAPI auraProto.AuraClient) (*TokenChecker, error) {
	if auraAPI == nil {
		return nil, errors.New("empty auraAPI")
	}

	t := &TokenChecker{
		userCache:          make(map[string]*auraProto.GetUserInfoResp),
		userCacheMx:        sync.RWMutex{},
		subscriptionList:   make(map[int64]*auraProto.SubscriptionWithPricing),
		subscriptionListMx: sync.RWMutex{},
		auraAPI:            auraAPI,
	}

	err := util.AsyncRunWithInterval(ctx, nil, subscriptionsListUpdateInterval, true, false, func(ctx context.Context) error {
		err := t.updateSubscriptionList(ctx)
		if err != nil {
			err = fmt.Errorf("TokenChecker.updateSubscriptionList: %s", err)
			log.Logger.Proxy.Error(err.Error())
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	t.setupStream(ctx)

	return t, nil
}

func (t *TokenChecker) setupStream(ctx context.Context) error {
	// clear cache before fetching fresh data
	t.userCacheMx.Lock()
	clear(t.userCache)
	t.userCacheMx.Unlock()

	err := t.loadAllTheUsers(ctx)
	if err != nil {
		return err
	}

	// start streaming updates in background
	go t.maintainUserStream(ctx)

	return nil
}

func (t *TokenChecker) loadAllTheUsers(ctx context.Context) error {
	// get initial data
	s, err := t.auraAPI.GetAllUsers(ctx, new(emptypb.Empty))
	if err != nil {
		return fmt.Errorf("getting initial users: %w", err)
	}

	// load initial data into cache
	for {
		userInfo, err := s.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("receiving initial users: %w", err)
		}

		t.userCacheMx.Lock()
		for _, tkn := range userInfo.GetUser().GetTokens() {
			t.userCache[tkn] = userInfo
		}
		t.userCacheMx.Unlock()
	}

	return nil
}

func (t *TokenChecker) maintainUserStream(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 1 * time.Minute

	initialLaunch := true

	for {
		if !initialLaunch {
			err := t.loadAllTheUsers(ctx)
			if err != nil {
				log.Logger.Proxy.Error("loadAllTheUsers error: ", err)
				time.Sleep(backoff)
				continue
			}
		}

		initialLaunch = false

		// start the stream
		stream, err := t.auraAPI.GetUserInfo(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return // context cancelled, exit
			case <-time.After(backoff):
				log.Logger.Proxy.Errorf("failed to start user stream: %v, retrying in %v", err, backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}

		// reset backoff on successful connection
		backoff = time.Second

		// process stream updates until an error occurs
		err = t.processStreamUpdates(ctx, &stream)
		if ctx.Err() != nil {
			return // context cancelled, exit
		}

		// if we're here, there was an error, so wait and retry
		log.Logger.Proxy.Errorf("stream error: %v, reconnecting in %v", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (t *TokenChecker) processStreamUpdates(ctx context.Context, stream *auraProto.Aura_GetUserInfoClient) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			userInfo, err := (*stream).Recv()
			if err != nil {
				if err == io.EOF {
					return fmt.Errorf("stream closed by server")
				}
				return err
			}

			t.userCacheMx.Lock()
			for _, tkn := range userInfo.GetUser().GetTokens() {
				t.userCache[tkn] = userInfo
			}
			for _, tkn := range userInfo.GetUser().GetDeletedTokens() {
				delete(t.userCache, tkn)
			}
			for _, tkn := range userInfo.GetUser().GetDeprecatedTokens() {
				delete(t.userCache, tkn)
			}
			t.userCacheMx.Unlock()
		}
	}
}

func (t *TokenChecker) CheckToken(cc *echoUtil.CustomContext, token string) (userInfo *auraProto.UserWithTokens, err error) {
	if token == "" {
		return userInfo, ErrEmptyAPIToken
	}

	// validate token
	_, err = uuid.Parse(token)
	if err != nil {
		return userInfo, fmt.Errorf("uuid.Parse(%s): %s", token, err)
	}

	user, err := t.getUserFromAPICached(cc, token)
	if err != nil {
		return userInfo, fmt.Errorf("getUserFromAPICached: %w", err)
	}
	userInfo = user.GetUser()

	return
}

func (t *TokenChecker) updateSubscriptionList(ctx context.Context) error {
	subscriptions, err := t.auraAPI.GetSubscriptions(ctx, new(emptypb.Empty))
	if err != nil {
		return fmt.Errorf("GetSubscriptions: %s", err)
	}
	convertedSubscriptions := make(map[int64]*auraProto.SubscriptionWithPricing, len(subscriptions.GetSubscriptions()))
	for _, sub := range subscriptions.GetSubscriptions() {
		convertedSubscriptions[sub.GetId()] = sub
	}

	t.subscriptionListMx.Lock()
	t.subscriptionList = convertedSubscriptions
	t.subscriptionListMx.Unlock()

	return nil
}

func (t *TokenChecker) getUserFromAPICached(cc *echoUtil.CustomContext, token string) (user *auraProto.GetUserInfoResp, err error) {
	t.userCacheMx.RLock()
	cachedUserInterface, ok := t.userCache[token]
	t.userCacheMx.RUnlock()

	if !ok {
		return nil, errors.New("no user's token found")
	}
	user = cachedUserInterface

	if user.GetUser().GetSubscriptionEndsOn() != nil && time.Now().After(user.GetUser().GetSubscriptionEndsOn().AsTime()) {
		return nil, errors.New("no active subscription")
	}
	t.subscriptionListMx.RLock()
	subcription, ok := t.subscriptionList[user.GetUser().GetSubscriptionId()]
	t.subscriptionListMx.RUnlock()
	if !ok {
		log.Logger.Proxy.Errorf("invalid SubscriptionId: %d", user.GetUser().GetSubscriptionId())
	}
	cc.SetSubscription(subcription)

	credits := int64(len(cc.GetReqMethods())) * cc.GetReqCost()
	if user.GetUser().GetMplxBalance() < credits {
		return user, ErrCreditsExhausted
	}
	cc.SetCreditsUsed(credits)

	return
}
