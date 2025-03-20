package solana

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/metrics"
	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	// Transport type identifiers
	UnifiedTransportType = "unified_transport"

	// Default values
	DefaultMaxAttempts = 10

	// Constants copied from publicTransport for slot calculations
	slotsPerSec                    = 2.5
	mainnetPreSetUpSlot            = 245091957
	mainnetPreSetUpGetSlotTimeUnix = 1706631908
	devnetPreSetUpSlot             = 276140561
	devnetPreSetUpGetSlotTimeUnix  = 1706592028
)

type UnifiedTransport struct {
	// HTTP requester to use for making requests
	httpRequester HTTPRequester

	// Method router for finding the right balancer
	methodRouter MethodRouter

	// Identifier for this transport
	transportType string

	// Maximum number of retry attempts
	maxAttempts int

	// Current slot information (for legacy compatibility)
	currentSlot int64
	getSlotTime time.Time
	isMainnet   bool
}

func NewUnifiedTransport(transportType string, methodRouter MethodRouter, httpRequester HTTPRequester, maxAttempts int, isMainnet bool) *UnifiedTransport {
	return &UnifiedTransport{
		transportType: transportType,
		methodRouter:  methodRouter,
		httpRequester: httpRequester,
		maxAttempts:   maxAttempts,
		isMainnet:     isMainnet,
	}
}

func (t *UnifiedTransport) isAvailable() bool {
	return t.methodRouter.IsAvailable()
}

func (t *UnifiedTransport) canHandle(methods []string) bool {
	if len(methods) == 0 {
		return false
	}
	for _, method := range methods {
		if !t.methodRouter.IsMethodSupported(method) {
			return false
		}
	}
	return true
}

func (t *UnifiedTransport) SendRequest(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	startTime := time.Now()

	respBody, statusCode, attempts, err := t.sendRequestWithRetries(c)
	transport.ResponsePostHandling(c, err, t.transportType, attempts, time.Since(startTime).Milliseconds())

	return respBody, statusCode, err
}

// sendRequestWithRetries sends a request with retries using the same robust logic as publicTransport
func (t *UnifiedTransport) sendRequestWithRetries(c *echoUtil.CustomContext) (respBody []byte, statusCode int, attempts int, err error) {
	methods := c.GetReqMethods()
	if len(methods) == 0 {
		return nil, http.StatusBadRequest, 0, fmt.Errorf("no methods specified in request")
	}

	// Get primary method (first in the list)
	primaryMethod := methods[0]

	// Get balancer for this method
	targetSelector, found := t.methodRouter.GetBalancerForMethod(primaryMethod)
	if !found || !targetSelector.IsAvailable() {
		return nil, http.StatusServiceUnavailable, 0, fmt.Errorf("no balancer available for method %s", primaryMethod)
	}

	var (
		target          *ProxyTarget
		targetIndex     int
		startTime       time.Time
		responseTime    int64
		firstSlotOnNode int64
		analyzeErr      *AnalyzeError
		invalidReqErr   bool
		reqCtx          = c.Request().Context()
	)

	exclude := make([]int, 0)
	attempts = 0

outerLoop:
	for ; attempts < t.maxAttempts; attempts++ {
		select {
		case <-reqCtx.Done():
			err = reqCtx.Err()
			break outerLoop
		default:
		}

		// Get next target excluding previously failed ones
		target, targetIndex, err = targetSelector.GetNext(exclude)
		if err != nil {
			// No more available targets
			break outerLoop
		}

		// Set provider in context for metrics
		c.SetProvider(target.provider)

		startTime = time.Now()

		// Handle request and error analysis in a self-contained function to simplify control flow
		mustContinue, isAvailable := func() (bool, bool) {
			// Make the request
			respBody, statusCode, err = t.httpRequester.DoRequest(c, target.url)
			responseTime = time.Since(startTime).Milliseconds()

			// Check for errors
			if err != nil {
				mute, isAvailable := isMutedErr(err, reqCtx.Err())
				if !mute {
					log.Logger.Proxy.Errorf("makeHTTPRequest (id %s): %s", c.GetReqID(), err)
				}
				return true, isAvailable
			}

			// Analyze response for RPC errors
			firstSlotOnNode, invalidReqErr, analyzeErr, err = rpcErrorAnalysis(decodeNodeResponse(c, respBody))
			if err != nil { // check for log
				log.Logger.Proxy.Errorf("responseError (id %s) (%s): %s", c.GetReqID(), target.url, err)
			}
			if invalidReqErr {
				c.SetProxyUserError(true)
				return false, true
			}
			if err != nil || analyzeErr != nil {
				return true, false
			}

			return false, true
		}()

		// Update metrics for partner node
		if target.provider != "" {
			metrics.IncPartnerNodeUsage(target.provider, !mustContinue)
			c.ReachPartnerNode()
		}

		// Update target stats
		if firstSlotOnNode != 0 {
			// slotAmount calculation if available
			firstSlotOnNode = calculateSlot(t.currentSlot, t.getSlotTime, firstSlotOnNode)
		}
		t.methodRouter.UpdateTargetStats(target, isAvailable, methods, responseTime, firstSlotOnNode)

		// If no need to continue, break the loop
		if !mustContinue {
			attempts++ // increment for success case
			break
		}

		// Add failed target to exclusion list
		exclude = append(exclude, targetIndex)
	}

	// Handle errors and edge cases
	if len(respBody) == 0 && err == nil {
		switch {
		case reqCtx.Err() != nil:
			err = reqCtx.Err()
		case target == nil:
			c.SetRPCErrors([]int{util.ExtraNodeNoAvailableTargetsErrorResponse.Error.Code})
			err = echo.NewHTTPError(http.StatusServiceUnavailable, util.ExtraNodeNoAvailableTargetsErrorResponse)
		default:
			c.SetRPCErrors([]int{util.ExtraNodeAttemptsExceededErrorResponse.Error.Code})
			err = echo.NewHTTPError(http.StatusInternalServerError, util.ExtraNodeNoAvailableTargetsErrorResponse)
		}
	}

	return respBody, statusCode, attempts, err
}

// calculateSlot calculates the slot amount based on current mainnet slot and timing
// Copied from publicTransport for consistency
func calculateSlot(mainnetSlot int64, getSlotTime time.Time, slot int64) int64 {
	return mainnetSlot + int64(time.Since(getSlotTime).Seconds()*slotsPerSec) - slot
}

func isMutedErr(err, contextErr error) (mute, isAvailable bool) {
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
