package proxy

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/log"
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

func (p *proxy) initProxyHandlers(tokenChecker *middlewares.TokenChecker) {
	apiTokenCheckerMiddleware := middlewares.APITokenCheckerMiddleware(tokenChecker)
	rateLimiterMiddleware := echoUtil.NewRateLimiter(func(c echo.Context) bool {
		// CustomContext must be inited before
		cc := c.(*echoUtil.CustomContext) //nolint:errcheck
		return !cc.GetTokenType().IsTokenRateLimited()
	})

	proxyMiddlewares := []echo.MiddlewareFunc{
		apiTokenCheckerMiddleware,
		rateLimiterMiddleware,
		echoUtil.RequestTimeoutMiddleware(func(c echo.Context) bool { return c.IsWebSocket() }),
		middlewares.StreamRateLimitMiddleware(func(c echo.Context) bool { return !c.IsWebSocket() }), // WS rate limiter
		middlewares.RequestIDMiddleware(),
		middlewares.NewLoggerMiddleware(p.statsCollector.Add, p.detailedRequestsCollector.Add),
		middlewares.NewMetricsMiddleware(),
	}
	p.router.POST("/", p.ProxyPostRouteHandler, proxyMiddlewares...)
	p.router.POST("/:token", p.ProxyPostRouteHandler, proxyMiddlewares...)
	p.router.GET("/service-status", p.serviceStatusHandler)
}

func (p *proxy) ProxyPostRouteHandler(c echo.Context) error {
	cp := util.NewRuntimeCheckpoint("ProxyPostRouteHandler")
	cc := c.(*echoUtil.CustomContext) //nolint:errcheck
	defer cc.GetMetrics().AddCheckpoint(cp)

	adapter, ok := p.adapters[c.Request().Host]
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, util.ErrChainNotSupported)
	}

	// common prepare
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

	resBody, resCode, err := adapter.ProxyPostRequest(cc)
	if err != nil {
		return transport.HandleError(err)
	}

	_, err = calculateCreditsCost(cc.GetReqMethods(), adapter.GetAvailableMethods())
	if err != nil {
		log.Logger.Proxy.Warnf("ProxyPostRouteHandler.calculateCreditsCost: %s", err)
		// no need to break exec flow
	}
	//p.requestCounter.IncUserRequests(cc.GetUserInfo(), int64(credits))

	setServiceHeaders(cc.Response().Header(), cc)

	return c.JSONBlob(resCode, resBody)
}
