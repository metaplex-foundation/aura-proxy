package solana

import (
	"errors"
	"strconv"
	"strings"
)

const (
	ChainName        = "solana"
	EclipseChainName = "eclipse"
)

func init() {
	for k, v := range CNFTMethodList {
		MethodList[k] = v
	}
}

var MethodList = map[string]uint{
	GetAccountInfo:                    3,
	GetBalance:                        3,
	GetBlock:                          3,
	GetBlockHeight:                    3,
	GetBlockProduction:                3,
	GetBlockCommitment:                3,
	GetBlocks:                         3,
	GetBlocksWithLimit:                3,
	GetBlockTime:                      3,
	GetClusterNodes:                   3,
	GetEpochInfo:                      3,
	GetEpochSchedule:                  3,
	GetFeeForMessage:                  3,
	GetFirstAvailableBlock:            3,
	GetGenesisHash:                    3,
	GetHealth:                         3,
	GetHighestSnapshotSlot:            3,
	GetIdentity:                       3,
	GetInflationGovernor:              3,
	GetInflationRate:                  3,
	GetInflationReward:                3,
	GetLargestAccounts:                3,
	GetLatestBlockhash:                3,
	GetLeaderSchedule:                 3,
	GetMaxRetransmitSlot:              3,
	GetMaxShredInsertSlot:             3,
	GetMinimumBalanceForRentExemption: 3,
	GetMultipleAccounts:               3,
	GetProgramAccounts:                3,
	GetRecentPerformanceSamples:       3,
	GetRecentPrioritizationFees:       3,
	GetSignaturesForAddress:           3,
	GetSignatureStatuses:              3,
	GetSlot:                           3,
	GetSlotLeader:                     3,
	GetSlotLeaders:                    3,
	GetStakeActivation:                3,
	GetStakeMinimumDelegation:         3,
	GetSupply:                         3,
	GetTokenAccountBalance:            3,
	GetTokenAccountsByDelegate:        3,
	GetTokenAccountsByOwner:           3,
	GetTokenLargestAccounts:           3,
	GetTokenSupply:                    3,
	GetTransaction:                    3,
	GetTransactionCount:               3,
	GetVersion:                        3,
	GetVoteAccounts:                   3,
	IsBlockhashValid:                  3,
	MinimumLedgerSlot:                 3,
	RequestAirdrop:                    3,
	SendTransaction:                   3,
	SimulateTransaction:               3,

	// deprecated methods, but works now on solana mainnet
	GetConfirmedBlock:                 3,
	GetConfirmedBlocks:                3,
	GetConfirmedBlocksWithLimit:       3,
	GetConfirmedSignaturesForAddress2: 3,
	GetConfirmedTransaction:           3,
	GetFeeCalculatorForBlockhash:      3,
	GetFeeRateGovernor:                3,
	GetFees:                           3,
	GetRecentBlockhash:                3,
	GetSnapshotSlot:                   3,
}

