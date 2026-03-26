# Chrome Pilot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a CLI tool + Chrome Extension that lets Claude Code control the user's existing Chrome browser, preserving login state and configuration.

**Architecture:** Go CLI (Cobra) communicates with a Go Daemon via JSON-RPC 2.0 over Unix Socket. The Daemon bridges to a Chrome Extension via WebSocket. The Extension executes DOM operations and generates accessibility snapshots. All patterns follow marki_agent conventions.

**Tech Stack:** Go 1.25, Cobra, gorilla/websocket, Chrome Extension Manifest V3, JavaScript

**Spec:** `docs/superpowers/specs/2026-03-25-chrome-pilot-design.md`

**Reference:** `/Users/JOYY/code/marki_agent/feedback_server/` (RPC, daemon, sockutil patterns)

**Known implementation notes (from plan review):**
1. Daemon struct must include `snapStore`, `tmpManager`, `session` fields — initialize in `New()`
2. `handlers.go` must import `"os"` for `os.Getpid()`
3. Use `strconv.Itoa()` not custom `itoa()` in snapshot store
4. `offscreen.js` `getToken()` must use callback pattern: `chrome.runtime.sendMessage({type:'get-token'}, (resp) => resolve(resp?.token))`
5. Content script injection must be a single `func:` call with `executeInPage` inlined as the `func` parameter — two separate `executeScript` calls do NOT share scope
6. `tab select`/`tab close` must convert positional index to Chrome tab ID via `tab.list` at daemon level
7. `manifest.json` needs `"storage"` permission for popup token persistence
8. `page.wait` must use async polling with `setTimeout` + `Promise`, not synchronous check
9. `snapshot --interactable` needs a `QueryInteractable()` method returning all nodes with non-empty `Ref`
10. `chrome.debugger` commands (`page.console`, `page.network`, `page.dialog`, `dom.upload`) need a separate extension handler path — not routed through content script. Implementation deferred to Phase 8 task.

---

## Phase 1: Foundation (Go Project + RPC + Daemon + CLI Basics)

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `cmd/root.go`
- Create: `Makefile`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/JOYY/code/rsearch/chrome_pliot
go mod init github.com/joyy/chrome-pilot
```

- [ ] **Step 2: Create config package**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	WSPort      int    `yaml:"ws_port"`
	IdleTimeout string `yaml:"idle_timeout"`
	SocketPath  string `yaml:"socket_path"`
	LogLevel    string `yaml:"log_level"`
	TmpMaxAge   string `yaml:"tmp_max_age"`
	TmpMaxSize  string `yaml:"tmp_max_size"`
}

func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: home dir: %w", err)
	}
	return filepath.Join(home, ".chrome-pilot"), nil
}

func DefaultConfig() *Config {
	dataDir, _ := DataDir()
	return &Config{
		WSPort:      9333,
		IdleTimeout: "30m",
		SocketPath:  filepath.Join(dataDir, "chrome-pilot.sock"),
		LogLevel:    "info",
		TmpMaxAge:   "24h",
		TmpMaxSize:  "500MB",
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}

func ConfigPath() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "config.yaml"), nil
}
```

- [ ] **Step 3: Create Cobra root command**

Create `cmd/root.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "chrome-pilot",
	Short: "Control your Chrome browser from Claude Code",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Create `main.go`:

```go
package main

import "github.com/joyy/chrome-pilot/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 4: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build install clean

build:
	go build -o chrome-pilot .

install:
	go install .

clean:
	rm -f chrome-pilot
```

- [ ] **Step 5: Install dependencies and verify build**

```bash
cd /Users/JOYY/code/rsearch/chrome_pliot
go get github.com/spf13/cobra@v1.10.2
go get gopkg.in/yaml.v3@v3.0.1
go mod tidy
go build -o chrome-pilot .
./chrome-pilot --help
```

Expected: Help output showing "Control your Chrome browser from Claude Code"

- [ ] **Step 6: Commit**

```bash
git init
echo -e "chrome-pilot\n.DS_Store\n*.log" > .gitignore
git add .
git commit -m "feat: project scaffolding with config, root command, and Makefile"
```

---

### Task 2: JSON-RPC 2.0 Server

**Files:**
- Create: `internal/rpc/protocol.go`
- Create: `internal/rpc/server.go`
- Create: `internal/rpc/server_test.go`

Follow the exact pattern from `/Users/JOYY/code/marki_agent/feedback_server/internal/rpc/`.

- [ ] **Step 1: Write test for RPC server**

Create `internal/rpc/server_test.go`:

```go
package rpc_test

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joyy/chrome-pilot/internal/rpc"
)

func TestServerPingPong(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv := rpc.NewServer(sock)
	srv.Register("ping", func(_ json.RawMessage) (interface{}, error) {
		return "pong", nil
	})

	go srv.Serve()
	defer srv.Stop()

	// Wait for server to start
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := `{"jsonrpc":"2.0","method":"ping","id":1}`
	conn.Write([]byte(req + "\n"))

	var resp struct {
		Result string `json:"result"`
		ID     int    `json:"id"`
	}
	json.NewDecoder(conn).Decode(&resp)

	if resp.Result != "pong" {
		t.Errorf("got %q, want %q", resp.Result, "pong")
	}
	if resp.ID != 1 {
		t.Errorf("got ID %d, want 1", resp.ID)
	}
}

func TestServerMethodNotFound(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv := rpc.NewServer(sock)
	go srv.Serve()
	defer srv.Stop()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := `{"jsonrpc":"2.0","method":"nonexistent","id":2}`
	conn.Write([]byte(req + "\n"))

	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(conn).Decode(&resp)

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("got code %d, want -32601", resp.Error.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/JOYY/code/rsearch/chrome_pliot
go test ./internal/rpc/ -v
```

Expected: FAIL (package doesn't exist yet)

- [ ] **Step 3: Create RPC protocol types**

Create `internal/rpc/protocol.go`:

```go
package rpc

import "encoding/json"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      int             `json:"id"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      int             `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	ErrParseError     = -32700
	ErrMethodNotFound = -32601
	ErrInternalError  = -32000
)

type HandlerFunc func(params json.RawMessage) (interface{}, error)
```

- [ ] **Step 4: Implement RPC server**

Create `internal/rpc/server.go`:

```go
package rpc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

type Server struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]HandlerFunc
	mu         sync.RWMutex
	done       chan struct{}
	wg         sync.WaitGroup
	onActivity func()
}

func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]HandlerFunc),
		done:       make(chan struct{}),
	}
}

func (s *Server) SetOnActivity(fn func()) {
	s.onActivity = fn
}

func (s *Server) Register(method string, handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
}

func (s *Server) Serve() error {
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rpc: remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("rpc: listen on %s: %w", s.socketPath, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				return fmt.Errorf("rpc: accept: %w", err)
			}
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConn(c)
		}(conn)
	}
}

func (s *Server) Stop() error {
	close(s.done)

	s.mu.RLock()
	ln := s.listener
	s.mu.RUnlock()

	if ln != nil {
		if err := ln.Close(); err != nil {
			return fmt.Errorf("rpc: close listener: %w", err)
		}
	}

	waitCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("rpc: timed out waiting for in-flight connections")
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	if s.onActivity != nil {
		s.onActivity()
	}

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResponse(conn, Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrParseError, Message: "parse error: " + err.Error()},
			ID:      0,
		})
		return
	}

	result, rpcErr := s.dispatch(req.Method, req.Params)
	resp := Response{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	writeResponse(conn, resp)
}

func (s *Server) dispatch(method string, params json.RawMessage) (json.RawMessage, *RPCError) {
	s.mu.RLock()
	handler, ok := s.handlers[method]
	s.mu.RUnlock()

	if !ok {
		return nil, &RPCError{Code: ErrMethodNotFound, Message: "method not found: " + method}
	}

	result, err := handler(params)
	if err != nil {
		return nil, &RPCError{Code: ErrInternalError, Message: err.Error()}
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return nil, &RPCError{Code: ErrInternalError, Message: "marshal result: " + err.Error()}
	}
	return raw, nil
}

func writeResponse(conn net.Conn, resp Response) {
	json.NewEncoder(conn).Encode(resp)
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/rpc/ -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/rpc/
git commit -m "feat: JSON-RPC 2.0 server over Unix socket"
```

---

### Task 3: Socket Client (CLI -> Daemon Communication)

**Files:**
- Create: `internal/sockutil/client.go`
- Create: `internal/sockutil/client_test.go`

- [ ] **Step 1: Write test**

Create `internal/sockutil/client_test.go`:

```go
package sockutil_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joyy/chrome-pilot/internal/rpc"
	"github.com/joyy/chrome-pilot/internal/sockutil"
	"path/filepath"
)

