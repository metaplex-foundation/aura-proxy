package solana

import (
	"github.com/labstack/echo/v4"
)

type wsTransport struct {
	t *ProxyTransport
}

func (pt *wsTransport) DefaultProxyWS(c echo.Context) error {
	return pt.t.DefaultProxyWS(c)
}
