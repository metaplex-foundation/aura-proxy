package middlewares

import (
	"time"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/metrics"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

func NewMetricsMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			cc := c.(*echoUtil.CustomContext) //nolint:errcheck
			chain := cc.GetChainName()

			if c.IsWebSocket() {
				metrics.DecWebsocketConnections(chain)
				return err
			}

			cp := util.NewRuntimeCheckpoint("NewMetricsMiddleware")

			if cc.GetTokenType() == models.OnlyPublicNodesTokenType { // our test req
				return err
			}
			rpcMethod := cc.GetReqMethod()
			success := !cc.GetProxyHasError() || cc.GetProxyUserError()

			if rpcErrors := cc.GetRPCErrors(); len(rpcErrors) != 0 {
				metrics.IncRPCErrors(cc.GetRPCError(), cc.GetProxyEndpoint(), cc.GetReqMethod())
			}

			metrics.IncHTTPResponsesTotalCnt(chain, rpcMethod, success, cc.GetTargetType())
			metrics.ObserveNodeAttempts(chain, rpcMethod, success, cc.GetProxyAttempts())
			metrics.ObserveNodeResponseTime(chain, rpcMethod, success, cc.GetProxyResponseTime())
			metrics.ObserveExecutionTime(chain, rpcMethod, success, time.Since(cc.GetReqDuration()))

			cc.GetMetrics().AddCheckpoint(cp)

			return err
		}
	}
}
