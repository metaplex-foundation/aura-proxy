package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/adm-metaex/aura-api/pkg/types"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/metrics"
	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

var (
	ErrFailToReadBody     = errors.New("fail to read body")
	ErrInvalidContentType = errors.New("supplied content type is not allowed. Content-Type: application/json is required")
)
var (
	MethodNotFoundRPCError = types.NewRPCError(solana.MethodNotFoundErrCode, "Method not found", nil)
	ParseErrorResponse     = types.NewRPCErrorResponse(types.ParseError, nil)
)

func MakeHTTPRequest(c *echoUtil.CustomContext, httpClient *http.Client, reqType, targetURL string, skipErrHandling bool) ([]byte, int, error) { //nolint:gocritic
	if reqType != http.MethodPost && reqType != http.MethodGet {
		return nil, http.StatusInternalServerError, fmt.Errorf("unknown request type: %s", reqType)
	}

	body := io.Reader(http.NoBody)
	if reqType == echo.POST {
		body = c.GetReqBody()
	}
	builtReq, err := http.NewRequestWithContext(c.Request().Context(), reqType, targetURL, body)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("NewRequest: %s", err)
	}

	// Set headers
	setProxyHeaders(c, builtReq)

	var buf bytes.Buffer
	startTime := time.Now()
	resp, err := httpClient.Do(builtReq)
	metrics.ObserveExternalRequests(c.GetChainName(), builtReq.Host, c.GetReqMethod(), err == nil, time.Since(startTime))
	if skipErrHandling {
		if resp != nil {
			_, _ = io.Copy(&buf, resp.Body) // ignore err
			resp.Body.Close()               //nolint:revive

			return buf.Bytes(), resp.StatusCode, nil
		}

		return buf.Bytes(), http.StatusInternalServerError, err
	}
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("do: %w", err)
	}
	if resp == nil {
		return nil, http.StatusInternalServerError, errors.New("resp == nil")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, resp.StatusCode, util.ErrBadStatusCode
	}

	_, err = io.Copy(&buf, resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("copy: %s", err)
	}

	return buf.Bytes(), resp.StatusCode, nil
}

func setProxyHeaders(c echo.Context, req *http.Request) {
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	// Fix header
	// Basically it's not good practice to unconditionally pass incoming x-real-ip header to upstream.
	// However, for backward compatibility, legacy behavior is preserved unless you configure Echo#IPExtractor.
	realIP := c.RealIP()
	if req.Header.Get(echo.HeaderXRealIP) == "" || c.Echo().IPExtractor != nil {
		req.Header.Set(echo.HeaderXRealIP, realIP)
	}
	if req.Header.Get(echo.HeaderXForwardedProto) == "" {
		req.Header.Set(echo.HeaderXForwardedProto, c.Scheme())
	}

	if realIP == "" {
		return
	}

	// If we aren't the first proxy retain prior
	// X-Forwarded-For information as a comma+space
	// separated list and fold multiple headers into one.
	if forwardedFor := req.Header.Values(echo.HeaderXForwardedFor); len(forwardedFor) > 0 {
		req.Header.Set(echo.HeaderXForwardedFor, strings.Join(append(forwardedFor, realIP), ", "))
	}
}

// StatusCodeContextCanceled is a custom HTTP status code for situations
// where a client unexpectedly closed the connection to the server.
// As there is no standard error code for "client closed connection", but
// various well-known HTTP clients and server implement this HTTP code we use
// 499 too instead of the more problematic 5xx, which does not allow to detect this situation
const StatusCodeContextCanceled = 499

func HandleError(err error) error {
	// If the client canceled the request (usually by closing the connection), we can report a
	// client error (4xx) instead of a server error (5xx) to correctly identify the situation.
	// The Go standard library (at of late 2020) wraps the exported, standard
	// context.Canceled error with unexported garbage value requiring a substring check, see
	// https://github.com/golang/go/blob/6965b01ea248cabb70c3749fd218b36089a21efb/src/net/net.go#L416-L430
	if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "operation was canceled") {
		return echo.NewHTTPError(StatusCodeContextCanceled, fmt.Sprintf("client closed connection: %v", err))
	} else if httErr, ok := err.(*echo.HTTPError); ok { //nolint:errorlint
		return httErr // return not changed err for user
	}

	return echo.NewHTTPError(http.StatusInternalServerError).WithInternal(err)
}

