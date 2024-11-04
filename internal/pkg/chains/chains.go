package chains

import (
	"aura-proxy/internal/pkg/chains/solana"
)

var PossibleChainsMethods = map[string]map[string]uint{
	solana.ChainName: solana.MethodList,
}
