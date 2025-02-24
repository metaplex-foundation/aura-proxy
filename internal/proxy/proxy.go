package proxy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	auraProto "github.com/adm-metaex/aura-api/pkg/proto"
	"github.com/adm-metaex/aura-api/pkg/types"
	"github.com/google/uuid"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo-contrib/pprof"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"aura-proxy/internal/pkg/collector"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/metrics"
	echoUtil "aura-proxy/internal/pkg/util/echo"
	"aura-proxy/internal/proxy/chains/solana"
	"aura-proxy/internal/proxy/config"
	"aura-proxy/internal/proxy/middlewares"
)

const (
	statusOperational     = "operational"
	statusKey             = "status"
	serviceKey            = "service"
	serverShutdownTimeout = time.Second * 5
	collectorInterval     = 10 * time.Second
)

type IRequestCounter interface {
	IncUserRequests(user *auraProto.UserWithTokens, currentReqCount int64, chain, token string, isMainnet bool)
}

type IStatCollector interface {
	Add(s *auraProto.Stat)
}

type proxy struct {
	waitGroup *sync.WaitGroup
	ctx       context.Context
	ctxCancel context.CancelFunc

	router        *echo.Echo
	metricsServer *echo.Echo

	statsCollector IStatCollector
	requestCounter IRequestCounter
	serviceName    string

	adapters map[string]Adapter // host
	certData []byte

	proxyPort   uint64
	metricsPort uint64

	isMainnet bool
}

type Adapter interface {
	GetName() string
	GetHostNames() []string
	GetAvailableMethods() map[string]uint // method name / cost
	ProxyPostRequest(c *echoUtil.CustomContext) ([]byte, int, error)
	ProxyWSRequest(c echo.Context) error
	PreparePostReq(c *echoUtil.CustomContext) *types.RPCResponse
}

func NewProxy(cfg config.Config) (p *proxy, err error) { //nolint:gocritic
	// increase uuid generation productivity
	uuid.EnableRandPool()

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			cancelFunc()
		}
	}()

	auraAPI, err := auraProto.NewClient(cfg.Proxy.AuraGRPCHost)
	if err != nil {
		return nil, fmt.Errorf("auraAPI NewClient: %s", err)
	}
	statCollector, err := collector.NewCollector[*auraProto.Stat](ctx, collectorInterval, auraAPI)
	if err != nil {
		return nil, fmt.Errorf("NewCollector: %s", err)
	}

	// load token to CustomContext. CustomContext must be inited before
	tokenChecker, err := middlewares.NewTokenChecker(ctx, auraAPI)
	if err != nil {
		return nil, fmt.Errorf("NewTokenChecker: %s", err)
	}
	wg := &sync.WaitGroup{}
	requestCounter := NewRequestCounter(ctx, wg, auraAPI)
	return InitProxy(ctx, cancelFunc, cfg, wg, statCollector, requestCounter, tokenChecker)
}

func InitProxy(ctx context.Context, cancel context.CancelFunc, cfg config.Config, wg *sync.WaitGroup, statCollector IStatCollector, requestCounter IRequestCounter, tokenChecker ITokenChecker) (p *proxy, err error) {
	p = &proxy{
		proxyPort:      cfg.Proxy.Port,
		metricsPort:    cfg.Proxy.MetricsPort,
		metricsServer:  initMetricsServer(),
		waitGroup:      wg,
		ctx:            ctx,
		ctxCancel:      cancel,
		statsCollector: statCollector,
		serviceName:    fmt.Sprintf("%s-%s", cfg.Service.Name, cfg.Service.Level),
		requestCounter: requestCounter,
		adapters:       make(map[string]Adapter),
		isMainnet:      cfg.Proxy.IsMainnet,
	}
	if cfg.Proxy.CertFile != "" {
		p.certData, err = os.ReadFile(cfg.Proxy.CertFile)
		if err != nil {
			return nil, fmt.Errorf("fail to read certificate (%s): %s", cfg.Proxy.CertFile, err)
		}
	}

	err = p.initAdapters(&cfg) // todo: should be injected
	if err != nil {
		return nil, fmt.Errorf("initAdapters: %s", err)
	}
	p.initProxyServer()

	p.initProxyHandlers(tokenChecker)
	return p, nil
}

