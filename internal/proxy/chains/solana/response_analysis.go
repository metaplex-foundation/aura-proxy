package solana

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/util"
	"aura-proxy/internal/pkg/util/echo"
)

const (
	jsonrpcField = "jsonrpc"
	errorField   = "error"
	codeField    = "code"
	resultField  = "result"
	messageField = "message"
)

var EmptyResponse = []byte("null")

func rpcErrorAnalysis(errs []error) (firstSlotOnNode int64, invalidReqErr bool, analyzeErr *AnalyzeError, err error) {
	if len(errs) == 0 {
		return
	}

	var joinedErr string
	for _, e := range errs {
		var rpcErr *jsonrpc.RPCError
		if !errors.As(e, &rpcErr) {
			joinedErr = fmt.Sprintf("%s%s; ", joinedErr, e.Error())
			continue
		}

		if rpcErr.Code == solana.BlockCleanedUpErrCode {
			slot, parseErr := solana.GetFirstAvailableSlot(rpcErr.Message)
			if parseErr == nil {
				firstSlotOnNode = int64(slot)
			}
		}

		method, _ := rpcErr.Data.(string) // assigned rpcMethod in decodeNodeResponse()

		// LongTermStorageSlotSkippedErrCode - https://support.quicknode.com/hc/en-us/articles/5793700679441-Why-are-slots-blocks-missing-on-Solana-
		switch rpcErr.Code {
		case solana.SendTransactionPreflightFailureErrCode, solana.TransactionSignatureVerificationFailureErrCode,
			solana.TransactionPrecompileVerificationFailureErrCode, solana.TransactionSignatureLenMismatchErrCode,
			solana.UnsupportedTransactionVersionErrCode, solana.ParseErrCode, solana.InvalidRequestErrCode,
			solana.InvalidParamsErrCode, solana.SlotSkippedErrCode, solana.LongTermStorageSlotSkippedErrCode,
			solana.BlockNotAvailableErrCode, solana.BlockStatusNotAvailableYetErrCode:
			if rpcErr.Code == solana.InvalidParamsErrCode &&
				(strings.Contains(rpcErr.Message, "BigTable query failed (maybe timeout due to too large range") ||
					strings.Contains(rpcErr.Message, "blockstore error")) {
				break
			}

			invalidReqErr = true

			continue
		}

		if isMethodNotAvailableByErrCode(rpcErr.Code) {
			if analyzeErr == nil {
				analyzeErr = NewAnalyzeError(ErrMethodNotAvailable, method)
			} else {
				analyzeErr.AddToPayload(method)
			}

			continue
		}
		// TODO: maybe jail all methods in this case
		// if rpcErr.Code == solana.NodeUnhealthyErrCode {}

		joinedErr = fmt.Sprintf("%srpcErr: code %d %s; ", joinedErr, rpcErr.Code, rpcErr.Message)
	}

	if joinedErr != "" { // joinedErr need just to log
		err = errors.New(joinedErr)
	}

	return
}
func isMethodNotAvailableByErrCode(code int) bool {
	return code == solana.MethodNotFoundErrCode || code == solana.TransactionHistoryNotAvailableErrCode
}
func decodeNodeResponse(c *echo.CustomContext, body []byte) (errs []error) {
	// clean possible old value
	c.SetRPCErrors(nil)

	if len(body) == 0 {
		return []error{errors.New("empty body")}
	}

	cp := util.NewRuntimeCheckpoint("decodeNodeResponse")
	defer c.GetMetrics().AddCheckpoint(cp)
	var errCodes []int
	switch fs := body[0]; {
	case fs == '{':
		errCode, err := checkRPCResp(body, c.GetReqMethod())
		if err != nil {
			errs = append(errs, err)
		}
		if errCode != 0 {
			errCodes = append(errCodes, errCode)
		}
	case fs == '[':
		var (
			rpcMethods = c.GetReqMethods()
			curIdx     int
		)
		_, err := jsonparser.ArrayEach(body, func(value []byte, _ jsonparser.ValueType, _ int, _ error) {
			if curIdx >= len(rpcMethods) {
				errs = append(errs, fmt.Errorf("validation: ArrayEach: index not found %d for %v", curIdx, rpcMethods))
				return
			}

			errorCode, err := checkRPCResp(value, rpcMethods[curIdx])
			if err != nil {
				errs = append(errs, err)
			}
			if errorCode != 0 {
				errCodes = append(errCodes, errorCode)
			}

			curIdx++
		})
		if err != nil {
			errs = append(errs, err)
		}
	default:
		return append(errs, fmt.Errorf("invalid json first symbol: %s", string(fs)))
	}

	if len(errCodes) != 0 {
		c.SetRPCErrors(errCodes)
	}
	if len(errs) != 0 {
		return errs
	}

	return nil
}
func checkRPCResp(body []byte, reqMethod string) (int, error) {
	if res, _, _, _ := jsonparser.Get(body, jsonrpcField); len(res) == 0 {
		return 0, ErrEmptyResponseBody
	}

	if errObj, _, _, _ := jsonparser.Get(body, errorField); len(errObj) != 0 {
		if errCode, _ := jsonparser.GetInt(errObj, codeField); errCode != 0 {
			message, _ := jsonparser.GetString(errObj, messageField)
			return int(errCode), &jsonrpc.RPCError{
				Code:    int(errCode),
				Message: message,
				Data:    reqMethod,
			}
		}
	}

	if reqMethod == solana.GetBlock {
		res, _, _, _ := jsonparser.Get(body, resultField)
		if bytes.Equal(res, EmptyResponse) {
			return 0, ErrEmptyResponseField
		}
	}

	return 0, nil
}
