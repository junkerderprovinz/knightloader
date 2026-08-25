// Package relay routes messages between KnightLoader instances that present
// the same relay key, so two instances behind NAT - neither of which can
// accept an inbound connection - can still reach each other by both dialling
// out to one place they can both reach.
//
// Possession of the key is the entire authorization model: the relay has no
// account list, no registration step and no database to check a key against.
// It groups whatever connections present the same string and knows nothing
// else about them. State is purely in memory, so a restart costs nothing but
// the current connection list, which rebuilds itself as instances reconnect.
//
// Every connection owns a bounded queue and a writer goroutine, the same
// shape internal/hub uses for the same reason: one instance on a bad link
// must not add its write timeout to every message the others are waiting for.
// Hub itself is not reused here because it holds one flat set of clients,
// while the whole job of this package is keeping key-grouped sets apart.
//
// Because "possession of the key" is the only credential, everything in this
// file that bounds a number (a queue depth, a connection count, an in-flight
// request count, a minimum key length) exists so that a connection which
// merely knows *some* key - guessed, leaked, or its own invented one, since
// nothing here validates a key against anything - cannot cost the relay, or
// a sibling sharing that key, more than a fixed, small amount. None of these
// are a complete defense against a determined attacker; they are a floor.
package relay

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// queueDepth is how many frames may sit unwritten for one connection. Relay
// traffic is request/response rather than a progress stream, so this is far
// more slack than a healthy instance ever uses - it is sized for a burst of
// answers arriving while one connection briefly stalls, not for steady load.
const queueDepth = 64

// writeTimeout bounds one frame write. It can only ever delay the connection
// it belongs to, because each connection is written by its own goroutine.
const writeTimeout = 5 * time.Second

// helloTimeout bounds how long a connection may stay open without having
// said who it is. Without it, anything that opens a socket and then goes
// quiet would hold a goroutine and a queue forever.
const helloTimeout = 10 * time.Second

// readLimit caps one inbound frame. The default 32 KiB is too small for the
// REST payloads this carries (a task list from a busy instance), and file
// bytes never travel this channel at all, so a few megabytes is generous
// without letting one client pin unbounded memory.
const readLimit = 8 << 20

// pendingTTL bounds how long the relay remembers who asked a question that
// was never answered. The requester has its own timeout; this only keeps the
// routing table from growing for the lifetime of the process when a target
// accepts a request and then dies without replying.
const pendingTTL = 2 * time.Minute

// minKeyLength is the shortest relay key the relay accepts. This is not
// entropy enforcement - the relay cannot tell "sixteen random characters"
// from "sixteen predictable ones" - it only rules out the laziest keys
// (single words, short PINs) at zero cost to a caller using a real one, the
// same way a minimum password length rules out nothing sophisticated but
// still removes the worst of the distribution.
const minKeyLength = 16

// maxPendingPerSender caps how many of one connection's own proxy-requests
// may be unanswered at once, per relay key. Without this, a connection that
// simply sends requests faster than its target answers them can pile frames
// into that target's own send queue until enqueue's overflow policy evicts
// the TARGET - punishing the side being flooded, not the side flooding it.
// Refusing the sender once it is already waiting on this many answers stops
// that pile-up before a single frame reaches the target's queue because of
// it, while a normal instance issuing a page's worth of concurrent calls
// never comes close.
const maxPendingPerSender = 32

// maxClientsPerKey bounds one key's own group size. A relay is meant to
// connect one person's own instances - a handful, generously a few dozen -
// so this is sized to never bother a real deployment while still capping how
// much registration state one guessed or leaked key can cause the relay to
// hold.
const maxClientsPerKey = 64

// maxTotalClients bounds every key combined, so an attacker cannot get
// around the per-key cap by inventing many different keys - each new,
// never-seen key costs the relay a fresh empty group otherwise, and nothing
// about this protocol requires a real deployment to ever approach this
// number.
const maxTotalClients = 2048

// Conn is the part of *websocket.Conn the registry actually uses. It is an
// interface so the registry can be tested without real sockets; the dynamic
// type has to be comparable, since connections are used as map keys.
type Conn interface {
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
	CloseNow() error
}

// client is one connection plus the queue its writer goroutine drains.
type client struct {
	conn     Conn
	key      string
	announce Announce
	send     chan []byte
	// quit is closed exactly once, by stop, to end the writer goroutine. The
	// send channel is deliberately never closed: frames are pushed into it
	// without holding the server lock, so closing it would race a live send.
	quit chan struct{}
	once sync.Once
}