func TestCallSuccess(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv := rpc.NewServer(sock)
	srv.Register("echo", func(params json.RawMessage) (interface{}, error) {
		var msg string
		json.Unmarshal(params, &msg)
		return msg, nil
	})
	go srv.Serve()
	defer srv.Stop()
	time.Sleep(50 * time.Millisecond)

	result, err := sockutil.Call(sock, "echo", "hello")
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	var got string
	json.Unmarshal(result, &got)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestCallNotRunning(t *testing.T) {
	_, err := sockutil.Call("/tmp/nonexistent.sock", "ping", nil)
	if err == nil {
		t.Fatal("expected error for non-running daemon")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/sockutil/ -v
```

- [ ] **Step 3: Implement socket client**

Create `internal/sockutil/client.go`:

```go
package sockutil

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type RPCCallError struct {
	Code    int
	Message string
}

func (e *RPCCallError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

func Call(socketPath, method string, params interface{}) (json.RawMessage, error) {
	deadline := time.Now().Add(30 * time.Second)

	conn, err := net.DialTimeout("unix", socketPath, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("sockutil: dial %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("sockutil: set deadline: %w", err)
	}

	var rawParams json.RawMessage
	if params != nil {
		rawParams, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("sockutil: marshal params: %w", err)
		}
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
		ID:      1,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("sockutil: encode request: %w", err)
	}

	var resp rpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("sockutil: decode response: %w", err)
	}

	if resp.Error != nil {
		return nil, &RPCCallError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return resp.Result, nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      int             `json:"id"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      int             `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/sockutil/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sockutil/
git commit -m "feat: socket client for CLI-daemon communication"
```

---

### Task 4: Daemon Lifecycle

**Files:**
- Create: `internal/daemon/daemon.go`
- Create: `internal/daemon/daemon_test.go`

- [ ] **Step 1: Write test**

Create `internal/daemon/daemon_test.go`:

```go
package daemon_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/daemon"
	"github.com/joyy/chrome-pilot/internal/sockutil"
)

func TestDaemonStartStop(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		WSPort:      0, // disabled for this test
		IdleTimeout: "10s",
		SocketPath:  filepath.Join(dir, "test.sock"),
		LogLevel:    "info",
	}

	d, err := daemon.New(cfg, dir)
	if err != nil {
		t.Fatalf("new daemon: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- d.Start() }()
	time.Sleep(100 * time.Millisecond)

	// Ping should work
	result, err := sockutil.Call(cfg.SocketPath, "ping", nil)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if string(result) != `"pong"` {
		t.Errorf("ping result: %s", result)
	}

	// Stop
	if err := d.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop within 5s")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/daemon/ -v
```

- [ ] **Step 3: Implement daemon**

Create `internal/daemon/daemon.go`:

```go
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/rpc"
)

type Daemon struct {
	cfg       *config.Config
	dataDir   string
	rpcServer *rpc.Server
	pidFile   string
	idleTimer *time.Timer
	done      chan struct{}
	closeOnce sync.Once
}

func New(cfg *config.Config, dataDir string) (*Daemon, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("daemon: create data dir: %w", err)
	}

	return &Daemon{
		cfg:       cfg,
		dataDir:   dataDir,
		rpcServer: rpc.NewServer(cfg.SocketPath),
		pidFile:   filepath.Join(dataDir, "chrome-pilot.pid"),
		done:      make(chan struct{}),
	}, nil
}

func (d *Daemon) Start() error {
	running, pid, err := d.IsRunning()
	if err != nil {
		return fmt.Errorf("daemon: check running: %w", err)
	}
	if running {
		return fmt.Errorf("daemon: already running (PID %d)", pid)
	}

	if err := d.writePID(); err != nil {
		return fmt.Errorf("daemon: write pid: %w", err)
	}

	// Register built-in handlers
	d.rpcServer.Register("ping", func(_ json.RawMessage) (interface{}, error) {
		return "pong", nil
	})
	d.rpcServer.Register("status", func(_ json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"daemon":    "running",
			"pid":       os.Getpid(),
			"extension": "not connected",
		}, nil
	})

	d.rpcServer.SetOnActivity(func() { d.resetIdleTimer() })

	serveErr := make(chan error, 1)
	go func() { serveErr <- d.rpcServer.Serve() }()

	d.resetIdleTimer()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigCh:
			d.Stop()
		case <-d.done:
		}
	}()

	select {
	case <-d.done:
	case err := <-serveErr:
		if err != nil {
			d.removePID()
			return fmt.Errorf("daemon: rpc server: %w", err)
		}
	}
	return nil
}

func (d *Daemon) Stop() error {
	var errs []string
	d.closeOnce.Do(func() { close(d.done) })

	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}
	if err := d.rpcServer.Stop(); err != nil {
		errs = append(errs, "stop rpc: "+err.Error())
	}
	if err := d.removePID(); err != nil && !os.IsNotExist(err) {
		errs = append(errs, "remove pid: "+err.Error())
	}
	if err := os.Remove(d.cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, "remove socket: "+err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("daemon: stop errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (d *Daemon) IsRunning() (bool, int, error) {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		// Stale/corrupt PID file, clean up
		os.Remove(d.pidFile)
		return false, 0, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, 0, nil
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process doesn't exist, clean up stale PID
		os.Remove(d.pidFile)
		return false, 0, nil
	}
	return true, pid, nil
}

func (d *Daemon) RPCServer() *rpc.Server {
	return d.rpcServer
}

func (d *Daemon) writePID() error {
	return os.WriteFile(d.pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)
}

func (d *Daemon) removePID() error {
	return os.Remove(d.pidFile)
}

func (d *Daemon) resetIdleTimer() {
	dur, err := time.ParseDuration(d.cfg.IdleTimeout)
	if err != nil || dur <= 0 {
		dur = 30 * time.Minute
	}
	if d.idleTimer != nil {
		d.idleTimer.Reset(dur)
		return
	}
	d.idleTimer = time.AfterFunc(dur, func() { d.Stop() })
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/daemon/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/
git commit -m "feat: daemon lifecycle with PID management and idle timeout"
```

---

### Task 5: CLI Commands — start / stop / status

**Files:**
- Create: `cmd/start.go`
- Create: `cmd/stop.go`
- Create: `cmd/status.go`

- [ ] **Step 1: Create start command**

Create `cmd/start.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/daemon"
	"github.com/spf13/cobra"
)

var foreground bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the chrome-pilot daemon",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().BoolVar(&foreground, "foreground", false, "Run in the foreground")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfgPath, err := config.ConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}

	if foreground {
		d, err := daemon.New(cfg, dataDir)
		if err != nil {
			return err
		}
		return d.Start()
	}

	// Background: re-exec with --foreground
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	child := exec.Command(self, "start", "--foreground")
	child.Stdout = nil
	child.Stderr = nil
	child.Stdin = nil
	if err := child.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Printf(`{"status":"started","pid":%d}`+"\n", child.Process.Pid)
	return nil
}
```

- [ ] **Step 2: Create stop command**

Create `cmd/stop.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the chrome-pilot daemon",
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	pidFile := dataDir + "/chrome-pilot.pid"

	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println(`{"status":"not running"}`)
			return nil
		}
		return fmt.Errorf("read pid: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidFile)
		fmt.Println(`{"status":"not running","note":"cleaned stale pid file"}`)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidFile)
		fmt.Println(`{"status":"not running"}`)
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		os.Remove(pidFile)
		fmt.Println(`{"status":"not running","note":"process already exited"}`)
		return nil
	}

	fmt.Printf(`{"status":"stopped","pid":%d}`+"\n", pid)
	return nil
}
```

- [ ] **Step 3: Create status command**

Create `cmd/status.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/sockutil"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon and extension connection status",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfgPath, err := config.ConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	result, err := sockutil.Call(cfg.SocketPath, "status", nil)
	if err != nil {
		fmt.Println(`{"daemon":"not running","extension":"unknown"}`)
		return nil
	}

	var status map[string]interface{}
	json.Unmarshal(result, &status)
	out, _ := json.Marshal(status)
	fmt.Println(string(out))
	return nil
}
```

- [ ] **Step 4: Build and test manually**

```bash
cd /Users/JOYY/code/rsearch/chrome_pliot
go build -o chrome-pilot .
./chrome-pilot status
./chrome-pilot start
./chrome-pilot status
./chrome-pilot stop
```

Expected:
- `status` (no daemon): `{"daemon":"not running","extension":"unknown"}`
- `start`: `{"status":"started","pid":XXXXX}`
- `status` (with daemon): `{"daemon":"running","pid":XXXXX,"extension":"not connected"}`
- `stop`: `{"status":"stopped","pid":XXXXX}`

- [ ] **Step 5: Commit**

```bash
git add cmd/ main.go
git commit -m "feat: CLI commands start/stop/status"
```

---

## Phase 2: WebSocket Bridge

### Task 6: WebSocket Server + Pending Map

**Files:**
- Create: `internal/bridge/wsserver.go`
- Create: `internal/bridge/pending.go`
- Create: `internal/bridge/wsserver_test.go`

- [ ] **Step 1: Install websocket dependency**

```bash
go get github.com/gorilla/websocket@v1.5.3
```

- [ ] **Step 2: Write test**

Create `internal/bridge/wsserver_test.go`:

```go
package bridge_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joyy/chrome-pilot/internal/bridge"
)

func TestWSServerSendAndWait(t *testing.T) {
	srv := bridge.NewWSServer("test-token")
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleWS))
	defer ts.Close()

	// Connect as extension
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	// Send auth
	ws.WriteJSON(map[string]interface{}{
		"method": "auth",
		"params": map[string]string{"token": "test-token"},
	})

	// Read auth response
	var authResp map[string]interface{}
	ws.ReadJSON(&authResp)

	time.Sleep(50 * time.Millisecond)

	if !srv.IsConnected() {
		t.Fatal("expected connected after auth")
	}

	// Simulate: daemon sends command, extension echoes back
	go func() {
		var msg bridge.WSMessage
		ws.ReadJSON(&msg)
		ws.WriteJSON(bridge.WSMessage{
			ID:     msg.ID,
			Result: json.RawMessage(`{"echoed":true}`),
		})
	}()

	result, err := srv.SendAndWait("test.method", map[string]string{"key": "val"}, 5*time.Second)
	if err != nil {
		t.Fatalf("send and wait: %v", err)
	}

	var got map[string]bool
	json.Unmarshal(result, &got)
	if !got["echoed"] {
		t.Errorf("expected echoed=true")
	}
}

func TestWSServerBadToken(t *testing.T) {
	srv := bridge.NewWSServer("correct-token")
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleWS))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	ws.WriteJSON(map[string]interface{}{
		"method": "auth",
		"params": map[string]string{"token": "wrong-token"},
	})

	// Should receive error and connection should close
	var resp map[string]interface{}
	ws.ReadJSON(&resp)

	if resp["error"] == nil {
		t.Fatal("expected auth error")
	}

	time.Sleep(50 * time.Millisecond)
	if srv.IsConnected() {
		t.Fatal("should not be connected with wrong token")
	}
}
```

- [ ] **Step 3: Implement pending map**

Create `internal/bridge/pending.go`:

```go
package bridge

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type pendingMap struct {
	mu      sync.Mutex
	pending map[string]chan *WSMessage
}

func newPendingMap() *pendingMap {
	return &pendingMap{
		pending: make(map[string]chan *WSMessage),
	}
}

func (p *pendingMap) Create(timeout time.Duration) (string, <-chan *WSMessage) {
	id := uuid.New().String()
	ch := make(chan *WSMessage, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()

	go func() {
		time.Sleep(timeout)
		p.mu.Lock()
		if c, ok := p.pending[id]; ok {
			delete(p.pending, id)
			c <- &WSMessage{ID: id, Error: fmt.Sprintf("timeout after %s", timeout)}
		}
		p.mu.Unlock()
	}()

	return id, ch
}

func (p *pendingMap) Resolve(id string, msg *WSMessage) bool {
	p.mu.Lock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.mu.Unlock()
	if ok {
		ch <- msg
		return true
	}
	return false
}
```

- [ ] **Step 4: Implement WebSocket server**

Create `internal/bridge/wsserver.go`:

```go
package bridge

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WSMessage struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

type EventHandler func(event string, data json.RawMessage)

type WSServer struct {
	token        string
	conn         *websocket.Conn
	mu           sync.Mutex
	pending      *pendingMap
	upgrader     websocket.Upgrader
	onEvent      EventHandler
}

func NewWSServer(token string) *WSServer {
	return &WSServer{
		token:   token,
		pending: newPendingMap(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *WSServer) SetOnEvent(fn EventHandler) {
	s.onEvent = fn
}

func (s *WSServer) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

func (s *WSServer) HandleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade: %v", err)
		return
	}

	// Read first message (must be auth)
	var authMsg struct {
		Method string `json:"method"`
		Params struct {
			Token string `json:"token"`
		} `json:"params"`
	}
	if err := ws.ReadJSON(&authMsg); err != nil {
		ws.Close()
		return
	}

	if authMsg.Method != "auth" || authMsg.Params.Token != s.token {
		ws.WriteJSON(map[string]interface{}{
			"error": "authentication failed",
		})
		ws.Close()
		return
	}

	// Check single-connection policy
	s.mu.Lock()
	if s.conn != nil {
		s.mu.Unlock()
		ws.WriteJSON(map[string]interface{}{
			"error": "another extension already connected",
		})
		ws.Close()
		return
	}
	s.conn = ws
	s.mu.Unlock()

	ws.WriteJSON(map[string]interface{}{"result": "authenticated"})

	// Read loop
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
		ws.Close()
	}()

	for {
		var msg WSMessage
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}

		if msg.Method == "ping" {
			ws.WriteJSON(WSMessage{Method: "pong"})
			continue
		}

		// Extension event (tab.navigated, tab.closed, etc.)
		if msg.Event != "" {
			if s.onEvent != nil {
				s.onEvent(msg.Event, msg.Data)
			}
			continue
		}

		// Response to a pending request
		if msg.ID != "" {
			s.pending.Resolve(msg.ID, &msg)
		}
	}
}

