package rpc_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joyy/chrome-pilot/internal/rpc"
)

// tempSocket returns a unique socket path inside the OS temp directory.
func tempSocket(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.TempDir(), fmt.Sprintf("test-rpc-%d.sock", time.Now().UnixNano()))
}

// startServer creates and starts a server in a background goroutine.
// It waits briefly to let the server begin listening before returning.
func startServer(t *testing.T, socketPath string) *rpc.Server {
	t.Helper()
	srv := rpc.NewServer(socketPath)
	go func() {
		if err := srv.Serve(); err != nil {
			_ = err
		}
	}()
	// Give the server a moment to bind the socket.
	time.Sleep(20 * time.Millisecond)
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = os.Remove(socketPath)
	})
	return srv
}

// callMethod sends a single JSON-RPC request over the Unix socket and returns
// the decoded response.
func callMethod(t *testing.T, socketPath string, method string, params interface{}, id int) rpc.Response {
	t.Helper()

	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	req := rpc.Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  json.RawMessage(rawParams),
		ID:      id,
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial unix socket: %v", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var resp rpc.Response
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return resp
}

// TestServerPingPong registers a "ping" handler that returns "pong" and
// verifies the client receives the expected result.
func TestServerPingPong(t *testing.T) {
	sock := tempSocket(t)
	srv := startServer(t, sock)

	srv.Register("ping", func(params json.RawMessage) (interface{}, error) {
		return "pong", nil
	})

	resp := callMethod(t, sock, "ping", nil, 1)

	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result != "pong" {
		t.Errorf("expected result %q, got %q", "pong", result)
	}

	if resp.ID != 1 {
		t.Errorf("expected response ID 1, got %d", resp.ID)
	}
}

// TestServerMethodNotFound calls a nonexistent method and verifies the
// server responds with error code -32601.
func TestServerMethodNotFound(t *testing.T) {
	sock := tempSocket(t)
	startServer(t, sock)

	resp := callMethod(t, sock, "nonexistent", nil, 2)

	if resp.Error == nil {
		t.Fatal("expected RPC error for unknown method, got nil")
	}

	if resp.Error.Code != rpc.ErrMethodNotFound {
		t.Errorf("expected error code %d, got %d", rpc.ErrMethodNotFound, resp.Error.Code)
	}

	if resp.ID != 2 {
		t.Errorf("expected response ID 2, got %d", resp.ID)
	}
}
