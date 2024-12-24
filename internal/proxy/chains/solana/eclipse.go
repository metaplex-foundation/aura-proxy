package solana

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

var (
	eclipseChainHosts = []string{
		fmt.Sprintf("%s%s", solana.EclipseChainName, util.ProxyBasePath), // TODO: TBD
		"node.aura-eclipse.com",
		"proxy.aura-eclipse.com",
	}
)

type EclipseAdapter struct {
	publicTransport *publicTransport
	cNFTTransport   *CNFTTransport
}

func NewEclipseAdapter(cfg *configtypes.SolanaConfig, isMainnet bool) (a *EclipseAdapter, err error) { //nolint:gocritic
	a = &EclipseAdapter{}
	err = a.initTransports(cfg, isMainnet)
	if err != nil {
		return nil, fmt.Errorf("initTransports: %s", err)
	}

	return a, nil
}

func (*EclipseAdapter) GetName() string {
	return solana.EclipseChainName
}
func (*EclipseAdapter) GetAvailableMethods() map[string]uint {
	return solana.MethodList
}
func (*EclipseAdapter) GetHostNames() []string {
	return eclipseChainHosts
}

func (s *EclipseAdapter) ProxyWSRequest(c echo.Context) error {
	return s.publicTransport.predefinedTransport.DefaultProxyWS(c) // TODO: resolve for devnet
}

func (s *EclipseAdapter) ProxyPostRequest(c *echoUtil.CustomContext) (resBody []byte, resCode int, err error) {
	reqMethods := c.GetReqMethods()

	if s.cNFTTransport.canHandle(reqMethods) {
		if s.cNFTTransport.isAvailable() {
			return s.cNFTTransport.SendRequest(c)
		}

		return nil, http.StatusServiceUnavailable, echo.NewHTTPError(http.StatusServiceUnavailable, util.ExtraNodeNoAvailableTargetsErrorResponse)
	}

	resBody, err = s.publicTransport.withContext(c).sendHTTPReq()
	return resBody, http.StatusOK, err
}

func (s *EclipseAdapter) initTransports(cfg *configtypes.SolanaConfig, isMainnet bool) (err error) { //nolint:gocritic
	s.publicTransport, err = NewPublicTransport(cfg.BasicRouteNodes, cfg.WSHostURL, isMainnet)
	if err != nil {
		return fmt.Errorf("NewDefaultProxyTransport: %s", err)
	}

	s.cNFTTransport = NewCNFTransport(cfg.DasAPIURL, cNFTTargetType, solana.CNFTMethodList)

	return nil
}
