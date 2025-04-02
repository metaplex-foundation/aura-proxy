package solana

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adm-metaex/aura-api/pkg/types"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"aura-proxy/internal/pkg/util"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// --- Mocks ---

// Mock rpcTransport
type mockRPCTransport struct {
	respBody      []byte
	statusCode    int
	err           error
	canHandleFn   func(methods []string) bool
	isAvailableFn func() bool
}

func (m *mockRPCTransport) SendRequest(c *echoUtil.CustomContext) ([]byte, int, error) {
	return m.respBody, m.statusCode, m.err
}

func (m *mockRPCTransport) canHandle(methods []string) bool {
	if m.canHandleFn != nil {
		return m.canHandleFn(methods)
	}
	return true // Default mock behavior
}

func (m *mockRPCTransport) isAvailable() bool {
	if m.isAvailableFn != nil {
		return m.isAvailableFn()
	}
	return true // Default mock behavior
}

// Mock wsProxyTransport
type mockWSProxyTransport struct {
	err error
}

func (m *mockWSProxyTransport) DefaultProxyWS(c echo.Context) error {
	return m.err
}

// --- Helpers ---

func setupTestAdapter(rpcMock *mockRPCTransport, wsMock *mockWSProxyTransport) *Adapter {
	adapter := &Adapter{
		rpcTransport: rpcMock,
		wsTransport:  wsMock,
		chainName:    "test-chain",
		hostNames:    []string{"test.host"},
	}
	return adapter
}

func setupEchoContext(method string, path string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	// Use buffer for request body if needed (POST)
	var reqBody *bytes.Buffer
	if method == http.MethodPost {
		reqBody = bytes.NewBufferString(`{"jsonrpc":"2.0","method":"testMethod","id":1}`)
	} else {
		reqBody = bytes.NewBuffer(nil) // GET has no body
	}
	req := httptest.NewRequest(method, path, reqBody)
	if method == http.MethodPost {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	// Wrap context for POST requests that use it
	if method == http.MethodPost {
		// Instantiate CustomContext directly, embedding the standard context
		customCtx := &echoUtil.CustomContext{Context: ctx}
		// Manually set fields usually handled by middleware/request processing
		customCtx.SetReqBody(reqBody.Bytes())
		customCtx.SetReqMethods([]string{"testMethod"})
		// customCtx.InitMetrics() // Optionally init other fields if needed by tested code
		return customCtx, rec
	}

	return ctx, rec
}

// --- Tests ---

func TestAdapter_ProxyPostRequest_Sanitization(t *testing.T) {
	tests := []struct {
		name                 string
		mockErr              error
		expectedStatusCode   int
		expectedBodyContains string // Check if body contains generic error msg
		expectedErrPayload   any    // Expected string or specific struct in HTTPError.Message
		transportAvailable   bool
		transportCanHandle   bool
		transportNil         bool
	}{
		{
			name:               "No error",
			mockErr:            nil,
			expectedStatusCode: http.StatusOK,
			transportAvailable: true,
			transportCanHandle: true,
		},
		{
			name:               "Generic error",
			mockErr:            errors.New("something went wrong"),
			expectedStatusCode: http.StatusInternalServerError,
			expectedErrPayload: "Internal server error",
			transportAvailable: true,
			transportCanHandle: true,
		},
		{
			name:               "Error with sensitive info",
			mockErr:            fmt.Errorf("connection refused to downstream.provider.com: %w", errors.New("network error")),
			expectedStatusCode: http.StatusInternalServerError,
			expectedErrPayload: "Internal server error",
			transportAvailable: true,
			transportCanHandle: true,
		},
		{
			name:               "Context Canceled",
			mockErr:            context.Canceled,
			expectedStatusCode: 499, // Custom code for client closed request
			expectedErrPayload: "Client closed request",
			transportAvailable: true,
			transportCanHandle: true,
		},
		{
			name:               "Context Deadline Exceeded",
			mockErr:            context.DeadlineExceeded,
			expectedStatusCode: http.StatusGatewayTimeout,
			expectedErrPayload: "Gateway timeout",
			transportAvailable: true,
			transportCanHandle: true,
		},
		{
			name:               "4xx HTTPError",
			mockErr:            echo.NewHTTPError(http.StatusBadRequest, "Invalid request parameter"),
			expectedStatusCode: http.StatusBadRequest,
			expectedErrPayload: "Invalid request parameter", // Should pass through
			transportAvailable: true,
			transportCanHandle: true,
		},
		{
			name:               "5xx HTTPError",
			mockErr:            echo.NewHTTPError(http.StatusServiceUnavailable, "Underlying service failed"),
			expectedStatusCode: http.StatusServiceUnavailable,
			expectedErrPayload: "Internal server error", // Should be sanitized
			transportAvailable: true,
			transportCanHandle: true,
		},
		{
			name:               "Transport Unavailable",
			mockErr:            nil, // Error comes from adapter check
			expectedStatusCode: http.StatusServiceUnavailable,
			expectedErrPayload: util.ExtraNodeNoAvailableTargetsErrorResponse, // Expect the struct
			transportAvailable: false,
			transportCanHandle: true,
		},
		{
			name:               "Transport Cannot Handle",
			mockErr:            nil, // Error comes from adapter check
			expectedStatusCode: http.StatusServiceUnavailable,
			expectedErrPayload: util.ExtraNodeNoAvailableTargetsErrorResponse, // Expect the struct
			transportAvailable: true,
			transportCanHandle: false,
		},
		{
			name:               "Transport Nil",
			mockErr:            nil, // Error comes from adapter check
			expectedStatusCode: http.StatusInternalServerError,
			expectedErrPayload: "RPC transport not available", // Specific message for this case
			transportNil:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rpcMock := &mockRPCTransport{
				err:        tc.mockErr,
				statusCode: http.StatusOK, // Assume OK unless error happens
				isAvailableFn: func() bool {
					return tc.transportAvailable
				},
				canHandleFn: func(methods []string) bool {
					return tc.transportCanHandle
				},
			}

			adapter := setupTestAdapter(rpcMock, nil)
			if tc.transportNil {
				adapter.rpcTransport = nil
			}

			ctx, _ := setupEchoContext(http.MethodPost, "/")
			customCtx, ok := ctx.(*echoUtil.CustomContext)
			require.True(t, ok, "Context should be CustomContext")

			_, resCode, err := adapter.ProxyPostRequest(customCtx)

			// Check returned error and status code based on expectation
			expectError := !tc.transportAvailable || !tc.transportCanHandle || tc.transportNil || tc.mockErr != nil

			if expectError {
				require.Error(t, err, "Expected an error but got nil")

				var httpErr *echo.HTTPError
				require.ErrorAs(t, err, &httpErr, "Expected error to be an *echo.HTTPError")

				assert.Equal(t, tc.expectedStatusCode, httpErr.Code, "Status code mismatch in error")

				// Check payload using type switch
				switch expectedPayload := tc.expectedErrPayload.(type) {
				case string:
					actualMessage, ok := httpErr.Message.(string)
					require.True(t, ok, "Expected HTTPError.Message to be a string")
					assert.Contains(t, actualMessage, expectedPayload, "Error message mismatch (string check)")
				case *types.RPCResponse: // Assuming this is the type of ExtraNodeNoAvailableTargetsErrorResponse
					assert.Equal(t, expectedPayload, httpErr.Message, "Error payload mismatch (struct check)")
				case nil:
					// No payload check needed if nil expected (e.g., maybe for some specific future error type)
				default:
					t.Fatalf("Unhandled expected error payload type: %T", expectedPayload)
				}
			} else {
				assert.NoError(t, err, "Expected no error but got one")
				assert.Equal(t, tc.expectedStatusCode, resCode, "Status code mismatch on success")
			}
		})
	}
}

