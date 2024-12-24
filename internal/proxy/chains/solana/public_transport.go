package solana

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/metrics"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	targetJailTime              = time.Second
	consecutiveSuccessResponses = 10
	limitWindowSeconds          = 10
	secondsInHour               = 3600

	slotsPerSec                    = 2.5
	mainnetPreSetUpSlot            = 245091957
	mainnetPreSetUpGetSlotTimeUnix = 1706631908
	devnetPreSetUpSlot             = 276140561
	devnetPreSetUpGetSlotTimeUnix  = 1706592028
	publicRPCTargetType            = "public_rpc"
)

type (
	publicTransport struct {
		getSlotTime                time.Time
		httpClient                 *http.Client
		rpcClient                  *rpc.Client
		recentlyUsedEndpointTarget *proxyTarget
		predefinedTransport        predefinedTransport
		targets                    []*proxyTarget

		maxAttempts    int
		targetsCounter int

		currentSlot int64

		targetsMx sync.Mutex
	}

	publicTransportWithContext struct {
		transport *publicTransport
		c         *echoUtil.CustomContext
	}
)

func NewPublicTransport(defaultSolanaURL []configtypes.SolanaNode, wsTargets []configtypes.WrappedURL, isMainnet bool) (*publicTransport, error) {
	pt := &publicTransport{
		httpClient:  &http.Client{Timeout: echoUtil.APIWriteTimeout - time.Second},
		maxAttempts: 10,
		currentSlot: mainnetPreSetUpSlot,
		getSlotTime: time.Unix(mainnetPreSetUpGetSlotTimeUnix, 0),
		rpcClient:   rpc.New(rpc.MainNetBeta_RPC),
	}
	if !isMainnet {
		pt.currentSlot = devnetPreSetUpSlot
		pt.getSlotTime = time.Unix(devnetPreSetUpGetSlotTimeUnix, 0)
		pt.rpcClient = rpc.New(rpc.DevNet_RPC)
	}

	predefinedTransportTargets := make([]*proxyTarget, 0, len(defaultSolanaURL))
	for i := range defaultSolanaURL {
		predefinedTransportTargets = append(predefinedTransportTargets, newProxyTarget(models.URLWithMethods{URL: defaultSolanaURL[i].URL.String()}, 0, defaultSolanaURL[i].Provider, defaultSolanaURL[i].NodeType))
	}
	pt.targets = predefinedTransportTargets
	pt.predefinedTransport = predefinedTransport{
		t: transport.NewDefaultProxyTransport(configtypes.Chain{WSHosts: wsTargets}),
	}

	return pt, nil
}

func (pt *publicTransport) withContext(c *echoUtil.CustomContext) *publicTransportWithContext {
	return &publicTransportWithContext{
		transport: pt,
		c:         c,
	}
}

func (*publicTransportWithContext) isMutedErr(err, contextErr error) (mute, isAvailable bool) {
	if errors.Is(err, util.ErrBadStatusCode) {
		return true, false
	}
	errS := err.Error()
	if errS == "EOF" || strings.Contains(errS, "i/o timeout") || strings.Contains(errS, "reset by peer") ||
		strings.Contains(errS, "connection refused") || strings.Contains(errS, "no route to host") {
		return true, false
	}

	// possible cases when the node is not guilty:
	// - context.DeadlineExceeded - node response timeout. Slow node or multiple attempts are passed
	// - context.Canceled - user cancelled request
	if err == context.DeadlineExceeded || err == context.Canceled || contextErr == context.DeadlineExceeded || contextErr == context.Canceled || //nolint:errorlint
		strings.Contains(errS, "deadline exceeded") || strings.Contains(errS, "canceled") {
		return true, true
	}

	return false, false
}

