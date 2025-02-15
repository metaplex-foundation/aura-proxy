package echo

import (
	"bytes"
	"context"
	"io"
	"time"

	auraProto "github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/adm-metaex/aura-api/pkg/types"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util"
)

const (
	timeout                 = APIWriteTimeout - time.Second
	bodyLimit               = 1000
	MultipleValuesRequested = "multiple_values"
)

type CustomContext struct {
	chainName           string
	proxyEndpoint       string
	targetType          string
	reqID               string
	statsAdditionalData string
	apiToken            string
	provider            string
	echo.Context

	userInfo          *auraProto.UserWithTokens
	subscription      *auraProto.SubscriptionWithPricing
	reqBody           *bytes.Reader
	metrics           *util.RuntimeMetrics
	rpcRequestsParsed types.RPCRequests
	rpcErrors         []int
	reqDuration       time.Time
	reqMethods        []string

	proxyAttempts     int
	proxyResponseTime int64
	reqBlock          int64
	creditsUsed       int64

	proxyUserError bool
	proxyHasError  bool
	arrayRequested bool
	isPartnerNode  bool

	requestType  types.RequestType
	isDASRequest bool
	isGPARequest bool
}

func CustomContextMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		cc := &CustomContext{Context: c}
		cc.InitReqDuration()
		cc.InitMetrics()

		return next(cc)
	}
}

func RequestTimeoutMiddleware(skipper middleware.Skipper) echo.MiddlewareFunc {
	if skipper == nil {
		skipper = middleware.DefaultSkipper
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if skipper(c) {
				return next(c)
			}

			// hande timeout
			timeoutCtx, cancel := context.WithTimeout(c.Request().Context(), timeout)
			defer cancel()
			c.SetRequest(c.Request().WithContext(timeoutCtx))

			return next(c)
		}
	}
}

func (c *CustomContext) SetReqMethods(reqMethods []string) {
	c.reqMethods = reqMethods
}
func (c *CustomContext) GetReqMethods() []string {
	return c.reqMethods
}
func (c *CustomContext) GetReqMethod() string {
	if len(c.reqMethods) == 1 {
		return c.reqMethods[0]
	}
	if len(c.reqMethods) > 1 {
		return MultipleValuesRequested
	}

	return ""
}

func (c *CustomContext) SetReqBody(reqBody []byte) {
	c.reqBody = bytes.NewReader(reqBody)
}
func (c *CustomContext) GetReqBody() *bytes.Reader {
	if c.reqBody == nil {
		return nil
	}

	_, err := c.reqBody.Seek(0, io.SeekStart)
	if err != nil {
		log.Logger.Proxy.Errorf("CustomContext.GetReqBody: Seek: %s", err)
	}

	return c.reqBody
}
func (c *CustomContext) GetTruncatedReqBody() []byte {
	reqBodyRaw := c.GetReqBody()
	if reqBodyRaw == nil {
		return []byte{}
	}
	reqBody, err := io.ReadAll(io.LimitReader(reqBodyRaw, bodyLimit)) // already truncated
	if err != nil {
		log.Logger.Proxy.Errorf("CustomContext.GetTruncatedReqBody: ReadAll: %s", err)
	}

	return reqBody
}
func (c *CustomContext) GetReqBodyString() string {
	reqBodyRaw := c.GetReqBody()
	if reqBodyRaw == nil {
		return ""
	}
	reqBody, err := io.ReadAll(reqBodyRaw)
	if err != nil {
		log.Logger.Proxy.Errorf("CustomContext.GetReqBodyString: ReadAll: %s", err)
	}

	return string(reqBody)
}

func (c *CustomContext) SetRPCErrors(rpcErrors []int) {
	c.rpcErrors = rpcErrors
}
func (c *CustomContext) GetRPCErrors() []int {
	return c.rpcErrors
}
func (c *CustomContext) GetRPCError() int {
	if len(c.rpcErrors) == 1 {
		return c.rpcErrors[0]
	}
	if len(c.rpcErrors) > 1 {
		return -1
	}

	return 0
}

func (c *CustomContext) SetChainName(n string) {
	c.chainName = n
}
func (c *CustomContext) GetChainName() string {
	return c.chainName
}

func (c *CustomContext) SetProxyEndpoint(proxyEndpoint string) {
	c.proxyEndpoint = proxyEndpoint
}
func (c *CustomContext) GetProxyEndpoint() string {
	return c.proxyEndpoint
}

func (c *CustomContext) SetTargetType(v string) {
	c.targetType = v
}
func (c *CustomContext) GetTargetType() string {
	return c.targetType
}

func (c *CustomContext) SetProxyAttempts(proxyAttempts int) {
	c.proxyAttempts = proxyAttempts
}
func (c *CustomContext) GetProxyAttempts() int {
	return c.proxyAttempts
}

func (c *CustomContext) SetProxyResponseTime(proxyResponseTime int64) {
	c.proxyResponseTime = proxyResponseTime
}
func (c *CustomContext) GetProxyResponseTime() int64 {
	return c.proxyResponseTime
}

func (c *CustomContext) SetProxyUserError(proxyUserError bool) {
	c.proxyUserError = proxyUserError
}
func (c *CustomContext) GetProxyUserError() bool {
	return c.proxyUserError
}