// stop ends the writer goroutine, which also closes the socket. It is
// idempotent because a failed write, a dropped queue and a Leave can all
// reach it for the same client.
func (cl *client) stop() { cl.once.Do(func() { close(cl.quit) }) }

// pending is one proxy-request in flight: who asked, who it was routed to,
// and when the relay stops caring. target is what routeResponse verifies an
// incoming answer's sender against - without it, any connection that learns
// or guesses a live request ID could hand the requester a forged answer.
type pending struct {
	from    Conn
	target  Conn
	expires time.Time
}

// pendingFailure is one request whose target left before answering it -
// through Leave or through Join replacing a stale connection - along with
// who is still waiting on it, so the caller can be told the truth
// immediately instead of learning it by timing out.
type pendingFailure struct {
	requestID string
	from      Conn
}

// Server is the whole relay: the key-grouped connection registry plus the
// WebSocket endpoint that fills it.
//
// pending is itself grouped by key, not one flat map: a request can only
// ever be routed between two connections that share a key in the first
// place, so scoping it the same way keeps one key's own traffic - however
// much of it - from costing every other key a bigger table to search and
// sweep under the same lock.
type Server struct {
	mu      sync.Mutex
	clients map[Conn]*client
	keys    map[string]map[Conn]*client
	pending map[string]map[string]pending // relay key -> request id -> pending

	// Admit decides whether a key may connect at all, and is consulted before
	// anything is registered. nil admits every key, which is what the
	// standalone relay wants: it is a rendezvous point that nobody's downloads
	// pass through, and grouping strangers by key already keeps them apart.
	//
	// A KnightLoader serving a relay from inside itself needs the opposite
	// default. There the relay rides on an address somebody published so their
	// own instances could find each other, and admitting every key would make
	// their server a rendezvous for whoever finds it. So that caller sets this,
	// and only its own key gets in.
	//
	// It is a function rather than a stored key because the answer changes
	// while the process runs: the switch can be turned off and the key
	// replaced, and a relay that had to be rebuilt for either would keep
	// serving the old answer to whoever was already connected.
	Admit func(key string) bool
}

// New returns an empty relay that admits every key. Set Admit to narrow it.
func New() *Server {
	return &Server{
		clients: map[Conn]*client{},
		keys:    map[string]map[Conn]*client{},
		pending: map[string]map[string]pending{},
	}
}

// admits reports whether key may connect. Reading the field under no lock is
// deliberate and safe in the one shape it is used in: it is set once, before
// the server is ever mounted, to a closure that reads live configuration
// itself.
func (s *Server) admits(key string) bool {
	return s.Admit == nil || s.Admit(key)
}

// Len reports how many connections are registered across every key.
func (s *Server) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

// Join registers a connection under its relay key and introduces it to the
// group both ways: every sibling learns the newcomer exists, and the newcomer
// learns about every sibling that was already there. A late joiner that only
// received future announcements would be blind to the instances it most wants
// to see - the ones that have been up all along.
//
// A second connection announcing an instance ID that is already on this key
// replaces the first rather than joining beside it. That is not a defensive
// guard against something impossible: an instance whose socket dies without a
// close frame reconnects while the relay still holds the dead one, and a
// proxy-request routed to that corpse would be answered by nobody. The
// replaced connection is dropped silently, without a presence(offline), since
// the announce that follows says the instance is here on a new socket - but
// any request that was still waiting on the OLD connection to answer is
// failed fast (see failPending), because that answer can now never arrive no
// matter how long its own requester waits for it.
//
// Reports whether the connection was actually admitted - false means the
// caller must close it, either because maxTotalClients/maxClientsPerKey was
// already reached (and this is not a reconnect that would free a slot first)
// or because the same connection somehow joined twice.
func (s *Server) Join(key string, c Conn, a Announce) bool {
	cl := &client{
		conn:     c,
		key:      key,
		announce: a,
		send:     make(chan []byte, queueDepth),
		quit:     make(chan struct{}),
	}

	s.mu.Lock()
	if _, ok := s.clients[c]; ok {
		s.mu.Unlock()
		return false
	}
	group := s.keys[key]
	if group == nil {
		group = map[Conn]*client{}
		s.keys[key] = group
	}
	var replaced *client
	siblings := make([]*client, 0, len(group))
	for _, other := range group {
		if other.announce.InstanceID == a.InstanceID {
			replaced = other
			continue
		}
		siblings = append(siblings, other)
	}
	if replaced == nil {
		if len(s.clients) >= maxTotalClients || len(group) >= maxClientsPerKey {
			s.mu.Unlock()
			return false
		}
	}
	var failures []pendingFailure
	if replaced != nil {
		_, failures = s.removeLocked(replaced.conn)
	}
	s.clients[c] = cl
	group[c] = cl
	s.mu.Unlock()

	if replaced != nil {
		replaced.stop()
	}
	s.failPending(failures, "the instance that would have answered this reconnected before it replied - retry")
	go s.writeLoop(cl)

	for _, sib := range siblings {
		s.enqueue(cl, frameOf(TypeAnnounce, sib.announce))
	}
	arrival := frameOf(TypeAnnounce, a)
	for _, sib := range siblings {
		s.enqueue(sib, arrival)
	}
	return true
}

