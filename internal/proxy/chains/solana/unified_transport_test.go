package solana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// Helper function to properly initialize the CustomContext with metrics
// to avoid the nil pointer dereference in decodeNodeResponse
func createTestCustomContext(request *http.Request, response http.ResponseWriter, methods []string, body []byte) *echoUtil.CustomContext {
	e := echo.New()
	c := &echoUtil.CustomContext{Context: e.NewContext(request, response)}
	c.InitMetrics() // Initialize metrics to prevent nil pointer dereference
	c.SetReqMethods(methods)
	c.SetReqBody(body)
	return c
}

// TestUnifiedTransport_SendRequest tests the basic send request functionality
func TestUnifiedTransport_SendRequest(t *testing.T) {
	// Create a JSON-RPC request with a standard method
	requestJSON := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "testMethod",
		"id":      1,
	}
	requestBytes, _ := json.Marshal(requestJSON)

	// Create a test context
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(requestBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := createTestCustomContext(req, rec, []string{"testMethod"}, requestBytes)

	// --- Test successful request ---
	mockSelector := &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: &ProxyTarget{url: "target1"}, Index: 0, Error: nil},
		},
		TargetsCount:  1,
		IsAvailableFn: func() bool { return true },
	}

	mockRouter := &MethodRouterWrapper{
		mockSelector: mockSelector,
	}

	// Create a valid response in JSON-RPC format
	validResponseJSON := map[string]interface{}{
		"jsonrpc": "2.0",
		"result":  map[string]string{"status": "success"},
		"id":      1,
	}
	validResponseBytes, _ := json.Marshal(validResponseJSON)

	mockRequester := &MockHTTPRequesterWrapper{
		Responses: []HTTPResponseWrapper{
			{RespBody: validResponseBytes, StatusCode: http.StatusOK, Error: nil},
		},
	}

	transport := NewUnifiedTransport("test_transport", mockRouter, mockRequester, 3, false)

	body, code, err := transport.SendRequest(c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, code)
	}
	if !bytes.Equal(body, validResponseBytes) {
		t.Errorf("Expected body '%s', got '%s'", validResponseBytes, body)
	}
	if mockSelector.CallCount != 1 {
		t.Errorf("Expected TargetSelector.GetNext to be called once, got %d", mockSelector.CallCount)
	}
	if mockRequester.CallCount != 1 {
		t.Errorf("Expected HTTPRequester.DoRequest to be called once, got %d", mockRequester.CallCount)
	}

	// --- Test request failure and retry ---
	mockSelector = &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: &ProxyTarget{url: "target1"}, Index: 0, Error: nil},
			{Target: &ProxyTarget{url: "target2"}, Index: 1, Error: nil},
		},
		TargetsCount:  2,
		IsAvailableFn: func() bool { return true },
	}

	mockRouter = &MethodRouterWrapper{
		mockSelector: mockSelector,
	}

	mockRequester = &MockHTTPRequesterWrapper{
		Responses: []HTTPResponseWrapper{
			{RespBody: nil, StatusCode: 0, Error: errors.New("connection refused")}, // Simulate a failure
			{RespBody: validResponseBytes, StatusCode: http.StatusOK, Error: nil},   // Successful retry
		},
	}

	// Create a new context for the retry test
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(requestBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = createTestCustomContext(req, rec, []string{"testMethod"}, requestBytes)

	transport = NewUnifiedTransport("test_transport", mockRouter, mockRequester, 3, false)

	body, code, err = transport.SendRequest(c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, code)
	}
	if !bytes.Equal(body, validResponseBytes) {
		t.Errorf("Expected body '%s', got '%s'", validResponseBytes, body)
	}
	if mockSelector.CallCount != 2 {
		t.Errorf("Expected TargetSelector.GetNext to be called twice, got %d", mockSelector.CallCount)
	}
	if mockRequester.CallCount != 2 {
		t.Errorf("Expected HTTPRequester.DoRequest to be called twice, got %d", mockRequester.CallCount)
	}

	// --- Test all targets failing ---
	mockSelector = &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: &ProxyTarget{url: "target1"}, Index: 0, Error: nil},
			{Target: &ProxyTarget{url: "target2"}, Index: 1, Error: nil},
		},
		TargetsCount:  2,
		IsAvailableFn: func() bool { return true },
	}

	mockRouter = &MethodRouterWrapper{
		mockSelector: mockSelector,
	}

	mockRequester = &MockHTTPRequesterWrapper{
		Responses: []HTTPResponseWrapper{
			{RespBody: nil, StatusCode: 0, Error: errors.New("connection refused")},
			{RespBody: nil, StatusCode: 0, Error: errors.New("connection refused")},
		},
	}

	// Create a new context for the all-failure test
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(requestBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = createTestCustomContext(req, rec, []string{"testMethod"}, requestBytes)

	transport = NewUnifiedTransport("test_transport", mockRouter, mockRequester, 3, false)

	_, code, err = transport.SendRequest(c)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	// UnifiedTransport may return different status code than CNFT
	if mockSelector.CallCount != 2 {
		t.Errorf("Expected TargetSelector.GetNext to be called twice, got %d", mockSelector.CallCount)
	}
	if mockRequester.CallCount != 2 {
		t.Errorf("Expected HTTPRequester.DoRequest to be called twice, got %d", mockRequester.CallCount)
	}

	// --- Test no available targets ---
	mockSelector = &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: nil, Index: 0, Error: errors.New("no available targets")},
		},
		TargetsCount:  0,
		IsAvailableFn: func() bool { return false },
	}

	mockRouter = &MethodRouterWrapper{
		mockSelector:  mockSelector,
		isAvailableFn: func() bool { return false },
	}

	mockRequester = &MockHTTPRequesterWrapper{
		Responses: []HTTPResponseWrapper{},
	}

	// Create a new context for the no-targets test
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(requestBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = createTestCustomContext(req, rec, []string{"testMethod"}, requestBytes)

	transport = NewUnifiedTransport("test_transport", mockRouter, mockRequester, 3, false)

	_, code, err = transport.SendRequest(c)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	if code != http.StatusServiceUnavailable {
		t.Errorf("Expected status code %d, got %d", http.StatusServiceUnavailable, code)
	}
}

