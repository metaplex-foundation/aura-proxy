package solana

import (
	"encoding/json"

	"github.com/adm-metaex/aura-api/pkg/types"

	solanaTypes "aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/transport"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

var BlockMethodsParamsRPCErr = types.NewRPCError(solanaTypes.InvalidParamsErrCode, "`params` should have at least 1 argument(s)", nil)

func (*solanaAdapter) PrepareGetReq(*echoUtil.CustomContext) {}
func (s *solanaAdapter) PreparePostReq(c *echoUtil.CustomContext) *types.RPCResponse {
	cp := util.NewRuntimeCheckpoint("solanaAdapter.PreparePostReq")

	parsedReqs, arrayRequested, rpcErrResponse := transport.ParseJSONRPCRequestBody(c.GetReqBody, s.GetAvailableMethods(), false)
	if rpcErrResponse != nil {
		c.SetRPCErrors([]int{rpcErrResponse.Error.Code})
		c.SetProxyUserError(true)
		return rpcErrResponse
	}

	block, rpcErrResponse := blockMethodsValidation(parsedReqs)
	if rpcErrResponse != nil {
		c.SetRPCErrors([]int{rpcErrResponse.Error.Code})
		c.SetProxyUserError(true)
		return rpcErrResponse
	}

	c.SetReqBlock(block)
	c.SetArrayRequested(arrayRequested)
	c.SetRPCRequestsParsed(parsedReqs)
	c.SetReqMethods(util.Map(parsedReqs, func(r *types.RPCRequest) string { return r.Method }))
	c.SetStatsAdditionalData(getContextValueForRequest(c.GetReqMethod(), parsedReqs))

	m := c.GetMetrics()
	m.SetTitle(c.GetReqMethod())
	m.AddCheckpoint(cp)

	return nil
}

func getContextValueForRequest(rpcMethod string, parsedRequests types.RPCRequests) (res string) {
	if rpcMethod == "" || rpcMethod == echoUtil.MultipleValuesRequested || len(parsedRequests) == 0 || parsedRequests[0] == nil {
		return
	}

	switch parsedRequests[0].Method {
	case solanaTypes.GetSignaturesForAddress, solanaTypes.GetTokenAccountsByOwner, solanaTypes.GetAccountInfo, solanaTypes.GetProgramAccounts,
		solanaTypes.GetStakeActivation, solanaTypes.GetTokenAccountBalance, solanaTypes.GetTokenAccountsByDelegate,
		solanaTypes.GetTokenLargestAccounts, solanaTypes.GetTokenSupply, solanaTypes.IsBlockhashValid, solanaTypes.GetTransaction,
		solanaTypes.GetBalance:
		if paramsArr, ok := parsedRequests[0].Params.([]interface{}); ok && len(paramsArr) > 0 {
			res, _ = paramsArr[0].(string)
		}
	case solanaTypes.GetBlock, solanaTypes.GetBlocks, solanaTypes.GetBlockCommitment, solanaTypes.GetBlocksWithLimit, solanaTypes.GetBlockTime:
		if paramsArr, ok := parsedRequests[0].Params.([]interface{}); ok && len(paramsArr) > 0 {
			resNumber, _ := paramsArr[0].(json.Number)
			res = resNumber.String()
		}
	}

	return
}

func blockMethodsValidation(parsedReqs types.RPCRequests) (int64, *types.RPCResponse) {
	var block int64

	for _, req := range parsedReqs {
		if !solanaTypes.BlockRelatedMethod(req.Method) {
			continue
		}

		paramsArr, ok := req.Params.([]interface{})
		if !ok {
			return block, types.NewRPCErrorResponse(types.InvalidReqError, req.ID)
		}
		if len(paramsArr) == 0 {
			return block, types.NewRPCErrorResponse(BlockMethodsParamsRPCErr, req.ID)
		}

		resNumber, ok := paramsArr[0].(json.Number)
		if !ok {
			return block, types.NewRPCErrorResponse(BlockMethodsParamsRPCErr, req.ID)
		}
		resInt, err := resNumber.Int64()
		if err != nil {
			return block, types.NewRPCErrorResponse(BlockMethodsParamsRPCErr, req.ID)
		}

		if block == 0 || block > resInt {
			block = resInt // find smaller
		}
	}

	return block, nil
}
