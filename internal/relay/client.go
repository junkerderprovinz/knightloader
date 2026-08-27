package relay

// The instance side of the relay: one outbound WebSocket an instance keeps
// open to a relay it can reach, so two instances behind NAT reach each other
// without either of them accepting an inbound connection.
//
// This type deliberately knows nothing about the app it runs inside. It is
// used unchanged by the container build (which already runs an event loop)
// and by the desktop build (which has no network listener at all and cannot
// be paired any other way), so it depends on protocol.go and the standard
// library and nothing else - an import of internal/app here would put the
// desktop build back where it started.
//
// Unlike Server, there is no bounded queue and no writer goroutine. Those
// exist there because one stalled connection must not delay the fan-out to
// every other connection on the same key; a client has exactly one peer, so a
// slow write can only ever delay the caller who asked for it, and
// coder/websocket serializes concurrent writes itself.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// connectPath is the relay endpoint every client dials. It is appended when
// the configured address names no path of its own, so a person can type the
// bare "https://relay.example.com" they gave their reverse proxy and still
// land on the right route.
const connectPath = "/relay/connect"

// proxyTimeout bounds one call to a sibling over the relay, matching
// federation's own peerTimeout for the direct-HTTP transport: the same call
// to the same peer's same REST route must not have two different patiences
// depending on which transport carried it.
const proxyTimeout = 15 * time.Second

// dialTimeout bounds one connection attempt. A relay that is switched off
// refuses instantly; this is for one that accepts TCP and then never finishes
// the upgrade, which would otherwise hold the reconnect loop forever.
const dialTimeout = 15 * time.Second

// minBackoff and maxBackoff bound how fast the reconnect loop retries. A
// relay outage must cost nothing but "no relay-visible siblings right now",
// so the loop keeps trying forever rather than giving up - but at a minute
// apart once it is clear nobody is answering, not once a second.
const (
	minBackoff = time.Second
	maxBackoff = time.Minute
)

// stableSession is how long a connection has to have lasted before the
// backoff resets to minBackoff. Resetting on every successful dial would let
// a relay that accepts the socket and then hangs up - a rejected key, a
// reverse proxy misrouting the upgrade - be dialled once a second forever,
// which is the exact case the backoff exists for.
const stableSession = 30 * time.Second

// pingInterval keeps the connection alive through whatever sits between the
// instance and the relay. A relay is normally reached through a reverse proxy
// (Nginx Proxy Manager, in this project's own deployment) that closes an idle
// upstream connection after a minute or so, and this socket is idle by nature:
// it carries nothing at all between one announce and the next proxy call.
const pingInterval = 30 * time.Second

// pingTimeout is how long a pong may take before the connection counts as
// dead. It is what turns a silently broken link - a NAT table that dropped the
// mapping, a relay that vanished without a close frame - into a reconnect
// instead of a socket that looks open and delivers nothing.
const pingTimeout = 10 * time.Second

// ProxyHandler answers one call a sibling made to this instance's own REST
// API. It is what makes the relay two-way: without it an instance could call
// its siblings but never be called, which is half a transport.
//
// It returns the status and body to send back, the same pair
// federation.Manager.Proxy hands its own callers, so the app side can adapt
// its existing HTTP handler without inventing a second result shape.
//
// It takes a ProxyCall, not a ProxyRequest: by the time a handler runs, the
// frame has been opened and the routing fields it was addressed with have
// done their job. A handler that cannot see the wire frame cannot
// accidentally trust a field the relay was free to write.
type ProxyHandler func(ctx context.Context, call ProxyCall) (status int, body []byte)

