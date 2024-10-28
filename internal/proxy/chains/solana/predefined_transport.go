package solana

import (
	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util/balancer"
)

type predefinedTransport struct {
	t       *transport.ProxyTransport
	targets *balancer.RoundRobin[*proxyTarget]
}

func (r *predefinedTransport) DefaultProxyWS(c echo.Context) (err error) {
	return r.t.DefaultProxyWS(c)
}

func (r *predefinedTransport) isAvailable() bool {
	return r.targets.IsAvailable()
}
