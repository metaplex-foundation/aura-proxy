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

		Solana  SolanaConfig `required:"true" split_words:"true"`
		Eclipse SolanaConfig `required:"false" split_words:"true"`
		Chains  Chains       `required:"false" split_words:"true"`

		Port        uint64 `required:"true" split_words:"true"`
		MetricsPort uint64 `required:"false" split_words:"true"`

		IsMainnet bool `required:"true" default:"true" split_words:"true"`
	}
	SolanaConfig struct {
		DasAPIURL       []WrappedURL `envconfig:"PROXY_SOLANA_DAS_API_URL" required:"true" split_words:"true"`
		BasicRouteNodes SolanaNodes  `envconfig:"PROXY_SOLANA_BASIC_ROUTE_NODES" required:"true" split_words:"true"`
		WSHostURL       []WrappedURL `envconfig:"PROXY_SOLANA_WS_HOST_URL" required:"false" split_words:"true"`
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
