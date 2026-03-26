package rpc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// Server is a JSON-RPC 2.0 server that listens on a Unix socket.
type Server struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]HandlerFunc
	mu         sync.RWMutex
	done       chan struct{}
	wg         sync.WaitGroup
	onActivity func()
}

// NewServer creates a new Server that will listen on the given Unix socket path.
func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]HandlerFunc),
		done:       make(chan struct{}),
	}
}

// Register adds a handler for the given method name.
// This method is thread-safe.
func (s *Server) Register(method string, handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
}

// SetOnActivity sets a callback that is invoked on each incoming RPC request.
// This can be used to reset an idle timer.
func (s *Server) SetOnActivity(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onActivity = fn
}

// Serve begins listening on the Unix socket and dispatching incoming connections.
// It removes any existing socket file before binding.
// Serve blocks until Stop is called or an unrecoverable error occurs.
func (s *Server) Serve() error {
	// Remove stale socket file if it exists.
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
				// Server was stopped; not an error.
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

// Stop closes the listener and waits up to 10 seconds for in-flight connections
// to finish before returning.
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

	// Wait for in-flight connections with a 10-second timeout.
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

// handleConn reads a single JSON-RPC request from conn, dispatches it, writes
// the response, and closes the connection.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	// Notify activity callback if set.
	s.mu.RLock()
	onActivity := s.onActivity
	s.mu.RUnlock()
	if onActivity != nil {
		onActivity()
	}

	var req Request
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&req); err != nil {
		// Cannot decode request; write a parse-error response with id=0.
		writeResponse(conn, Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ErrParseError,
				Message: "parse error: " + err.Error(),
			},
			ID: 0,
		})
		return
	}

	result, rpcErr := s.dispatch(req.Method, req.Params)

	resp := Response{
		JSONRPC: "2.0",
		ID:      req.ID,
	}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}

	writeResponse(conn, resp)
}

// dispatch looks up and calls the handler for the given method.
// Returns the raw JSON result or an RPCError.
func (s *Server) dispatch(method string, params json.RawMessage) (json.RawMessage, *RPCError) {
	s.mu.RLock()
	handler, ok := s.handlers[method]
	s.mu.RUnlock()

	if !ok {
		return nil, &RPCError{
			Code:    ErrMethodNotFound,
			Message: "method not found: " + method,
		}
	}

	result, err := handler(params)
	if err != nil {
		return nil, &RPCError{
			Code:    ErrInternalError,
			Message: err.Error(),
		}
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return nil, &RPCError{
			Code:    ErrInternalError,
			Message: "failed to marshal result: " + err.Error(),
		}
	}

	return raw, nil
}

// writeResponse encodes and writes a Response to w using a json.Encoder.
func writeResponse(w net.Conn, resp Response) {
	enc := json.NewEncoder(w)
	_ = enc.Encode(resp)
}
