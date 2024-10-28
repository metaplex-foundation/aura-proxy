package main

import (
	"flag"
	"time"

	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
	"aura-proxy/internal/proxy"
	"aura-proxy/internal/proxy/config"
)

const (
	waitTimeout = time.Second * 5
)

type flags struct {
	logLevel string
	envFile  string
}

// Setup flags
func getFlags() (f flags) {
	flag.StringVar(&f.logLevel, "log", "info", "log level [debug|info|warn|error|crit]")
	flag.StringVar(&f.envFile, "envFile", "", "path to .env file")
	flag.Parse()

	return
}

func main() {
	f := getFlags()
	err := log.Setup(f.logLevel)
	if err != nil {
		log.Logger.Proxy.Fatalf("Log setup: %s", err)
	}

	cfg, err := configtypes.LoadFile[config.Config](f.envFile)
	if err != nil {
		log.Logger.Proxy.Fatalf("Config: %s", err)
	}

	log.Logger.Proxy.Infof("Start service")

	app, err := proxy.NewProxy(cfg)
	if err != nil {
		log.Logger.Proxy.Fatalf("NewProxy: %s", err)
	}

	// Proxy
	go func() {
		if err := app.Run(); err != nil {
			log.Logger.Proxy.Fatalf("Proxy: %s", err)
		}
	}()
	// Metrics
	go func() {
		if err := app.RunMetrics(); err != nil {
			log.Logger.Proxy.Fatalf("Metrics: %s", err)
		}
	}()

	// Termination handler.
	util.GracefulStop(app.WaitGroup(), waitTimeout, func() {
		err = app.Stop()
		if err != nil {
			log.Logger.Proxy.Errorf(err.Error())
		}
	})
}