// Leave unregisters a connection, stops its writer (which closes the socket),
// tells its siblings it went offline, and fails fast any request that was
// still waiting on it to answer (see failPending). It is safe for a
// connection that was never joined and safe to call twice, so callers can put
// it in a defer without bookkeeping.
func (s *Server) Leave(c Conn) {
	s.mu.Lock()
	cl, failures := s.removeLocked(c)
	var siblings []*client
	if cl != nil {
		for _, sib := range s.keys[cl.key] {
			siblings = append(siblings, sib)
		}
	}
	s.mu.Unlock()
	if cl == nil {
		return
	}
	cl.stop()
	s.failPending(failures, "the instance that would have answered this disconnected before it replied")
	gone := frameOf(TypePresence, Presence{InstanceID: cl.announce.InstanceID})
	for _, sib := range siblings {
		s.enqueue(sib, gone)
	}
}

// Route handles one frame a client sent up its socket.
//
// Only the two proxy frames mean anything inbound: announce and presence are
// things the relay tells clients, never the other way round, and hello is
// consumed once before the read loop starts. An unparseable or unrecognised
// frame is ignored rather than closing the connection - the read loop's job
// is to notice a dead socket, not to police a client that is otherwise
// working, and a newer client speaking a frame type this build predates must
// not be disconnected for it.
func (s *Server) Route(c Conn, frame []byte) {
	env, err := Decode(frame)
	if err != nil {
		return
	}
	switch env.Type {
	case TypeProxyRequest:
		var req ProxyRequest
		if env.Into(&req) != nil || req.RequestID == "" || req.Target == "" {
			return
		}
		s.routeRequest(c, req, frame)
	case TypeProxyResponse:
		var resp ProxyResponse
		if env.Into(&resp) != nil || resp.RequestID == "" {
			return
		}
		s.routeResponse(c, resp.RequestID, frame)
	}
}

// routeRequest forwards a call to the sibling it names, verbatim: the relay
// reads the routing fields off a copy and never re-marshals the frame, so a
// body it does not understand cannot be mangled on the way through.
//
// A target nobody is connected as gets an immediate error response instead of
// silence - the caller would otherwise sit out its own timeout to learn a
// fact the relay knew the moment it looked, and "your other instance is
// offline" is exactly what the Instances page wants to say. A sender already
// holding maxPendingPerSender unanswered requests to this key gets an
// immediate refusal too, before the frame ever reaches the target's own
// queue - see maxPendingPerSender's own comment for why that has to happen
// here rather than at enqueue time.
func (s *Server) routeRequest(c Conn, req ProxyRequest, frame []byte) {
	s.mu.Lock()
	cl := s.clients[c]
	if cl == nil {
		s.mu.Unlock()
		return
	}
	var target *client
	for _, sib := range s.keys[cl.key] {
		if sib.conn != c && sib.announce.InstanceID == req.Target {
			target = sib
			break
		}
	}
	keyPending := s.pending[cl.key]
	if keyPending == nil {
		keyPending = map[string]pending{}
		s.pending[cl.key] = keyPending
	}
	sweepPendingLocked(keyPending)

	var refusal *ProxyResponse
	switch {
	case target == nil:
		refusal = &ProxyResponse{
			RequestID: req.RequestID,
			Status:    http.StatusBadGateway,
			Error:     "no instance " + req.Target + " is connected with this relay key",
		}
	case inFlightFromLocked(keyPending, c) >= maxPendingPerSender:
		refusal = &ProxyResponse{
			RequestID: req.RequestID,
			Status:    http.StatusTooManyRequests,
			Error:     "too many requests from this instance are still waiting on an answer",
		}
	default:
		keyPending[req.RequestID] = pending{from: c, target: target.conn, expires: time.Now().Add(pendingTTL)}
	}
	s.mu.Unlock()

	if refusal != nil {
		s.enqueue(cl, frameOf(TypeProxyResponse, *refusal))
		return
	}
	s.enqueue(target, frame)
}

