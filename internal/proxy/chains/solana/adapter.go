package solana

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util"
	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	cNFTTargetType      = "c_nft"
	methodTargetType    = "method_based"
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

type Adapter struct {
	// Legacy transports (for backward compatibility)
	publicTransport  *publicTransport
	cNFTTransport    *CNFTTransport
	wsTransport      *wsTransport
	
	// New method-based transport and router
	methodTransport  *MethodTransport
	methodRouter     MethodRouter
	
	chainName        string
	availableMethods map[string]uint
	hostNames        []string
	isMainnet        bool
}

func NewSolanaAdapter(cfg *configtypes.SolanaConfig, isMainnet bool) (*Adapter, error) { //nolint:gocritic
	return newAdapter(cfg, isMainnet, solana.ChainName, solana.MethodList, solanaChainHosts)
}

func NewEclipseAdapter(cfg *configtypes.SolanaConfig, isMainnet bool) (*Adapter, error) { //nolint:gocritic
	return newAdapter(cfg, isMainnet, solana.EclipseChainName, solana.MethodList, eclipseChainHosts)
}

func newAdapter(cfg *configtypes.SolanaConfig, isMainnet bool, chainName string, availableMethods map[string]uint, hostNames []string) (*Adapter, error) {
	a := &Adapter{
		chainName:        chainName,
		availableMethods: availableMethods,
		hostNames:        hostNames,
		isMainnet:        isMainnet, // Store isMainnet
	}
	
	// Initialize the method-based routing if it's available in config or legacy config
	if hasAnyRoutingConfig(cfg) {
		// Create a method router
		methodRouter, err := NewMethodBasedRouter(cfg)
		if err != nil {
			return nil, fmt.Errorf("creating method router: %w", err)
		}
		
		// Store the router for direct access to balancers
		a.methodRouter = methodRouter
		
		// Create a method transport
		a.methodTransport = NewMethodTransport(
			methodTargetType,
			methodRouter,
			&RealHTTPRequester{},
			10, // max attempts
		)
	}
	
	// Initialize legacy transports for backward compatibility
	err := a.initLegacyTransports(cfg)
	if err != nil {
		return nil, fmt.Errorf("initLegacyTransports: %w", err)
	}

	return a, nil
}

// hasAnyRoutingConfig checks if the configuration contains any routing configuration (method-based or legacy)
func hasAnyRoutingConfig(cfg *configtypes.SolanaConfig) bool {
	return len(cfg.Providers) > 0 || len(cfg.BasicRouteNodes) > 0 || len(cfg.DasAPINodes) > 0 || len(cfg.WSHostNodes) > 0
}

// hasMethodBasedConfig checks if the configuration contains method-based routing configuration
func hasMethodBasedConfig(cfg *configtypes.SolanaConfig) bool {
	return len(cfg.Providers) > 0
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
	// Try to use method-based routing for WebSockets first
	if s.methodRouter != nil {
		balancer, found := s.methodRouter.GetBalancerForMethod(WebSocketMethodName)
		if found && balancer != nil && balancer.IsAvailable() {
			return s.methodBasedProxyWS(c, balancer)
		}
	}
	
	// Fall back to legacy WebSocket transport
	if s.wsTransport != nil {
		return s.wsTransport.DefaultProxyWS(c)
	}
	
	return echo.NewHTTPError(http.StatusServiceUnavailable, "No WebSocket handlers configured")
}

// methodBasedProxyWS handles WebSocket proxy using method-based routing
func (s *Adapter) methodBasedProxyWS(c echo.Context, wsBalancer balancer.TargetSelector[*ProxyTarget]) error {
	target, _, err := wsBalancer.GetNext(nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "No WebSocket targets available")
	}
	
	cc, ok := c.(*echoUtil.CustomContext)
	if ok {
		cc.SetProvider(target.provider)
	}
	
	var wrapped configtypes.WrappedURL
	err = wrapped.UnmarshalText([]byte(target.url))
	if err != nil {
		return fmt.Errorf("UnmarshalText: %s", err)
	}
	
	c.Request().Host = wrapped.Host
	c.Request().URL = &url.URL{}
	
	reverseProxy := &httputil.ReverseProxy{Director: func(req *http.Request) { rewriteRequestURL(req, wrapped.ToURLPtr()) }}
	reverseProxy.ServeHTTP(c.Response(), c.Request())
	
	return nil
}

