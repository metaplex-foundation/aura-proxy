package solana

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// MethodTransport is a transport that uses method-based routing
type MethodTransport struct {
	httpRequester HTTPRequester
	methodRouter  MethodRouter
	targetType    string
	maxAttempts   int
}

// Make this a variable so it can be replaced in tests
var NewMethodTransport = func(
	targetType string,
	methodRouter MethodRouter,
	requester HTTPRequester,
	maxAttempts int,
) *MethodTransport {
	if maxAttempts <= 0 {
		maxAttempts = 3 // Default to 3 attempts
	}
	
	return &MethodTransport{
		httpRequester: requester,
		methodRouter:  methodRouter,
		targetType:    targetType,
		maxAttempts:   maxAttempts,
	}
}

// isAvailable checks if the transport has any available targets
func (t *MethodTransport) isAvailable() bool {
	return t.methodRouter.IsAvailable()
}

// canHandle checks if this transport can handle the given methods
func (t *MethodTransport) canHandle(methods []string) bool {
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

// SendRequest sends a request using method-based routing
func (t *MethodTransport) SendRequest(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	startTime := time.Now()

	respBody, statusCode, err = t.sendRequestWithRetries(c)
	
	responseTime := time.Since(startTime).Milliseconds()
	transport.ResponsePostHandling(c, err, t.targetType, 1, responseTime)

	return respBody, statusCode, err
}

// sendRequestWithRetries sends a request with retries using balancer exclude functionality
func (t *MethodTransport) sendRequestWithRetries(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	methods := c.GetReqMethods()
	reqCtx := c.Request().Context()
	
	// If no methods specified, can't route
	if len(methods) == 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("no methods specified in request")
	}
	
	// Get primary method (first in the list)
	primaryMethod := methods[0]
	
	// Get balancer for this method
	balancer, found := t.methodRouter.GetBalancerForMethod(primaryMethod)
	if !found || balancer == nil {
		return nil, http.StatusServiceUnavailable, fmt.Errorf("no balancer available for method %s", primaryMethod)
	}
	
	// Keep track of excluded targets
	exclude := make([]int, 0)
	
	// Try multiple targets with exclusion if needed
	for i := 0; i < t.maxAttempts; i++ {
		// Check if request context has been canceled
		select {
		case <-reqCtx.Done():
			return nil, http.StatusRequestTimeout, reqCtx.Err()
		default:
		}
		
		// Get next target excluding previously failed ones
		target, targetIndex, err := balancer.GetNext(exclude)
		if err != nil {
			return nil, http.StatusServiceUnavailable, fmt.Errorf("no available targets for method %s: %w", primaryMethod, err)
		}
		
		// Set provider in context for metrics and logging
		c.SetProvider(target.provider)
		
		// Send request to target
		respStartTime := time.Now()
		respBody, statusCode, err := t.httpRequester.DoRequest(c, target.url)
		responseTime := time.Since(respStartTime).Milliseconds()
		
		if err == nil {
			// Success - update stats and return
			t.methodRouter.UpdateTargetStats(target, true, methods, responseTime, 0)
			return respBody, statusCode, nil
		}
		
		// Error - update stats and add this target to exclusion list
		t.methodRouter.UpdateTargetStats(target, false, methods, responseTime, 0)
		exclude = append(exclude, targetIndex)
	}
	
	// If we've exhausted all attempts, return an error
	return nil, http.StatusServiceUnavailable, echo.NewHTTPError(
		http.StatusServiceUnavailable, 
		util.ExtraNodeNoAvailableTargetsErrorResponse,
	)
}