// routeResponse sends an answer back to whoever asked, by request ID - the
// relay keeps that mapping itself rather than trusting the responder to
// address the reply. c is the connection this frame actually arrived on, and
// it must be the same connection routeRequest recorded as the target for
// this request ID, or the frame is silently dropped: without that check, any
// connection that learns or guesses a live request ID - including one on an
// entirely different key, since a request ID alone carries no key of its own
// - could hand the original requester a forged answer. Request IDs are
// generated client-side as 128 bits of crypto-random hex (see
// internal/relay/client.go), which makes guessing one infeasible in
// practice; this check is what makes it impossible in principle, for a
// connection that is not the requester and not the target either.
func (s *Server) routeResponse(c Conn, requestID string, frame []byte) {
	s.mu.Lock()
	cl := s.clients[c]
	if cl == nil {
		s.mu.Unlock()
		return
	}
	keyPending := s.pending[cl.key]
	p, ok := keyPending[requestID]
	if !ok || p.target != c {
		s.mu.Unlock()
		return
	}
	delete(keyPending, requestID)
	from := s.clients[p.from]
	s.mu.Unlock()
	if from == nil {
		return
	}
	s.enqueue(from, frame)
}

// removeLocked unregisters a connection and returns its client entry (or nil
// if it was never registered) plus every request this connection's departure
// just orphaned: entries where it was the requester are simply forgotten (its
// own socket is gone, nobody is waiting to receive the answer any more);
// entries where it was the TARGET are returned so the caller can fail them
// fast once the lock is released, instead of leaving their own requester to
// learn the same fact by timing out. Caller holds mu.
func (s *Server) removeLocked(c Conn) (*client, []pendingFailure) {
	cl := s.clients[c]
	if cl == nil {
		return nil, nil
	}
	delete(s.clients, c)
	group := s.keys[cl.key]
	delete(group, c)
	// An emptied group is deleted, not kept: a relay that has been up for
	// months would otherwise hold one empty map per key it has ever seen.
	if len(group) == 0 {
		delete(s.keys, cl.key)
	}
	keyPending := s.pending[cl.key]
	var failures []pendingFailure
	for id, p := range keyPending {
		switch c {
		case p.from:
			delete(keyPending, id)
		case p.target:
			delete(keyPending, id)
			failures = append(failures, pendingFailure{requestID: id, from: p.from})
		}
	}
	if len(keyPending) == 0 {
		delete(s.pending, cl.key)
	}
	return cl, failures
}

// failPending answers every request removeLocked found orphaned with a
// synthetic error response, addressed to whichever connection actually asked
// it - reusing the exact response shape routeRequest's own "no instance
// connected" answer already uses for a target that was never there to begin
// with. Called after the lock that produced failures has already been
// released, since enqueue takes it again.
func (s *Server) failPending(failures []pendingFailure, reason string) {
	for _, f := range failures {
		s.mu.Lock()
		from := s.clients[f.from]
		s.mu.Unlock()
		if from == nil {
			continue
		}
		s.enqueue(from, frameOf(TypeProxyResponse, ProxyResponse{
			RequestID: f.requestID,
			Status:    http.StatusBadGateway,
			Error:     reason,
		}))
	}
}

// sweepPendingLocked drops one key's own requests that nobody ever answered.
// Caller holds mu. Scoped to a single key's map (see Server's own doc
// comment) rather than the old process-wide table, so the cost of sweeping
// is bounded by how much traffic THIS key has generated, never by every
// other key sharing the same relay.
func sweepPendingLocked(keyPending map[string]pending) {
	now := time.Now()
	for id, p := range keyPending {
		if now.After(p.expires) {
			delete(keyPending, id)
		}
	}
}

// inFlightFromLocked counts how many of one key's pending requests were sent
// by from - what maxPendingPerSender is checked against. Caller holds mu.
func inFlightFromLocked(keyPending map[string]pending, from Conn) int {
	n := 0
	for _, p := range keyPending {
		if p.from == from {
			n++
		}
	}
	return n
}