// ClientOptions configures a Client. URL, Key and Self.InstanceID are
// required; the rest is optional.
type ClientOptions struct {
	// URL is the relay's address, with or without the connect path and with
	// either an http(s) or a ws(s) scheme - a person configuring this has the
	// address they gave their reverse proxy, not a WebSocket URL.
	URL string
	// Key is the relay key: the only credential this protocol has, and the
	// only thing that decides which instances see each other.
	Key string
	// FrameKey is the 32-byte key every proxy frame is sealed under. It must
	// be the same on every instance in the group and must NOT be derivable
	// from Key, or the relay could compute it from the hello frame it is
	// already given - see key.go's DeriveFrameKey and FrameKeyFromRelayKey
	// for the two ways it is produced and what each one is worth.
	FrameKey []byte
	// Self is what siblings will see on their Instances page.
	Self Announce
	// Serve answers calls siblings make to this instance. A nil Serve answers
	// every inbound call with 501 rather than leaving the caller to time out:
	// a client that cannot serve is a fact its siblings should learn at once.
	Serve ProxyHandler
	// OnChange fires whenever anything the Instances page shows about the
	// relay changes: a sibling arriving or leaving, and the connection itself
	// coming up or going down - an outage is a change to that page even
	// though no single sibling did anything. It lets a UI be pushed an update
	// instead of polling for one. It runs on the client's own goroutine and
	// must not block; nil to ignore the event entirely.
	OnChange func()
}

// Client is one instance's connection to a relay: it announces itself, tracks
// which siblings are currently visible, answers calls they make, and makes
// calls to them.
//
// Every method is safe to call whether or not the relay is reachable. A
// Client that cannot connect reports no siblings and fails proxy calls
// immediately; it never blocks the caller waiting for a relay to come back,
// because an unreachable relay must not touch anything else the instance
// does.
type Client struct {
	url string
	key string
	// frameKey seals and opens every proxy frame this client sends or
	// receives. Distinct from key, and that separation is the point: key is
	// handed to the relay in the hello frame, this one never leaves the
	// process. See key.go's DeriveFrameKey.
	frameKey []byte
	self     Announce
	serve    ProxyHandler
	onChange func()

	// minBackoff and maxBackoff shadow the package constants so tests can
	// exercise a real reconnect without waiting out a real backoff. They are
	// not part of the option struct: a retry cadence is this package's
	// judgement, not something a caller has any basis to tune.
	minBackoff time.Duration
	maxBackoff time.Duration

	mu       sync.Mutex
	conn     *websocket.Conn
	siblings map[string]Announce
	pending  map[string]chan ProxyResponse
	started  bool

	startOnce sync.Once
	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
}

// NewClient validates the configuration and returns a Client that is not yet
// connected. A bad address, a missing key or a missing instance ID is an error
// here rather than a connection that quietly never works: all three are
// permanent misconfigurations, and the settings page that produced them is
// the only place they can be fixed.
func NewClient(opts ClientOptions) (*Client, error) {
	connect, err := connectURL(opts.URL)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(opts.Key)) < minKeyLength {
		return nil, errors.New("relay: no relay key configured, or it is shorter than the minimum the relay itself will accept")
	}
	if opts.Self.InstanceID == "" {
		return nil, errors.New("relay: no instance id to announce")
	}
	// A wrong-length frame key is refused here rather than at the first proxy
	// call. Both are permanent misconfigurations of the same kind as the two
	// above, and this one would otherwise present as "the relay connects, the
	// siblings appear, and every call to them fails" - the most expensive
	// possible place to discover it.
	if len(opts.FrameKey) != 32 {
		return nil, fmt.Errorf("relay: frame key is %d bytes, want 32", len(opts.FrameKey))
	}
	return &Client{
		url:        connect,
		key:        opts.Key,
		frameKey:   opts.FrameKey,
		self:       opts.Self,
		serve:      opts.Serve,
		onChange:   opts.OnChange,
		minBackoff: minBackoff,
		maxBackoff: maxBackoff,
		siblings:   map[string]Announce{},
		pending:    map[string]chan ProxyResponse{},
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}, nil
}

