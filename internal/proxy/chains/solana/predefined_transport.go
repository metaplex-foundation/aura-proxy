package solana

import (
	"github.com/labstack/echo/v4"
)

type predefinedTransport struct {
	t *ProxyTransport
}

func (r *predefinedTransport) DefaultProxyWS(c echo.Context) (err error) {
	return r.t.DefaultProxyWS(c)
}
