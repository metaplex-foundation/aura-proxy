package integrationtest

import (
	echoUtil "aura-proxy/internal/pkg/util/echo"

	auraProto "github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/labstack/echo/v4"
)

// IStatCollector dummy
type testStatCollector struct{}

func (t *testStatCollector) Add(s *auraProto.Stat) {}

// IRequestCounter dummy
type testRequestCounter struct{}

func (t *testRequestCounter) IncUserRequests(user *auraProto.UserWithTokens, currentReqCount int64, chain, token, requestType string, isMainnet bool) {
	// no-op
}

// testTokenChecker implements the ITokenChecker interface.
type testTokenChecker struct{}

// CheckToken always returns a dummy user.
func (t *testTokenChecker) CheckToken(cc *echoUtil.CustomContext, token string) (*auraProto.UserWithTokens, error) {
	cc.SetSubscription(&auraProto.SubscriptionWithPricing{
		Pricing: &auraProto.Pricing{
			SolanaWebsocket: &auraProto.PricingModel{
				RequestsPerSecond: 10,
			},
		},
	})

	return &auraProto.UserWithTokens{
		User:           "dummyUserID",
		SubscriptionId: 42, // or whatever test value you want
		Tokens:         []string{"testToken1", "testToken2"},
		MplxBalance:    1000,
		// fill in other fields as needed
	}, nil
}

// UserBalanceMiddleware returns a no-op middleware that simply calls next.
func (t *testTokenChecker) UserBalanceMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// do nothing, just proceed
			return next(c)
		}
	}
}
