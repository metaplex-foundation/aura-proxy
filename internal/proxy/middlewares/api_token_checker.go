package middlewares

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	auraProto "github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"google.golang.org/protobuf/types/known/emptypb"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	subscriptionsListUpdateInterval = 5 * time.Minute
	userCacheTTL                    = 10 * time.Minute
	userInfoCacheInterval           = 10 * time.Minute
)

var (
	ErrEmptyAPIToken    = errors.New("Usage without a token is no longer available. For future use, register and receive a free API") // TODO: remove
	ErrCreditsExhausted = errors.New("You've exhausted the credits for current subscription. Please upgrade your plan")
)

func APITokenCheckerMiddleware(tokenChecker *TokenChecker) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cp := util.NewRuntimeCheckpoint("APITokenCheckerMiddleware")
			cc := c.(*echoUtil.CustomContext) //nolint:errcheck

			token := strings.TrimRight(c.Param(echoUtil.TokenParamName), "/")
			userInfo, err := tokenChecker.CheckToken(c.Request().Context(), token)
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
			tokenChecker.subscriptionListMx.RLock()
			subcription, ok := tokenChecker.subscriptionList[userInfo.GetSubscriptionId()]
			tokenChecker.subscriptionListMx.RUnlock()
			if !ok {
				log.Logger.Proxy.Errorf("invalid SubscriptionId: %d", userInfo.GetSubscriptionId())
			}
			cc.SetSubscription(subcription)

			return next(c)
		}
	}
}

type TokenChecker struct {
	userCache          *cache.Cache
	auraAPI            auraProto.AuraClient
	subscriptionList   map[int64]*auraProto.SubscriptionWithPricing
	subscriptionListMx sync.RWMutex
}

func NewTokenChecker(ctx context.Context, auraAPI auraProto.AuraClient) (*TokenChecker, error) {
	if auraAPI == nil {
		return nil, errors.New("empty auraAPI")
	}

	t := &TokenChecker{
		userCache:          cache.New(userCacheTTL, userCacheTTL),
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

	return t, nil
}

func (t *TokenChecker) CheckToken(ctx context.Context, token string) (userInfo *auraProto.UserWithTokens, err error) {
	if token == "" {
		return userInfo, ErrEmptyAPIToken
	}

	// validate token
	_, err = uuid.Parse(token)
	if err != nil {
		return userInfo, fmt.Errorf("uuid.Parse(%s): %s", token, err)
	}

	user, err := t.getUserFromAPICached(ctx, token)
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

func (t *TokenChecker) getUserFromAPICached(ctx context.Context, token string) (user *auraProto.GetUserInfoResp, err error) {
	cachedUserInterface, ok := t.userCache.Get(token)
	user, _ = cachedUserInterface.(*auraProto.GetUserInfoResp)
	if !ok || (user.GetUser().GetSubscriptionEndsOn() != nil && time.Now().After(user.GetUser().GetSubscriptionEndsOn().AsTime())) {
		user, err = t.auraAPI.GetUserInfo(ctx, &auraProto.GetUserInfoReq{ApiToken: token})
		if err != nil {
			return user, fmt.Errorf("GetUserInfo: %s", err)
		}
		for _, tkn := range user.GetUser().GetTokens() {
			t.userCache.Set(tkn, user, userInfoCacheInterval)
		}
	}

	if user.GetUser().GetSubscriptionEndsOn() != nil && time.Now().After(user.GetUser().GetSubscriptionEndsOn().AsTime()) {
		return nil, errors.New("no active subscription")
	}
	if user.GetUser().GetMplxBalance() <= 0 {
		return nil, ErrCreditsExhausted
	}

	return
}
