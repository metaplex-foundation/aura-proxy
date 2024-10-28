package configtypes

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// struct field names are used for env variable names. Edit with care
type (
	ProxyConfig struct {
		Hostname     string `envconfig:"HOSTNAME" default:"unspecified" required:"true" split_words:"true"`
		CertFile     string `required:"false" split_words:"true"`
		AuraGRPCHost string `required:"true" split_words:"true"`

		Solana SolanaConfig `required:"true" split_words:"true"`

		Port        uint64 `required:"true" split_words:"true"`
		MetricsPort uint64 `required:"false" split_words:"true"`
	}
	SolanaConfig struct {
		DasAPIURL         []WrappedURL    `envconfig:"PROXY_SOLANA_DAS_API_URL" required:"false" split_words:"true"`
		FailoverEndpoints FailoverTargets `required:"false" split_words:"true"`
	}
)

// struct field names are used for env variable names. Edit with care
type (
	ServiceConfig struct {
		Name  string `envconfig:"NAME" default:"unspecified" required:"false"`
		Level string `envconfig:"LEVEL" required:"false"`
	}
)

type FailoverTargets []struct {
	Name           string
	URL            WrappedURL
	ReqLimitHourly uint64
}

func (f *FailoverTargets) Decode(value string) error {
	if value == "" {
		return nil
	}

	return json.Unmarshal([]byte(value), &f)
}

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
	WrappedURL url.URL
)

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
