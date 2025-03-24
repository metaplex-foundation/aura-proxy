package solana

import (
	"fmt"

	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// MockTargetSelector implements the TargetSelector interface for testing.
type MockTargetSelector struct {
	Targets              []*ProxyTarget
	NextResponses        []NextResponse // Sequence of responses
	CallCount            int            // Track how many times GetNext is called
	IsAvailableFn        func() bool
	TargetsCount         int
	UpdateStatsCallCount int
	UpdateStatsArgs      []UpdateStatsArgs
}

type NextResponse struct {
	Target *ProxyTarget
	Index  int
	Error  error
}

type UpdateStatsArgs struct {
	Target       *ProxyTarget
	Success      bool
	Methods      []string
	ResponseTime int64
	SlotAmount   int64
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

// Implement MethodRouter for the MockTargetSelector
func (m *MockTargetSelector) GetBalancerForMethod(method string) (balancer.TargetSelector[*ProxyTarget], bool) {
	return m, true
}

func (m *MockTargetSelector) IsMethodSupported(method string) bool {
	return true
}

func (m *MockTargetSelector) UpdateTargetStats(target *ProxyTarget, success bool, methods []string, responseTimeMs, slotAmount int64) {
	m.UpdateStatsCallCount++
	m.UpdateStatsArgs = append(m.UpdateStatsArgs, UpdateStatsArgs{
		Target:       target,
		Success:      success,
		Methods:      methods,
		ResponseTime: responseTimeMs,
		SlotAmount:   slotAmount,
	})
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
