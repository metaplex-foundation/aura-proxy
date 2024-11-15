package solana

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	cNFTTargetType = "c_nft"
)

var (
	chainHosts = []string{
		fmt.Sprintf("%s%s", solana.ChainName, util.ProxyBasePath), // TODO: TBD
		"localhost:8000", // for local tests
	}
)

type Adapter struct {
	publicTransport *publicTransport
	cNFTTransport   *CNFTTransport
	auraAPI         proto.AuraClient
}

func NewSolanaAdapter(auraAPI proto.AuraClient, cfg *configtypes.SolanaConfig, isMainnet bool) (a *Adapter, err error) { //nolint:gocritic
	if auraAPI == nil {
		return nil, errors.New("empty auraAPI")
	}
	a = &Adapter{
		auraAPI: auraAPI,
	}

	err = a.initTransports(cfg, isMainnet)
	if err != nil {
		return nil, fmt.Errorf("initTransports: %s", err)
	}

	return a, nil
}

func (*Adapter) GetName() string {
	return solana.ChainName
}
func (*Adapter) GetAvailableMethods() map[string]uint {
	return solana.MethodList
}
func (*Adapter) GetHostNames() []string {
	return chainHosts
}

func (s *Adapter) ProxyWSRequest(c echo.Context) error {
	return s.publicTransport.predefinedTransport.DefaultProxyWS(c) // TODO: resolve for devnet
}

func (s *Adapter) ProxyPostRequest(c *echoUtil.CustomContext) (resBody []byte, resCode int, err error) {
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

func (s *Adapter) initTransports(cfg *configtypes.SolanaConfig, isMainnet bool) (err error) { //nolint:gocritic
	s.publicTransport, err = NewPublicTransport(cfg.BasicRouteNodes, cfg.WSHostURL, isMainnet)
	if err != nil {
		return fmt.Errorf("NewDefaultProxyTransport: %s", err)
	}

	s.cNFTTransport = NewCNFTransport(cfg.DasAPIURL, cNFTTargetType, solana.CNFTMethodList)

	return nil
}
