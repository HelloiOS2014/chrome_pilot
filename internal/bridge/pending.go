package bridge

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// pendingMap manages in-flight request/response correlations.
// Each pending entry is identified by a UUID and holds a channel
// on which the matching response (or timeout signal) will arrive.
type pendingMap struct {
	mu      sync.Mutex
	entries map[string]chan *WSMessage
}

func newPendingMap() *pendingMap {
	return &pendingMap{
		entries: make(map[string]chan *WSMessage),
	}
}

// Create allocates a new pending slot. It returns the request ID and a
// receive-only channel. The channel will receive exactly one message:
// either the matched response or a nil (on timeout).
func (p *pendingMap) Create(timeout time.Duration) (string, <-chan *WSMessage) {
	id := uuid.New().String()
	ch := make(chan *WSMessage, 1)

	p.mu.Lock()
	p.entries[id] = ch
	p.mu.Unlock()

	go func() {
		time.Sleep(timeout)
		p.mu.Lock()
		if _, ok := p.entries[id]; ok {
			delete(p.entries, id)
			ch <- nil // signal timeout
		}
		p.mu.Unlock()
	}()

	return id, ch
}

// Resolve delivers msg to the pending slot identified by id.
// Returns true if the slot existed and was resolved, false otherwise.
func (p *pendingMap) Resolve(id string, msg *WSMessage) bool {
	p.mu.Lock()
	ch, ok := p.entries[id]
	if ok {
		delete(p.entries, id)
	}
	p.mu.Unlock()

	if !ok {
		return false
	}
	ch <- msg
	return true
}