func (s *WSServer) SendAndWait(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	s.mu.Lock()
	ws := s.conn
	s.mu.Unlock()

	if ws == nil {
		return nil, fmt.Errorf("extension not connected")
	}

	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	id, ch := s.pending.Create(timeout)

	s.mu.Lock()
	err = ws.WriteJSON(WSMessage{
		ID:     id,
		Method: method,
		Params: rawParams,
	})
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write ws: %w", err)
	}

	resp := <-ch
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Result, nil
}
```

- [ ] **Step 5: Install uuid dependency, run tests**

```bash
go get github.com/google/uuid
go test ./internal/bridge/ -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/ go.mod go.sum
git commit -m "feat: WebSocket server with token auth and pending request map"
```

---

### Task 7: Integrate WebSocket into Daemon + Token Generation

**Files:**
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Add token generation and WS server to daemon**

Add to `internal/daemon/daemon.go` — update the `Daemon` struct and `New`/`Start` functions:

```go
// Add to imports:
import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"github.com/joyy/chrome-pilot/internal/bridge"
)

// Update struct to include wsServer:
type Daemon struct {
	// ... existing fields ...
	wsServer  *bridge.WSServer
	tokenFile string
}

// In New(), after creating rpcServer:
token, err := generateToken()
if err != nil {
	return nil, fmt.Errorf("daemon: generate token: %w", err)
}
tokenFile := filepath.Join(dataDir, "token")
if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
	return nil, fmt.Errorf("daemon: write token: %w", err)
}
wsServer := bridge.NewWSServer(token)

// In Start(), after registering RPC handlers, before serveErr:
// Start WebSocket server
if d.cfg.WSPort > 0 {
	go func() {
		addr := fmt.Sprintf(":%d", d.cfg.WSPort)
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", d.wsServer.HandleWS)
		http.ListenAndServe(addr, mux)
	}()
}

// Update status handler to check ws connection:
d.rpcServer.Register("status", func(_ json.RawMessage) (interface{}, error) {
	extStatus := "not connected"
	if d.wsServer.IsConnected() {
		extStatus = "connected"
	}
	return map[string]interface{}{
		"daemon":    "running",
		"pid":       os.Getpid(),
		"extension": extStatus,
		"ws_port":   d.cfg.WSPort,
	}, nil
})
```

Add token generation helper:

```go
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 2: Build and verify**

```bash
go build -o chrome-pilot .
./chrome-pilot start
./chrome-pilot status
cat ~/.chrome-pilot/token
./chrome-pilot stop
```

Expected: status shows `"extension":"not connected"`, token file exists with 0600 permissions

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/
git commit -m "feat: integrate WebSocket server and token auth into daemon"
```

---

## Phase 3: Chrome Extension (Minimal Viable)

### Task 8: Extension — Offscreen Document + Background + Popup

**Files:**
- Create: `extension/manifest.json`
- Create: `extension/offscreen.html`
- Create: `extension/offscreen.js`
- Create: `extension/background.js`
- Create: `extension/popup/popup.html`
- Create: `extension/popup/popup.js`

- [ ] **Step 1: Create manifest.json**

Create `extension/manifest.json`:

```json
{
  "manifest_version": 3,
  "name": "Chrome Pilot",
  "version": "0.1.0",
  "description": "Bridge between Chrome and chrome-pilot daemon",
  "permissions": [
    "tabs",
    "activeTab",
    "scripting",
    "cookies",
    "debugger",
    "downloads",
    "offscreen",
    "alarms",
    "storage"
  ],
  "host_permissions": ["<all_urls>"],
  "background": {
    "service_worker": "background.js"
  },
  "action": {
    "default_popup": "popup/popup.html"
  }
}
```

- [ ] **Step 2: Create offscreen document (WebSocket client)**

Create `extension/offscreen.html`:

```html
<!DOCTYPE html>
<html><body><script src="offscreen.js"></script></body></html>
```

Create `extension/offscreen.js`:

```javascript
let ws = null;
let reconnectDelay = 1000;
const MAX_RECONNECT_DELAY = 30000;
const WS_URL = 'ws://localhost:9333/ws';

async function getToken() {
  return new Promise(resolve => {
    chrome.runtime.sendMessage({ type: 'get-token' }, (resp) => {
      resolve(resp?.token || '');
    });
  });
}

async function connect() {
  const token = await getToken();
  if (!token) {
    setTimeout(connect, reconnectDelay);
    return;
  }

  ws = new WebSocket(WS_URL);

  ws.onopen = () => {
    reconnectDelay = 1000;
    // Authenticate
    ws.send(JSON.stringify({ method: 'auth', params: { token } }));
  };

  ws.onmessage = async (event) => {
    const msg = JSON.parse(event.data);

    if (msg.result === 'authenticated') {
      chrome.runtime.sendMessage({ type: 'ws-status', connected: true });
      return;
    }

    if (msg.error === 'authentication failed') {
      chrome.runtime.sendMessage({ type: 'ws-status', connected: false, error: 'auth failed' });
      ws.close();
      return;
    }

    if (msg.method === 'pong') return;

    // Forward command to background for execution
    if (msg.id && msg.method) {
      try {
        const response = await chrome.runtime.sendMessage({
          type: 'execute-command',
          id: msg.id,
          method: msg.method,
          params: msg.params ? JSON.parse(JSON.stringify(msg.params)) : {}
        });
        ws.send(JSON.stringify({
          id: msg.id,
          result: response.result || null,
          error: response.error || null
        }));
      } catch (err) {
        ws.send(JSON.stringify({
          id: msg.id,
          error: err.message
        }));
      }
    }
  };

  ws.onclose = () => {
    ws = null;
    chrome.runtime.sendMessage({ type: 'ws-status', connected: false });
    setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY);
  };

  ws.onerror = () => {
    // onclose will fire after this
  };
}

// Heartbeat
setInterval(() => {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ method: 'ping' }));
  }
}, 20000);

connect();
```

- [ ] **Step 3: Create background service worker**

Create `extension/background.js`:

```javascript
let wsConnected = false;
let storedToken = '';

// Offscreen document management
async function ensureOffscreen() {
  const exists = await chrome.offscreen.hasDocument();
  if (!exists) {
    await chrome.offscreen.createDocument({
      url: 'offscreen.html',
      reasons: ['WORKERS'],
      justification: 'WebSocket connection to chrome-pilot daemon'
    });
  }
}

// Keep offscreen alive
chrome.alarms.create('check-offscreen', { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener(async (alarm) => {
  if (alarm.name === 'check-offscreen') {
    await ensureOffscreen();
  }
});

// Handle messages from offscreen and popup
chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.type === 'get-token') {
    sendResponse({ token: storedToken });
    return;
  }

  if (msg.type === 'set-token') {
    // Forward to offscreen
    return;
  }

  if (msg.type === 'ws-status') {
    wsConnected = msg.connected;
    return;
  }

  if (msg.type === 'get-status') {
    sendResponse({ connected: wsConnected });
    return;
  }

  if (msg.type === 'configure') {
    storedToken = msg.token || '';
    ensureOffscreen();
    return;
  }

  if (msg.type === 'execute-command') {
    handleCommand(msg).then(sendResponse);
    return true; // async response
  }
});

async function handleCommand(msg) {
  const { method, params } = msg;

  try {
    // Route by command category
    if (method.startsWith('tab.')) {
      return await handleTabCommand(method, params);
    }
    if (method.startsWith('cookie.')) {
      return await handleCookieCommand(method, params);
    }
    if (method === 'page.screenshot') {
      return await handleScreenshot(params);
    }
    // DOM and snapshot commands -> content script
    if (method.startsWith('dom.') || method.startsWith('snapshot') || method.startsWith('page.')) {
      return await handleContentScriptCommand(method, params);
    }
    return { error: `unknown method: ${method}` };
  } catch (err) {
    return { error: err.message };
  }
}

// Tab commands
async function handleTabCommand(method, params) {
  switch (method) {
    case 'tab.list': {
      const tabs = await chrome.tabs.query({});
      return { result: tabs.map((t, i) => ({
        index: i, id: t.id, title: t.title, url: t.url, active: t.active
      })) };
    }
    case 'tab.new': {
      const tab = await chrome.tabs.create({ url: params.url });
      return { result: { id: tab.id, url: tab.url } };
    }
    case 'tab.select': {
      await chrome.tabs.update(params.tabId, { active: true });
      return { result: { success: true } };
    }
    case 'tab.close': {
      await chrome.tabs.remove(params.tabId);
      return { result: { success: true } };
    }
    default:
      return { error: `unknown tab command: ${method}` };
  }
}

// Cookie commands
async function handleCookieCommand(method, params) {
  switch (method) {
    case 'cookie.list': {
      const cookies = await chrome.cookies.getAll(params.domain ? { domain: params.domain } : {});
      return { result: cookies.map(c => ({
        name: c.name, value: c.value, domain: c.domain, path: c.path
      })) };
    }
    case 'cookie.get': {
      const cookie = await chrome.cookies.get({ url: params.url, name: params.name });
      return { result: cookie };
    }
    default:
      return { error: `unknown cookie command: ${method}` };
  }
}

// Screenshot
async function handleScreenshot(params) {
  const tabId = params.tabId || (await getActiveTabId());
  const dataUrl = await chrome.tabs.captureVisibleTab(null, { format: 'png' });
  return { result: { dataUrl } };
}

