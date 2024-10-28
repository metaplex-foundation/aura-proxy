package solana

import (
	"context"
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

type solanaAdapter struct {
	publicTransport *publicTransport
	cNFTTransport   *CNFTTransport
	auraAPI         proto.AuraClient
	failoverTargets configtypes.FailoverTargets
}

func NewSolanaAdapter(ctx context.Context, auraAPI proto.AuraClient, cfg configtypes.SolanaConfig, defaultSolanaURL []configtypes.WrappedURL) (a *solanaAdapter, err error) { //nolint:gocritic
	if auraAPI == nil {
		return nil, errors.New("empty auraAPI")
	}

	a = &solanaAdapter{
		auraAPI:         auraAPI,
		failoverTargets: cfg.FailoverEndpoints,
	}

	err = a.initTransports(ctx, cfg, defaultSolanaURL)
	if err != nil {
		return nil, fmt.Errorf("initTransports: %s", err)
	}

	return a, nil
}

func (*solanaAdapter) GetName() string {
	return solana.ChainName
}
func (*solanaAdapter) GetAvailableMethods() map[string]uint {
	return solana.MethodList
}

func (s *solanaAdapter) ProxyWSRequest(c echo.Context) error {
	return s.publicTransport.predefinedTransport.DefaultProxyWS(c) // TODO: resolve for devnet
}

func (s *solanaAdapter) ProxyPostRequest(c *echoUtil.CustomContext) (resBody []byte, resCode int, err error) {
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

func (s *solanaAdapter) initTransports(ctx context.Context, cfg configtypes.SolanaConfig, defaultSolanaURL []configtypes.WrappedURL) (err error) { //nolint:gocritic
	wsTargets := []configtypes.WrappedURL{}
	if len(defaultSolanaURL) != 0 {
		wsTargets = defaultSolanaURL
	}
	if len(wsTargets) == 0 {
		for i := range s.failoverTargets {
			wsTargets = append(wsTargets, s.failoverTargets[i].URL)
		}
	}

	s.publicTransport, err = NewPublicTransport(s.failoverTargets, defaultSolanaURL, wsTargets)
	if err != nil {
		return fmt.Errorf("NewDefaultProxyTransport: %s", err)
	}

	s.cNFTTransport = NewCNFTransport(cfg.DasAPIURL, cNFTTargetType, solana.CNFTMethodList)

	return nil
}