func TestAdapter_ProxyWSRequest_Sanitization(t *testing.T) {
	tests := []struct {
		name               string
		mockErr            error
		expectedErrMessage string // Expected message in the returned error
		transportNil       bool
	}{
		{
			name: "No error",
		},
		{
			name:               "Generic error",
			mockErr:            errors.New("something went wrong in WS"),
			expectedErrMessage: "Internal server error",
		},
		{
			name:               "Error with sensitive info",
			mockErr:            fmt.Errorf("ws connection refused to ws.downstream.provider.com: %w", errors.New("network error")),
			expectedErrMessage: "Internal server error",
		},
		{
			name:               "Context Canceled",
			mockErr:            context.Canceled,
			expectedErrMessage: "Client closed request",
		},
		{
			name:               "Context Deadline Exceeded",
			mockErr:            context.DeadlineExceeded,
			expectedErrMessage: "Gateway timeout",
		},
		{
			name:               "4xx HTTPError",
			mockErr:            echo.NewHTTPError(http.StatusUnauthorized, "Authentication required"),
			expectedErrMessage: "Authentication required", // Should pass through
		},
		{
			name:               "5xx HTTPError",
			mockErr:            echo.NewHTTPError(http.StatusInternalServerError, "WS handler crashed"),
			expectedErrMessage: "Internal server error", // Should be sanitized
		},
		{
			name:               "Transport Nil",
			mockErr:            nil, // Error comes from adapter check
			expectedErrMessage: "Internal server error",
			transportNil:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wsMock := &mockWSProxyTransport{
				err: tc.mockErr,
			}

			adapter := setupTestAdapter(nil, wsMock)
			if tc.transportNil {
				adapter.wsTransport = nil
			}

			ctx, _ := setupEchoContext(http.MethodGet, "/ws") // WS is typically GET

			err := adapter.ProxyWSRequest(ctx)

			if tc.mockErr != nil || tc.transportNil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrMessage, "Error message mismatch")

				// Also check that the original sensitive error message is NOT present
				if tc.name == "Error with sensitive info" {
					assert.NotContains(t, err.Error(), "ws.downstream.provider.com", "Sanitized error should not contain sensitive info")
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
