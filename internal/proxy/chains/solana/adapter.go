package solana

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	cNFTTargetType   = "c_nft"
	methodTargetType = "method_based"
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
	rpcTransport *UnifiedTransport
	wsTransport  *wsTransport

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
	return s.wsTransport.DefaultProxyWS(c) // TODO: resolve for devnet
}

func (s *Adapter) ProxyPostRequest(c *echoUtil.CustomContext) (resBody []byte, resCode int, err error) {
	reqMethods := c.GetReqMethods()

	if s.rpcTransport == nil || !s.rpcTransport.canHandle(reqMethods) || !s.rpcTransport.isAvailable() {
		return nil, http.StatusServiceUnavailable, echo.NewHTTPError(http.StatusServiceUnavailable, util.ExtraNodeNoAvailableTargetsErrorResponse)
	}
	return s.rpcTransport.SendRequest(c)
}