// Content script injection — IMPORTANT: must be a single func: call
// because separate executeScript calls do NOT share scope
async function handleContentScriptCommand(method, params) {
  const tabId = params.tabId || (await getActiveTabId());

  try {
    // Single injection: pass method+params, content.js executeInPage is inlined via files[]
    // First inject content.js to define functions, then call via a combined approach
    const results = await chrome.scripting.executeScript({
      target: { tabId },
      func: contentScriptEntry,
      args: [method, params]
    });

    if (results && results[0]) {
      return results[0].result;
    }
    return { error: 'no result from content script' };
  } catch (e) {
    if (e.message.includes('Cannot access')) {
      return { error: 'cannot inject into chrome:// pages' };
    }
    return { error: e.message };
  }
}

async function getActiveTabId() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab?.id;
}

// This entire function is injected into the page as a single func: call.
// It contains all content script logic inline.
// The full implementation is in content.js — during build, this will be replaced
// with the content of content.js. For now, minimal placeholder:
function contentScriptEntry(method, params) {
  // This will be replaced with the full executeInPage from content.js
  // See Task 10 for the actual implementation
  if (method === 'page.content') {
    const format = params.format || 'text';
    if (format === 'html') return { result: document.documentElement.outerHTML };
    return { result: document.body.innerText };
  }
  return { error: `content script: unimplemented method: ${method}` };
}

// Tab change monitoring -> notify daemon to clear snapshots
chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
  if (changeInfo.url) {
    // Send event to offscreen -> daemon
    chrome.runtime.sendMessage({
      type: 'send-event',
      event: 'tab.navigated',
      data: { tabId, url: changeInfo.url }
    });
  }
});

chrome.tabs.onRemoved.addListener((tabId) => {
  chrome.runtime.sendMessage({
    type: 'send-event',
    event: 'tab.closed',
    data: { tabId }
  });
});

