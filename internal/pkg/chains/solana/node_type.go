package solana

import (
	"fmt"
)

type NodeType struct {
	Name                  string
	AvailableSlotsHistory int64
}

const (
	// supports all accounts related methods and sendTransaction
	basicSolanaNode = "basic_node"
	// supports all accounts related methods, sendTransaction
	// also supports transaction and blocks related methods with
	extendedSolanaNode = "extended_node"
	ArchiveSolanaNode  = "archive_node"
	// support all DAS-API methods
	//fullDasAPINode     = ""
	//basicWebsocketNode = ""
	//fullWebsocketNode  = ""
)

func (n NodeType) IsSupportMethod(method string) (bool, error) {
	switch method {
	case GetAccountInfo, GetBalance, GetClusterNodes, GetEpochInfo, GetEpochSchedule, GetFeeForMessage, GetGenesisHash, GetHealth, GetHighestSnapshotSlot, GetIdentity,
		GetInflationGovernor, GetInflationRate, GetInflationReward, GetLargestAccounts, GetLatestBlockhash, GetMaxRetransmitSlot, GetMaxShredInsertSlot,
		GetMinimumBalanceForRentExemption, GetMultipleAccounts, GetRecentPerformanceSamples, GetRecentPrioritizationFees, GetSlot, GetSlotLeader, GetSlotLeaders,
		GetStakeActivation, GetStakeMinimumDelegation, GetTokenAccountBalance, GetTokenAccountsByDelegate, GetTokenAccountsByOwner, GetTokenLargestAccounts, GetVersion,
		GetVoteAccounts, MinimumLedgerSlot, RequestAirdrop, SendTransaction, SimulateTransaction, GetFeeCalculatorForBlockhash, GetFeeRateGovernor, GetRecentBlockhash,
		GetFees, GetSnapshotSlot:
		return n.Name == basicSolanaNode || n.Name == extendedSolanaNode || n.Name == ArchiveSolanaNode, nil
	case GetBlock, GetBlockHeight, GetBlockProduction, GetBlockCommitment, GetBlocks, GetBlocksWithLimit, GetBlockTime, GetFirstAvailableBlock, GetProgramAccounts,
		GetSignaturesForAddress, GetSignatureStatuses, GetSupply, GetTokenSupply, GetTransaction, GetTransactionCount, GetConfirmedBlock, GetConfirmedBlocks,
		GetConfirmedBlocksWithLimit, GetConfirmedSignaturesForAddress2, GetConfirmedTransaction, GetLeaderSchedule:
		return n.Name == extendedSolanaNode || n.Name == ArchiveSolanaNode, nil
	default:
		return false, fmt.Errorf("invalid requested method: %s", method)
	}
}
