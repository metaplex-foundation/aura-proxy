package transport

import (
	"context"
	"errors"

	"aura-proxy/internal/pkg/util/echo"
)

func ResponsePostHandling(c *echo.CustomContext, err error, targetType string, i int, responseTimeMs int64) {
	c.SetTargetType(targetType)
	c.SetProxyAttempts(i)

	if responseTimeMs != 0 {
		c.SetProxyResponseTime(responseTimeMs)
	}

	if !errors.Is(err, context.Canceled) {
		c.SetProxyHasError(err != nil)
	}
}
