package solana

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/configtypes"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// MockHTTPRequesterWithResponseMap implements HTTPRequester with predefined responses per URL
type MockHTTPRequesterWithResponseMap struct {
	Responses map[string][]byte
	Errors    map[string]error
	Calls     map[string]int
}

func NewMockHTTPRequester() *MockHTTPRequesterWithResponseMap {
	return &MockHTTPRequesterWithResponseMap{
		Responses: make(map[string][]byte),
		Errors:    make(map[string]error),
		Calls:     make(map[string]int),
	}
}

func (m *MockHTTPRequesterWithResponseMap) DoRequest(c *echoUtil.CustomContext, targetURL string) (respBody []byte, statusCode int, err error) {
	m.Calls[targetURL]++
	
	if err, ok := m.Errors[targetURL]; ok && err != nil {
		return nil, http.StatusInternalServerError, err
	}
	
	if resp, ok := m.Responses[targetURL]; ok {
		return resp, http.StatusOK, nil
	}
	
	return []byte(`{"result":true}`), http.StatusOK, nil
}

// Create a testing helper to create Echo contexts with method information
func createTestContext(t *testing.T, method string) *echoUtil.CustomContext {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	res := httptest.NewRecorder()
	c := e.NewContext(req, res)
	
	ctx := &MockCustomContext{
		CustomContext: c.(*echoUtil.CustomContext),
		methods:       []string{method},
	}
	
	return ctx
}

func TestAdapter_BackwardCompatibility(t *testing.T) {
	// Create a configuration with only legacy format
	cfg := &configtypes.SolanaConfig{
		DasAPINodes: []configtypes.SolanaNode{
			{
				URL:      createWrappedURL("https://das1.example.com"),
				Provider: "DAS1",
				NodeType: solana.NodeType{Name: "das"},
			},
		},
		BasicRouteNodes: []configtypes.SolanaNode{
			{
				URL:      createWrappedURL("https://basic1.example.com"),
				Provider: "Basic1",
				NodeType: solana.NodeType{Name: "basic"},
			},
		},
	}

	// Set up mock HTTP requester
	mockRequester := NewMockHTTPRequester()
	originalRequester := &RealHTTPRequester{}
	
	// Temporarily replace the real requester (saved to be restored later)
	originalCNFTHTTPRequester := func(transport *CNFTTransport) HTTPRequester {
		return transport.httpRequester
	}
	
	// Mock the HTTP requester for testing
	patchHTTPRequester := func() {
		// Override NewCNFTransport to use our mock requester
		originalNewCNFTransport := NewCNFTransport
		NewCNFTransport = func(targetType string, methodList map[string]uint, targetSelector balancer.TargetSelector[*ProxyTarget], requester HTTPRequester) *CNFTTransport {
			transport := originalNewCNFTransport(targetType, methodList, targetSelector, mockRequester)
			return transport
		}
		
		// Restore NewCNFTransport after the test
		t.Cleanup(func() {
			NewCNFTransport = originalNewCNFTransport
		})
	}
	
	// Apply the patch
	patchHTTPRequester()
	
	// Create adapter
	adapter, err := newAdapter(cfg, true, "test", solana.MethodList, []string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	
	// Test DAS methods are routed to DAS nodes
	t.Run("DAS method: getAssetProof", func(t *testing.T) {
		ctx := createTestContext(t, "getAssetProof")
		adapter.ProxyPostRequest(ctx)
		
		if ctx.GetProvider() != "DAS1" {
			t.Errorf("Expected provider DAS1 for method getAssetProof, got %s", ctx.GetProvider())
		}
		
		// Restore the real HTTP requester
		if adapter.cNFTTransport != nil {
			adapter.cNFTTransport.httpRequester = originalRequester
		}
	})
}

func TestAdapter_MethodBasedRouting(t *testing.T) {
	// Create a configuration with method-based routing
	cfg := &configtypes.SolanaConfig{
		// Legacy configuration for backward compatibility
		DasAPINodes: []configtypes.SolanaNode{
			{
				URL:      createWrappedURL("https://das1.example.com"),
				Provider: "DAS1",
				NodeType: solana.NodeType{Name: "das"},
			},
		},
		BasicRouteNodes: []configtypes.SolanaNode{
			{
				URL:      createWrappedURL("https://basic1.example.com"),
				Provider: "Basic1",
				NodeType: solana.NodeType{Name: "basic"},
			},
		},
		
		// Method groups
		MethodGroups: []configtypes.MethodGroupConfig{
			{
				Name:    "proofMethods",
				Methods: []string{"getAssetProof", "getAssetProofs"},
			},
		},
		
		// New method-based routing
		Providers: []configtypes.ProviderConfig{
			{
				Name: "FastProvider",
				Endpoints: []configtypes.EndpointConfig{
					{
						URL:      "https://fast1.example.com",
						Weight:   2.0,
						Methods:  []string{"getLatestBlockhash", "getSlot"},
						NodeType: solana.NodeType{Name: "fast"},
					},
				},
			},
			{
				Name: "ProofProvider",
				Endpoints: []configtypes.EndpointConfig{
					{
						URL:         "https://proof1.example.com",
						Weight:      1.0,
						MethodGroups: []string{"proofMethods"},
						NodeType:    solana.NodeType{Name: "proof"},
					},
				},
			},
		},
	}

	// Create a mock HTTP requester for testing
	mockRequester := NewMockHTTPRequester()
	
	// Save original constructor to restore later
	originalNewMethodTransport := NewMethodTransport
	
	// Override the method transport constructor to use our mock
	NewMethodTransport = func(targetType string, methodRouter MethodRouter, requester HTTPRequester, maxAttempts int) *MethodTransport {
		return originalNewMethodTransport(targetType, methodRouter, mockRequester, maxAttempts)
	}
	
	// Restore the original constructor after the test
	t.Cleanup(func() {
		NewMethodTransport = originalNewMethodTransport
	})
	
	// Create adapter
	adapter, err := newAdapter(cfg, true, "test", solana.MethodList, []string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	
	// Test new method-based routing
	t.Run("Fast method: getLatestBlockhash", func(t *testing.T) {
		ctx := createTestContext(t, "getLatestBlockhash")
		adapter.ProxyPostRequest(ctx)
		
		if ctx.GetProvider() != "FastProvider" {
			t.Errorf("Expected provider FastProvider for method getLatestBlockhash, got %s", ctx.GetProvider())
		}
	})
	
	t.Run("Proof method: getAssetProof", func(t *testing.T) {
		ctx := createTestContext(t, "getAssetProof")
		adapter.ProxyPostRequest(ctx)
		
		// Should route to the ProofProvider through method-based routing, not DAS1
		if ctx.GetProvider() != "ProofProvider" {
			t.Errorf("Expected provider ProofProvider for method getAssetProof, got %s", ctx.GetProvider())
		}
	})
	
	// Test method not in method-based routing falls back to legacy routing
	t.Run("Other method: getTransaction", func(t *testing.T) {
		ctx := createTestContext(t, "getTransaction")
		adapter.ProxyPostRequest(ctx)
		
		// Should fall back to BasicRouteNodes
		if ctx.GetProvider() != "Basic1" {
			t.Errorf("Expected provider Basic1 for method getTransaction, got %s", ctx.GetProvider())
		}
	})
} 