// Package hub is a minimal WebSocket fan-out: the app broadcasts task updates,
// every connected UI receives them live.
//
// Every connection owns a bounded queue and a writer goroutine. Broadcast only
// ever hands a message to those queues, so a viewer on a bad link cannot add its
// write timeout to the updates everybody else is waiting for.
package hub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// queueDepth is how many messages may sit unwritten for one connection. Progress
// events arrive roughly every 500 ms per running task, so this is a few seconds
// of slack for a client that briefly hiccups, while still capping how much
// memory a single bad connection can pin.
const queueDepth = 64

// writeTimeout bounds one frame write. It can only ever delay the connection it
// belongs to, because each connection is written by its own goroutine, so it
// never reaches the broadcast path.
const writeTimeout = 5 * time.Second

// Conn is the part of *websocket.Conn the hub actually uses. It is an interface
// so tests can register a connection that blocks or fails on demand; the dynamic
// type has to be comparable, since connections are used as map keys.
type Conn interface {
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
	CloseNow() error
}

// client is one connection plus the queue its writer goroutine drains.
type client struct {
	conn Conn
	send chan []byte
	// quit is closed exactly once, by stop, to end the writer goroutine. The
	// send channel is never closed: Broadcast pushes into it without holding
	// the hub lock, so closing it would race a live send.
	quit chan struct{}
	once sync.Once

	// subs is the set of Broadcast kinds this connection wants, nil until it
	// asks for a narrower set, which means everything.
	//
	// Read and written only under the owning Hub's mu, same as the map that
	// holds this client, so wants must not be called without mu held.
	subs map[string]bool

	// visible is true while the viewer reports it can see the connection's
	// page, see SetVisible. Guarded by the Hub's mu like subs.
	visible bool
}

// stop ends the writer goroutine. It is idempotent because both Remove and a
// failed write can reach it for the same client.
func (cl *client) stop() { cl.once.Do(func() { close(cl.quit) }) }

// wants reports whether typ should reach this connection. Caller holds mu.
func (cl *client) wants(typ string) bool {
	return cl.subs == nil || cl.subs[typ]
}

// Hub is the set of connected UIs.
type Hub struct {
	mu      sync.Mutex
	clients map[Conn]*client
	// lastSeen is when each kind last had a watcher that is not connected
	// any more: a socket that dropped, or a client that polls (see Seen).
	lastSeen map[string]time.Time
}

// New returns an empty Hub.
func New() *Hub { return &Hub{clients: map[Conn]*client{}, lastSeen: map[string]time.Time{}} }

// Add registers a connection and starts the goroutine that writes to it.
// Registering the same connection twice is a no-op.
func (h *Hub) Add(c Conn) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		h.mu.Unlock()
		return
	}
	cl := &client{
		conn: c,
		send: make(chan []byte, queueDepth),
		quit: make(chan struct{}),
	}
	h.clients[c] = cl
	h.mu.Unlock()
	go h.writeLoop(cl)
}

// Remove unregisters a connection and stops its writer, which also closes the
// socket. It is safe for a connection that was never added and safe to call
// twice, so callers can put it in a defer without bookkeeping.
func (h *Hub) Remove(c Conn) {
	h.mu.Lock()
	cl := h.clients[c]
	delete(h.clients, c)
	if cl != nil && cl.visible {
		now := time.Now()
		for typ := range cl.subs {
			h.lastSeen[typ] = now
		}
	}
	h.mu.Unlock()
	if cl != nil {
		cl.stop()
	}
}

// Len reports how many connections are currently registered.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Send writes one {type,data} message to a single connection, bypassing the
// hub. It is for connections the hub does not own yet; for a registered
// connection prefer SendTo, which keeps the message in order with broadcasts.
func Send(ctx context.Context, c Conn, typ string, data any) error {
	msg, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, msg)
}

// SendTo queues one {type,data} message for a single registered connection. It
// reports false if the connection is not registered, or if its queue was full,
// in which case the client is dropped exactly as it would be by Broadcast.
func (h *Hub) SendTo(c Conn, typ string, data any) bool {
	msg, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return false
	}
	h.mu.Lock()
	cl := h.clients[c]
	h.mu.Unlock()
	if cl == nil {
		return false
	}
	return h.enqueue(cl, msg)
}