// TestUnifiedTransport_DAS_Fast_Path tests the DAS method shortcut functionality
func TestUnifiedTransport_DAS_Fast_Path(t *testing.T) {
	// Create a test context with a DAS method
	requestMethod := "getAsset" // This is a DAS method
	requestJSON := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  requestMethod,
		"id":      1,
	}
	requestBytes, _ := json.Marshal(requestJSON)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(requestBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := createTestCustomContext(req, rec, []string{requestMethod}, requestBytes)

	// --- Test DAS method fast path ---
	mockSelector := &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: &ProxyTarget{url: "target1", provider: "test_provider"}, Index: 0, Error: nil},
		},
		TargetsCount:  1,
		IsAvailableFn: func() bool { return true },
	}

	mockRouter := &MethodRouterWrapper{
		mockSelector: mockSelector,
	}

	responseJSON := map[string]interface{}{
		"jsonrpc": "2.0",
		"result":  map[string]interface{}{"id": "asset123"},
		"id":      1,
	}
	responseBytes, _ := json.Marshal(responseJSON)

	mockRequester := &MockHTTPRequesterWrapper{
		Responses: []HTTPResponseWrapper{
			{RespBody: responseBytes, StatusCode: http.StatusOK, Error: nil},
		},
	}

	transport := NewUnifiedTransport("test_transport", mockRouter, mockRequester, 3, true)

	// Execute the request with a DAS method
	body, code, err := transport.SendRequest(c)

	// Verify that the test passed
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, code)
	}

	// Verify we got the correct response body
	if !bytes.Equal(body, responseBytes) {
		t.Errorf("Expected response body '%s', got '%s'", responseBytes, body)
	}

	// Verify that only one request was made (no retries)
	if mockRequester.CallCount != 1 {
		t.Errorf("Expected HTTPRequester.DoRequest to be called once, got %d", mockRequester.CallCount)
	}
}

// TestUnifiedTransport_ContextCancellation tests that context cancellation works properly
func TestUnifiedTransport_ContextCancellation(t *testing.T) {
	// Create a test context with a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	requestJSON := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "testMethod",
		"id":      1,
	}
	requestBytes, _ := json.Marshal(requestJSON)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(requestBytes))
	req = req.WithContext(ctx)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := createTestCustomContext(req, rec, []string{"testMethod"}, requestBytes)

	mockSelector := &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: &ProxyTarget{url: "target1"}, Index: 0, Error: nil},
		},
		IsAvailableFn: func() bool { return true },
	}

	mockRouter := &MethodRouterWrapper{
		mockSelector: mockSelector,
	}

	// Create a context-aware requester
	mockRequester := &ContextAwareHTTPRequester{
		blockForever: make(chan struct{}), // This channel will never be written to
	}

	transport := NewUnifiedTransport("test_transport", mockRouter, mockRequester, 3, false)

	// Cancel the context after a short time
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, _, err := transport.SendRequest(c)
	if err == nil {
		t.Fatalf("Expected context cancellation error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

// Additional wrapper types to avoid conflicts with existing mock types

// MockHTTPRequesterWrapper is a test double for HTTPRequester
type MockHTTPRequesterWrapper struct {
	Responses []HTTPResponseWrapper
	CallCount int
}

type HTTPResponseWrapper struct {
	RespBody   []byte
	StatusCode int
	Error      error
}

func (m *MockHTTPRequesterWrapper) DoRequest(c *echoUtil.CustomContext, targetURL string) ([]byte, int, error) {
	if m.CallCount >= len(m.Responses) {
		return nil, 0, errors.New("no more mock responses")
	}
	resp := m.Responses[m.CallCount]
	m.CallCount++
	return resp.RespBody, resp.StatusCode, resp.Error
}

// ContextAwareHTTPRequester is a special mock that responds to context cancellation
type ContextAwareHTTPRequester struct {
	blockForever chan struct{}
}

func (c *ContextAwareHTTPRequester) DoRequest(ctx *echoUtil.CustomContext, targetURL string) ([]byte, int, error) {
	select {
	case <-c.blockForever: // This will never happen
		return []byte("OK"), http.StatusOK, nil
	case <-ctx.Request().Context().Done():
		return nil, 0, ctx.Request().Context().Err()
	}
}

// MethodRouterWrapper wraps a MockTargetSelector as a MethodRouter
type MethodRouterWrapper struct {
	mockSelector      *MockTargetSelector
	isAvailableFn     func() bool
	updateStatsCalled bool
}

func (m *MethodRouterWrapper) GetBalancerForMethod(method string) (balancer.TargetSelector[*ProxyTarget], bool) {
	return m.mockSelector, true
}

func (m *MethodRouterWrapper) IsMethodSupported(method string) bool {
	return true
}

func (m *MethodRouterWrapper) IsAvailable() bool {
	if m.isAvailableFn != nil {
		return m.isAvailableFn()
	}
	return m.mockSelector.IsAvailable()
}

func (m *MethodRouterWrapper) UpdateTargetStats(target *ProxyTarget, success bool, methods []string, responseTime, slotAmount int64) {
	m.updateStatsCalled = true
}
