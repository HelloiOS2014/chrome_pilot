package sockutil_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joyy/chrome-pilot/internal/rpc"
	"github.com/joyy/chrome-pilot/internal/sockutil"
)

// tempSocket returns a unique socket path inside the OS temp directory.
func tempSocket(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.TempDir(), fmt.Sprintf("test-sockutil-%d.sock", time.Now().UnixNano()))
}

// startServer creates and starts an rpc.Server in a background goroutine.
// It waits briefly for the server to begin listening before returning.
func startServer(t *testing.T, socketPath string) *rpc.Server {
	t.Helper()
	srv := rpc.NewServer(socketPath)
	go func() {
		_ = srv.Serve()
	}()
	// Give the server a moment to bind the socket.
	time.Sleep(20 * time.Millisecond)
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = os.Remove(socketPath)
	})
	return srv
}

// TestCallEcho starts a server, registers an "echo" handler, calls it via
// sockutil.Call, and verifies the returned result matches the sent params.
func TestCallEcho(t *testing.T) {
	sock := tempSocket(t)
	srv := startServer(t, sock)

	srv.Register("echo", func(params json.RawMessage) (interface{}, error) {
		// Unmarshal the string the caller sent and echo it back.
		var msg string
		if err := json.Unmarshal(params, &msg); err != nil {
			return nil, err
		}
		return msg, nil
	})

	result, err := sockutil.Call(sock, "echo", "hello")
	if err != nil {
		t.Fatalf("Call returned unexpected error: %v", err)
	}

	var got string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if got != "hello" {
		t.Errorf("expected echo result %q, got %q", "hello", got)
	}
}

// TestCallRPCError verifies that when the server returns a JSON-RPC error
// object, Call surfaces it as an *RPCCallError with the correct code and
// message.
func TestCallRPCError(t *testing.T) {
	sock := tempSocket(t)
	srv := startServer(t, sock)

	srv.Register("fail", func(params json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("something went wrong")
	})

	_, err := sockutil.Call(sock, "fail", nil)
	if err == nil {
		t.Fatal("expected an error from Call, got nil")
	}

	rpcErr, ok := err.(*sockutil.RPCCallError)
	if !ok {
		t.Fatalf("expected *sockutil.RPCCallError, got %T: %v", err, err)
	}

	if rpcErr.Code == 0 {
		t.Errorf("expected non-zero error code, got 0")
	}

	if !strings.Contains(rpcErr.Message, "something went wrong") {
		t.Errorf("expected error message to contain %q, got %q", "something went wrong", rpcErr.Message)
	}
}

// TestCallMethodNotFound verifies that calling an unregistered method returns
// an *RPCCallError (the server responds with -32601 Method Not Found).
func TestCallMethodNotFound(t *testing.T) {
	sock := tempSocket(t)
	startServer(t, sock)

	_, err := sockutil.Call(sock, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected an error for unknown method, got nil")
	}

	rpcErr, ok := err.(*sockutil.RPCCallError)
	if !ok {
		t.Fatalf("expected *sockutil.RPCCallError, got %T: %v", err, err)
	}

	// JSON-RPC 2.0 method-not-found code is -32601.
	if rpcErr.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", rpcErr.Code)
	}
}

// TestCallNonexistentSocket verifies that Call returns an error (not a panic)
// when the socket file does not exist.
func TestCallNonexistentSocket(t *testing.T) {
	_, err := sockutil.Call("/tmp/no-such-socket-file.sock", "ping", nil)
	if err == nil {
		t.Fatal("expected dial error for nonexistent socket, got nil")
	}
}
