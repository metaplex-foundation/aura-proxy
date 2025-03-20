package configtypes

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"

	"aura-proxy/internal/pkg/chains/solana"
)

// struct field names are used for env variable names. Edit with care
type (
	ProxyConfig struct {
		CertFile     string `required:"false" split_words:"true"`
		AuraGRPCHost string `envconfig:"PROXY_AURA_GRPC_HOST" required:"true" split_words:"true"`

		Solana  SolanaConfig `envconfig:"PROXY_SOLANA_CONFIG" required:"true" split_words:"true"`
		Eclipse SolanaConfig `envconfig:"PROXY_ECLIPSE_CONFIG" required:"false" split_words:"true"`
		Chains  Chains       `required:"false" split_words:"true"`

		Port        uint64 `required:"true" split_words:"true"`
		MetricsPort uint64 `required:"false" split_words:"true"`

		IsMainnet bool `required:"true" default:"true" split_words:"true"`
	}
	SolanaConfig struct {
		// Legacy configuration (for backward compatibility)
		DasAPINodes     SolanaNodes `json:"dasAPINodes"`
		BasicRouteNodes SolanaNodes `json:"basicRouteNodes"`
		WSHostNodes     SolanaNodes `json:"WSHostNodes"`

		// Method groups shared across providers
		MethodGroups []MethodGroupConfig `json:"methodGroups,omitempty"`

		// New method-based routing configuration
		Providers []ProviderConfig `json:"providers,omitempty"`
	}

	// New configuration types for method-based routing
	ProviderConfig struct {
		Name      string           `json:"name"`
		Endpoints []EndpointConfig `json:"endpoints"`
	}

	EndpointConfig struct {
		URL            string          `json:"url"`
		Weight         float64         `json:"weight,omitempty"`         // Default: 1.0
		NodeType       solana.NodeType `json:"nodeType,omitempty"`       // For backward compatibility
		Methods        []string        `json:"methods,omitempty"`        // Methods this endpoint handles well
		ExcludeMethods []string        `json:"excludeMethods,omitempty"` // Methods to exclude
		MethodGroups   []string        `json:"methodGroups,omitempty"`   // Named method groups
		HandleOther    bool            `json:"handleOther,omitempty"`    // Handle methods not explicitly assigned elsewhere
	}

	MethodGroupConfig struct {
		Name    string   `json:"name"`
		Methods []string `json:"methods"`
	}
)

// struct field names are used for env variable names. Edit with care
type (
	ServiceConfig struct {
		Name  string `envconfig:"NAME" default:"unspecified" required:"false"`
		Level string `envconfig:"LEVEL" required:"false"`
	}
)

type PossibleConfig interface {
	Validate() error
}

func LoadFile[T PossibleConfig](envFile string) (c T, err error) {
	if envFile != "" {
		err = godotenv.Load(envFile)
		if err != nil {
			return c, fmt.Errorf("godotenv.Load (%s): %w", envFile, err)
		}
	}

	err = envconfig.Process("", &c)
	if err != nil {
		return c, err
	}

	err = c.Validate()
	if err != nil {
		return c, fmt.Errorf("validate: %s", err)
	}

	return c, nil
}

type (
	SolanaNodes []SolanaNode
	SolanaNode  struct {
		URL      WrappedURL
		Provider string
		NodeType solana.NodeType
	}
	Chains map[string]Chain
	Chain  struct {
		Hosts   []WrappedURL
		WSHosts []WrappedURL
	}

	WrappedURL url.URL
)

func (s *SolanaConfig) Decode(value string) error {
	if value == "" {
		return nil
	}

	return json.Unmarshal([]byte(value), &s)
}

func (c *SolanaNodes) Decode(value string) error {
	if value == "" {
		return nil
	}

	return json.Unmarshal([]byte(value), &c)
}

func (c *Chains) Decode(value string) error {
	if value == "" {
		return nil
	}

	return json.Unmarshal([]byte(value), &c)
}

func (w *WrappedURL) UnmarshalText(text []byte) error {
	u, err := url.ParseRequestURI(string(text))
	if err != nil {
		return err
	}
	*w = WrappedURL(*u)

	return nil
}
func (w *WrappedURL) String() string {
	return w.ToURLPtr().String()
}
func (w *WrappedURL) ToURLPtr() *url.URL {
	t := url.URL(*w)

	return &t
}