// Startup
ensureOffscreen();
```

- [ ] **Step 4: Create popup**

Create `extension/popup/popup.html`:

```html
<!DOCTYPE html>
<html>
<head>
  <style>
    body { width: 250px; padding: 12px; font-family: system-ui; font-size: 13px; }
    .status { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
    .dot { width: 10px; height: 10px; border-radius: 50%; }
    .dot.green { background: #22c55e; }
    .dot.red { background: #ef4444; }
    .dot.gray { background: #9ca3af; }
    label { display: block; margin-bottom: 4px; font-weight: 500; }
    input { width: 100%; padding: 4px 8px; box-sizing: border-box; }
    button { margin-top: 8px; padding: 4px 12px; }
  </style>
</head>
<body>
  <div class="status">
    <div class="dot" id="statusDot"></div>
    <span id="statusText">Checking...</span>
  </div>
  <label>Token</label>
  <input id="tokenInput" type="text" placeholder="Paste token from ~/.chrome-pilot/token" />
  <button id="saveBtn">Save & Connect</button>
  <script src="popup.js"></script>
</body>
</html>
```

Create `extension/popup/popup.js`:

```javascript
const dot = document.getElementById('statusDot');
const text = document.getElementById('statusText');
const tokenInput = document.getElementById('tokenInput');
const saveBtn = document.getElementById('saveBtn');

chrome.runtime.sendMessage({ type: 'get-status' }, (resp) => {
  if (resp?.connected) {
    dot.className = 'dot green';
    text.textContent = 'Connected';
  } else {
    dot.className = 'dot red';
    text.textContent = 'Disconnected';
  }
});

chrome.storage.local.get('token', (data) => {
  tokenInput.value = data.token || '';
});

saveBtn.addEventListener('click', () => {
  const token = tokenInput.value.trim();
  chrome.storage.local.set({ token });
  chrome.runtime.sendMessage({ type: 'configure', token });
  window.close();
});
```

- [ ] **Step 5: Load extension in Chrome and test connection**

1. Open `chrome://extensions`
2. Enable "Developer mode"
3. Click "Load unpacked" → select `extension/` directory
4. Start daemon: `./chrome-pilot start`
5. Copy token: `cat ~/.chrome-pilot/token`
6. Click extension popup → paste token → Save & Connect
7. Check status: `./chrome-pilot status`

Expected: `"extension":"connected"`

- [ ] **Step 6: Commit**

```bash
git add extension/
git commit -m "feat: Chrome Extension with offscreen WebSocket, background router, and popup"
```

---

## Phase 4: First End-to-End Flow — Tab Commands

### Task 9: CLI Tab Commands + Daemon Handlers

**Files:**
- Create: `cmd/tab.go`
- Create: `internal/daemon/handlers.go`

- [ ] **Step 1: Create daemon handler registration**

Create `internal/daemon/handlers.go`:

```go
package daemon

import (
	"encoding/json"
	"fmt"
	"time"
)

func (d *Daemon) registerHandlers() {
	d.rpcServer.Register("ping", func(_ json.RawMessage) (interface{}, error) {
		return "pong", nil
	})
	d.rpcServer.Register("status", d.handleStatus)
	d.rpcServer.Register("tab.list", d.forwardToExtension("tab.list"))
	d.rpcServer.Register("tab.new", d.forwardToExtension("tab.new"))
	d.rpcServer.Register("tab.select", d.forwardToExtension("tab.select"))
	d.rpcServer.Register("tab.close", d.forwardToExtension("tab.close"))
}

func (d *Daemon) handleStatus(_ json.RawMessage) (interface{}, error) {
	extStatus := "not connected"
	if d.wsServer.IsConnected() {
		extStatus = "connected"
	}
	return map[string]interface{}{
		"daemon":    "running",
		"pid":       fmt.Sprintf("%d", pid()),
		"extension": extStatus,
		"ws_port":   d.cfg.WSPort,
	}, nil
}

// forwardToExtension creates a handler that forwards the RPC call to the Extension via WebSocket
func (d *Daemon) forwardToExtension(method string) func(json.RawMessage) (interface{}, error) {
	return func(params json.RawMessage) (interface{}, error) {
		if !d.wsServer.IsConnected() {
			return nil, fmt.Errorf("extension not connected, check Chrome extension status")
		}
		result, err := d.wsServer.SendAndWait(method, json.RawMessage(params), 10*time.Second)
		if err != nil {
			return nil, err
		}
		// Unwrap: the extension returns {result: ...}, we need the inner value
		var wrapper struct {
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := json.Unmarshal(result, &wrapper); err != nil {
			// Not wrapped, return as-is
			return json.RawMessage(result), nil
		}
		if wrapper.Error != "" {
			return nil, fmt.Errorf("%s", wrapper.Error)
		}
		return json.RawMessage(wrapper.Result), nil
	}
}

func pid() int {
	return os.Getpid()
}
```

Update `daemon.go` `Start()` to call `d.registerHandlers()` instead of inline handler registration.

- [ ] **Step 2: Create tab CLI command**

Create `cmd/tab.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/sockutil"
	"github.com/spf13/cobra"
)

var tabCmd = &cobra.Command{
	Use:   "tab",
	Short: "Manage browser tabs",
}

var tabListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all open tabs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("tab.list", nil)
	},
}

var tabNewCmd = &cobra.Command{
	Use:   "new <url>",
	Short: "Open a new tab",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("tab.new", map[string]string{"url": args[0]})
	},
}

var tabSelectCmd = &cobra.Command{
	Use:   "select <tabId>",
	Short: "Switch to a tab by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("tab.select", map[string]string{"tabId": args[0]})
	},
}

var tabCloseCmd = &cobra.Command{
	Use:   "close [tabId]",
	Short: "Close a tab (default: current tab)",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{}
		if len(args) > 0 {
			params["tabId"] = args[0]
		}
		return callAndPrint("tab.close", params)
	},
}

func init() {
	tabCmd.AddCommand(tabListCmd, tabNewCmd, tabSelectCmd, tabCloseCmd)
	rootCmd.AddCommand(tabCmd)
}

// callAndPrint - shared helper for all CLI commands
func callAndPrint(method string, params interface{}) error {
	cfgPath, _ := config.ConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	result, err := sockutil.Call(cfg.SocketPath, method, params)
	if err != nil {
		errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Println(string(errJSON))
		return nil
	}
	fmt.Println(string(result))
	return nil
}
```

- [ ] **Step 3: Build and test end-to-end**

```bash
go build -o chrome-pilot .
./chrome-pilot stop  # ensure clean state
./chrome-pilot start
./chrome-pilot tab list
./chrome-pilot tab new "https://example.com"
./chrome-pilot tab list
./chrome-pilot stop
```

Expected: `tab list` returns JSON array of open Chrome tabs

- [ ] **Step 4: Commit**

```bash
git add cmd/tab.go internal/daemon/handlers.go
git commit -m "feat: tab commands with end-to-end daemon-extension flow"
```

---

## Phase 5: Snapshot Engine

### Task 10: Content Script — Accessibility Tree + Ref Generation

**Files:**
- Create: `extension/content.js`
- Modify: `extension/background.js` (use content.js)

- [ ] **Step 1: Create content script**

Create `extension/content.js` — this is the core snapshot engine that runs in the page:

```javascript
// content.js - injected into page via chrome.scripting.executeScript
// Receives (method, params) as arguments

function executeInPage(method, params) {
  // ===== SNAPSHOT =====
  if (method === 'snapshot') {
    return generateSnapshot(params);
  }

  // ===== DOM COMMANDS =====
  if (method === 'dom.click') {
    return domClick(params);
  }
  if (method === 'dom.type') {
    return domType(params);
  }
  if (method === 'dom.hover') {
    return domHover(params);
  }
  if (method === 'dom.key') {
    return domKey(params);
  }
  if (method === 'dom.select') {
    return domSelect(params);
  }
  if (method === 'dom.eval') {
    return domEval(params);
  }
  if (method === 'page.content') {
    const format = params.format || 'text';
    if (format === 'html') return { result: document.documentElement.outerHTML };
    return { result: document.body.innerText };
  }
  if (method === 'page.wait') {
    return pageWait(params);
  }

  return { error: `unimplemented: ${method}` };
}

// ===== SNAPSHOT GENERATION =====

const INTERACTABLE_ROLES = new Set([
  'button', 'link', 'textbox', 'checkbox', 'radio', 'combobox',
  'slider', 'switch', 'tab', 'menuitem', 'option', 'searchbox',
  'spinbutton', 'treeitem'
]);

const SEMANTIC_TAGS = {
  'A': 'link', 'BUTTON': 'button', 'INPUT': 'textbox', 'TEXTAREA': 'textbox',
  'SELECT': 'combobox', 'H1': 'heading', 'H2': 'heading', 'H3': 'heading',
  'H4': 'heading', 'H5': 'heading', 'H6': 'heading', 'NAV': 'navigation',
  'MAIN': 'main', 'ASIDE': 'complementary', 'HEADER': 'banner',
  'FOOTER': 'contentinfo', 'FORM': 'form', 'TABLE': 'table',
  'IMG': 'img', 'DIALOG': 'dialog'
};

let refCounter = 0;

function generateSnapshot(params) {
  refCounter = 0;
  const root = document.body;
  const tree = buildTree(root, 0, params.maxDepth || 50);
  const stats = { totalNodes: 0, interactable: 0 };
  countNodes(tree, stats);

  return {
    result: {
      tree,
      stats,
      url: location.href,
      title: document.title
    }
  };
}

function buildTree(el, depth, maxDepth) {
  if (depth > maxDepth) return null;
  if (!isVisible(el)) return null;

  const role = getRole(el);
  const name = getAccessibleName(el);
  const isInteractable = INTERACTABLE_ROLES.has(role) || el.isContentEditable;

  let ref = null;
  if (isInteractable && isVisible(el)) {
    ref = 'e' + (++refCounter);
    el.setAttribute('data-cp-ref', ref);
  }

  const children = [];
  for (const child of el.children) {
    const node = buildTree(child, depth + 1, maxDepth);
    if (node) children.push(node);
  }

  // Skip nodes with no semantic value and no ref
  if (!role && !ref && children.length === 0) return null;
  // Collapse: if no role/ref but has exactly one child, return child
  if (!role && !ref && children.length === 1) return children[0];

  const node = {};
  if (role) node.role = role;
  if (name) node.name = name;
  if (ref) node.ref = ref;
  if (children.length > 0) node.children = children;

  return node;
}

function getRole(el) {
  const ariaRole = el.getAttribute('role');
  if (ariaRole) return ariaRole;

  const tag = el.tagName;
  if (SEMANTIC_TAGS[tag]) {
    if (tag === 'INPUT') {
      const type = el.type || 'text';
      if (type === 'checkbox') return 'checkbox';
      if (type === 'radio') return 'radio';
      if (type === 'submit' || type === 'button') return 'button';
      return 'textbox';
    }
    return SEMANTIC_TAGS[tag];
  }

  // Check cursor pointer for clickable divs
  if (el.onclick || el.style.cursor === 'pointer') return 'button';

  return null;
}

function getAccessibleName(el) {
  const ariaLabel = el.getAttribute('aria-label');
  if (ariaLabel) return ariaLabel.slice(0, 100);

  const labelledBy = el.getAttribute('aria-labelledby');
  if (labelledBy) {
    const labelEl = document.getElementById(labelledBy);
    if (labelEl) return labelEl.textContent.trim().slice(0, 100);
  }

  if (el.tagName === 'IMG') return el.alt || '';
  if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
    return el.placeholder || el.name || '';
  }

  const text = el.textContent?.trim();
  if (text && text.length < 100 && el.children.length === 0) return text;

  return '';
}

function isVisible(el) {
  if (el.nodeType !== 1) return false;
  const style = getComputedStyle(el);
  if (style.display === 'none' || style.visibility === 'hidden') return false;
  if (el.getAttribute('aria-hidden') === 'true') return false;
  return true;
}

function countNodes(node, stats) {
  if (!node) return;
  stats.totalNodes++;
  if (node.ref) stats.interactable++;
  if (node.children) node.children.forEach(c => countNodes(c, stats));
}

// ===== DOM COMMANDS =====

function findByRef(ref) {
  const el = document.querySelector(`[data-cp-ref="${ref}"]`);
  if (!el) return { error: `ref ${ref} not found, run snapshot first` };
  return { element: el };
}

function domClick(params) {
  const { element, error } = findByRef(params.ref);
  if (error) return { error };

  const opts = {};
  if (params.button === 'right') opts.button = 2;
  if (params.button === 'middle') opts.button = 1;

  if (params.double) {
    element.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, ...opts }));
  } else {
    element.click();
  }
  return { result: { success: true } };
}

function domType(params) {
  const { element, error } = findByRef(params.ref);
  if (error) return { error };

  element.focus();
  if (params.slowly) {
    for (const char of params.text) {
      element.dispatchEvent(new KeyboardEvent('keydown', { key: char, bubbles: true }));
      element.dispatchEvent(new InputEvent('input', { data: char, inputType: 'insertText', bubbles: true }));
      element.dispatchEvent(new KeyboardEvent('keyup', { key: char, bubbles: true }));
    }
    // Set value directly as well for frameworks
    const nativeSetter = Object.getOwnPropertyDescriptor(
      Object.getPrototypeOf(element), 'value'
    )?.set;
    if (nativeSetter) {
      nativeSetter.call(element, element.value + params.text);
    }
  } else {
    const nativeSetter = Object.getOwnPropertyDescriptor(
      Object.getPrototypeOf(element), 'value'
    )?.set;
    if (nativeSetter) {
      nativeSetter.call(element, params.text);
    } else {
      element.value = params.text;
    }
    element.dispatchEvent(new Event('input', { bubbles: true }));
    element.dispatchEvent(new Event('change', { bubbles: true }));
  }

  if (params.submit) {
    const form = element.closest('form');
    if (form) form.submit();
    else element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  }

  return { result: { success: true } };
}

function domHover(params) {
  const { element, error } = findByRef(params.ref);
  if (error) return { error };
  element.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true }));
  element.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
  return { result: { success: true } };
}

function domKey(params) {
  const target = document.activeElement || document.body;
  target.dispatchEvent(new KeyboardEvent('keydown', { key: params.key, bubbles: true }));
  target.dispatchEvent(new KeyboardEvent('keyup', { key: params.key, bubbles: true }));
  return { result: { success: true } };
}

function domSelect(params) {
  const { element, error } = findByRef(params.ref);
  if (error) return { error };
  const values = Array.isArray(params.values) ? params.values : [params.values];
  for (const opt of element.options) {
    opt.selected = values.includes(opt.value) || values.includes(opt.text);
  }
  element.dispatchEvent(new Event('change', { bubbles: true }));
  return { result: { success: true } };
}

function domEval(params) {
  try {
    const fn = new Function('return (' + params.js + ')')();
    if (params.ref) {
      const { element, error } = findByRef(params.ref);
      if (error) return { error };
      const result = fn(element);
      return { result: JSON.parse(JSON.stringify(result)) };
    }
    const result = fn();
    return { result: JSON.parse(JSON.stringify(result)) };
  } catch (e) {
    return { error: e.message };
  }
}

async function pageWait(params) {
  const timeout = (params.time || 10) * 1000;
  const interval = 200;
  const deadline = Date.now() + timeout;

  if (params.time && !params.text && !params.textGone) {
    await new Promise(r => setTimeout(r, params.time * 1000));
    return { result: { success: true } };
  }

  while (Date.now() < deadline) {
    const bodyText = document.body.innerText;
    if (params.text && bodyText.includes(params.text)) {
      return { result: { found: true } };
    }
    if (params.textGone && !bodyText.includes(params.textGone)) {
      return { result: { gone: true } };
    }
    await new Promise(r => setTimeout(r, interval));
  }

  if (params.text) return { error: `timeout: text "${params.text}" not found` };
  if (params.textGone) return { error: `timeout: text "${params.textGone}" still present` };
  return { result: { success: true } };
}
```

- [ ] **Step 2: Update background.js to use content.js**

Replace the `contentScriptEntry` placeholder in `background.js` with the full `executeInPage` function from `content.js`. Since `chrome.scripting.executeScript` with `func:` requires a single self-contained function, the entire content script logic must be inlined as the `func` parameter.

In practice during development: copy the `executeInPage` function body from `content.js` into `contentScriptEntry` in `background.js`, or use a build step to concatenate them.

The `handleContentScriptCommand` in background.js already uses the single `func:` call pattern (set up in Task 8). Just replace `contentScriptEntry` with the full `executeInPage` from content.js.

- [ ] **Step 3: Test snapshot manually**

```bash
./chrome-pilot start
# Navigate Chrome to any page, then:
# (We'll add snapshot CLI in next task)
```

- [ ] **Step 4: Commit**

```bash
git add extension/content.js extension/background.js
git commit -m "feat: content script with snapshot generation, DOM commands, and ref system"
```

---

### Task 11: Snapshot Store in Daemon

**Files:**
- Create: `internal/snapshot/store.go`
- Create: `internal/snapshot/store_test.go`

- [ ] **Step 1: Write test**

Create `internal/snapshot/store_test.go`:

```go
package snapshot_test

import (
	"testing"

	"github.com/joyy/chrome-pilot/internal/snapshot"
)

func TestStoreSaveAndQuery(t *testing.T) {
	s := snapshot.NewStore()

	tree := &snapshot.Node{
		Role: "main",
		Children: []*snapshot.Node{
			{Role: "heading", Name: "Dashboard", Ref: "e1"},
			{Role: "button", Name: "Submit", Ref: "e2"},
			{Role: "textbox", Name: "Search", Ref: "e3"},
		},
	}

	s.Save(123, tree, "https://example.com", "Example")

	// Query by ref
	node := s.QueryRef(123, "e2")
	if node == nil || node.Name != "Submit" {
		t.Errorf("expected Submit, got %v", node)
	}

	// Query by role
	buttons := s.QueryRole(123, "button")
	if len(buttons) != 1 || buttons[0].Ref != "e2" {
		t.Errorf("expected 1 button, got %d", len(buttons))
	}

	// Search by text
	results := s.Search(123, "Sub")
	if len(results) != 1 || results[0].Ref != "e2" {
		t.Errorf("expected 1 search result, got %d", len(results))
	}

	// Clear
	s.Clear(123)
	node = s.QueryRef(123, "e2")
	if node != nil {
		t.Error("expected nil after clear")
	}
}

func TestStoreSummary(t *testing.T) {
	s := snapshot.NewStore()

	tree := &snapshot.Node{
		Role: "main",
		Children: []*snapshot.Node{
			{Role: "navigation", Name: "Nav", Ref: "e1", Children: []*snapshot.Node{
				{Role: "link", Name: "Home", Ref: "e2"},
				{Role: "link", Name: "About", Ref: "e3"},
			}},
			{Role: "heading", Name: "Welcome"},
			{Role: "button", Name: "Login", Ref: "e4"},
		},
	}

	s.Save(1, tree, "https://example.com", "Example")

	summary := s.Summary(1)
	if summary == nil {
		t.Fatal("expected summary")
	}
	if len(summary.Landmarks) != 1 || summary.Landmarks[0].Name != "Nav" {
		t.Errorf("unexpected landmarks: %v", summary.Landmarks)
	}
	if len(summary.Headings) != 1 || summary.Headings[0] != "Welcome" {
		t.Errorf("unexpected headings: %v", summary.Headings)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/snapshot/ -v
```

- [ ] **Step 3: Implement snapshot store**

Create `internal/snapshot/store.go`:

```go
package snapshot

import (
	"strings"
	"sync"
	"time"
)

type Node struct {
	Role     string  `json:"role,omitempty"`
	Name     string  `json:"name,omitempty"`
	Ref      string  `json:"ref,omitempty"`
	Children []*Node `json:"children,omitempty"`
}

type TabSnapshot struct {
	ID       string
	URL      string
	Title    string
	Tree     *Node
	Previous *Node
	Index    map[string]*Node // ref -> node
	LastUsed time.Time
}

type SummaryResult struct {
	SnapshotID  string          `json:"snapshotId"`
	URL         string          `json:"url"`
	Title       string          `json:"title"`
	Stats       Stats           `json:"stats"`
	Landmarks   []LandmarkInfo  `json:"landmarks"`
	Headings    []string        `json:"headings"`
}

type Stats struct {
	TotalNodes   int `json:"totalNodes"`
	Interactable int `json:"interactable"`
}

type LandmarkInfo struct {
	Role     string `json:"role"`
	Name     string `json:"name"`
	Ref      string `json:"ref,omitempty"`
	Children int    `json:"children"`
}

type Store struct {
	mu   sync.Mutex
	tabs map[int]*TabSnapshot
	seq  int
}

func NewStore() *Store {
	return &Store{tabs: make(map[int]*TabSnapshot)}
}

func (s *Store) Save(tabID int, tree *Node, url, title string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	id := "snap_" + itoa(s.seq)

	existing := s.tabs[tabID]
	var prev *Node
	if existing != nil {
		prev = existing.Tree
	}

	index := make(map[string]*Node)
	buildIndex(tree, index)

	s.tabs[tabID] = &TabSnapshot{
		ID:       id,
		URL:      url,
		Title:    title,
		Tree:     tree,
		Previous: prev,
		Index:    index,
		LastUsed: time.Now(),
	}

	return id
}

func (s *Store) Summary(tabID int) *SummaryResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.tabs[tabID]
	if snap == nil {
		return nil
	}
	snap.LastUsed = time.Now()

	stats := Stats{}
	countNodes(snap.Tree, &stats)

	landmarks := findLandmarks(snap.Tree)
	headings := findHeadings(snap.Tree)

	// Fallback: no landmarks -> top interactable elements
	if len(landmarks) == 0 {
		landmarks = fallbackLandmarks(snap.Tree)
	}

	return &SummaryResult{
		SnapshotID: snap.ID,
		URL:        snap.URL,
		Title:      snap.Title,
		Stats:      stats,
		Landmarks:  landmarks,
		Headings:   headings,
	}
}

func (s *Store) QueryRef(tabID int, ref string) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.tabs[tabID]
	if snap == nil {
		return nil
	}
	snap.LastUsed = time.Now()
	return snap.Index[ref]
}

func (s *Store) QueryRole(tabID int, role string) []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.tabs[tabID]
	if snap == nil {
		return nil
	}
	snap.LastUsed = time.Now()

	var results []*Node
	for _, n := range snap.Index {
		if n.Role == role {
			results = append(results, n)
		}
	}
	return results
}

func (s *Store) Search(tabID int, text string) []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.tabs[tabID]
	if snap == nil {
		return nil
	}
	snap.LastUsed = time.Now()

	text = strings.ToLower(text)
	var results []*Node
	searchTree(snap.Tree, text, &results)
	return results
}

func (s *Store) Subtree(tabID int, ref string, depth int) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.tabs[tabID]
	if snap == nil {
		return nil
	}
	snap.LastUsed = time.Now()

	node := snap.Index[ref]
	if node == nil {
		return nil
	}
	if depth > 0 {
		return pruneDepth(node, depth)
	}
	return node
}

func (s *Store) Clear(tabID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tabs, tabID)
}

func (s *Store) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabs = make(map[int]*TabSnapshot)
}

func (s *Store) QueryInteractable(tabID int) []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.tabs[tabID]
	if snap == nil {
		return nil
	}
	snap.LastUsed = time.Now()

	var results []*Node
	for _, n := range snap.Index {
		if n.Ref != "" {
			results = append(results, n)
		}
	}
	return results
}

func (s *Store) ExpireOlderThan(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, snap := range s.tabs {
		if now.Sub(snap.LastUsed) > d {
			delete(s.tabs, id)
		}
	}
}

// --- helpers ---

func buildIndex(n *Node, idx map[string]*Node) {
	if n == nil {
		return
	}
	if n.Ref != "" {
		idx[n.Ref] = n
	}
	for _, c := range n.Children {
		buildIndex(c, idx)
	}
}

func countNodes(n *Node, stats *Stats) {
	if n == nil {
		return
	}
	stats.TotalNodes++
	if n.Ref != "" {
		stats.Interactable++
	}
	for _, c := range n.Children {
		countNodes(c, stats)
	}
}

func findLandmarks(n *Node) []LandmarkInfo {
	landmarkRoles := map[string]bool{
		"navigation": true, "main": true, "complementary": true,
		"banner": true, "contentinfo": true, "form": true,
	}
	var results []LandmarkInfo
	var walk func(*Node)
	walk = func(node *Node) {
		if node == nil {
			return
		}
		if landmarkRoles[node.Role] {
			results = append(results, LandmarkInfo{
				Role: node.Role, Name: node.Name, Ref: node.Ref,
				Children: countChildren(node),
			})
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(n)
	return results
}

func findHeadings(n *Node) []string {
	var headings []string
	var walk func(*Node)
	walk = func(node *Node) {
		if node == nil {
			return
		}
		if node.Role == "heading" && node.Name != "" {
			headings = append(headings, node.Name)
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(n)
	return headings
}

func fallbackLandmarks(n *Node) []LandmarkInfo {
	var results []LandmarkInfo
	var walk func(*Node)
	walk = func(node *Node) {
		if node == nil || len(results) >= 20 {
			return
		}
		if node.Ref != "" {
			results = append(results, LandmarkInfo{
				Role: node.Role, Name: node.Name, Ref: node.Ref,
			})
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(n)
	return results
}

func countChildren(n *Node) int {
	count := 0
	var walk func(*Node)
	walk = func(node *Node) {
		count++
		for _, c := range node.Children {
			walk(c)
		}
	}
	for _, c := range n.Children {
		walk(c)
	}
	return count
}

func searchTree(n *Node, text string, results *[]*Node) {
	if n == nil {
		return
	}
	if strings.Contains(strings.ToLower(n.Name), text) && n.Ref != "" {
		*results = append(*results, n)
	}
	for _, c := range n.Children {
		searchTree(c, text, results)
	}
}

func pruneDepth(n *Node, depth int) *Node {
	if n == nil || depth < 0 {
		return nil
	}
	copy := &Node{Role: n.Role, Name: n.Name, Ref: n.Ref}
	if depth > 0 && n.Children != nil {
		for _, c := range n.Children {
			pruned := pruneDepth(c, depth-1)
			if pruned != nil {
				copy.Children = append(copy.Children, pruned)
			}
		}
	}
	return copy
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
```

Import `"strconv"` in the file header.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/snapshot/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/snapshot/
git commit -m "feat: snapshot store with summary, ref query, role filter, text search, and subtree expansion"
```

---

### Task 12: Snapshot CLI + Daemon Integration

**Files:**
- Create: `cmd/snapshot.go`
- Modify: `internal/daemon/handlers.go`

- [ ] **Step 1: Add snapshot handlers to daemon**

Add to `internal/daemon/handlers.go`:

```go
// In registerHandlers():
d.rpcServer.Register("snapshot", d.handleSnapshot)
d.rpcServer.Register("snapshot.query", d.handleSnapshotQuery)
d.rpcServer.Register("snapshot.info", d.handleSnapshotInfo)
d.rpcServer.Register("snapshot.clear", d.handleSnapshotClear)

func (d *Daemon) handleSnapshot(params json.RawMessage) (interface{}, error) {
	var p struct {
		TabID int `json:"tabId,omitempty"`
	}
	json.Unmarshal(params, &p)

	// Get snapshot from extension
	result, err := d.wsServer.SendAndWait("snapshot", params, 30*time.Second)
	if err != nil {
		return nil, err
	}

	// Parse tree from extension response
	var extResult struct {
		Result struct {
			Tree  *snapshot.Node `json:"tree"`
			Stats struct {
				TotalNodes   int `json:"totalNodes"`
				Interactable int `json:"interactable"`
			} `json:"stats"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"result"`
		Error string `json:"error"`
	}
	json.Unmarshal(result, &extResult)
	if extResult.Error != "" {
		return nil, fmt.Errorf("%s", extResult.Error)
	}

	tabID := p.TabID
	if tabID == 0 {
		tabID = d.session.WorkingTabID
	}

	// Save to store
	snapID := d.snapStore.Save(tabID, extResult.Result.Tree, extResult.Result.URL, extResult.Result.Title)
	d.session.WorkingTabID = tabID

	// Return summary
	summary := d.snapStore.Summary(tabID)
	summary.SnapshotID = snapID
	return summary, nil
}

func (d *Daemon) handleSnapshotQuery(params json.RawMessage) (interface{}, error) {
	var p struct {
		TabID        int    `json:"tabId,omitempty"`
		Ref          string `json:"ref,omitempty"`
		Depth        int    `json:"depth,omitempty"`
		Role         string `json:"role,omitempty"`
		Search       string `json:"search,omitempty"`
		Interactable bool   `json:"interactable,omitempty"`
	}
	json.Unmarshal(params, &p)

	tabID := p.TabID
	if tabID == 0 {
		tabID = d.session.WorkingTabID
	}

	if p.Ref != "" {
		node := d.snapStore.Subtree(tabID, p.Ref, p.Depth)
		if node == nil {
			return nil, fmt.Errorf("ref %s not found", p.Ref)
		}
		return node, nil
	}
	if p.Role != "" {
		return d.snapStore.QueryRole(tabID, p.Role), nil
	}
	if p.Search != "" {
		return d.snapStore.Search(tabID, p.Search), nil
	}
	if p.Interactable {
		return d.snapStore.QueryInteractable(tabID), nil
	}
	return nil, fmt.Errorf("specify --ref, --role, --search, or --interactable")
}
```

- [ ] **Step 2: Create snapshot CLI**

Create `cmd/snapshot.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	snapRef          string
	snapDepth        int
	snapRole         string
	snapSearch       string
	snapInteractable bool
	snapTabID        int
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Capture and query page accessibility snapshot",
	RunE:  runSnapshot,
}

var snapshotInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show snapshot cache status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("snapshot.info", nil)
	},
}

var snapshotClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear snapshot cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("snapshot.clear", map[string]int{"tabId": snapTabID})
	},
}

