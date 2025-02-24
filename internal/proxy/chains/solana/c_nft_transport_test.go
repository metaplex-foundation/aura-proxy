package solana

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// MockTargetSelector implements the TargetSelector interface for testing.
type MockTargetSelector struct {
	Targets       []*ProxyTarget
	NextResponses []NextResponse // Sequence of responses
	CallCount     int            // Track how many times GetNext is called
	IsAvailableFn func() bool
	TargetsCount  int
}

type NextResponse struct {
	Target *ProxyTarget
	Index  int
	Error  error
}

func (m *MockTargetSelector) GetNext(exclude []int) (*ProxyTarget, int, error) {
	if m.CallCount >= len(m.NextResponses) {
		return nil, 0, fmt.Errorf("mock TargetSelector: no more responses defined (called %d times)", m.CallCount)
	}
	resp := m.NextResponses[m.CallCount]
	m.CallCount++
	return resp.Target, resp.Index, resp.Error
}

func (m *MockTargetSelector) IsAvailable() bool {
	return m.IsAvailableFn()
}

func (m *MockTargetSelector) GetTargetsCount() int {
	return m.TargetsCount
}

// MockHTTPRequester implements the HTTPRequester interface for testing.
type MockHTTPRequester struct {
	Responses []HTTPResponse // Sequence of responses
	CallCount int            // Track how many times DoRequest is called
}

type HTTPResponse struct {
	RespBody   []byte
	StatusCode int
	Error      error
}

func (m *MockHTTPRequester) DoRequest(c *echoUtil.CustomContext, targetURL string) (respBody []byte, statusCode int, err error) {
	if m.CallCount >= len(m.Responses) {
		return nil, 0, fmt.Errorf("mock HTTPRequester: no more responses defined (called %d times)", m.CallCount)
	}
	resp := m.Responses[m.CallCount]
	m.CallCount++
	return resp.RespBody, resp.StatusCode, resp.Error
}

func TestCNFTTransport_SendRequest(t *testing.T) {
	// Create a test context.
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"testMethod","id":1}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echoUtil.CustomContext{Context: e.NewContext(req, rec)}
	c.SetReqBody([]byte(`{"jsonrpc":"2.0","method":"testMethod","id":1}`))

	// --- Test successful request ---
	mockSelector := &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: &ProxyTarget{url: "target1"}, Index: 0, Error: nil},
		},
		TargetsCount:  1,
		IsAvailableFn: func() bool { return true },
	}
	mockRequester := &MockHTTPRequester{
		Responses: []HTTPResponse{
			{RespBody: []byte("OK"), StatusCode: http.StatusOK, Error: nil},
		},
	}
	transport := NewCNFTransport("test", solana.CNFTMethodList, mockSelector, mockRequester)

	body, code, err := transport.SendRequest(&c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, code)
	}
	if string(body) != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", string(body))
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
	mockRequester = &MockHTTPRequester{
		Responses: []HTTPResponse{
			{RespBody: nil, StatusCode: 0, Error: errors.New("connection refused")}, // Simulate a failure
			{RespBody: []byte("OK"), StatusCode: http.StatusOK, Error: nil},         // Successful retry
		},
	}
	transport = NewCNFTransport("test", solana.CNFTMethodList, mockSelector, mockRequester)

	body, code, err = transport.SendRequest(&c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, code)
	}
	if string(body) != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", string(body))
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
	mockRequester = &MockHTTPRequester{
		Responses: []HTTPResponse{
			{RespBody: nil, StatusCode: 0, Error: errors.New("connection refused")},
			{RespBody: nil, StatusCode: 0, Error: errors.New("connection refused")},
		},
	}
	transport = NewCNFTransport("test", solana.CNFTMethodList, mockSelector, mockRequester)

	_, code, err = transport.SendRequest(&c)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	if code != http.StatusServiceUnavailable {
		t.Errorf("Expected status code %d, got %d", http.StatusServiceUnavailable, code)
	}
	expectedErrMsg := "all targets failed"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
	if mockSelector.CallCount != 2 {
		t.Errorf("Expected TargetSelector.GetNext to be called twice, got %d", mockSelector.CallCount)
	}
	if mockRequester.CallCount != 2 {
		t.Errorf("Expected HTTPRequester.DoRequest to be called twice, got %d", mockRequester.CallCount)
	}

	// --- Test all targets failed when no available targets ---
	mockSelector = &MockTargetSelector{
		NextResponses: []NextResponse{},
		TargetsCount:  0,
		IsAvailableFn: func() bool { return false },
	}
	mockRequester = &MockHTTPRequester{} // No responses needed
	transport = NewCNFTransport("test", solana.CNFTMethodList, mockSelector, mockRequester)

	_, code, err = transport.SendRequest(&c)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	if code != http.StatusServiceUnavailable {
		t.Errorf("Expected status code %d, got %d", http.StatusServiceUnavailable, code)
	}
	expectedErrMsg = "all targets failed"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
	if mockSelector.CallCount != 0 {
		t.Errorf("Expected TargetSelector.GetNext to be called zero times, got %d", mockSelector.CallCount)
	}
	if mockRequester.CallCount != 0 {
		t.Errorf("Expected HTTPRequester.DoRequest to be called zero times, got %d", mockRequester.CallCount)
	}
}

