package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/adm-metaex/aura-api/pkg/types"
	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/metrics"
	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
	"aura-proxy/internal/proxy/middlewares"
)

const (
	headerNodeReqAttempts  = "X-NODE-REQ-ATTEMPTS"
	headerNodeResponseTime = "X-NODE-RESPONSE-TIME"
	headerNodeEndpoint     = "X-NODE-ENDPOINT"
)

func setServiceHeaders(h http.Header, cc *echoUtil.CustomContext) {
	if endpoint := cc.GetProxyEndpoint(); endpoint != "" {
		h.Set(headerNodeEndpoint, endpoint)
	}
	h.Set(headerNodeReqAttempts, fmt.Sprintf("%d", cc.GetProxyAttempts()))
	h.Set(headerNodeResponseTime, fmt.Sprintf("%dms", cc.GetProxyResponseTime()))
	h.Set(echo.HeaderXRequestID, cc.GetReqID())
}

func (p *proxy) serviceStatusHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		serviceKey: p.serviceName,
		statusKey:  statusOperational,
	})
}

type ITokenChecker interface {
	middlewares.ITokenChecker
	UserBalanceMiddleware() echo.MiddlewareFunc
}

func (p *proxy) initProxyHandlers(tokenChecker ITokenChecker) {
	apiTokenCheckerMiddleware := middlewares.APITokenCheckerMiddleware(tokenChecker)
	rateLimiterMiddleware := echoUtil.NewRateLimiter(func(c echo.Context) bool {
		return false
		// CustomContext must be inited before
		//cc := c.(*echoUtil.CustomContext) //nolint:errcheck
		//return !cc.GetTokenType().IsTokenRateLimited()
	})

	proxyMiddlewares := []echo.MiddlewareFunc{
		p.RequestPrepareMiddleware(),
		apiTokenCheckerMiddleware,
		// the request id middleware should be the first in the chain as it sets the request id for the context used by other middlewares including the clickhouse stats collector
		middlewares.RequestIDMiddleware(),
		// it's not only a logger, but also the clickhouse stats collector, so it should be the first middleware in the chain after the request prepare, user token check and request id middlewares
		middlewares.NewLoggerMiddleware(p.statsCollector.Add, p.isMainnet),
		rateLimiterMiddleware,
		middlewares.StreamRateLimitMiddleware(func(c echo.Context) bool { return !c.IsWebSocket() }), // WS rate limiter
		tokenChecker.UserBalanceMiddleware(),
		echoUtil.RequestTimeoutMiddleware(func(c echo.Context) bool { return c.IsWebSocket() }),
		// post-processing middlewares
		middlewares.NewMetricsMiddleware(),
	}
	p.router.POST("/", p.ProxyPostRouteHandler, proxyMiddlewares...)
	p.router.POST("/:token", p.ProxyPostRouteHandler, proxyMiddlewares...)
	p.router.GET("/service-status", p.serviceStatusHandler)
	p.router.GET("/", p.ProxyGetRouteHandler, proxyMiddlewares...)
	p.router.GET(echoUtil.ProxyPathWithToken, p.ProxyGetRouteHandler, proxyMiddlewares...)
	p.router.GET("/:token/", p.ProxyGetRouteHandler, proxyMiddlewares...)
}

func (p *proxy) ProxyGetRouteHandler(c echo.Context) error {
	cp := util.NewRuntimeCheckpoint("ProxyGetRouteHandler")
	cc := c.(*echoUtil.CustomContext) //nolint:errcheck
	defer cc.GetMetrics().AddCheckpoint(cp)

	adapter, ok := p.adapters[c.Request().Host]
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, util.ErrChainNotSupported)
	}

	// common prepare
	transport.PrepareGetRequest(cc, adapter.GetName())
	if c.IsWebSocket() {
		metrics.IncWebsocketConnections(cc.GetChainName())
		return adapter.ProxyWSRequest(c)
	}
	return echo.NewHTTPError(http.StatusMethodNotAllowed)
}

func (p *proxy) ProxyPostRouteHandler(c echo.Context) error {
	cp := util.NewRuntimeCheckpoint("ProxyPostRouteHandler")
	cc := c.(*echoUtil.CustomContext) //nolint:errcheck
	defer cc.GetMetrics().AddCheckpoint(cp)

	adapter, ok := p.adapters[c.Request().Host]
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, util.ErrChainNotSupported)
	}

	resBody, resCode, err := adapter.ProxyPostRequest(cc)
	if err != nil {
		return transport.HandleError(err)
	}
	p.requestCounter.IncUserRequests(cc.GetUserInfo(), cc.GetCreditsUsed(), cc.GetChainName(), cc.GetAPIToken(), cc.GetRequestType().String(), p.isMainnet)

	setServiceHeaders(cc.Response().Header(), cc)

	return c.JSONBlob(resCode, resBody)
}

func (p *proxy) RequestPrepareMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cp := util.NewRuntimeCheckpoint("RequestPrepareMiddleware")
			cc := c.(*echoUtil.CustomContext) //nolint:errcheck
			defer cc.GetMetrics().AddCheckpoint(cp)

			cc.SetReqTime(time.Now().UTC().Unix())

			adapter, ok := p.adapters[c.Request().Host]
			if !ok {
				return echo.NewHTTPError(http.StatusBadRequest, util.ErrChainNotSupported)
			}
			if c.IsWebSocket() {
				cc.SetRequestType(types.Websocket)
				cc.SetChainName(adapter.GetName())
				return next(c)
			}

			// common prepare
			// also here we set chain name, taken from adapter, to custom context
			err := transport.PreparePostRequest(cc, adapter.GetName())
			if err != nil {
				if errors.Is(err, transport.ErrInvalidContentType) {
					return c.String(http.StatusUnsupportedMediaType, err.Error())
				}

				return err
			}

			// chain specific prepare
			rpcErrResponse := adapter.PreparePostReq(cc)
			if rpcErrResponse != nil {
				return echo.NewHTTPError(http.StatusOK, rpcErrResponse)
			}
			if len(cc.GetRPCRequestsParsed()) == 0 {
				return c.NoContent(http.StatusOK)
			}
			isGPARequest := slices.Contains(cc.GetReqMethods(), solana.GetProgramAccounts)
			if isGPARequest && cc.GetArrayRequested() {
				return echo.NewHTTPError(http.StatusBadRequest, util.ErrGPAArrayRequest)
			}
			cc.SetIsGPARequest(isGPARequest)
			var isDasRequest bool
			for _, method := range cc.GetReqMethods() {
				if _, ok := solana.CNFTMethodList[method]; ok {
					isDasRequest = true
					break
				}
			}
			cc.SetIsDASRequest(isDasRequest)

			if isGPARequest {
				cc.SetRequestType(types.GPA)
			} else if isDasRequest {
				cc.SetRequestType(types.DAS)
			} else {
				cc.SetRequestType(types.RPC)
			}
			// TODO: in future here will be added SWQOS

			return next(c)
		}
	}
}
