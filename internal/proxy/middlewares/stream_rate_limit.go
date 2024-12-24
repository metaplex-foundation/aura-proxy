package middlewares

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	echoUtil "aura-proxy/internal/pkg/util/echo"
)

const (
	temporaryPremiumUserID   = "bCEdDk8w8zMt8RvEOQwojQR3LRw1" // TODO: remove temp logic
	vipMaxConnectionsPerHost = 30
	maxConnectionsPerHost    = 5
	headerXForwardedFor      = "X-Forwarded-For"
	headerXRealIP            = "X-Real-Ip"
)

func StreamRateLimitMiddleware(skipper middleware.Skipper) echo.MiddlewareFunc {
	if skipper == nil {
		skipper = middleware.DefaultSkipper
	}
	limiter := &WSRateLimiter{
		rateLimitMap: make(map[string]byte),
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			if skipper(c) {
				return next(c)
			}

			uID := c.(*echoUtil.CustomContext).GetUserInfo().GetUser()
			if uID == "" {
				uID, err = limiter.getRealIP(c.Request())
				if err != nil {
					return echo.ErrTooManyRequests
				}
			}

			if !limiter.checkAndIncRateLimits(uID) {
				return echo.ErrTooManyRequests
			}
			defer limiter.decHostConnections(uID)

			return next(c)
		}
	}
}

type WSRateLimiter struct {
	rateLimitMap map[string]byte
	mutex        sync.Mutex
}

// forked method
func (*WSRateLimiter) getRealIP(request *http.Request) (string, error) {
	if ip := request.Header.Get(headerXForwardedFor); ip != "" {
		i := strings.IndexAny(ip, ",")
		if i > 0 {
			xffip := strings.TrimSpace(ip[:i])
			xffip = strings.TrimPrefix(xffip, "[")
			xffip = strings.TrimSuffix(xffip, "]")
			return xffip, nil
		}
		return ip, nil
	}
	if ip := request.Header.Get(headerXRealIP); ip != "" {
		ip = strings.TrimPrefix(ip, "[")
		ip = strings.TrimSuffix(ip, "]")
		return ip, nil
	}

	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return "", err
	}

	return host, nil
}

func (rl *WSRateLimiter) checkAndIncRateLimits(accID string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	if accID == temporaryPremiumUserID {
		if rl.rateLimitMap[accID] >= vipMaxConnectionsPerHost {
			return false
		}
	} else if rl.rateLimitMap[accID] >= maxConnectionsPerHost {
		return false
	}

	rl.rateLimitMap[accID]++

	return true
}

func (rl *WSRateLimiter) decHostConnections(host string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	if rl.rateLimitMap[host] > 0 {
		rl.rateLimitMap[host]--
	}

	if rl.rateLimitMap[host] == 0 {
		delete(rl.rateLimitMap, host)
	}
}
