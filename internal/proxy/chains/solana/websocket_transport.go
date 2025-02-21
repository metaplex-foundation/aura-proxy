package solana

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

type (
	ProxyTransport struct {
		httpClient *http.Client
		wsTargets  *balancer.RoundRobin[*ProxyTarget]
	}
)

func NewDefaultProxyTransport(hosts []configtypes.SolanaNode) *ProxyTransport {
	targets := make([]*ProxyTarget, 0, len(hosts))
	for i := range hosts {
		targets = append(targets, NewProxyTarget(models.URLWithMethods{URL: hosts[i].URL.String()}, 0, hosts[i].Provider, hosts[i].NodeType))
	}

	return &ProxyTransport{
		httpClient: &http.Client{Timeout: echoUtil.APIWriteTimeout - time.Second},
		wsTargets:  balancer.NewRoundRobin(targets),
	}
}

func (p *ProxyTransport) DefaultProxyWS(c echo.Context) (err error) {
	target, _, err := p.wsTargets.GetNext(nil)
	if err != nil {
		return err
	}
	if target == nil {
		return errors.New("empty target")
	}

	cc := c.(*echoUtil.CustomContext) //nolint:errcheck
	cc.SetProvider(target.provider)

	var wrapped configtypes.WrappedURL
	err = wrapped.UnmarshalText([]byte(target.url))
	if err != nil {
		return fmt.Errorf("UnmarshalText: %s", err)
	}

	c.Request().Host = wrapped.Host
	c.Request().URL = &url.URL{}
	if additionalPath := p.getRestPath(c); additionalPath != "" {
		c.Request().URL, err = url.Parse(additionalPath)
		if err != nil {
			return fmt.Errorf("Parse: %s", err)
		}
	}

	reverseProxy := &httputil.ReverseProxy{Director: func(req *http.Request) { rewriteRequestURL(req, wrapped.ToURLPtr()) }}
	reverseProxy.ServeHTTP(c.Response(), c.Request())

	return nil
}

func (p *ProxyTransport) ProxySSE(c echo.Context) (err error) {
	target, _, err := p.wsTargets.GetNext(nil)
	if err != nil {
		return err
	}
	if target == nil {
		return errors.New("empty target")
	}

	var wrapped configtypes.WrappedURL
	err = wrapped.UnmarshalText([]byte(target.url))
	if err != nil {
		return fmt.Errorf("UnmarshalText: %s", err)
	}

	c.Request().Host = wrapped.Host
	if additionalPath := p.getRestPath(c); additionalPath != "" {
		c.Request().URL, err = url.Parse(additionalPath)
		if err != nil {
			return fmt.Errorf("Parse: %s", err)
		}
	} else {
		c.Request().URL = &url.URL{}
	}

	reverseProxy := &httputil.ReverseProxy{Director: func(req *http.Request) { rewriteRequestURL(req, wrapped.ToURLPtr()) }}
	reverseProxy.ServeHTTP(c.Response(), c.Request())

	return nil
}
func rewriteRequestURL(req *http.Request, target *url.URL) {
	targetQuery := target.RawQuery
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path, req.URL.RawPath = joinURLPath(target, req.URL)
	if targetQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = targetQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
	}
}

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	// Same as singleJoiningSlash, but uses EscapedPath to determine
	// whether a slash should be added
	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	if bpath == "" {
		return a.Path, apath
	}

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath //nolint:revive
	}
	return a.Path + b.Path, apath + bpath
}
func singleJoiningSlash(a, b string) string {
	if b == "" {
		return a
	}
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func (*ProxyTransport) getRestPath(c echo.Context) string {
	var u strings.Builder
	restPath := c.Param(echoUtil.RestPathParamName)
	if restPath != "" {
		u.WriteByte('/')        //nolint:revive
		u.WriteString(restPath) //nolint:revive
	} else if strings.HasSuffix(c.Path(), "/") { // route /:token/
		u.WriteByte('/') //nolint:revive
	}
	if c.QueryString() != "" {
		u.WriteByte('?')               //nolint:revive
		u.WriteString(c.QueryString()) //nolint:revive
	}

	return u.String()
}