func init() {
	snapshotCmd.Flags().StringVar(&snapRef, "ref", "", "Expand subtree by ref")
	snapshotCmd.Flags().IntVar(&snapDepth, "depth", 0, "Limit expansion depth")
	snapshotCmd.Flags().StringVar(&snapRole, "role", "", "Filter by ARIA role")
	snapshotCmd.Flags().StringVar(&snapSearch, "search", "", "Search by text")
	snapshotCmd.Flags().BoolVar(&snapInteractable, "interactable", false, "Show only interactable elements")
	snapshotCmd.Flags().IntVar(&snapTabID, "tab", 0, "Target tab ID")

	snapshotCmd.AddCommand(snapshotInfoCmd, snapshotClearCmd)
	rootCmd.AddCommand(snapshotCmd)
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	// If query flags are set, query existing snapshot
	if snapRef != "" || snapRole != "" || snapSearch != "" || snapInteractable {
		params := map[string]interface{}{
			"ref":          snapRef,
			"depth":        snapDepth,
			"role":         snapRole,
			"search":       snapSearch,
			"interactable": snapInteractable,
			"tabId":        snapTabID,
		}
		return callAndPrint("snapshot.query", params)
	}

	// Otherwise, capture new snapshot
	params := map[string]interface{}{"tabId": snapTabID}
	return callAndPrint("snapshot", params)
}
```

- [ ] **Step 3: Build and test end-to-end**

```bash
go build -o chrome-pilot .
./chrome-pilot start
# Navigate Chrome to any page
./chrome-pilot snapshot
./chrome-pilot snapshot --role button
./chrome-pilot snapshot --search "Submit"
./chrome-pilot snapshot --ref e1
./chrome-pilot stop
```

Expected: `snapshot` returns summary JSON; query commands return filtered results

- [ ] **Step 4: Commit**

```bash
git add cmd/snapshot.go internal/daemon/handlers.go
git commit -m "feat: snapshot CLI with summary, ref expansion, role filter, and text search"
```

---

## Phase 6: DOM + Page Commands

### Task 13: DOM CLI Commands

**Files:**
- Create: `cmd/dom.go`
- Modify: `internal/daemon/handlers.go`

- [ ] **Step 1: Register DOM handlers in daemon**

Add to `registerHandlers()`:

```go
d.rpcServer.Register("dom.click", d.forwardToExtension("dom.click"))
d.rpcServer.Register("dom.type", d.forwardToExtension("dom.type"))
d.rpcServer.Register("dom.hover", d.forwardToExtension("dom.hover"))
d.rpcServer.Register("dom.drag", d.forwardToExtension("dom.drag"))
d.rpcServer.Register("dom.key", d.forwardToExtension("dom.key"))
d.rpcServer.Register("dom.select", d.forwardToExtension("dom.select"))
d.rpcServer.Register("dom.fill", d.forwardToExtension("dom.fill"))
d.rpcServer.Register("dom.eval", d.forwardToExtension("dom.eval"))
```

- [ ] **Step 2: Create DOM CLI commands**

Create `cmd/dom.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var domCmd = &cobra.Command{
	Use:   "dom",
	Short: "Interact with page DOM elements",
}