// Broadcast queues a {type,data} message for every connected client whose own
// subscription (see Subscribe) admits typ: everyone, for a connection that
// never narrowed itself. It never waits for a write, so the cost of a
// broadcast does not depend on the slowest client connected.
func (h *Hub) Broadcast(typ string, data any) {
	msg, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return
	}
	h.mu.Lock()
	// wants is decided in here, under the same lock subs is written under,
	// rather than after Unlock below: a client slice built first and
	// filtered afterwards would read subs unsynchronized the moment a
	// concurrent Subscribe/Unsubscribe is written to it.
	clients := make([]*client, 0, len(h.clients))
	for _, cl := range h.clients {
		if cl.wants(typ) {
			clients = append(clients, cl)
		}
	}
	h.mu.Unlock()
	// The marshalled message is shared by every queue; nothing writes to it
	// after this point, so one copy is enough. enqueue runs outside mu because
	// a full queue makes it call Remove, which takes mu again.
	for _, cl := range clients {
		h.enqueue(cl, msg)
	}
}

// Subscribe narrows which Broadcast kinds a connection receives from then on.
//
// The first call narrows a connection from "everything" to the named kinds; a
// later call adds to the set already in force, so two calls compose rather
// than the second replacing the first. A kind of "*" resets to "everything",
// which is the way out of a narrowed stream without enumerating every kind
// this build knows.
//
// Subscribing an unregistered connection is a no-op: there is no client entry
// for a set to live on.
func (h *Hub) Subscribe(c Conn, kinds []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cl := h.clients[c]
	if cl == nil {
		return
	}
	for _, k := range kinds {
		if k == "*" {
			cl.subs = nil
			return
		}
	}
	if cl.subs == nil {
		cl.subs = map[string]bool{}
	}
	for _, k := range kinds {
		cl.subs[k] = true
	}
}

// Unsubscribe removes kinds from a connection's own allowlist.
//
// It is a no-op on a connection that never called Subscribe: there is no
// allowlist to remove from, and "everything except these" would need a
// blocklist this package does not have. Subscribe with the kinds wanted
// instead.
func (h *Hub) Unsubscribe(c Conn, kinds []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cl := h.clients[c]
	if cl == nil || cl.subs == nil {
		return
	}
	for _, k := range kinds {
		delete(cl.subs, k)
	}
}

// SetVisible records whether the viewer behind a connection can see it: a
// browser tab in the foreground or a window on screen reports true, the same
// tab in the background or the window minimised false. A connection that
// never reports does not count as a viewer.
func (h *Hub) SetVisible(c Conn, visible bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cl := h.clients[c]; cl != nil {
		cl.visible = visible
	}
}

// Seen records a viewer of typ that holds no connection, such as an app that
// polls over HTTP. It counts for Watched like a socket that dropped just now.
func (h *Hub) Seen(typ string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastSeen[typ] = time.Now()
}

// Watched reports whether somebody watches typ: a connection that asked for
// typ by name and reports its page on screen, or a viewer gone less than grace
// ago, since a dropped socket may be reconnecting and a polling client is seen
// only now and then.
func (h *Hub) Watched(typ string, grace time.Duration) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, cl := range h.clients {
		if cl.subs[typ] && cl.visible {
			return true
		}
	}
	at, ok := h.lastSeen[typ]
	return ok && time.Since(at) < grace
}

// enqueue hands one message to a client, or drops the client if it cannot keep
// up. It reports whether the message was queued.
func (h *Hub) enqueue(cl *client, msg []byte) bool {
	select {
	case cl.send <- msg:
		return true
	default:
		// The queue is full, so this client is not draining events as fast as
		// the app produces them. Drop it rather than wait: a dropped viewer
		// heals itself, because the UI reconnects and asks for a fresh
		// snapshot, whereas a hub stalled behind one bad link stops updating
		// every other viewer as well.
		h.Remove(cl.conn)
		return false
	}
}

// writeLoop is the only goroutine that writes to a connection, which is what
// keeps a slow link off the broadcast path.
func (h *Hub) writeLoop(cl *client) {
	// The writer owns the socket: whatever ends the loop closes it too, so a
	// dropped client is torn down without the broadcaster ever waiting on a
	// close handshake.
	defer func() { _ = cl.conn.CloseNow() }()
	for {
		// A stopped client must not keep draining a queue somebody filled just
		// before the drop. The two-case select below would pick a ready send
		// about half the time, so quit gets checked on its own first.
		select {
		case <-cl.quit:
			return
		default:
		}
		select {
		case <-cl.quit:
			return
		case msg := <-cl.send:
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			err := cl.conn.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				// The socket is gone or wedged past the timeout; unregister so
				// later broadcasts stop queueing for a client nobody drains.
				h.Remove(cl.conn)
				return
			}
		}
	}
}
