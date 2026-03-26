package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialWS connects a test WebSocket client to the given httptest.Server URL.
func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

// authClient performs the auth handshake and returns the client connection.
func authClient(t *testing.T, conn *websocket.Conn, token string) {
	t.Helper()
	params, _ := json.Marshal(map[string]string{"token": token})
	err := conn.WriteJSON(&WSMessage{Method: "auth", Params: json.RawMessage(params)})
	if err != nil {
		t.Fatalf("write auth: %v", err)
	}
	var resp WSMessage
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read auth response: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("auth failed: %s", resp.Error)
	}
}

// TestWSServerSendAndWait verifies the full command/response round-trip:
// 1. client connects and authenticates
// 2. server calls SendAndWait
// 3. extension (test client) reads the command and echoes back a result
// 4. SendAndWait returns the result
func TestWSServerSendAndWait(t *testing.T) {
	const token = "test-secret"
	srv := NewWSServer(token)

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleWS))
	defer ts.Close()

	// Connect as the extension.
	extConn := dialWS(t, ts)
	defer extConn.Close()
	authClient(t, extConn, token)

	// Goroutine acting as the extension: read one command and echo result.
	done := make(chan struct{})
	go func() {
		defer close(done)
		var cmd WSMessage
		if err := extConn.ReadJSON(&cmd); err != nil {
			t.Errorf("extension read command: %v", err)
			return
		}
		// Echo back a response with the same ID.
		resp := &WSMessage{
			ID:     cmd.ID,
			Result: json.RawMessage(`{"navigated":true}`),
		}
		if err := extConn.WriteJSON(resp); err != nil {
			t.Errorf("extension write response: %v", err)
		}
	}()

	result, err := srv.SendAndWait("tabs.navigate", map[string]string{"url": "https://example.com"}, 5*time.Second)
	if err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["navigated"] != true {
		t.Errorf("unexpected result: %v", got)
	}

	<-done
}

// TestWSServerBadToken verifies that connections with a wrong token are
// rejected before they are accepted into the server state.
func TestWSServerBadToken(t *testing.T) {
	const token = "correct-token"
	srv := NewWSServer(token)

	ts := httptest.NewServer(http.HandlerFunc(srv.HandleWS))
	defer ts.Close()

	extConn := dialWS(t, ts)
	defer extConn.Close()

	// Send wrong token.
	params, _ := json.Marshal(map[string]string{"token": "wrong-token"})
	_ = extConn.WriteJSON(&WSMessage{Method: "auth", Params: json.RawMessage(params)})

	var resp WSMessage
	if err := extConn.ReadJSON(&resp); err != nil {
		// Connection may be closed immediately; that is also a valid rejection.
		return
	}
	if resp.Error == "" {
		t.Errorf("expected error response for bad token, got: %+v", resp)
	}

	// The server must NOT consider itself connected.
	if srv.IsConnected() {
		t.Error("server reports connected after bad-token auth")
	}
}