var (
	domRef     string
	domButton  string
	domDouble  bool
	domMod     string
	domText    string
	domSlowly  bool
	domSubmit  bool
	domStartRef string
	domEndRef   string
	domValues  string
	domFields  string
	domPaths   string
	domJS      string
	domTabID   int
)

var domClickCmd = &cobra.Command{
	Use:   "click",
	Short: "Click an element by ref",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.click", map[string]interface{}{
			"ref": domRef, "button": domButton, "double": domDouble, "tabId": domTabID,
		})
	},
}

var domTypeCmd = &cobra.Command{
	Use:   "type",
	Short: "Type text into an element",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.type", map[string]interface{}{
			"ref": domRef, "text": domText, "slowly": domSlowly, "submit": domSubmit, "tabId": domTabID,
		})
	},
}

var domHoverCmd = &cobra.Command{
	Use:   "hover",
	Short: "Hover over an element",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.hover", map[string]interface{}{"ref": domRef, "tabId": domTabID})
	},
}

var domDragCmd = &cobra.Command{
	Use:   "drag",
	Short: "Drag and drop between elements",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.drag", map[string]interface{}{
			"startRef": domStartRef, "endRef": domEndRef, "tabId": domTabID,
		})
	},
}

var domKeyCmd = &cobra.Command{
	Use:   "key <key>",
	Short: "Press a keyboard key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.key", map[string]interface{}{"key": args[0], "tabId": domTabID})
	},
}

var domSelectCmd = &cobra.Command{
	Use:   "select",
	Short: "Select option in dropdown",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.select", map[string]interface{}{
			"ref": domRef, "values": domValues, "tabId": domTabID,
		})
	},
}

var domFillCmd = &cobra.Command{
	Use:   "fill",
	Short: "Fill multiple form fields",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.fill", map[string]interface{}{"fields": domFields, "tabId": domTabID})
	},
}

var domEvalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Execute JavaScript in page",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("dom.eval", map[string]interface{}{
			"js": domJS, "ref": domRef, "tabId": domTabID,
		})
	},
}

func init() {
	// Click flags
	domClickCmd.Flags().StringVar(&domRef, "ref", "", "Element ref from snapshot")
	domClickCmd.Flags().StringVar(&domButton, "button", "left", "Mouse button (left|right|middle)")
	domClickCmd.Flags().BoolVar(&domDouble, "double", false, "Double click")
	domClickCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	// Type flags
	domTypeCmd.Flags().StringVar(&domRef, "ref", "", "Element ref")
	domTypeCmd.Flags().StringVar(&domText, "text", "", "Text to type")
	domTypeCmd.Flags().BoolVar(&domSlowly, "slowly", false, "Type one char at a time")
	domTypeCmd.Flags().BoolVar(&domSubmit, "submit", false, "Press Enter after typing")
	domTypeCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	// Hover
	domHoverCmd.Flags().StringVar(&domRef, "ref", "", "Element ref")
	domHoverCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	// Drag
	domDragCmd.Flags().StringVar(&domStartRef, "start-ref", "", "Source element ref")
	domDragCmd.Flags().StringVar(&domEndRef, "end-ref", "", "Target element ref")
	domDragCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	// Key
	domKeyCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	// Select
	domSelectCmd.Flags().StringVar(&domRef, "ref", "", "Element ref")
	domSelectCmd.Flags().StringVar(&domValues, "values", "", "Comma-separated values")
	domSelectCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	// Fill
	domFillCmd.Flags().StringVar(&domFields, "fields", "", "JSON array of field specs")
	domFillCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	// Eval
	domEvalCmd.Flags().StringVar(&domJS, "js", "", "JavaScript function to execute")
	domEvalCmd.Flags().StringVar(&domRef, "ref", "", "Element ref (optional)")
	domEvalCmd.Flags().IntVar(&domTabID, "tab", 0, "Target tab ID")

	domCmd.AddCommand(domClickCmd, domTypeCmd, domHoverCmd, domDragCmd,
		domKeyCmd, domSelectCmd, domFillCmd, domEvalCmd)
	rootCmd.AddCommand(domCmd)
}
```

- [ ] **Step 3: Build and test**

```bash
go build -o chrome-pilot .
./chrome-pilot start
./chrome-pilot snapshot  # get refs
./chrome-pilot dom click --ref e5
./chrome-pilot dom type --ref e3 --text "hello"
./chrome-pilot dom eval --js "() => document.title"
./chrome-pilot stop
```

- [ ] **Step 4: Commit**

```bash
git add cmd/dom.go internal/daemon/handlers.go
git commit -m "feat: DOM interaction commands (click, type, hover, drag, key, select, fill, eval)"
```

---

### Task 14: Page CLI Commands + Screenshot

**Files:**
- Create: `cmd/page.go`
- Create: `internal/tmpfile/manager.go`
- Modify: `internal/daemon/handlers.go`

- [ ] **Step 1: Create temp file manager**

Create `internal/tmpfile/manager.go`:

```go
package tmpfile

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Manager struct {
	dir    string
	maxAge time.Duration
}

func NewManager(dir string, maxAge time.Duration) (*Manager, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("tmpfile: create dir: %w", err)
	}
	return &Manager{dir: dir, maxAge: maxAge}, nil
}

func (m *Manager) SaveScreenshot(dataURL string) (string, error) {
	// Strip data:image/png;base64, prefix
	idx := strings.Index(dataURL, ",")
	if idx < 0 {
		return "", fmt.Errorf("invalid data URL")
	}
	data, err := base64.StdEncoding.DecodeString(dataURL[idx+1:])
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	filename := fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
	path := filepath.Join(m.dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}
	return path, nil
}

func (m *Manager) Clean(before time.Duration) (int, int64, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	cutoff := time.Now().Add(-before)
	deleted := 0
	var freed int64

	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(m.dir, e.Name())
			freed += info.Size()
			os.Remove(path)
			deleted++
		}
	}
	return deleted, freed, nil
}

func (m *Manager) DryRun(before time.Duration) (int, int64) {
	entries, _ := os.ReadDir(m.dir)
	cutoff := time.Now().Add(-before)
	count := 0
	var size int64
	for _, e := range entries {
		info, _ := e.Info()
		if info != nil && info.ModTime().Before(cutoff) {
			count++
			size += info.Size()
		}
	}
	return count, size
}

func (m *Manager) CleanAll() (int, int64, error) {
	return m.Clean(0)
}

func (m *Manager) AutoClean() {
	m.Clean(m.maxAge)
}
```

- [ ] **Step 2: Create page CLI commands**

Create `cmd/page.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Page-level operations",
}

var pageNavigateCmd = &cobra.Command{
	Use:   "navigate <url>",
	Short: "Navigate to URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.navigate", map[string]interface{}{"url": args[0], "tabId": pageTabID})
	},
}

var pageBackCmd = &cobra.Command{
	Use:   "back",
	Short: "Go back in history",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.back", map[string]interface{}{"tabId": pageTabID})
	},
}

var pageScreenshotCmd = &cobra.Command{
	Use:   "screenshot",
	Short: "Take a screenshot",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.screenshot", map[string]interface{}{
			"full": pageFull, "ref": pageRef, "file": pageFile, "tabId": pageTabID,
		})
	},
}

var pageWaitCmd = &cobra.Command{
	Use:   "wait",
	Short: "Wait for condition",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.wait", map[string]interface{}{
			"text": waitText, "textGone": waitTextGone, "time": waitTime, "tabId": pageTabID,
		})
	},
}

var pageConsoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Get console messages",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.console", map[string]interface{}{"level": consoleLevel, "tabId": pageTabID})
	},
}

var pageNetworkCmd = &cobra.Command{
	Use:   "network",
	Short: "Get network requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.network", map[string]interface{}{
			"includeStatic": netIncludeStatic, "tabId": pageTabID,
		})
	},
}

var pageContentCmd = &cobra.Command{
	Use:   "content",
	Short: "Get page content",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.content", map[string]interface{}{"format": contentFormat, "tabId": pageTabID})
	},
}