func (p *proxy) initAdapters(cfg *config.Config) error { //nolint:gocritic
	// Conditionally initialize SolanaAdapter.
	if len(cfg.Proxy.Solana.BasicRouteNodes) > 0 || len(cfg.Proxy.Solana.WSHostNodes) > 0 || len(cfg.Proxy.Solana.DasAPINodes) > 0 {
		solanaAdapter, err := solana.NewSolanaAdapter(&cfg.Proxy.Solana, cfg.Proxy.IsMainnet)
		if err != nil {
			return fmt.Errorf("NewSolanaAdapter: %s", err)
		}
		for _, n := range solanaAdapter.GetHostNames() {
			p.adapters[n] = solanaAdapter
		}
	}

	// Conditionally initialize EclipseAdapter.
	if len(cfg.Proxy.Eclipse.DasAPINodes) > 0 && len(cfg.Proxy.Eclipse.BasicRouteNodes) > 0 {
		eclipseAdapter, err := solana.NewEclipseAdapter(&cfg.Proxy.Eclipse, cfg.Proxy.IsMainnet)
		if err != nil {
			return fmt.Errorf("NewEclipseAdapter: %s", err)
		}
		for _, n := range eclipseAdapter.GetHostNames() {
			p.adapters[n] = eclipseAdapter
		}
	}

	return nil
}

func (p *proxy) initProxyServer() {
	s := echo.New()
	echoUtil.SetupServer(s, true)

	echoUtil.InitBaseMiddlewares(s, middlewares.CORSWithConfig(middlewares.CORSConfig{
		// forked cors middleware
		AllowOrigins: []string{"*"},
	}))

	// temp. Profile middleware
	pprof.Register(s, "/pprof/d877cb77-e163-4542-9401-017dea48be76")

	s.Use(echoprometheus.NewMiddleware("aura"))
	p.router = s
}

func initMetricsServer() *echo.Echo {
	s := echo.New()
	echoUtil.SetupServer(s, false)
	s.HideBanner = true

	s.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		DisableStackAll: true,
		LogErrorFunc:    echoUtil.LogPanic,
	}))
	s.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true,
		LogMethod: true,
		LogError:  true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error != nil {
				log.Logger.Proxy.Errorf("metrics: code %d method %s: %s", v.Status, v.Method, v.Error)
			}
			return nil
		},
	}))
	s.GET("/metrics", echoprometheus.NewHandler())
	metrics.InitStartTime()

	return s
}

func (p *proxy) Run() (err error) {
	addr := fmt.Sprintf(":%d", p.proxyPort)
	if len(p.certData) != 0 {
		err = p.router.StartTLS(addr, p.certData, p.certData)
	} else {
		err = p.router.Start(addr)
	}
	if err != http.ErrServerClosed { //nolint:errorlint
		return err
	}

	return nil
}

func (p *proxy) RunMetrics() (err error) {
	if p.metricsPort == 0 {
		return nil
	}

	err = p.metricsServer.Start(fmt.Sprintf(":%d", p.metricsPort))
	if err != http.ErrServerClosed { //nolint:errorlint
		return err
	}

	return nil
}

func (p *proxy) Stop() error {
	ctx, cancel := context.WithTimeout(p.ctx, serverShutdownTimeout)
	defer cancel()

	go p.metricsServer.Shutdown(ctx) //nolint:errcheck
	err := p.router.Shutdown(ctx)
	if err != nil {
		log.Logger.Proxy.Errorf("router.Shutdown: %s", err)
	}
	p.ctxCancel()

	return nil
}

func (p *proxy) WaitGroup() *sync.WaitGroup {
	return p.waitGroup
}