// Start connects in the background and keeps reconnecting for as long as the
// Client lives. It returns immediately and never reports a dial failure:
// there is nothing a caller could do with one that the reconnect loop is not
// already doing. Calling it twice is a no-op, and calling it after Close does
// nothing at all - the same shape schedule.Runner.Start already uses, and for
// the same reason: a boot that fails between NewClient and Start still runs
// the deferred Close.
func (c *Client) Start() {
	c.startOnce.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		select {
		case <-c.stop:
			return
		default:
		}
		c.started = true
		go c.run()
	})
}

// Close ends the connection and the reconnect loop, and waits for the loop to
// finish so a caller can tear down whatever Serve talks to without racing it.
func (c *Client) Close() error {
	c.closeOnce.Do(func() { close(c.stop) })
	c.mu.Lock()
	conn, started := c.conn, c.started
	c.mu.Unlock()
	if conn != nil {
		_ = conn.CloseNow()
	}
	if started {
		<-c.done
	}
	return nil
}

// Connected reports whether the relay connection is up right now. It is the
// one honest answer to "is relay pairing working", which no list of siblings
// can give: an empty list means either "the relay is down" or "you are the
// only instance connected", and those are different things to tell a user.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Siblings returns the instances currently visible through the relay, sorted
// by instance ID. It is empty whenever the relay is unreachable, which is the
// whole failure mode: no relay means no relay-visible peers, never an error
// anybody has to handle.
//
// Sorted by ID rather than name because the ID is the only field the relay
// guarantees is unique on a key - two instances that both defaulted their name
// to the same hostname must still come out in a stable order.
func (c *Client) Siblings() []Announce {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Announce, 0, len(c.siblings))
	for _, a := range c.siblings {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstanceID < out[j].InstanceID })
	return out
}

// Proxy calls one sibling's REST API through the relay and waits for its
// answer, the same (method, path, body) -> (body, status, error) shape
// federation.Manager.Proxy already speaks over direct HTTP.
//
// An error means the call did not reach the sibling and produce an answer:
// the relay is not connected, the sibling is not, or nobody replied in time.
// A reply the sibling actually produced is returned as its own body and status
// however bad that status is, exactly as the HTTP transport does, so a caller
// can tell "your other instance said no" from "your other instance is gone".
func (c *Client) Proxy(ctx context.Context, target, method, path string, body []byte, authorization string) ([]byte, int, error) {
	id, err := requestID()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	c.mu.Lock()
	conn := c.conn
	answer := make(chan ProxyResponse, 1)
	if conn != nil {
		c.pending[id] = answer
	}
	c.mu.Unlock()
	if conn == nil {
		return nil, http.StatusBadGateway, errors.New("relay: not connected")
	}
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, proxyTimeout)
	defer cancel()
	sealed, err := SealCall(c.frameKey, id, target, ProxyCall{
		Method: method, Path: path, Body: body, Authorization: authorization,
	})
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	frame := frameOf(TypeProxyRequest, ProxyRequest{RequestID: id, Target: target, Sealed: sealed})
	if err := writeFrameTo(ctx, conn, frame); err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("relay: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, http.StatusGatewayTimeout, fmt.Errorf("relay: %s did not answer: %w", target, ctx.Err())
	case resp := <-answer:
		// Error is set only when the relay answered instead of the target -
		// "nobody is connected as that instance", or the connection died
		// under this call. Both are failures of the transport, not answers
		// from the peer, so they come back as errors.
		//
		// Read before Sealed, and it must stay that way: the relay writes
		// Error in the clear because it holds no key, so this is the one
		// field on a response a hostile relay can author. Answering the
		// error first, and never treating an unsealed response as a result,
		// is what keeps that limited to denial of service - see
		// ProxyResponse.Error's own comment.
		if resp.Error != "" {
			return nil, http.StatusBadGateway, errors.New("relay: " + resp.Error)
		}
		result, err := OpenResult(c.frameKey, id, resp.Sealed)
		if err != nil {
			// The peer is on this key but its frames do not open under this
			// secret, or something rewrote them in flight. Not a transport
			// failure and not an answer, so it is neither reported as the
			// peer being gone nor allowed to look like a reply.
			return nil, http.StatusBadGateway, fmt.Errorf("relay: %s answered unreadably: %w", target, err)
		}
		return result.Body, result.Status, nil
	}
}

