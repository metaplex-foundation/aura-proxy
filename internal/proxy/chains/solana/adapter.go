package solana

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

var (
	solanaChainHosts = []string{
		"aura-mainnet.metaplex.com",
		"aura-devnet.metaplex.com",
		"mainnet-aura.metaplex.com",
		"devnet-aura.metaplex.com",
		"aura-dev.metaplex.com", // dev environment
		"localhost:2011",        // for local tests
		"127.0.0.1:2011",        // for local tests
	}
	eclipseChainHosts = []string{
		"aura-eclipse-mainnet.metaplex.com",
		"eclipse-mainnet-aura.metaplex.com",
		"aura-ecl-dev.metaplex.com", // dev environment
		//"localhost:2011", // for local tests
		//"127.0.0.1:2011", // for local tests
	}
)

// --- Interfaces for Transports ---

type rpcTransport interface {
	SendRequest(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error)
	canHandle(methods []string) bool
	isAvailable() bool
}

// Renamed to avoid collision with existing type
type wsProxyTransport interface {
	DefaultProxyWS(c echo.Context) error
}

// --- Adapter Struct ---

type Adapter struct {
	rpcTransport rpcTransport     // Use interface type
	wsTransport  wsProxyTransport // Use interface type (renamed)

	chainName        string
	availableMethods map[string]uint
	hostNames        []string
	isMainnet        bool
}

func NewSolanaAdapter(router *MethodBasedRouter, isMainnet bool) (*Adapter, error) { //nolint:gocritic
	return newAdapter(router, isMainnet, solana.ChainName, solana.MethodList, solanaChainHosts)
}

func NewEclipseAdapter(router *MethodBasedRouter, isMainnet bool) (*Adapter, error) { //nolint:gocritic
	return newAdapter(router, isMainnet, solana.EclipseChainName, solana.MethodList, eclipseChainHosts)
}

func newAdapter(router *MethodBasedRouter, isMainnet bool, chainName string, availableMethods map[string]uint, hostNames []string) (*Adapter, error) {
	a := &Adapter{
		chainName:        chainName,
		availableMethods: availableMethods,
		hostNames:        hostNames,
		isMainnet:        isMainnet, // Store isMainnet
	}

	// Create unified transport with the method router
	a.rpcTransport = NewUnifiedTransport(
		UnifiedTransportType,
		router,
		&RealHTTPRequester{},
		DefaultMaxAttempts,
		isMainnet,
	)
	if router.wsTargetInfo != nil && router.wsTargetInfo.balancer != nil {
		// Assign the concrete type *wsTransport, which implements wsProxyTransport
		a.wsTransport = &wsTransport{
			t: NewDefaultProxyTransport(router.wsTargetInfo.balancer),
		}
	}

	return a, nil
}

func (s *Adapter) GetName() string {
	return s.chainName
}
func (s *Adapter) GetAvailableMethods() map[string]uint {
	return s.availableMethods
}
func (s *Adapter) GetHostNames() []string {
	return s.hostNames
}

// ProxyWSRequest handles WebSocket proxy requests
func (s *Adapter) ProxyWSRequest(c echo.Context) error {
	// Ensure wsTransport is initialized
	if s.wsTransport == nil {
		log.Logger.Proxy.Error("WebSocket transport not initialized")
		return sanitizeError(errors.New("WebSocket transport not available")) // Sanitize potential nil pointer error
	}
	err := s.wsTransport.DefaultProxyWS(c)
	// Sanitize errors returned *before* or *after* the proxy attempt (e.g., target selection error).
	// Note: Errors during active proxying might be handled by ReverseProxy's default ErrorHandler,
	// which could still leak info. For full protection, ReverseProxy needs a custom ErrorHandler.
	return sanitizeError(err)
}

func (s *Adapter) ProxyPostRequest(c *echoUtil.CustomContext) (resBody []byte, resCode int, err error) {
	reqMethods := c.GetReqMethods()

	if s.rpcTransport == nil {
		log.Logger.Proxy.Error("RPC transport not initialized")
		// Directly return a generic error without calling sanitizeError
		err = echo.NewHTTPError(http.StatusInternalServerError, "RPC transport not available")
		resCode = http.StatusInternalServerError
		return nil, resCode, err
	}

	if !s.rpcTransport.canHandle(reqMethods) || !s.rpcTransport.isAvailable() {
		// This path already returns a generic, safe error response. No change needed.
		return nil, http.StatusServiceUnavailable, echo.NewHTTPError(http.StatusServiceUnavailable, util.ExtraNodeNoAvailableTargetsErrorResponse)
	}

	resBody, resCode, err = s.rpcTransport.SendRequest(c)
	// Sanitize the error before returning it to the framework/client
	return resBody, resCode, sanitizeError(err)
}

// sanitizeError inspects errors, logs sensitive details internally, and returns a generic error to the client.
func sanitizeError(originalErr error) error {
	if originalErr == nil {
		return nil
	}

	// Check if it's already a user-facing Echo error
	var httpErr *echo.HTTPError
	if errors.As(originalErr, &httpErr) {
		// Check if it's a client error (4xx) or server error (5xx)
		if httpErr.Code >= 400 && httpErr.Code < 500 {
			// Assume 4xx messages are safe for the client
			log.Logger.Proxy.Debugf("Returning existing client HTTPError (code %d): %v", httpErr.Code, httpErr.Message)
			return httpErr // Return as is
		}
		// For 5xx errors, log the original but return a generic message
		log.Logger.Proxy.Errorf("Internal server error occurred (sanitized 5xx HTTPError). Original code: %d, Original message: %v, Internal: %v", httpErr.Code, httpErr.Message, httpErr.Internal)
		// Return a new HTTPError with the same code but generic message
		return echo.NewHTTPError(httpErr.Code, "Internal server error")
	}

	// Handle context errors specifically
	if errors.Is(originalErr, context.Canceled) {
		// Log the original error for debugging context cancellation issues
		log.Logger.Proxy.Warnf("Request canceled by client or upstream context: %v", originalErr)
		// 499 Client Closed Request is a common non-standard code for this
		return echo.NewHTTPError(499, "Client closed request")
	}
	if errors.Is(originalErr, context.DeadlineExceeded) {
		// Log the original error for debugging timeout issues
		log.Logger.Proxy.Warnf("Request deadline exceeded: %v", originalErr)
		// 504 Gateway Timeout is appropriate
		return echo.NewHTTPError(http.StatusGatewayTimeout, "Gateway timeout")
	}

	// For any other error type, log it and return a generic error
	// This catches potential errors from MakeHTTPRequest containing URLs,
	// ReverseProxy errors, URL parsing errors, transport initialization errors etc.
	log.Logger.Proxy.Errorf("Internal server error occurred (sanitized): %v", originalErr) // Log the original error
	// Return a generic error to the client
	return echo.NewHTTPError(http.StatusInternalServerError, "Internal server error") // Generic message
}
