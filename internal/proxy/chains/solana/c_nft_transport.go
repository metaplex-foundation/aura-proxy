package solana

import (
	"net/http"
	"time"

	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

type (
	CNFTTransport struct {
		httpClient  *http.Client
		auraTargets *balancer.RoundRobin[*ProxyTarget]
		methodList  map[string]uint
		targetType  string
	}
)

func NewCNFTransport(hosts []configtypes.SolanaNode, targetType string, methodList map[string]uint) *CNFTTransport {
	predefinedTransportTargets := make([]*ProxyTarget, 0, len(hosts))
	for i := range hosts {
		predefinedTransportTargets = append(predefinedTransportTargets, NewProxyTarget(models.URLWithMethods{URL: hosts[i].URL.String()}, 0, hosts[i].Provider, hosts[i].NodeType))
	}

	return &CNFTTransport{
		httpClient:  &http.Client{Timeout: echoUtil.APIWriteTimeout - time.Second},
		auraTargets: balancer.NewRoundRobin(predefinedTransportTargets),
		targetType:  targetType,
		methodList:  methodList,
	}
}

func (p *CNFTTransport) isAvailable() bool {
	return p.auraTargets.IsAvailable()
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

func (p *CNFTTransport) selectTargetAndSendReq(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	availableAuraTries := p.auraTargets.GetTargetsCount()
	counter := p.auraTargets.GetCounter()
	for {
		if availableAuraTries <= 0 {
			break
		}
		target := p.auraTargets.GetByCounter(counter)
		c.SetProvider(target.provider)
		p.auraTargets.IncCounter()
		respBody, statusCode, err = transport.MakeHTTPRequest(c, p.httpClient, http.MethodPost, target.url, false)
		if err == nil {
			return respBody, statusCode, nil
		}
		counter++
		availableAuraTries--
	}
	return
}

func (p *CNFTTransport) SendRequest(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	startTime := time.Now()

	respBody, statusCode, err = p.selectTargetAndSendReq(c)
	transport.ResponsePostHandling(c, err, p.targetType, 1, time.Since(startTime).Milliseconds())

	return respBody, statusCode, err
}