// rewriteRequestURL rewrites the request URL for WebSocket proxy
func rewriteRequestURL(req *http.Request, targetURL *url.URL) {
	req.URL.Scheme = targetURL.Scheme
	req.URL.Host = targetURL.Host
	req.URL.Path = targetURL.Path
	
	if targetURL.RawQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = targetURL.RawQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = targetURL.RawQuery + "&" + req.URL.RawQuery
	}
}

func (s *Adapter) ProxyPostRequest(c *echoUtil.CustomContext) (resBody []byte, resCode int, err error) {
	reqMethods := c.GetReqMethods()
	
	// Try method-based routing first if available
	if s.methodTransport != nil && s.methodTransport.canHandle(reqMethods) {
		if s.methodTransport.isAvailable() {
			return s.methodTransport.SendRequest(c)
		}
		
		return nil, http.StatusServiceUnavailable, echo.NewHTTPError(http.StatusServiceUnavailable, util.ExtraNodeNoAvailableTargetsErrorResponse)
	}
	
	// Fall back to legacy routing if method-based routing is not available or can't handle the request
	if s.cNFTTransport != nil && s.cNFTTransport.canHandle(reqMethods) {
		if s.cNFTTransport.isAvailable() {
			return s.cNFTTransport.SendRequest(c)
		}

		return nil, http.StatusServiceUnavailable, echo.NewHTTPError(http.StatusServiceUnavailable, util.ExtraNodeNoAvailableTargetsErrorResponse)
	}

	if s.publicTransport == nil {
		return nil, http.StatusServiceUnavailable, echo.NewHTTPError(http.StatusServiceUnavailable, fmt.Sprintf("No RPC nodes configured for %s", s.chainName))
	}

	resBody, err = s.publicTransport.withContext(c).sendHTTPReq()
	return resBody, http.StatusOK, err
}

func (s *Adapter) initLegacyTransports(cfg *configtypes.SolanaConfig) (err error) { //nolint:gocritic
	var rpcTargets []*ProxyTarget

	// Create ProxyTargets for BasicRouteNodes.
	for _, node := range cfg.BasicRouteNodes {
		rpcTargets = append(rpcTargets, NewProxyTarget(models.URLWithMethods{URL: node.URL.String()}, 0, node.Provider, node.NodeType))
	}

	// Create publicTransport only if there are BasicRouteNodes.
	if len(rpcTargets) > 0 {
		s.publicTransport, err = NewPublicTransport(rpcTargets, s.isMainnet)
		if err != nil {
			return fmt.Errorf("NewPublicTransport: %s", err)
		}
	}

	// Always create wsTransport if WSHostNodes are configured.
	if len(cfg.WSHostNodes) > 0 {
		s.wsTransport = &wsTransport{
			t: NewDefaultProxyTransport(cfg.WSHostNodes),
		}
	}

	// Create CNFTTransport only if DasAPINodes are configured.
	if len(cfg.DasAPINodes) > 0 {
		solanaChainWeights := make([]float64, len(cfg.DasAPINodes))
		for i := range cfg.DasAPINodes {
			solanaChainWeights[i] = 1.0
		}

		// Create ProxyTargets for DasAPINodes
		var dasAPITargets []*ProxyTarget
		for _, node := range cfg.DasAPINodes {
			dasAPITargets = append(dasAPITargets, NewProxyTarget(models.URLWithMethods{URL: node.URL.String()}, 0, node.Provider, node.NodeType))
		}

		pb, err := balancer.NewProbabilisticBalancer(dasAPITargets, solanaChainWeights)
		if err != nil {
			return fmt.Errorf("creating probabilistic balancer: %w", err)
		}
		s.cNFTTransport = NewCNFTransport(cNFTTargetType, solana.CNFTMethodList, pb, &RealHTTPRequester{})
	}

	return nil
}
