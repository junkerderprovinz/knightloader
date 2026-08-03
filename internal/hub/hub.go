// Package hub is a minimal WebSocket fan-out: the app broadcasts task updates,
// every connected UI receives them live.
package hub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Hub struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func New() *Hub { return &Hub{conns: map[*websocket.Conn]struct{}{}} }

func (h *Hub) Add(c *websocket.Conn)    { h.mu.Lock(); h.conns[c] = struct{}{}; h.mu.Unlock() }
func (h *Hub) Remove(c *websocket.Conn) { h.mu.Lock(); delete(h.conns, c); h.mu.Unlock() }

// Send writes one {type,data} message to a single connection.
func Send(ctx context.Context, c *websocket.Conn, typ string, data any) error {
	msg, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, msg)
}

// Broadcast sends a {type,data} message to every connected client.
func (h *Hub) Broadcast(typ string, data any) {
	msg, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return
	}
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
			c.CloseNow()
			h.Remove(c)
		}
		cancel()
	}
}