func (c *CustomContext) SetProxyHasError(proxyHasError bool) {
	c.proxyHasError = proxyHasError
}
func (c *CustomContext) GetProxyHasError() bool {
	return c.proxyHasError
}

func (c *CustomContext) InitReqDuration() {
	c.reqDuration = time.Now()
}
func (c *CustomContext) GetReqDuration() time.Time {
	return c.reqDuration
}

func (c *CustomContext) SetUserInfo(u *auraProto.UserWithTokens) {
	if u == nil {
		return
	}

	c.userInfo = u
}

func (c *CustomContext) GetUserInfo() *auraProto.UserWithTokens {
	return c.userInfo
}
func (c *CustomContext) GetAPIToken() string {
	return c.apiToken
}
func (c *CustomContext) SetAPIToken(apiToken string) {
	c.apiToken = apiToken
}

func (c *CustomContext) GetTokenType() models.TokenType {
	return models.DefaultTokenType
}

func (c *CustomContext) SetRPCRequestsParsed(u types.RPCRequests) {
	c.rpcRequestsParsed = u
}
func (c *CustomContext) GetRPCRequestsParsed() types.RPCRequests {
	return c.rpcRequestsParsed
}

func (c *CustomContext) SetArrayRequested(p bool) {
	c.arrayRequested = p
}
func (c *CustomContext) GetArrayRequested() bool {
	return c.arrayRequested
}

func (c *CustomContext) SetReqID(reqID string) {
	c.reqID = reqID
}
func (c *CustomContext) GetReqID() string {
	return c.reqID
}

func (c *CustomContext) SetStatsAdditionalData(d string) {
	c.statsAdditionalData = d
}
func (c *CustomContext) GetStatsAdditionalData() string {
	return c.statsAdditionalData
}

func (c *CustomContext) InitMetrics() {
	c.metrics = util.NewRuntimeMetrics()
}
func (c *CustomContext) GetMetrics() *util.RuntimeMetrics {
	return c.metrics
}

func (c *CustomContext) SetReqBlock(reqBlock int64) {
	c.reqBlock = reqBlock
}
func (c *CustomContext) GetReqBlock() int64 {
	return c.reqBlock
}
func (c *CustomContext) ReachPartnerNode() {
	c.isPartnerNode = true
}
func (c *CustomContext) GetIsPartnerNode() bool {
	return c.isPartnerNode
}

func (c *CustomContext) GetReqPerSecond() int32 {
	switch {
	case c.chainName == solana.DasChainName:
		return c.subscription.GetPricing().GetAuraDas().GetRequestsPerSecond()
	case c.chainName == solana.EclipseDasChainName:
		return c.subscription.GetPricing().GetEclipseDas().GetRequestsPerSecond()
	case !c.isDASRequest && !c.isGPARequest && c.chainName == solana.ChainName:
		return c.subscription.GetPricing().GetSolanaRpc().GetRequestsPerSecond()
	case !c.isDASRequest && !c.isGPARequest && c.chainName == solana.EclipseChainName:
		return c.subscription.GetPricing().GetEclipseRpc().GetRequestsPerSecond()
	case c.isGPARequest:
		return c.subscription.GetPricing().GetGetProgramAccounts().GetRequestsPerSecond()
	default:
		return 10
	}
}

func (c *CustomContext) GetReqCost() int64 {
	switch {
	case c.chainName == solana.DasChainName:
		return c.subscription.GetPricing().GetAuraDas().GetPriceMplx()
	case c.chainName == solana.EclipseDasChainName:
		return c.subscription.GetPricing().GetEclipseDas().GetPriceMplx()
	case !c.isDASRequest && !c.isGPARequest && c.chainName == solana.ChainName:
		return c.subscription.GetPricing().GetSolanaRpc().GetPriceMplx()
	case !c.isDASRequest && !c.isGPARequest && c.chainName == solana.EclipseChainName:
		return c.subscription.GetPricing().GetEclipseRpc().GetPriceMplx()
	case c.isGPARequest:
		return c.subscription.GetPricing().GetGetProgramAccounts().GetPriceMplx()
	default:
		return 10
	}
}

func (c *CustomContext) SetIsDASRequest(isDASRequest bool) {
	c.isDASRequest = isDASRequest
}
func (c *CustomContext) GetIsDASRequest() bool {
	return c.isDASRequest
}
func (c *CustomContext) SetIsGPARequest(isGPARequest bool) {
	c.isGPARequest = isGPARequest
}
func (c *CustomContext) GetIsGPARequest() bool {
	return c.isGPARequest
}

func (c *CustomContext) SetSubscription(subscription *auraProto.SubscriptionWithPricing) {
	c.subscription = subscription
}
func (c *CustomContext) GetSubscription() *auraProto.SubscriptionWithPricing {
	return c.subscription
}

func (c *CustomContext) SetCreditsUsed(creditsUsed int64) {
	c.creditsUsed = creditsUsed
}
func (c *CustomContext) GetCreditsUsed() int64 {
	return c.creditsUsed
}

func (c *CustomContext) SetProvider(provider string) {
	c.provider = provider
}
func (c *CustomContext) GetProvider() string {
	return c.provider
}

func (c *CustomContext) SetRequestType(requestType types.RequestType) {
	c.requestType = requestType
}
func (c *CustomContext) GetRequestType() types.RequestType {
	return c.requestType
}
