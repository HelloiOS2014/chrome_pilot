package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSMessage is the envelope exchanged between the server and the
// Chrome extension over the WebSocket connection.
type WSMessage struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// EventHandler is called for unsolicited events pushed by the extension
// (e.g. "tab.navigated", "tab.closed").
type EventHandler func(event string, data json.RawMessage)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSServer manages a single WebSocket connection to the Chrome extension.
type WSServer struct {
	token   string
	pending *pendingMap
	onEvent EventHandler

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewWSServer creates a WSServer that requires the given token for auth.
func NewWSServer(token string) *WSServer {
	return &WSServer{
		token:   token,
		pending: newPendingMap(),
	}
}

// SetOnEvent registers a callback for extension-initiated events.
func (s *WSServer) SetOnEvent(fn EventHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = fn
}

// IsConnected reports whether an authenticated extension is connected.
func (s *WSServer) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

// HandleWS is the HTTP handler that upgrades the connection to WebSocket.
// Protocol:
//  1. The first message from the extension must be an auth message:
//     {"method":"auth","params":{"token":"<token>"}}
//  2. After auth succeeds the server enters a read loop handling:
//     - ping messages  (method == "ping")
//     - event messages (event != "")
//     - response messages (id != "", result or error present)
func (s *WSServer) HandleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("bridge: ws upgrade error: %v", err)
		return
	}

	// --- single-connection policy ---
	s.mu.Lock()
	if s.conn != nil {
		s.mu.Unlock()
		_ = ws.WriteJSON(&WSMessage{Error: "another extension already connected"})
		ws.Close()
		return
	}
	s.mu.Unlock()

	// --- auth handshake ---
	ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	var authMsg WSMessage
	if err := ws.ReadJSON(&authMsg); err != nil {
		log.Printf("bridge: auth read error: %v", err)
		ws.Close()
		return
	}
	ws.SetReadDeadline(time.Time{}) // clear deadline

	if authMsg.Method != "auth" {
		_ = ws.WriteJSON(&WSMessage{Error: "first message must be auth"})
		ws.Close()
		return
	}

	var authParams struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(authMsg.Params, &authParams); err != nil || authParams.Token != s.token {
		_ = ws.WriteJSON(&WSMessage{Error: "invalid token"})
		ws.Close()
		return
	}

	// Accept the connection.
	s.mu.Lock()
	s.conn = ws
	s.mu.Unlock()

	log.Printf("bridge: extension connected")
	_ = ws.WriteJSON(&WSMessage{Method: "auth", Result: json.RawMessage(`{"ok":true}`)})

	// --- read loop ---
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
		ws.Close()
		log.Printf("bridge: extension disconnected")
	}()

	for {
		var msg WSMessage
		if err := ws.ReadJSON(&msg); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("bridge: read error: %v", err)
			}
			return
		}

		switch {
		case msg.Method == "ping":
			_ = ws.WriteJSON(&WSMessage{Method: "pong"})

		case msg.Event != "":
			s.mu.Lock()
			handler := s.onEvent
			s.mu.Unlock()
			if handler != nil {
				handler(msg.Event, msg.Data)
			}

		case msg.ID != "":
			// Response to a command we sent.
			s.pending.Resolve(msg.ID, &msg)

		default:
			log.Printf("bridge: unhandled message: %+v", msg)
		}
	}
}

// SendAndWait sends a command to the extension and waits for the
// matching response, up to timeout. Returns the raw JSON result or
// an error (including timeout and extension-reported errors).
func (s *WSServer) SendAndWait(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()

	if conn == nil {
		return nil, errors.New("bridge: no extension connected")
	}

	id, ch := s.pending.Create(timeout)

	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("bridge: marshal params: %w", err)
	}

	msg := &WSMessage{
		ID:     id,
		Method: method,
		Params: json.RawMessage(rawParams),
	}

	s.mu.Lock()
	err = conn.WriteJSON(msg)
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("bridge: send: %w", err)
	}

	resp := <-ch
	if resp == nil {
		return nil, fmt.Errorf("bridge: timeout waiting for %s", method)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("bridge: extension error: %s", resp.Error)
	}
	return resp.Result, nil
}
