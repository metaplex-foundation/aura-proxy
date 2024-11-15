package config

import (
	"fmt"

	"aura-proxy/internal/pkg/chains"
	"aura-proxy/internal/pkg/configtypes"
)

type Config struct {
	Service configtypes.ServiceConfig
	Proxy   configtypes.ProxyConfig
}

func (c Config) Validate() error { //nolint:gocritic
	if err := c.Proxy.Validate(chains.PossibleChainsMethods); err != nil {
		return fmt.Errorf("proxy: %s", err)
	}

	return nil
}