// https://docs.solana.com/api/http
const (
	GetAccountInfo                    = "getAccountInfo"
	SendTransaction                   = "sendTransaction"
	GetSignaturesForAddress           = "getSignaturesForAddress"
	GetLatestBlockhash                = "getLatestBlockhash"
	GetSlot                           = "getSlot"
	GetTransaction                    = "getTransaction"
	GetInflationReward                = "getInflationReward"
	GetProgramAccounts                = "getProgramAccounts"
	GetSignatureStatuses              = "getSignatureStatuses"
	GetTokenAccountBalance            = "getTokenAccountBalance"
	GetMultipleAccounts               = "getMultipleAccounts"
	GetEpochInfo                      = "getEpochInfo"
	GetBalance                        = "getBalance"
	GetRecentPerformanceSamples       = "getRecentPerformanceSamples"
	GetVoteAccounts                   = "getVoteAccounts"
	GetInflationRate                  = "getInflationRate"
	GetSupply                         = "getSupply"
	GetBlockTime                      = "getBlockTime"
	GetBlockHeight                    = "getBlockHeight"
	GetMinimumBalanceForRentExemption = "getMinimumBalanceForRentExemption"
	IsBlockhashValid                  = "isBlockhashValid"
	GetTransactionCount               = "getTransactionCount"
	GetTokenAccountsByOwner           = "getTokenAccountsByOwner"
	GetBlock                          = "getBlock"
	GetBlocks                         = "getBlocks"
	GetBlocksWithLimit                = "getBlocksWithLimit"
	GetVersion                        = "getVersion"
	GetTokenLargestAccounts           = "getTokenLargestAccounts"
	GetBlockCommitment                = "getBlockCommitment"
	GetStakeActivation                = "getStakeActivation"
	GetTokenAccountsByDelegate        = "getTokenAccountsByDelegate"
	GetTokenSupply                    = "getTokenSupply"
	GetLeaderSchedule                 = "getLeaderSchedule"
	GetConfirmedBlock                 = "getConfirmedBlock"
	GetFirstAvailableBlock            = "getFirstAvailableBlock"
	GetIdentity                       = "getIdentity"
	GetConfirmedBlocks                = "getConfirmedBlocks"
	GetConfirmedSignaturesForAddress2 = "getConfirmedSignaturesForAddress2"
	GetConfirmedTransaction           = "getConfirmedTransaction"
	GetBlockProduction                = "getBlockProduction"
	GetClusterNodes                   = "getClusterNodes"
	GetEpochSchedule                  = "getEpochSchedule"
	GetFeeForMessage                  = "getFeeForMessage"
	GetGenesisHash                    = "getGenesisHash"
	GetHealth                         = "getHealth"
	GetHighestSnapshotSlot            = "getHighestSnapshotSlot"
	GetInflationGovernor              = "getInflationGovernor"
	GetLargestAccounts                = "getLargestAccounts"
	GetMaxRetransmitSlot              = "getMaxRetransmitSlot"
	GetMaxShredInsertSlot             = "getMaxShredInsertSlot"
	GetRecentPrioritizationFees       = "getRecentPrioritizationFees"
	GetSlotLeader                     = "getSlotLeader"
	GetSlotLeaders                    = "getSlotLeaders"
	GetStakeMinimumDelegation         = "getStakeMinimumDelegation"
	MinimumLedgerSlot                 = "minimumLedgerSlot"
	RequestAirdrop                    = "requestAirdrop"
	SimulateTransaction               = "simulateTransaction"
	GetConfirmedBlocksWithLimit       = "getConfirmedBlocksWithLimit"
	GetFeeCalculatorForBlockhash      = "getFeeCalculatorForBlockhash"
	GetFeeRateGovernor                = "getFeeRateGovernor"
	GetFees                           = "getFees"
	GetRecentBlockhash                = "getRecentBlockhash"
	GetSnapshotSlot                   = "getSnapshotSlot"
)

const (
	BlockCleanedUpErrCode                           = -32001
	SendTransactionPreflightFailureErrCode          = -32002
	TransactionSignatureVerificationFailureErrCode  = -32003
	BlockNotAvailableErrCode                        = -32004
	NodeUnhealthyErrCode                            = -32005
	TransactionPrecompileVerificationFailureErrCode = -32006
	SlotSkippedErrCode                              = -32007
	NoSnapshotErrCode                               = -32008
	LongTermStorageSlotSkippedErrCode               = -32009
	KeyExcludedFromSecondaryIndexErrCode            = -32010
	TransactionHistoryNotAvailableErrCode           = -32011
	ScanErrCode                                     = -32012
	TransactionSignatureLenMismatchErrCode          = -32013
	BlockStatusNotAvailableYetErrCode               = -32014
	UnsupportedTransactionVersionErrCode            = -32015
	MinContextSlotNotReachedErrCode                 = -32016
	ParseErrCode                                    = -32700
	InvalidRequestErrCode                           = -32600
	MethodNotFoundErrCode                           = -32601
	InvalidParamsErrCode                            = -32602
	InternalErrorErrCode                            = -32603
)

func GetFirstAvailableSlot(message string) (uint64, error) {
	strArray := strings.Split(message, ": ")
	if len(strArray) == 0 {
		return 0, errors.New("invalid BlockCleanedUpMsg")
	}

	return strconv.ParseUint(strArray[len(strArray)-1], 10, 64)
}

func TxRelatedMethod(method string) bool {
	return method == GetTransaction || method == GetLeaderSchedule ||
		method == GetSignaturesForAddress || method == GetSignatureStatuses
}

func BlockRelatedMethod(method string) bool {
	return method == GetBlock || method == GetBlockTime || method == GetBlockCommitment || method == GetConfirmedBlock
}
