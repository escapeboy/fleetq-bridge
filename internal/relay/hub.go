package relay

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/fleetq/fleetq-bridge/internal/tunnel"
)

// Conn represents an active bridge daemon connection.
type Conn struct {
	TeamID    string
	SessionID string // assigned by Laravel on registration
	apiKey    string
	ws        *websocket.Conn
	send      chan []byte
	cancel    context.CancelFunc
}

// SendFrame encodes a frame and queues it for delivery.
// Uses a 2s blocking timeout so heartbeat acks are not silently dropped when
// the writeLoop is momentarily busy (e.g. writing a large agent request frame).
// Returns an error only after the timeout, allowing callers to decide whether
// to close the connection.
func (c *Conn) SendFrame(frame *tunnel.Frame) error {
	var buf bytes.Buffer
	if err := frame.Encode(&buf); err != nil {
		return err
	}
	select {
	case c.send <- buf.Bytes():
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("send buffer full for team %s", c.TeamID)
	}
}

// Hub maintains the registry of active bridge daemon connections.
type Hub struct {
	mu    sync.RWMutex
	conns map[string]*Conn // team_id → conn
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{conns: make(map[string]*Conn)}
}

// Register adds a connection. Any previous connection for the same team is closed.
func (h *Hub) Register(conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if old, ok := h.conns[conn.TeamID]; ok {
		old.cancel()
	}
	h.conns[conn.TeamID] = conn
}

// Unregister removes a connection (only if it matches the given pointer).
func (h *Hub) Unregister(conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if current, ok := h.conns[conn.TeamID]; ok && current == conn {
		delete(h.conns, conn.TeamID)
	}
}

// Get returns the active connection for a team, if any.
func (h *Hub) Get(teamID string) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.conns[teamID]
	return c, ok
}

// Count returns the number of active connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}
