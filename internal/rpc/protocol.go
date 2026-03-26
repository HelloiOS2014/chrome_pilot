package rpc

import "encoding/json"

// Request represents a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      int             `json:"id"`
}

// Response represents a JSON-RPC 2.0 response message.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	ErrParseError     = -32700
	ErrMethodNotFound = -32601
	ErrInternalError  = -32000
)

// HandlerFunc is a function that handles a JSON-RPC method call.
// It receives the raw JSON params and returns a result or an error.
type HandlerFunc func(params json.RawMessage) (interface{}, error)
