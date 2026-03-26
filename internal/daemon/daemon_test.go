package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/sockutil"
)

// TestDaemonPing starts a daemon with temporary paths, sends a ping via the
// RPC socket, verifies the "pong" response, then stops the daemon.
func TestDaemonPing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	cfg := &config.Config{
		WSPort:      0, // disable WebSocket server
		IdleTimeout: "30m",
		SocketPath:  socketPath,
		LogLevel:    "info",
	}

	d, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Verify token file was created with the right permissions.
	info, err := os.Stat(filepath.Join(tmpDir, "token"))
	if err != nil {
		t.Fatalf("token file missing: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("token file permissions: got %v, want 0600", info.Mode().Perm())
	}

	// Start the daemon in a goroutine; it blocks until stopped.
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- d.Start()
	}()

	// Wait until the socket file appears (daemon is ready to accept).
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for daemon socket")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Ping via sockutil.Call.
	raw, err := sockutil.Call(socketPath, "ping", nil)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}

	var result string
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal ping result: %v", err)
	}
	if result != "pong" {
		t.Errorf("ping result: got %q, want %q", result, "pong")
	}

	// Also verify status handler returns expected fields.
	rawStatus, err := sockutil.Call(socketPath, "status", nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var status map[string]interface{}
	if err := json.Unmarshal(rawStatus, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status["daemon"] != "running" {
		t.Errorf("status[daemon]: got %v, want running", status["daemon"])
	}

	// Stop the daemon.
	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Drain the start goroutine.
	select {
	case err := <-startErrCh:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Start goroutine did not return after Stop")
	}

	// PID file should be gone after Stop.
	if _, err := os.Stat(filepath.Join(tmpDir, "daemon.pid")); !os.IsNotExist(err) {
		t.Error("PID file still exists after Stop")
	}
}

// TestIsRunning verifies stale-PID cleanup and live-process detection.
func TestIsRunning(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	cfg := &config.Config{
		WSPort:      0,
		IdleTimeout: "30m",
		SocketPath:  socketPath,
	}

	d, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// No PID file yet — should not be running.
	running, pid, err := d.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning (no file): %v", err)
	}
	if running || pid != 0 {
		t.Errorf("IsRunning (no file): got running=%v pid=%d", running, pid)
	}

	// Write a stale PID (PID 1 always exists on Unix but belongs to init, not us;
	// however writing an obviously dead PID is more reliable for testing).
	stalePID := "999999999"
	if err := os.WriteFile(d.pidFile, []byte(stalePID), 0600); err != nil {
		t.Fatalf("write stale pid: %v", err)
	}

	running, pid, err = d.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning (stale): %v", err)
	}
	if running {
		t.Errorf("IsRunning (stale): expected not running, got pid=%d", pid)
	}
	// Stale PID file should have been cleaned up.
	if _, err := os.Stat(d.pidFile); !os.IsNotExist(err) {
		t.Error("stale PID file not removed")
	}

	// Write our own PID — should now be running.
	if err := d.writePID(); err != nil {
		t.Fatalf("writePID: %v", err)
	}

	running, pid, err = d.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning (self): %v", err)
	}
	if !running {
		t.Error("IsRunning (self): expected running=true")
	}
	if pid != os.Getpid() {
		t.Errorf("IsRunning (self): pid=%d, want %d", pid, os.Getpid())
	}
}

// TestGetters verifies the exported getter methods return non-nil values.
func TestGetters(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		WSPort:      0,
		IdleTimeout: "30m",
		SocketPath:  filepath.Join(tmpDir, "test.sock"),
	}

	d, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if d.RPCServer() == nil {
		t.Error("RPCServer() returned nil")
	}
	if d.WSServer() == nil {
		t.Error("WSServer() returned nil")
	}
	if d.SnapStore() == nil {
		t.Error("SnapStore() returned nil")
	}
}
