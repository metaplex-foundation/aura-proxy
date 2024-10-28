package solana

const (
	GetAsset                  = "getAsset"
	GetAssetBatch             = "getAssetBatch"
	GetAssetProof             = "getAssetProof"
	GetAssetProofBatch        = "getAssetProofBatch"
	GetAssetsByOwner          = "getAssetsByOwner"
	GetAssetsByAuthority      = "getAssetsByAuthority"
	GetAssetsByCreator        = "getAssetsByCreator"
	GetAssetsByGroup          = "getAssetsByGroup"
	GetGrouping               = "getGrouping"
	SearchAssets              = "searchAssets"
	GetTokenAccounts          = "getTokenAccounts"
	GetSignaturesForAsset     = "getSignaturesForAsset"
	GetSignaturesForAssetV2   = "getSignaturesForAssetV2"
	GetAuraHealth             = "getAuraHealth"
	GetAssets                 = "getAssets"
	GetAssetsAlias            = "get_assets"
	GetAssetProofs            = "getAssetProofs"
	GetAssetProofsAlias       = "get_asset_proofs"
	GetAssetSignatures        = "getAssetSignatures"
	GetAssetSignaturesAlias   = "get_asset_signatures"
	GetAssetSignaturesV2      = "getAssetSignaturesV2"
	GetAssetSignaturesV2Alias = "get_asset_signatures_v2"
)

var CNFTMethodList = map[string]uint{
	GetAsset:                  3,
	GetAssetBatch:             3,
	GetAssetProof:             3,
	GetAssetProofBatch:        3,
	GetAssetsByOwner:          3,
	GetAssetsByAuthority:      3,
	GetAssetsByCreator:        3,
	GetAssetsByGroup:          3,
	GetGrouping:               3,
	SearchAssets:              3,
	GetTokenAccounts:          3,
	GetSignaturesForAsset:     3,
	GetSignaturesForAssetV2:   3,
	GetAuraHealth:             3,
	GetAssets:                 3,
	GetAssetsAlias:            3,
	GetAssetProofs:            3,
	GetAssetProofsAlias:       3,
	GetAssetSignatures:        3,
	GetAssetSignaturesAlias:   3,
	GetAssetSignaturesV2:      3,
	GetAssetSignaturesV2Alias: 3,
}
