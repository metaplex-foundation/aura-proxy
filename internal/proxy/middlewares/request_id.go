package middlewares

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// RequestIDMiddleware RequestID returns a X-Request-ID middleware.
func RequestIDMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cp := util.NewRuntimeCheckpoint("RequestIDMiddleware")
			rid, err := uuid.NewRandom()
			if err != nil {
				log.Logger.Proxy.Errorf("uuid.NewRandom: %s", err)
			}
			cc := c.(*echoUtil.CustomContext) //nolint:errcheck
			cc.SetReqID(rid.String())

			m := cc.GetMetrics()
			m.SetNamespace(rid.String())
			m.AddCheckpoint(cp)

			return next(c)
		}
	}
}
