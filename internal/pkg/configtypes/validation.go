package configtypes

import (
	"errors"
	"fmt"

	"aura-proxy/internal/pkg/log"
)

var ErrInvalidPort = errors.New("invalid port")

func (p ProxyConfig) Validate() error { //nolint:gocritic
	if p.Port == 0 {
		return ErrInvalidPort
	}
	err := p.Solana.Validate(p.Hostname)
	if err != nil {
		return fmt.Errorf("solana config: %s", err)
	}

	return nil
}

func (s SolanaConfig) Validate(hostname string) error { //nolint:gocritic
	for i := range s.FailoverEndpoints {
		err := s.FailoverEndpoints[i].URL.Validate()
		if err != nil {
			return err
		}
		if s.FailoverEndpoints[i].Name == "" {
			log.Logger.Proxy.Warnf("FailoverEndpoints: empty name for %s", s.FailoverEndpoints[i].URL.String())
		}
	}

	return nil
}

//func (c Chains) Validate(possibleChains map[string]map[string]uint) error {
//	for chainName, chain := range c {
//		if _, ok := possibleChains[chainName]; !ok {
//			return fmt.Errorf("unsupported chain: %s. Possible chains %v", chainName, possibleChains)
//		}
//
//		err := chain.Validate()
//		if err != nil {
//			return fmt.Errorf("chain %s: %s", chainName, err)
//		}
//	}
//
//	return nil
//}
//
//func (c Chain) Validate() error {
//	if len(c.Hosts) == 0 {
//		return errors.New("empty host list")
//	}
//	for i := range c.Hosts {
//		err := c.Hosts[i].Validate()
//		if err != nil {
//			return err
//		}
//	}
//
//	if len(c.WSHosts) == 0 {
//		return errors.New("empty ws host list")
//	}
//	for i := range c.WSHosts {
//		err := c.WSHosts[i].Validate()
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}

func (w *WrappedURL) Validate() error {
	if w.Host == "" {
		return fmt.Errorf("invalid host: %s", w.String())
	}

	return nil
}