var pageResizeCmd = &cobra.Command{
	Use:   "resize",
	Short: "Resize browser window",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.resize", map[string]interface{}{
			"width": resizeW, "height": resizeH, "tabId": pageTabID,
		})
	},
}

var pageDialogCmd = &cobra.Command{
	Use:   "dialog",
	Short: "Handle browser dialog",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.dialog", map[string]interface{}{
			"accept": dialogAccept, "text": dialogText, "tabId": pageTabID,
		})
	},
}

var pageCloseCmd = &cobra.Command{
	Use:   "close",
	Short: "Close current page",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.close", map[string]interface{}{"tabId": pageTabID})
	},
}

var (
	pageTabID        int
	pageFull         bool
	pageRef          string
	pageFile         string
	waitText         string
	waitTextGone     string
	waitTime         int
	consoleLevel     string
	netIncludeStatic bool
	contentFormat    string
	resizeW, resizeH int
	dialogAccept     bool
	dialogText       string
)

func init() {
	pageScreenshotCmd.Flags().BoolVar(&pageFull, "full", false, "Full page screenshot")
	pageScreenshotCmd.Flags().StringVar(&pageRef, "ref", "", "Element ref for element screenshot")
	pageScreenshotCmd.Flags().StringVar(&pageFile, "file", "", "Output file path")

	pageWaitCmd.Flags().StringVar(&waitText, "text", "", "Wait for text to appear")
	pageWaitCmd.Flags().StringVar(&waitTextGone, "text-gone", "", "Wait for text to disappear")
	pageWaitCmd.Flags().IntVar(&waitTime, "time", 0, "Wait N seconds")

	pageConsoleCmd.Flags().StringVar(&consoleLevel, "level", "info", "Log level filter")
	pageNetworkCmd.Flags().BoolVar(&netIncludeStatic, "include-static", false, "Include static resources")
	pageContentCmd.Flags().StringVar(&contentFormat, "format", "text", "Output format (html|text)")

	pageResizeCmd.Flags().IntVar(&resizeW, "width", 0, "Window width")
	pageResizeCmd.Flags().IntVar(&resizeH, "height", 0, "Window height")

	pageDialogCmd.Flags().BoolVar(&dialogAccept, "accept", false, "Accept the dialog")
	pageDialogCmd.Flags().StringVar(&dialogText, "text", "", "Dialog prompt text")

	// Add --tab to all
	for _, c := range []*cobra.Command{pageNavigateCmd, pageBackCmd, pageScreenshotCmd,
		pageWaitCmd, pageConsoleCmd, pageNetworkCmd, pageContentCmd,
		pageResizeCmd, pageDialogCmd, pageCloseCmd} {
		c.Flags().IntVar(&pageTabID, "tab", 0, "Target tab ID")
	}

	pageCmd.AddCommand(pageNavigateCmd, pageBackCmd, pageScreenshotCmd,
		pageWaitCmd, pageConsoleCmd, pageNetworkCmd, pageContentCmd,
		pageResizeCmd, pageDialogCmd, pageCloseCmd)
	rootCmd.AddCommand(pageCmd)
}
```

- [ ] **Step 3: Add screenshot handler to daemon** (saves to file, returns path)

Add to `handlers.go`:

```go
d.rpcServer.Register("page.screenshot", d.handleScreenshot)

func (d *Daemon) handleScreenshot(params json.RawMessage) (interface{}, error) {
	result, err := d.wsServer.SendAndWait("page.screenshot", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var extResult struct {
		Result struct {
			DataUrl string `json:"dataUrl"`
		} `json:"result"`
	}
	json.Unmarshal(result, &extResult)

	path, err := d.tmpManager.SaveScreenshot(extResult.Result.DataUrl)
	if err != nil {
		return nil, err
	}
	return map[string]string{"path": path}, nil
}
```

Register remaining page commands as `forwardToExtension`.

- [ ] **Step 4: Build and test**

```bash
go build -o chrome-pilot .
./chrome-pilot start
./chrome-pilot page navigate "https://example.com"
./chrome-pilot page screenshot
./chrome-pilot page content
./chrome-pilot stop
```

- [ ] **Step 5: Commit**

```bash
git add cmd/page.go internal/tmpfile/ internal/daemon/handlers.go
git commit -m "feat: page commands (navigate, screenshot, content, wait, resize, dialog)"
```

---

## Phase 7: Cookie + Clean + Skill

### Task 15: Cookie CLI + Clean CLI

**Files:**
- Create: `cmd/cookie.go`
- Create: `cmd/clean.go`

- [ ] **Step 1: Create cookie CLI**

Create `cmd/cookie.go`:

```go
package cmd

import "github.com/spf13/cobra"

var cookieCmd = &cobra.Command{Use: "cookie", Short: "Manage cookies"}

var cookieDomain string

var cookieListCmd = &cobra.Command{
	Use: "list", Short: "List cookies",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("cookie.list", map[string]string{"domain": cookieDomain})
	},
}

var cookieGetCmd = &cobra.Command{
	Use: "get", Short: "Get a specific cookie",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		return callAndPrint("cookie.get", map[string]string{"name": name, "domain": cookieDomain})
	},
}

func init() {
	cookieListCmd.Flags().StringVar(&cookieDomain, "domain", "", "Filter by domain")
	cookieGetCmd.Flags().String("name", "", "Cookie name")
	cookieGetCmd.Flags().StringVar(&cookieDomain, "domain", "", "Cookie domain")
	cookieCmd.AddCommand(cookieListCmd, cookieGetCmd)
	rootCmd.AddCommand(cookieCmd)
}
```

- [ ] **Step 2: Create clean CLI**

Create `cmd/clean.go`:

```go
package cmd

import (
	"fmt"
	"time"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/tmpfile"
	"github.com/spf13/cobra"
)

var cleanBefore string
var cleanDryRun bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean temporary files",
	RunE: func(cmd *cobra.Command, args []string) error {
		dataDir, err := config.DataDir()
		if err != nil {
			return err
		}
		mgr, err := tmpfile.NewManager(dataDir+"/tmp", 24*time.Hour)
		if err != nil {
			return err
		}

		dur := 24 * time.Hour * 365 * 10 // default: clean all
		if cleanBefore != "" {
			dur, err = parseDuration(cleanBefore)
			if err != nil {
				return fmt.Errorf("invalid duration: %s", cleanBefore)
			}
		}

		if cleanDryRun {
			count, size := mgr.DryRun(dur)
			fmt.Printf(`{"count":%d,"size":"%s"}`+"\n", count, formatBytes(size))
			return nil
		}

		count, freed, err := mgr.Clean(dur)
		if err != nil {
			return err
		}
		fmt.Printf(`{"deleted":%d,"freed":"%s"}`+"\n", count, formatBytes(freed))
		return nil
	},
}

func init() {
	cleanCmd.Flags().StringVar(&cleanBefore, "before", "", "Clean files older than (e.g., 3d, 1h)")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview without deleting")
	rootCmd.AddCommand(cleanCmd)
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("too short")
	}
	unit := s[len(s)-1]
	val := s[:len(s)-1]
	var n int
	fmt.Sscanf(val, "%d", &n)
	switch unit {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return time.ParseDuration(s)
	}
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%dB", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
}
```

- [ ] **Step 3: Build and verify**

```bash
go build -o chrome-pilot .
./chrome-pilot clean --dry-run
./chrome-pilot cookie list --domain github.com
```

- [ ] **Step 4: Commit**

```bash
git add cmd/cookie.go cmd/clean.go
git commit -m "feat: cookie and clean CLI commands"
```

---

### Task 16: Skill File

**Files:**
- Create: `skills/chrome-pilot/SKILL.md`

- [ ] **Step 1: Create the skill**

Create `skills/chrome-pilot/SKILL.md` with the full command reference, workflow guidance, and error handling table from the spec document. Include:

1. Prerequisites (daemon + extension connection check)
2. Core workflow: `snapshot(summary) → expand/search → operate → snapshot(incremental)`
3. Complete command reference (all commands from Section 6 of spec)
4. Error handling table (from Section 10 of spec)

- [ ] **Step 2: Commit**

```bash
git add skills/
git commit -m "feat: chrome-pilot Claude Code skill"
```

---

## Phase 8: Hardening

### Task 17: Extension Tab Events → Daemon Snapshot Cleanup

**Files:**
- Modify: `extension/offscreen.js`
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Add event forwarding to offscreen.js**

In `offscreen.js`, handle `send-event` messages from background:

```javascript
chrome.runtime.onMessage.addListener((msg) => {
  if (msg.type === 'send-event' && ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({
      id: null,
      event: msg.event,
      data: msg.data
    }));
  }
});
```

- [ ] **Step 2: Handle events in daemon**

In `daemon.go`, after creating wsServer:

```go
d.wsServer.SetOnEvent(func(event string, data json.RawMessage) {
	var tabData struct {
		TabID int `json:"tabId"`
	}
	json.Unmarshal(data, &tabData)

	switch event {
	case "tab.navigated", "tab.closed":
		d.snapStore.Clear(tabData.TabID)
	}
})
```

- [ ] **Step 3: Test: navigate a tab, verify snapshot is cleared**

```bash
./chrome-pilot start
./chrome-pilot snapshot
# Navigate Chrome to a different page
./chrome-pilot snapshot info  # should show empty/new state
./chrome-pilot stop
```

- [ ] **Step 4: Commit**

```bash
git add extension/offscreen.js internal/daemon/daemon.go
git commit -m "feat: tab navigation events clear snapshot cache"
```

---

### Task 18: Snapshot Expiry Timer + Daemon Auto-clean on Start

**Files:**
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Add periodic snapshot expiry**

In daemon `Start()`, after resetIdleTimer:

```go
// Snapshot expiry: every 60s, expire snapshots older than 10min
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

// Auto-clean temp files on startup
d.tmpManager.AutoClean()
```

- [ ] **Step 2: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat: periodic snapshot expiry and auto-clean temp files on startup"
```

---

### Task 19: Final Integration Test

- [ ] **Step 1: Full end-to-end test**

```bash
go build -o chrome-pilot . && go install .
chrome-pilot start
chrome-pilot status
chrome-pilot tab list
chrome-pilot tab new "https://example.com"
chrome-pilot snapshot
chrome-pilot snapshot --ref e1
chrome-pilot dom click --ref e2
chrome-pilot snapshot  # should show incremental changes
chrome-pilot page screenshot
chrome-pilot dom eval --js "() => document.title"
chrome-pilot page content --format text
chrome-pilot cookie list
chrome-pilot snapshot clear
chrome-pilot clean --dry-run
chrome-pilot stop
```

Verify each command returns valid JSON and expected results.

- [ ] **Step 2: Run all Go tests**

```bash
cd /Users/JOYY/code/rsearch/chrome_pliot
go test ./... -v
```

Expected: All PASS

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "feat: chrome-pilot v0.1.0 - complete CLI + daemon + extension"
```
