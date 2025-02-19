module aura-proxy

go 1.24

replace github.com/adm-metaex/aura-api => github.com/adm-metaex/aura-api v0.0.6-0.20250218162136-ad82036d10de

require (
	github.com/adm-metaex/aura-api v0.0.5
	github.com/buger/jsonparser v1.1.1
	github.com/gagliardetto/solana-go v1.12.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.4.2
	github.com/joho/godotenv v1.5.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/labstack/echo-contrib v0.17.2
	github.com/labstack/echo/v4 v4.13.3
	github.com/labstack/gommon v0.4.2
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/prometheus/client_golang v1.20.5
	github.com/sirupsen/logrus v1.9.3
	golang.org/x/time v0.8.0
	google.golang.org/protobuf v1.36.1
)

require github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect

require (
	filippo.io/edwards25519 v1.0.0-rc.1 // indirect; indirectgit config --global --unset url.git@github.com:.insteadof
	github.com/andres-erbsen/clock v0.0.0-20160526145045-9e14626cd129 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blendle/zapdriver v1.3.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/fatih/color v1.16.0 // indirect
	github.com/gagliardetto/binary v0.8.0 // indirect
	github.com/gagliardetto/treeout v0.1.4 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/logrusorgru/aurora v2.0.3+incompatible // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mostynb/zstdpool-freelist v0.0.0-20201229113212-927304c0c3b1 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/onsi/gomega v1.10.3 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.61.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/streamingfast/logging v0.0.0-20230608130331-f22c91403091 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.mongodb.org/mongo-driver v1.12.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/ratelimit v0.2.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/term v0.27.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241015192408-796eee8c2d53 // indirect
	google.golang.org/grpc v1.69.2 // indirect
)