func (ptc *publicTransportWithContext) sendHTTPReq() (respBody []byte, err error) {
	var (
		i               int
		target          *proxyTarget
		startTime       time.Time
		reqMethods      = ptc.c.GetReqMethods()
		reqType         = ptc.c.GetTokenType()
		responseTime    int64
		firstSlotOnNode int64
		analyzeErr      *AnalyzeError
		invalidReqErr   bool
		reqCtx          = ptc.c.Request().Context()
	)

outerLoop:
	for ; i < ptc.transport.maxAttempts; i++ {
		select {
		case <-reqCtx.Done():
			err = reqCtx.Err()
			break outerLoop
		default:
		}

		localTarget := ptc.transport.NextAvailableTarget(reqMethods, reqType, ptc.c)
		if localTarget == nil { // prevent empty target var for after loop logic
			break outerLoop
		}
		target = localTarget

		startTime = time.Now()
		mustContinue, isAvailable := func() (bool, bool) {
			var err error                                                                                                     // prevent set parent's err var
			respBody, _, err = transport.MakeHTTPRequest(ptc.c, ptc.transport.httpClient, http.MethodPost, target.url, false) //nolint:contextcheck
			responseTime = time.Since(startTime).Milliseconds()
			if err != nil {
				mute, isAvailable := ptc.isMutedErr(err, reqCtx.Err())
				if !mute {
					log.Logger.Proxy.Errorf("makeHTTPRequest (id %s): %s", ptc.c.GetReqID(), err)
				}

				return true, isAvailable
			}

			firstSlotOnNode, invalidReqErr, analyzeErr, err = rpcErrorAnalysis(decodeNodeResponse(ptc.c, respBody))
			if err != nil { // check for log
				log.Logger.Proxy.Errorf("responseError (id %s) (%s): %s", ptc.c.GetReqID(), target.url, err)
			}
			if invalidReqErr {
				ptc.c.SetProxyUserError(true)
				return false, true
			}
			if err != nil || analyzeErr != nil {
				return true, false
			}

			return false, true
		}()

		if target.provider != "" {
			metrics.IncPartnerNodeUsage(target.provider, !mustContinue)
			ptc.c.ReachPartnerNode()
			ptc.c.SetProvider(target.provider)
		}
		if firstSlotOnNode != 0 {
			// slotAmount calculation
			firstSlotOnNode = calculateSlot(ptc.transport.currentSlot, ptc.transport.getSlotTime, firstSlotOnNode)
		}
		target.UpdateStats(isAvailable, reqMethods, responseTime, firstSlotOnNode)

		if mustContinue {
			continue
		}

		i++ // increment for success case
		break
	}

	if len(respBody) == 0 && err == nil {
		switch {
		case reqCtx.Err() != nil:
			err = reqCtx.Err()
		case target == nil:
			ptc.c.SetRPCErrors([]int{util.ExtraNodeNoAvailableTargetsErrorResponse.Error.Code})
			err = echo.NewHTTPError(http.StatusServiceUnavailable, util.ExtraNodeNoAvailableTargetsErrorResponse)
		default:
			ptc.c.SetRPCErrors([]int{util.ExtraNodeAttemptsExceededErrorResponse.Error.Code})
			err = echo.NewHTTPError(http.StatusInternalServerError, util.ExtraNodeNoAvailableTargetsErrorResponse)
		}
	}
	log.Logger.Proxy.Debugf("Request sent to: %s", target.provider)

	targetType := publicRPCTargetType
	if target != nil {
		targetType = target.provider
	}

	transport.ResponsePostHandling(ptc.c, err, targetType, i, responseTime)

	return respBody, err
}

func (pt *publicTransport) getNextTarget(reqMethods []string, tokenType models.TokenType, c *echoUtil.CustomContext) *proxyTarget {
	var (
		candidate               *proxyTarget
		candidateFailedReqs     uint64
		candidateLastRespTime   int64
		avaliableRecentEndpoint bool
	)

	pt.targetsMx.Lock()
	defer pt.targetsMx.Unlock()

	for i := 0; i < len(pt.targets); i++ {
		pt.targetsCounter %= len(pt.targets)
		t := pt.targets[pt.targetsCounter]
		pt.targetsCounter++

		isAvailable, failedReqs, lastRespTime := t.isAvailable(reqMethods, tokenType, pt.currentSlot, pt.getSlotTime, c)
		if !isAvailable {
			continue
		}
		if t == pt.recentlyUsedEndpointTarget {
			avaliableRecentEndpoint = true
			continue
		}

		if tokenType.UseFirstEndpoint() {
			pt.recentlyUsedEndpointTarget = t
			return t
		}
		// return candidate with lower speed and error rate
		if candidate == nil || (tokenType == models.ReliableTokenType && failedReqs < candidateFailedReqs) || (tokenType == models.SpeedTokenType && lastRespTime < candidateLastRespTime) {
			candidate = t
			candidateFailedReqs = failedReqs
			candidateLastRespTime = lastRespTime
		}
	}
	// we do not found any available target besides recentEndpoint, so route request to that target
	if candidate == nil && avaliableRecentEndpoint {
		return pt.recentlyUsedEndpointTarget
	}
	if candidate != nil {
		pt.recentlyUsedEndpointTarget = candidate
	}

	return candidate
}

func (pt *publicTransport) NextAvailableTarget(reqMethods []string, reqType models.TokenType, c *echoUtil.CustomContext) *proxyTarget {
	target := pt.getNextTarget(reqMethods, reqType, c)
	if target != nil {
		return target
	}

	return nil
}

func (pt *publicTransport) SyncSlotFromMainnet(ctx context.Context) error {
	slot, err := pt.rpcClient.GetSlot(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("GetSlot: %s", err)
	}

	pt.currentSlot, pt.getSlotTime = int64(slot), time.Now()

	return nil
}

func calculateSlot(mainnetSlot int64, getSlotTime time.Time, slot int64) int64 {
	return mainnetSlot + int64(time.Since(getSlotTime).Seconds()*slotsPerSec) - slot
}
