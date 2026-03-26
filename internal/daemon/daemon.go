package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joyy/chrome-pilot/internal/bridge"
	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/rpc"
	"github.com/joyy/chrome-pilot/internal/snapshot"
)

// SessionState holds per-session working context.
type SessionState struct {
	WorkingTabID int
}

// Daemon manages the lifecycle of the chrome-pilot background process.
// It owns the RPC server (Unix socket for CLI) and the WebSocket server
// (for the Chrome extension), plus supporting stores.
type Daemon struct {
	cfg        *config.Config
	dataDir    string
	rpcServer  *rpc.Server
	wsServer   *bridge.WSServer
	snapStore  *snapshot.Store
	tmpManager interface{} // nil until tmpfile task is implemented
	session    *SessionState
	token     string
	pidFile   string
	tokenFile string
	idleTimer  *time.Timer
	done       chan struct{}
	closeOnce  sync.Once
}

// New creates a new Daemon, initialising directories, the RPC server, the WS
// server (with a freshly generated token), and the snapshot store.
// The token is written to dataDir/token with 0600 permissions.
func New(cfg *config.Config, dataDir string) (*Daemon, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("daemon: create data dir: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("daemon: generate token: %w", err)
	}

	tokenFile := filepath.Join(dataDir, "token")
	if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		return nil, fmt.Errorf("daemon: write token file: %w", err)
	}

	rpcSrv := rpc.NewServer(cfg.SocketPath)
	wsSrv := bridge.NewWSServer(token)
	snapStore := snapshot.NewStore()

	d := &Daemon{
		cfg:       cfg,
		dataDir:   dataDir,
		rpcServer: rpcSrv,
		wsServer:  wsSrv,
		snapStore: snapStore,
		session:   &SessionState{},
		token:     token,
		pidFile:   filepath.Join(dataDir, "daemon.pid"),
		tokenFile: tokenFile,
		done:      make(chan struct{}),
	}

	return d, nil
}

// Start begins the daemon: checks for an existing running instance, writes
// the PID file, registers built-in handlers, starts the RPC server and the
// optional WS HTTP server, arms the idle timer, and waits for SIGTERM/SIGINT.
func (d *Daemon) Start() error {
	// Check if an instance is already running.
	running, pid, err := d.IsRunning()
	if err != nil {
		return fmt.Errorf("daemon: check running: %w", err)
	}
	if running {
		return fmt.Errorf("daemon: already running (pid %d)", pid)
	}

	// Write our own PID file.
	if err := d.writePID(); err != nil {
		return fmt.Errorf("daemon: write pid: %w", err)
	}

	// Register all RPC handlers.
	d.RegisterHandlers()

	// Hook idle timer reset into the RPC server activity callback.
	idleTimeout, err := time.ParseDuration(d.cfg.IdleTimeout)
	if err != nil {
		idleTimeout = 30 * time.Minute
	}
	d.idleTimer = time.AfterFunc(idleTimeout, func() {
		log.Printf("daemon: idle timeout reached, stopping")
		_ = d.Stop()
	})
	d.rpcServer.SetOnActivity(func() {
		if d.idleTimer != nil {
			d.idleTimer.Reset(idleTimeout)
		}
	})

	// Periodically expire snapshots that have not been accessed recently.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.snapStore.ExpireOlderThan(10 * time.Minute)
			case <-d.done:
				return
			}
		}
	}()

	// Register WS event handler to clear snapshots on navigation/close.
	d.wsServer.SetOnEvent(func(event string, data json.RawMessage) {
		switch event {
		case "tab.navigated", "tab.closed":
			var payload struct {
				TabID int `json:"tabId"`
			}
			if err := json.Unmarshal(data, &payload); err == nil && payload.TabID != 0 {
				d.snapStore.Clear(payload.TabID)
			}
		}
	})

	// Start RPC server in background goroutine.
	rpcErrCh := make(chan error, 1)
	go func() {
		if err := d.rpcServer.Serve(); err != nil {
			rpcErrCh <- err
		}
		close(rpcErrCh)
	}()

	// Start WebSocket HTTP server (only when WSPort > 0).
	if d.cfg.WSPort > 0 {
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", d.wsServer.HandleWS)
		mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Write([]byte(d.token))
		})
		addr := fmt.Sprintf(":%d", d.cfg.WSPort)
		httpSrv := &http.Server{Addr: addr, Handler: mux}
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("daemon: ws http server: %v", err)
			}
		}()
	}

	// Wait for shutdown signal or done channel.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		log.Printf("daemon: received signal %s, stopping", sig)
		return d.Stop()
	case err := <-rpcErrCh:
		if err != nil {
			return fmt.Errorf("daemon: rpc server: %w", err)
		}
		return nil
	case <-d.done:
		return nil
	}
}

// Stop shuts down the daemon cleanly: closes the done channel (once), stops
// the idle timer, stops the RPC server, and removes the PID and socket files.
func (d *Daemon) Stop() error {
	var stopErr error
	d.closeOnce.Do(func() {
		close(d.done)

		if d.idleTimer != nil {
			d.idleTimer.Stop()
		}

		if err := d.rpcServer.Stop(); err != nil {
			stopErr = fmt.Errorf("daemon: stop rpc server: %w", err)
		}

		// Remove PID file.
		if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
			log.Printf("daemon: remove pid file: %v", err)
		}

		// Remove socket file.
		if err := os.Remove(d.cfg.SocketPath); err != nil && !os.IsNotExist(err) {
			log.Printf("daemon: remove socket file: %v", err)
		}
	})
	return stopErr
}

// IsRunning reads the PID file and checks whether the process is alive using
// signal 0.  If the PID file exists but the process is gone the stale file is
// removed. Returns (alive, pid, error).
func (d *Daemon) IsRunning() (bool, int, error) {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("daemon: read pid file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Corrupt PID file — treat as not running, remove it.
		_ = os.Remove(d.pidFile)
		return false, 0, nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(d.pidFile)
		return false, 0, nil
	}

	// Signal 0 checks whether the process exists without sending a real signal.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process is gone — remove stale PID file.
		_ = os.Remove(d.pidFile)
		return false, 0, nil
	}

	return true, pid, nil
}

// RPCServer returns the daemon's RPC server for external handler registration.
func (d *Daemon) RPCServer() *rpc.Server {
	return d.rpcServer
}

// WSServer returns the daemon's WebSocket server.
func (d *Daemon) WSServer() *bridge.WSServer {
	return d.wsServer
}

// SnapStore returns the daemon's snapshot store.
func (d *Daemon) SnapStore() *snapshot.Store {
	return d.snapStore
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// writePID writes the current process ID to the PID file.
func (d *Daemon) writePID() error {
	pid := strconv.Itoa(os.Getpid())
	return os.WriteFile(d.pidFile, []byte(pid), 0600)
}

// generateToken creates a 32-byte random token encoded as a hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