func PrepareGetRequest(c *echoUtil.CustomContext, chainName string) {
	c.SetChainName(chainName)
}
func PreparePostRequest(c *echoUtil.CustomContext, chainName string) error {
	cp := util.NewRuntimeCheckpoint("PreparePostRequest")
	defer c.GetMetrics().AddCheckpoint(cp)

	// validation
	if !echoUtil.IsContentTypeValid(c.Request().Header.Get(echo.HeaderContentType)) {
		c.SetProxyUserError(true)
		return ErrInvalidContentType
	}

	c.SetChainName(chainName)

	// get & save body
	reqBody, err := getRequestBody(c.Request().Body)
	if err != nil {
		c.SetRPCErrors([]int{ParseErrorResponse.Error.Code})
		c.SetProxyUserError(true)
		return echo.NewHTTPError(http.StatusOK, ParseErrorResponse)
	}
	c.SetReqBody(reqBody)

	return nil
}
func getRequestBody(body io.ReadCloser) (res []byte, err error) {
	if body == nil {
		return nil, ErrFailToReadBody
	}
	res, err = io.ReadAll(body)
	if err != nil {
		return nil, ErrFailToReadBody
	}
	body.Close() //nolint:revive

	res = bytes.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, res)
	if len(res) == 0 {
		return nil, ErrFailToReadBody
	}

	return res, nil
}

func ParseJSONRPCRequestBody(getReqBody func() *bytes.Reader, methodList map[string]uint, skipMethodCheckErr bool) (parsedReqs types.RPCRequests, arrayRequested bool, rpcErr *types.RPCResponse) {
	reqBody := getReqBody()
	if reqBody == nil {
		log.Logger.Proxy.Errorf("ParseJSONRPCRequestBody: internal reqBody == nil")
		return parsedReqs, arrayRequested, ParseErrorResponse
	}
	fs, err := reqBody.ReadByte()
	if err != nil {
		return parsedReqs, arrayRequested, ParseErrorResponse
	}

	switch fs {
	case '{':
		parsedJSON := types.RPCRequest{}
		decoder := util.NewJSONDecoder(getReqBody(), true)
		err := decoder.Decode(&parsedJSON)
		if err != nil {
			return parsedReqs, arrayRequested, ParseErrorResponse
		}
		rpcErr := checkJSONRPCBody(parsedJSON, methodList, skipMethodCheckErr)
		if rpcErr != nil {
			return parsedReqs, arrayRequested, types.NewRPCErrorResponse(rpcErr, parsedJSON.ID)
		}

		parsedReqs = append(parsedReqs, &parsedJSON)
	case '[':
		arrayRequested = true
		decoder := util.NewJSONDecoder(getReqBody(), true)
		err := decoder.Decode(&parsedReqs)
		if err != nil {
			return parsedReqs, arrayRequested, ParseErrorResponse
		}

		for _, r := range parsedReqs {
			if r == nil {
				return parsedReqs, arrayRequested, ParseErrorResponse
			}

			rpcErr := checkJSONRPCBody(*r, methodList, skipMethodCheckErr)
			if rpcErr != nil {
				return parsedReqs, arrayRequested, types.NewRPCErrorResponse(rpcErr, r.ID)
			}
		}
	default:
		return parsedReqs, arrayRequested, ParseErrorResponse
	}

	return
}
func checkJSONRPCBody(req types.RPCRequest, methodList map[string]uint, skipMethodCheckErr bool) *jsonrpc.RPCError {
	if req.JSONRPC != types.JSONRPCVersion {
		return types.InvalidReqError
	}
	if !skipMethodCheckErr {
		_, ok := methodList[req.Method]
		if !ok {
			return MethodNotFoundRPCError
		}
	}

	return nil
}
