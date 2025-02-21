package solana

import (
	"fmt"
	"net/http"
	"time"

	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// HTTPRequester interface defines how to make HTTP requests.
type HTTPRequester interface {
	DoRequest(c *echoUtil.CustomContext, targetURL string) (respBody []byte, statusCode int, err error)
}

// RealHTTPRequester is the production implementation of HTTPRequester.
type RealHTTPRequester struct{}

func (r *RealHTTPRequester) DoRequest(c *echoUtil.CustomContext, targetURL string) (respBody []byte, statusCode int, err error) {
	return transport.MakeHTTPRequest(c, &http.Client{Timeout: echoUtil.APIWriteTimeout - time.Second}, http.MethodPost, targetURL, false)
}

type (
	CNFTTransport struct {
		httpRequester  HTTPRequester // Use the interface
		targetSelector balancer.TargetSelector[*ProxyTarget]
		methodList     map[string]uint
		targetType     string
	}
)

func NewCNFTransport(targetType string, methodList map[string]uint, targetSelector balancer.TargetSelector[*ProxyTarget], requester HTTPRequester) *CNFTTransport {
	return &CNFTTransport{
		httpRequester:  requester, // Inject the requester
		targetSelector: targetSelector,
		targetType:     targetType,
		methodList:     methodList,
	}
}

func (p *CNFTTransport) isAvailable() bool {
	return p.targetSelector.IsAvailable()
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

func (p *CNFTTransport) sendRequestWithRetries(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	var (
		target *ProxyTarget
		index  int
	)

	exclude := make([]int, 0)
	for i := 0; i < p.targetSelector.GetTargetsCount(); i++ {
		target, index, err = p.targetSelector.GetNext(exclude)
		if err != nil {
			return nil, http.StatusServiceUnavailable, fmt.Errorf("no available targets: %w", err)
		}
		c.SetProvider(target.provider)

		respBody, statusCode, err = p.httpRequester.DoRequest(c, target.url) // Use the injected requester
		if err == nil {
			return respBody, statusCode, nil
		}
		exclude = append(exclude, index) // Exclude the failed target
	}

	return nil, http.StatusServiceUnavailable, fmt.Errorf("all targets failed")
}

func (p *CNFTTransport) SendRequest(c *echoUtil.CustomContext) (respBody []byte, statusCode int, err error) {
	startTime := time.Now()

	respBody, statusCode, err = p.sendRequestWithRetries(c)
	transport.ResponsePostHandling(c, err, p.targetType, 1, time.Since(startTime).Milliseconds())

	return respBody, statusCode, err
}
