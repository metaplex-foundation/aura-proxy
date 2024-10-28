package solana

import (
	"net/http"
	"time"

	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util"
	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

type (
	CNFTTransport struct {
		httpClient  *http.Client
		targets     *balancer.RoundRobin[string]
		auraTargets *balancer.RoundRobin[targetWithName]
		methodList  map[string]uint
		targetType  string
	}
	targetWithName struct {
		name   string
		target string
	}
)

func NewCNFTransport(hosts []configtypes.WrappedURL, targetType string, methodList map[string]uint) *CNFTTransport {
	return &CNFTTransport{
		httpClient: &http.Client{Timeout: echoUtil.APIWriteTimeout - time.Second},
		targets:    balancer.NewRoundRobin(util.Map(hosts, func(t configtypes.WrappedURL) string { return t.String() })),
		//auraTargets: balancer.NewRoundRobin(util.Map(auraTargets, func(t configtypes.WrappedURL) string { return t.String() })),
		targetType: targetType,
		methodList: methodList,
	}
}

func (p *CNFTTransport) isAvailable() bool {
	return p.targets.IsAvailable()
}
func (p *CNFTTransport) canHandle(methods []string) bool {
	if len(methods) == 0 {
		return false
	}

	for _, method := range methods {
		if _, ok := p.methodList[method]; !ok {
			return false
		}
	}

	return true
}

func (p *CNFTTransport) selectTargetAndSendReq(c *echoUtil.CustomContext) (respBody []byte, statusCode int, targetName string, err error) {
	if p.auraTargets.IsAvailable() {
		namedTarget := p.auraTargets.GetNext()
		respBody, statusCode, err = transport.MakeHTTPRequest(c, p.httpClient, http.MethodPost, namedTarget.target, false)
		if err == nil {
			return respBody, statusCode, namedTarget.name, nil
		}
	}

	// failover
	target := p.targets.GetNext()
	respBody, statusCode, err = transport.MakeHTTPRequest(c, p.httpClient, http.MethodPost, target, false)
	return
}

func (p *CNFTTransport) SendRequest(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	startTime := time.Now()

	var targetName string
	respBody, statusCode, targetName, err = p.selectTargetAndSendReq(c)
	transport.ResponsePostHandling(c, err, targetName, p.targetType, 1, time.Since(startTime).Milliseconds())

	return respBody, statusCode, err
}
