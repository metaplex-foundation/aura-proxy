package solana

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util"
	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	cNFTTargetType = "c_nft"
)

var (
	solanaChainHosts = []string{
		"aura-mainnet.metaplex.com",
		"aura-devnet.metaplex.com",
		"mainnet-aura.metaplex.com",
		"devnet-aura.metaplex.com",
		"localhost:2011", // for local tests
		"127.0.0.1:2011", // for local tests
	}
	eclipseChainHosts = []string{
		"aura-eclipse-mainnet.metaplex.com",
		"eclipse-mainnet-aura.metaplex.com",
		//"localhost:2011", // for local tests
		//"127.0.0.1:2011", // for local tests
	}
)

type Adapter struct {
	publicTransport  *publicTransport
	cNFTTransport    *CNFTTransport
	wsTransport      *wsTransport
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
	err := a.initTransports(cfg)
	if err != nil {
		return nil, fmt.Errorf("initTransports: %s", err)
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

func (s *Adapter) ProxyWSRequest(c echo.Context) error {
	return s.wsTransport.DefaultProxyWS(c) // TODO: resolve for devnet
}

func (s *Adapter) ProxyPostRequest(c *echoUtil.CustomContext) (resBody []byte, resCode int, err error) {
	reqMethods := c.GetReqMethods()

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

func (s *Adapter) initTransports(cfg *configtypes.SolanaConfig) (err error) { //nolint:gocritic
	var allTargets []*ProxyTarget

	// Create ProxyTargets for BasicRouteNodes.
	for _, node := range cfg.BasicRouteNodes {
		allTargets = append(allTargets, NewProxyTarget(models.URLWithMethods{URL: node.URL.String()}, 0, node.Provider, node.NodeType))
	}

	// Create publicTransport only if there are BasicRouteNodes.
	if len(allTargets) > 0 {
		s.publicTransport, err = NewPublicTransport(allTargets, cfg.WSHostNodes, s.isMainnet) // Use s.isMainnet
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
