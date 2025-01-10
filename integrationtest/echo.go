package integrationtest

import (
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

// startEchoWSServer starts a simple Echo server with a WebSocket endpoint
// that echoes back all messages. It returns the *echo.Echo and the actual address in the form "http://host:port/ws"
// that the server is listening on. The http protocol is used because the reverser proxy will use HTTP to connect to the upstream and upgrade to WS.
// the reverse proxy doesn't support ws or wss protocol and will fail to connect to the upstream.
func startEchoWSServer(t *testing.T) (*echo.Echo, string) {
	t.Helper()

	e := echo.New()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // in tests, allow all
		},
	}

	e.GET("/ws", func(c echo.Context) error {
		// Upgrade
		conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}
		defer conn.Close()

		// Echo loop
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return nil // client closed or other error
			}
			// echo it back
			if err := conn.WriteMessage(msgType, msg); err != nil {
				return nil
			}
		}
	})

	// Start server on random port
	go func() {
		// In real code, handle the error properly
		if err := e.Start(":0"); err != nil && err != http.ErrServerClosed {
			log.Fatalf("echo server failed: %v", err)
		}
	}()

	// Wait briefly for the server to start
	time.Sleep(100 * time.Millisecond)

	// Retrieve the actual port
	addr := e.ListenerAddr().String() // something like "0.0.0.0:12345"
	wsURL := "http://" + addr + "/ws"

	return e, wsURL
}
