package echo

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/adm-metaex/aura-api/pkg/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

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
	apiToken            struct {
		token     string
		tokenType models.TokenType
	}

	echo.Context

	//userInfo          *proto.GetUserInfoResp
	reqBody           *bytes.Reader
	metrics           *util.RuntimeMetrics
	rpcRequestsParsed types.RPCRequests
	rpcErrors         []int
	reqDuration       time.Time
	reqMethods        []string
	projectUUID       uuid.UUID

	proxyAttempts     int
	proxyResponseTime int64
	reqBlock          int64

	proxyUserError   bool
	proxyHasError    bool
	arrayRequested   bool
	isPartnerNode    bool
	isTrackedRequest bool
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

//	func (c *CustomContext) SetUserInfo(u *proto.GetUserInfoResp) {
//		if u == nil {
//			return
//		}
//
//		c.userInfo = u
//		c.projectUUID = util.ParseUUIDOrDefault(u.GetProjectUuid())
//	}
//
//	func (c *CustomContext) GetUserInfo() *proto.GetUserInfoResp {
//		return c.userInfo
//	}
func (c *CustomContext) GetProjectUUID() uuid.UUID {
	return c.projectUUID
}

func (c *CustomContext) SetUserUID(u string, tokenType models.TokenType) {
	c.apiToken.token = u
	c.apiToken.tokenType = tokenType
}
func (c *CustomContext) GetUserUID() string {
	return c.apiToken.token
}
func (c *CustomContext) GetTokenType() models.TokenType {
	if c.apiToken.tokenType == "" {
		return models.DefaultTokenType
	}

	return c.apiToken.tokenType
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

func (c *CustomContext) GetReqPerSecond() int64 {
	//if c.userInfo == nil || c.userInfo.GetRequestPerSecond() == 0 {
	return c.GetTokenType().GetReqPerSecond()
	//}

	//return c.userInfo.GetRequestPerSecond()
}

func (c *CustomContext) SetIsTrackedRequest(v bool) {
	c.isTrackedRequest = v
}
func (c *CustomContext) IsTrackedRequest() bool {
	return c.isTrackedRequest
}
