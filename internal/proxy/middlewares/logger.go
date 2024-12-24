package middlewares

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"google.golang.org/protobuf/types/known/timestamppb"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	durationThreshold = 31 * time.Second
)

func NewLoggerMiddleware(saveLog func(s *proto.Stat)) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:       true,
		LogMethod:       true,
		LogLatency:      true,
		LogError:        true,
		LogUserAgent:    true,
		LogURI:          true,
		LogResponseSize: true,
		LogHost:         true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			cp := util.NewRuntimeCheckpoint("LoggerMiddleware")
			cc := c.(*echoUtil.CustomContext) //nolint:errcheck
			endpoint := cc.GetProxyEndpoint()
			if errors.Is(v.Error, context.DeadlineExceeded) {
				v.Status = http.StatusRequestTimeout // because this err code assigned after NewLoggerMiddlewares
			}

			if !cc.IsWebSocket() {
				saveLog(buildStatStruct(cc.GetReqID(), v.Status, v.Latency.Milliseconds(), endpoint,
					cc.GetProxyAttempts(), cc.GetProxyResponseTime(), cc.GetReqMethod(), cc.GetRPCError(), v.UserAgent,
					cc.GetStatsAdditionalData(), cc.GetUserInfo().GetUser(), cc.GetChainName(), cc.GetAPIToken(), cc.GetProvider(), v.ResponseSize, cc.GetCreditsUsed(), cc.GetTargetType()))
			}

			m := cc.GetMetrics()
			m.AddCheckpoint(cp)
			metricsLog := m.String()
			if (v.Error != nil || len(cc.GetRPCErrors()) != 0) && !cc.GetProxyUserError() || v.Status >= http.StatusBadRequest {
				log.Logger.Proxy.Errorf("%d %s, id: %s, latency: %d, endpoint: %s, rpc_method: %v, chain: %s, attempts: %d, node_response_time: %dms, "+
					"rpc_error_code: %v, error: %s, user_err: %t, request_body: %s,  user_agent: %s, path: %s, host: %s, metrics: %s, isWs: %t",
					v.Status, v.Method, cc.GetReqID(), v.Latency.Milliseconds(), endpoint, cc.GetReqMethods(), cc.GetChainName(), cc.GetProxyAttempts(), cc.GetProxyResponseTime(),
					cc.GetRPCErrors(), util.ErrMsg(v.Error), cc.GetProxyUserError(), cc.GetTruncatedReqBody(), v.UserAgent, v.URI, v.Host, metricsLog, cc.IsWebSocket())
			} else if cc.GetIsPartnerNode() || v.Latency > durationThreshold {
				log.Logger.Proxy.Debugf("%d %s, id: %s, latency: %d, endpoint: %s, rpc_method: %v, chain: %s, attempts: %d, node_response_time: %dms, "+
					"request_body: %s,  user_agent: %s, path: %s, host: %s, metrics: %s, isWs: %t",
					v.Status, v.Method, cc.GetReqID(), v.Latency.Milliseconds(), endpoint, cc.GetReqMethods(), cc.GetChainName(), cc.GetProxyAttempts(), cc.GetProxyResponseTime(),
					cc.GetTruncatedReqBody(), v.UserAgent, v.URI, v.Host, metricsLog, cc.IsWebSocket())
			}

			return nil
		},
	})
}

func buildStatStruct(requestUUID string, statusCode int, latency int64, endpoint string, attempts int, responseTime int64,
	rpcMethod string, rpcErrorCode int, userAgent, statsAdditionalData, userUID, chainName, token, provider string, responseSizeBytes, methodCost int64, targetType string) *proto.Stat {
	return &proto.Stat{
		UserUid:           userUID,
		TokenUuid:         token,
		RequestUuid:       requestUUID,
		Status:            uint32(statusCode),
		ExecutionTimeMs:   latency,
		Endpoint:          endpoint,
		Attempts:          uint32(attempts),
		ResponseTimeMs:    responseTime,
		RpcErrorCode:      strconv.FormatInt(int64(rpcErrorCode), 10),
		UserAgent:         userAgent,
		RpcMethod:         rpcMethod,
		RpcRequestData:    statsAdditionalData,
		Timestamp:         timestamppb.New(time.Now().UTC()),
		Chain:             chainName,
		ResponseSizeBytes: responseSizeBytes,
		TargetType:        targetType,
		Provider:          provider,
		MethodCost:        methodCost,
	}
}