func TestCNFTTransport_sendRequestWithRetries(t *testing.T) {
	// Create a test context.
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"testMethod","id":1}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echoUtil.CustomContext{Context: e.NewContext(req, rec)}
	c.SetReqBody([]byte(`{"jsonrpc":"2.0","method":"testMethod","id":1}`))

	// --- Test successful request ---
	mockSelector := &MockTargetSelector{
		NextResponses: []NextResponse{
			{Target: &ProxyTarget{url: "target1"}, Index: 0, Error: nil},
		},
		TargetsCount:  1,
		IsAvailableFn: func() bool { return true },
	}
	mockRequester := &MockHTTPRequester{
		Responses: []HTTPResponse{
			{RespBody: []byte("success"), StatusCode: http.StatusOK, Error: nil},
		},
	}
	transport := NewCNFTransport("test", solana.CNFTMethodList, mockSelector, mockRequester)

	respBody, statusCode, err := transport.sendRequestWithRetries(&c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, statusCode)
	}
	if !bytes.Equal(respBody, []byte("success")) {
		t.Errorf("Expected response body 'success', got '%s'", string(respBody))
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
			{Target: &ProxyTarget{url: "target1"}, Index: 0, Error: nil}, // First request fails
			{Target: &ProxyTarget{url: "target2"}, Index: 1, Error: nil}, // Second request succeeds
		},
		TargetsCount:  2,
		IsAvailableFn: func() bool { return true },
	}
	mockRequester = &MockHTTPRequester{
		Responses: []HTTPResponse{
			{RespBody: []byte("error"), StatusCode: http.StatusInternalServerError, Error: errors.New("request failed")},
			{RespBody: []byte("success"), StatusCode: http.StatusOK, Error: nil},
		},
	}
	transport = NewCNFTransport("test", solana.CNFTMethodList, mockSelector, mockRequester)

	respBody, statusCode, err = transport.sendRequestWithRetries(&c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, statusCode)
	}
	if !bytes.Equal(respBody, []byte("success")) {
		t.Errorf("Expected response body 'success', got '%s'", string(respBody))
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
	mockRequester = &MockHTTPRequester{
		Responses: []HTTPResponse{
			{RespBody: []byte("error"), StatusCode: http.StatusInternalServerError, Error: errors.New("request failed")},
			{RespBody: []byte("error"), StatusCode: http.StatusInternalServerError, Error: errors.New("request failed")},
		},
	}
	transport = NewCNFTransport("test", solana.CNFTMethodList, mockSelector, mockRequester)

	_, statusCode, err = transport.sendRequestWithRetries(&c)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	if statusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status code %d, got %d", http.StatusServiceUnavailable, statusCode)
	}
	expectedErrMsg := "all targets failed"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
	if mockSelector.CallCount != 2 {
		t.Errorf("Expected TargetSelector.GetNext to be called twice, got %d", mockSelector.CallCount)
	}
	if mockRequester.CallCount != 2 {
		t.Errorf("Expected HTTPRequester.DoRequest to be called twice, got %d", mockRequester.CallCount)
	}
}