// enqueue hands one frame to a connection, or drops that connection if it
// cannot keep up. A full queue means this instance is not draining as fast as
// its siblings produce - dropping it heals itself, because a client
// reconnects and re-announces, whereas a relay stalled behind one bad link
// stops routing for everybody on that key.
//
// This is safe to keep as an eviction rather than a silent frame-drop only
// because routeRequest's own maxPendingPerSender check already keeps a
// malicious sender from ever forcing enough proxy-request frames into a
// healthy target's queue to trigger it - what reaches here for ordinary
// announce/presence/response traffic is bounded by how many siblings and how
// many genuinely in-flight requests a key actually has, not by how fast one
// connection chooses to send.
func (s *Server) enqueue(cl *client, frame []byte) {
	select {
	case cl.send <- frame:
	default:
		s.Leave(cl.conn)
	}
}

// writeLoop is the only goroutine that writes to a connection, which is what
// keeps a slow link off the routing path.
func (s *Server) writeLoop(cl *client) {
	// The writer owns the socket: whatever ends the loop closes it too, so a
	// dropped client is torn down without the router ever waiting on a close
	// handshake.
	defer func() { _ = cl.conn.CloseNow() }()
	for {
		// A stopped client must not keep draining a queue somebody filled
		// just before the drop. The two-case select below would pick a ready
		// send about half the time, so quit gets checked on its own first.
		select {
		case <-cl.quit:
			return
		default:
		}
		select {
		case <-cl.quit:
			return
		case frame := <-cl.send:
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			err := cl.conn.Write(ctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				// The socket is gone or wedged past the timeout; unregister
				// so later frames stop queueing for a client nobody drains.
				s.Leave(cl.conn)
				return
			}
		}
	}
}

// frameOf marshals one outbound frame. Every payload the relay itself sends
// is a struct of strings, ints and byte slices from protocol.go, none of
// which encoding/json can fail on, so there is no error here for a caller to
// act on - unlike the inbound direction, where the bytes come from a client
// and every parse is checked.
func frameOf(typ string, data any) []byte {
	b, _ := Encode(typ, data)
	return b
}

// ServeHTTP is the relay endpoint itself: one long-lived WebSocket per
// instance, dialled outbound by both sides.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// No origin check, unlike internal/api's own socket. There it stops
		// another website driving a logged-in instance through the visitor's
		// browser; here there is no UI, no session and no cookie to ride -
		// the key in the first frame is the only credential, and a page
		// without it gets nothing. Every legitimate client is cross-origin
		// by construction, so an origin check would only lock out clients
		// while stopping nothing.
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	c.SetReadLimit(readLimit)

	hello, err := readHello(r.Context(), c)
	if err != nil {
		c.Close(websocket.StatusPolicyViolation, "the first frame must be a hello with a relay key of at least 16 characters and an instance id")
		return
	}
	// Checked before Join, so a key this relay does not serve costs one
	// goroutine for the length of a handshake and never appears in the
	// registry. The close reason says the key was refused rather than that the
	// relay is picky about which ones: an instance whose own key stopped
	// matching needs to know that is what happened.
	if !s.admits(hello.Key) {
		c.Close(websocket.StatusPolicyViolation, "this relay does not serve that relay key")
		return
	}
	if !s.Join(hello.Key, c, hello.Announce) {
		c.Close(websocket.StatusPolicyViolation, "too many instances are already connected with this relay key")
		return
	}
	// Leave stops the writer, which closes the socket - so this is the whole
	// teardown, and a client that vanishes mid-request is indistinguishable
	// from one that closed cleanly, as it should be.
	defer s.Leave(c)
	for {
		_, frame, err := c.Read(r.Context())
		if err != nil {
			return
		}
		s.Route(c, frame)
	}
}

// readHello consumes the one frame that authenticates a connection. It is
// read before the connection joins anything, on its own deadline, so an
// unauthenticated socket costs one goroutine for helloTimeout at most.
func readHello(ctx context.Context, c *websocket.Conn) (Hello, error) {
	ctx, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()
	_, frame, err := c.Read(ctx)
	if err != nil {
		return Hello{}, err
	}
	env, err := Decode(frame)
	if err != nil {
		return Hello{}, err
	}
	if env.Type != TypeHello {
		return Hello{}, errors.New("first frame is not a hello")
	}
	var h Hello
	if err := env.Into(&h); err != nil {
		return Hello{}, err
	}
	if len(h.Key) < minKeyLength || h.Announce.InstanceID == "" {
		return Hello{}, errors.New("hello carries no relay key, a key shorter than the minimum, or no instance id")
	}
	return h, nil
}
