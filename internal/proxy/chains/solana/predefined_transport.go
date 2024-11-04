package solana

import (
	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/transport"
)

type predefinedTransport struct {
	t *transport.ProxyTransport
}

func (r *predefinedTransport) DefaultProxyWS(c echo.Context) (err error) {
	return r.t.DefaultProxyWS(c)
}