// run is the reconnect loop: one session at a time, forever, until Close.
func (c *Client) run() {
	defer close(c.done)
	wait := c.minBackoff
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		began := time.Now()
		c.session()
		if time.Since(began) >= stableSession {
			wait = c.minBackoff
		}
		if !c.sleep(wait) {
			return
		}
		if wait *= 2; wait > c.maxBackoff {
			wait = c.maxBackoff
		}
	}
}

// sleep waits out one backoff, reporting false if Close happened first.
func (c *Client) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.stop:
		return false
	case <-t.C:
		return true
	}
}

// session runs one connection from dial to death. It returns when the socket
// is gone, whatever killed it - a failed dial, a rejected key, a dead link or
// Close - because every one of those means the same thing to the loop above:
// try again later.
func (c *Client) session() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Close has to end a session that is currently blocked in Read, and Read
	// only unblocks on its context. This goroutine ends with the session
	// either way, so it cannot outlive the connection it is cancelling for.
	go func() {
		select {
		case <-c.stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	conn, _, err := websocket.Dial(dialCtx, c.url, nil)
	dialCancel()
	if err != nil {
		return
	}
	conn.SetReadLimit(readLimit)
	defer func() { _ = conn.CloseNow() }()

	// The hello carries both the key and this instance's announce, so the
	// relay can introduce it to its siblings without ever re-broadcasting the
	// key - see Hello's own doc comment. It goes out before the connection is
	// published below, so nothing can try to proxy over a socket the relay
	// has not yet accepted.
	helloCtx, helloCancel := context.WithTimeout(ctx, writeTimeout)
	err = writeFrameTo(helloCtx, conn, frameOf(TypeHello, Hello{Key: c.key, Announce: c.self}))
	helloCancel()
	if err != nil {
		return
	}

	c.connected(conn)
	defer c.disconnected()
	go c.keepalive(ctx, conn)

	for {
		_, frame, err := conn.Read(ctx)
		if err != nil {
			return
		}
		c.handle(ctx, conn, frame)
	}
}

// keepalive proves the link is still there, and tears the session down when it
// is not. Ping waits for the pong, which the read loop in session is what
// actually receives - so this only ever runs alongside a live reader.
func (c *Client) keepalive(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				// Closing here is what ends the read loop, which ends the
				// session and hands the reconnect loop its turn.
				_ = conn.CloseNow()
				return
			}
		}
	}
}

// handle dispatches one frame the relay sent down.
//
// An unparseable or unknown frame is ignored rather than dropping the
// connection, the same tolerance Server.Route already shows: a relay newer
// than this build speaking a frame type it does not know must not cost the
// instance its relay peers.
func (c *Client) handle(ctx context.Context, conn *websocket.Conn, frame []byte) {
	env, err := Decode(frame)
	if err != nil {
		return
	}
	switch env.Type {
	case TypeAnnounce:
		var a Announce
		if env.Into(&a) != nil || a.InstanceID == "" {
			return
		}
		c.mu.Lock()
		c.siblings[a.InstanceID] = a
		c.mu.Unlock()
		c.changed()
	case TypePresence:
		var p Presence
		// Only an offline report means anything: an arrival is an Announce,
		// which carries the name and deployment a bare presence flag could
		// not, so an Online=true frame would tell this client to list a
		// sibling it cannot name.
		if env.Into(&p) != nil || p.InstanceID == "" || p.Online {
			return
		}
		c.mu.Lock()
		delete(c.siblings, p.InstanceID)
		c.mu.Unlock()
		c.changed()
	case TypeProxyResponse:
		var resp ProxyResponse
		if env.Into(&resp) != nil || resp.RequestID == "" {
			return
		}
		c.deliver(resp)
	case TypeProxyRequest:
		var req ProxyRequest
		if env.Into(&req) != nil || req.RequestID == "" {
			return
		}
		// Its own goroutine: answering means running a real API call, and the
		// read loop has to stay free to receive the next frame - including the
		// answer to a call this instance is itself waiting on.
		go c.answer(ctx, conn, req)
	}
}

