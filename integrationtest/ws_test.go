package integrationtest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/proxy"
	"aura-proxy/internal/proxy/config"
)

func makeWrappedURL(upstreamWSURL string) (configtypes.WrappedURL, error) {
	parsed, err := url.Parse(upstreamWSURL)
	if err != nil {
		return configtypes.WrappedURL{}, err
	}
	return configtypes.WrappedURL(*parsed), nil
}

// TestProxyWebSocketIntegration spins up an echo WS server, configures the proxy
// to point to that server, and verifies we can echo a message through the proxy.
func TestProxyWebSocketIntegration(t *testing.T) {
	// 1. Start echo WS server (upstream)
	upstreamServer, upstreamWSURL := startEchoWSServer(t)
	defer func() {
		_ = upstreamServer.Shutdown(context.Background())
	}()

	u, err := makeWrappedURL(upstreamWSURL)
	if err != nil {
		t.Fatalf("makeWrappedURL error: %v", err)
	}
	// 2. Create the proxy config, referencing the upstream's URL
	// the upstream address should be using http protocol because the reverse proxy will use HTTP to connect to the upstream and upgrade to WS.
	cfg := config.Config{
		Proxy: configtypes.ProxyConfig{
			Port:      44999, // local test port for proxy
			IsMainnet: true,

			// For Solana:
			Solana: configtypes.SolanaConfig{
				WSHostURL: []configtypes.WrappedURL{u}, // If your solana adapter uses WS
			},
		},
		Service: configtypes.ServiceConfig{
			Name:  "test",
			Level: "local",
		},
	}

	// 3. Create stubs for statCollector, requestCounter, tokenChecker
	sc := &testStatCollector{}
	rc := &testRequestCounter{}
	tc := &testTokenChecker{}

	// 4. Initialize the proxy
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := &sync.WaitGroup{}
	p, err := proxy.InitProxy(ctx, cancel, cfg, wg, sc, rc, tc)
	if err != nil {
		t.Fatalf("InitProxy error: %v", err)
	}

	// 5. Run the proxy in a goroutine
	go func() {
		if err := p.Run(); err != nil && err != http.ErrServerClosed {
			t.Errorf("Proxy run error: %v", err)
		}
	}()
	defer func() {
		// Stop the proxy at test end
		_ = p.Stop()
	}()

	// Wait a bit for the proxy to start listening
	time.Sleep(100 * time.Millisecond)

	// 6. Connect to the proxy via WebSocket
	proxyWSURL := fmt.Sprintf("ws://127.0.0.1:%d/testToken1", cfg.Proxy.Port)

	dialer := &websocket.Dialer{}
	headers := http.Header{
		"Host":         []string{"mainnet-aura.metaplex.com"},
		"Content-Type": []string{"application/json"},
	}

	conn, _, err := dialer.Dial(proxyWSURL, headers)
	if err != nil {
		t.Fatalf("failed to dial proxy websocket: %v", err)
	}
	defer conn.Close()

	// 7. Send a test message
	testMessage := "Hello from test"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMessage)); err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}

	// 8. Read echo response
	msgType, resp, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage error: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Errorf("Expected TextMessage, got %v", msgType)
	}
	if string(resp) != testMessage {
		t.Errorf("Expected echo %q, got %q", testMessage, resp)
	}
}
