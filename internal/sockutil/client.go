package sockutil

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// RPCCallError is returned when the server responds with a JSON-RPC error object.
type RPCCallError struct {
	Code    int
	Message string
}

func (e *RPCCallError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Call connects to the daemon Unix socket at socketPath, sends a JSON-RPC 2.0
// request for the given method and params, and returns the raw result JSON.
// The entire round-trip is bounded by a 30-second deadline.
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
		rawParams, _ = json.Marshal(params)
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

// Internal types mirror internal/rpc protocol types to avoid an import cycle.

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