// answer runs one inbound call and sends the result back.
//
// A frame that will not open is dropped without a reply, deliberately. It
// means the sender is on this relay key but does not hold this group's
// secret, or something rewrote the frame in flight - neither is a peer owed
// an answer, and an error reply would only confirm to whoever sent it that
// their key was accepted while their secret was not.
func (c *Client) answer(ctx context.Context, conn *websocket.Conn, req ProxyRequest) {
	call, err := OpenCall(c.frameKey, req.RequestID, c.self.InstanceID, req.Sealed)
	if err != nil {
		return
	}
	result := ProxyResult{Status: http.StatusNotImplemented}
	if c.serve != nil {
		serveCtx, cancel := context.WithTimeout(ctx, proxyTimeout)
		result.Status, result.Body = c.serve(serveCtx, call)
		cancel()
	}
	sealed, err := SealResult(c.frameKey, req.RequestID, result)
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_ = writeFrameTo(writeCtx, conn, frameOf(TypeProxyResponse, ProxyResponse{
		RequestID: req.RequestID, Sealed: sealed,
	}))
}

// deliver hands a response to the Proxy call waiting on it. A response nobody
// is waiting for is dropped: the caller already gave up, or the relay answered
// a request this client never made.
func (c *Client) deliver(resp ProxyResponse) {
	c.mu.Lock()
	answer := c.pending[resp.RequestID]
	delete(c.pending, resp.RequestID)
	c.mu.Unlock()
	if answer != nil {
		answer <- resp
	}
}

// connected publishes a live socket so Proxy can use it.
func (c *Client) connected(conn *websocket.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.changed()
}

// disconnected forgets everything that was only true while the connection was
// up: the sibling list, because a relay peer exists exactly as long as the
// relay says so, and every call still in flight, which is failed at once
// rather than left to time out - the caller can be told "the relay connection
// dropped" now, and would learn nothing more by waiting.
func (c *Client) disconnected() {
	c.mu.Lock()
	c.conn = nil
	c.siblings = map[string]Announce{}
	waiting := make([]chan ProxyResponse, 0, len(c.pending))
	for id, answer := range c.pending {
		waiting = append(waiting, answer)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	for _, answer := range waiting {
		// Error only, no sealed blob: Proxy reads Error first and never
		// reaches OpenResult, and this is a local failure with no frame
		// behind it to seal in the first place.
		answer <- ProxyResponse{
			Error: "the relay connection dropped before the answer arrived",
		}
	}
	c.changed()
}

// changed notifies the caller that the sibling list is different now. Called
// without the lock held, since the callback is somebody else's code and may
// well call back into Siblings.
func (c *Client) changed() {
	if c.onChange != nil {
		c.onChange()
	}
}

// writeFrameTo writes one frame under the caller's deadline, capped by
// writeTimeout so no single write can outlast the ping that would have
// declared the link dead.
func writeFrameTo(ctx context.Context, conn *websocket.Conn, frame []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, frame)
}

// requestID is the token a response is matched back by. Random rather than a
// counter: a reconnect restarts this client's own numbering, and a late answer
// to a pre-reconnect request would then be delivered to whoever inherited that
// number.
func requestID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// connectURL turns whatever address a person configured into the WebSocket URL
// to dial. Both scheme families are accepted because both are things people
// legitimately have written down: the https:// address they gave their reverse
// proxy, and the wss:// URL a WebSocket client would normally be handed.
func connectURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("relay: %q is not a relay address", raw)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("relay: %q must be an http(s) or ws(s) address", raw)
	}
	// A path that was typed stays: a relay behind a reverse proxy can be
	// mounted anywhere, and only the caller knows where.
	if u.Path == "" || u.Path == "/" {
		u.Path = connectPath
	}
	return u.String(), nil
}
