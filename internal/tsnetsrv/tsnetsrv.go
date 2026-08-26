// Package tsnetsrv is KnightLoader's Tailscale integration: log in once per
// instance, and this instance becomes reachable at a real, public
// https://<name>.<tailnet>.ts.net address via Tailscale Funnel - no relay
// address, no shared key, no manual field to type on any other device.
//
// This exists as the answer to a direct complaint (jdp, 2026-08-26): "das
// ist alles viel zu kompliziert für User... ist das nur mit einem VPS
// möglich?" and, once relay/pairing were floated as the fix, "man soll auf
// dem handy nicht auch noch tailscale installieren müssen. das muss alles
// super einfach sein und out of the box funktionieren". This package is
// deliberately SERVER-SIDE ONLY: a phone, a browser, or the browser
// extension never needs Tailscale installed at all, because Funnel turns
// the connection into an ordinary https:// URL any client already knows
// how to reach - see internal/api/routes_tsnet.go for the one-time login
// flow this exposes over HTTP, and routes_remote.go for how the funnel
// address joins the same address list pairing and the QR code already draw
// from.
package tsnetsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

// Status is the coarse state CheckLinks-style callers (the settings page)
// actually need to draw a UI from - not tsnet's own much finer internal
// state machine, which this package deliberately does not expose.
type Status string

const (
	StatusOff        Status = "off"        // never started, Stop was called, or the last attempt failed
	StatusConnecting Status = "connecting" // Start called, waiting on login and/or the tailnet handshake
	StatusConnected  Status = "connected"  // logged in, reachable on the tailnet
	StatusError      Status = "error"      // the last attempt's Up() or funnel listener failed
)

// authURLPattern pulls the interactive login link out of tsnet's own
// UserLogf stream. tsnet does not hand this back as a return value or a
// field anywhere - logging it to a caller-supplied function is documented
// as the intended way to surface it to a human, so this is a regex over log
// lines, not a workaround for a missing API.
var authURLPattern = regexp.MustCompile(`https://login\.tailscale\.com/\S+`)

// Info is what GET /api/tsnet/status answers with.
type Info struct {
	Status Status `json:"status"`
	// AuthURL is set only while Status is "connecting" and tsnet has logged
	// one - the link to open to complete login. Empty once connected: an
	// already-authorized instance has nothing left to click.
	AuthURL string `json:"authUrl,omitempty"`
	// FunnelURL is the public https:// address other devices - a phone with
	// no Tailscale installed at all, the browser extension, another
	// KnightLoader instance - can just use like any other server address,
	// once Funnel is up. Empty until the funnel listener actually starts.
	FunnelURL string `json:"funnelUrl,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	// Error carries the last attempt's Up() or funnel listener failure
	// message verbatim, most commonly "Funnel is not enabled for this
	// tailnet" - which needs one manual, one-time click in the Tailscale
	// admin console (see settings.access.tsnet.funnelErrorHint), not a code
	// fix here. Cleared the moment a new Start succeeds past the point that
	// produced it, and always cleared by Stop.
	Error string `json:"error,omitempty"`
}

// Manager owns at most one *tsnet.Server for the process lifetime. Start is
// idempotent while a connection is live or connecting; a FAILED attempt does
// not stay "running" - see run's own comment on why srv is cleared on error,
// which is what lets a later Start actually retry rather than silently
// no-op forever on the same dead server. Stop tears down both the funnel
// listener and the node itself, logging this instance out of Tailscale
// rather than merely pausing it (TSNET_FORCE_LOGIN would be needed to log
// back in as a different identity otherwise, which is not what a person
// pressing a "Trennen" button is asking for).
//
// Concurrency: every field below is guarded by mu. run's own goroutine is
// matched to the *tsnet.Server it was launched for; if Stop (or a later
// Start) has already moved m.srv on to something else - or to nil - by the
// time run reacquires the lock, run treats itself as superseded and returns
// without touching any field, so a slow or racing goroutine from an earlier
// generation can never clobber a newer one's state. cancel/done are how Stop
// gets a goroutine that is still blocked inside srv.Up to actually stop:
// tsnet.Server.Close's own doc comment is explicit that it "must not be
// called before or concurrently with Start" (Up calls Start internally), so
// Stop cancels the context Up was given and waits for run to fully return
// -  which Up is documented to honour promptly - before ever calling Close,
// rather than racing the two the way calling Close directly from Stop while
// Up is still in flight would.
type Manager struct {
	// dir is where tsnet persists this node's own Tailscale identity
	// (WireGuard keys, the tailnet it belongs to) - stable across restarts
	// on purpose, the same reason a.dataDir itself is: a fresh directory
	// every boot would mean logging in again every time the process
	// restarts, not once ever.
	dir string
	// controlURL overrides tsnet's own default coordination server, set only
	// by tests (same-package, so no exported setter exists) - it lets a test
	// drive a real *tsnet.Server through a real, fast-failing Up() without
	// reaching Tailscale's actual control plane or holding real credentials.
	controlURL string
	// startTimeout, set only by tests, bounds the context Up() is given -
	// tsnet's local "needs interactive login" state blocks Up() forever
	// against a fake control URL with no network error of its own ever
	// occurring (verified empirically), so this is how a test forces Up() to
	// return a real error quickly without needing a real Tailscale account.
	startTimeout time.Duration
	// handler is called lazily, once the tailnet connection is actually up,
	// not captured at construction time - Manager is built before
	// api.Handler(a) finishes wiring the real handler (see app.New's own
	// comment on why), and a stale nil handler captured too early would
	// silently serve nothing on the funnel listener forever.
	handler func() http.Handler

	mu     sync.Mutex
	srv    *tsnet.Server
	cancel context.CancelFunc // cancels the in-flight Up() belonging to srv, if any
	done   chan struct{}      // closed by run() when it returns for srv, however it ends

	status    Status
	authURL   string
	err       error
	funnelLn  net.Listener
	funnelURL string
	hostname  string
}

func New(stateDir string, handler func() http.Handler) *Manager {
	return &Manager{dir: stateDir, handler: handler, status: StatusOff}
}

// Start begins (or resumes, if this instance already logged in during an
// earlier run) the connection. hostname becomes both this node's Tailscale
// name and the left-hand label of its funnel address
// (<hostname>.<tailnet>.ts.net) - defaulted to "knightloader" rather than
// left for tsnet to invent one from the binary name, so a person with
// several instances is not left telling three identically-named nodes
// apart in the Tailscale admin console.
func (m *Manager) Start(hostname string) error {
	m.mu.Lock()
	if m.srv != nil {
		m.mu.Unlock()
		return nil // already running or connecting - Start is idempotent
	}
	if hostname == "" {
		hostname = "knightloader"
	}
	// Declared before assignment, not `srv := &tsnet.Server{...}`, so the
	// UserLogf closure below can refer to srv at all - a struct literal
	// cannot reference the variable it is itself being assigned to.
	var srv *tsnet.Server
	srv = &tsnet.Server{
		Dir:        m.dir,
		Hostname:   hostname,
		ControlURL: m.controlURL,
		UserLogf: func(format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			log.Printf("tsnet: %s", msg)
			if u := authURLPattern.FindString(msg); u != "" {
				m.mu.Lock()
				// Only while this srv is still the current one - an auth URL
				// logged by a generation Stop already moved past has nothing
				// left to show it to.
				if m.srv == srv {
					m.authURL = u
				}
				m.mu.Unlock()
			}
		},
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if m.startTimeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), m.startTimeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	done := make(chan struct{})
	m.srv = srv
	m.cancel = cancel
	m.done = done
	m.status = StatusConnecting
	m.hostname = hostname
	m.authURL = ""
	m.err = nil
	m.mu.Unlock()

	go m.run(ctx, srv, done)
	return nil
}

// run blocks on srv.Up (interactive login happens here, on the very first
// connection - UserLogf above is how its URL escapes this goroutine to the
// status endpoint), then opens the funnel listener and serves this
// instance's own handler on it for as long as the connection lasts.
//
// Every reacquisition of the lock below starts by checking m.srv == srv -
// see the Manager doc comment for why. A failed Up or ListenFunnel also
// clears m.srv (not just m.status), which is what lets Start actually retry
// afterward instead of forever seeing a non-nil srv left over from the dead
// attempt.
func (m *Manager) run(ctx context.Context, srv *tsnet.Server, done chan struct{}) {
	defer close(done)

	_, err := srv.Up(ctx)
	m.mu.Lock()
	if m.srv != srv {
		// Superseded by Stop (possibly followed by a new Start) while Up was
		// still in flight - Stop already did or will do the real cleanup for
		// this srv; nothing here belongs to the current generation anymore.
		m.mu.Unlock()
		return
	}
	if err != nil {
		m.status = StatusError
		m.err = err
		m.srv = nil
		m.mu.Unlock()
		return
	}
	m.status = StatusConnected
	m.authURL = ""
	m.mu.Unlock()

	// Port 443 is Funnel's own HTTPS port - one of exactly three it will
	// ever answer on (443/8443/10000, a tailnet-wide restriction, not this
	// app's choice), and the one that needs no port number typed into a
	// URL anywhere this instance's address is shown or scanned.
	ln, lnErr := srv.ListenFunnel("tcp", ":443")
	m.mu.Lock()
	if m.srv != srv {
		m.mu.Unlock()
		if lnErr == nil {
			_ = ln.Close()
		}
		return
	}
	if lnErr != nil {
		// The single most common cause: Funnel has never been turned on for
		// this tailnet (a one-time click in the Tailscale admin console,
		// off by default for every new account) - surfaced verbatim rather
		// than reworded, since tsnet's own message already names this.
		m.status = StatusError
		m.err = fmt.Errorf("connected to Tailscale, but the public address could not be opened: %w", lnErr)
		m.srv = nil
		m.mu.Unlock()
		return
	}
	domains := srv.CertDomains()
	funnelURL := ""
	if len(domains) > 0 {
		funnelURL = "https://" + domains[0]
	}
	m.funnelLn = ln
	m.funnelURL = funnelURL
	m.mu.Unlock()

	// Blocks for the life of the listener; Stop() closing ln is what ends
	// this, the same shutdown shape internal/relay.Server's own ServeHTTP
	// loop already uses.
	_ = http.Serve(ln, m.handler())
}

// Stop logs this instance out of Tailscale entirely, not merely pausing the
// funnel listener - a person pressing "Trennen" is asking to leave the
// tailnet, not to hide behind a closed door on it. A later Start reconnects
// as the SAME node identity (the state directory is untouched), so this is
// not the same as deleting the node from the Tailscale admin console.
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.srv == nil {
		m.status = StatusOff
		m.err = nil
		m.mu.Unlock()
		return nil
	}
	srv := m.srv
	cancel := m.cancel
	done := m.done
	ln := m.funnelLn
	// Marked as no-longer-current before anything below runs, so a run()
	// goroutine that reacquires the lock while this is in flight sees
	// m.srv != srv and treats itself as superseded rather than writing state
	// for a generation Stop already tore down.
	m.srv = nil
	m.cancel = nil
	m.done = nil
	m.funnelLn = nil
	m.status = StatusOff
	m.authURL = ""
	m.funnelURL = ""
	m.err = nil
	m.mu.Unlock()

	// Aborts an in-flight Up() the way tsnet's own docs call for (cancelling
	// its context), rather than calling srv.Close() while Up() might still be
	// running - see the Manager doc comment for the exact contract this
	// avoids violating. A no-op once Up() has already returned.
	if cancel != nil {
		cancel()
	}
	// Unblocks a run() goroutine sitting in http.Serve(ln, ...) the same way
	// the pre-generation-tracking version of this method already did.
	if ln != nil {
		_ = ln.Close()
	}
	// Waits for run() to fully return - whichever branch it takes - before
	// Close() below runs, so Close() is never concurrent with Up()/Start()
	// for this srv.
	if done != nil {
		<-done
	}
	return srv.Close()
}

// Info reports the current state for GET /api/tsnet/status to poll - see
// that route's own comment for the polling cadence this is built for.
func (m *Manager) Info() Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := Info{Status: m.status, AuthURL: m.authURL, FunnelURL: m.funnelURL, Hostname: m.hostname}
	if m.err != nil {
		info.Error = m.err.Error()
	}
	return info
}

// PeerInstance is one other device on the same tailnet that answered a
// KnightLoader health probe - a candidate for the existing "add a peer by
// address" flow (POST /api/instances, internal/api/routes_federation.go),
// reached with no pairing code and no relay key at all, because both
// instances already share the one login that got each of them here (jdp,
// 2026-08-27: "wie genau wird das jetzt umgesetzt?" - this is the answer:
// once two of a person's own KnightLoader instances are both logged into
// the same Tailscale account, tsnet's own LocalClient().Status() already
// lists them to each other, so there is nothing left to hand-configure).
type PeerInstance struct {
	Hostname string `json:"hostname"`
	URL      string `json:"url"`
}

// Peers lists every OTHER device in the SAME Tailscale account (never a
// device merely shared into this tailnet by someone else - see the UserID
// comparison below, the one thing that keeps this from surfacing a
// housemate's laptop just because it is visible on the same tailnet) that
// is online right now and answers like a KnightLoader instance. Nil,
// without error, when not connected - the caller reads that exactly like
// "no candidates yet", not a failure.
//
// The probe (looksLikeKnightLoader) is what keeps this from listing every
// device the person owns - a phone, a router, anything else living in the
// same tailnet - as if it were a KnightLoader instance to add.
func (m *Manager) Peers(ctx context.Context) ([]PeerInstance, error) {
	m.mu.Lock()
	srv := m.srv
	connected := m.status == StatusConnected
	m.mu.Unlock()
	if srv == nil || !connected {
		return nil, nil
	}

	lc, err := srv.LocalClient()
	if err != nil {
		return nil, err
	}
	st, err := lc.Status(ctx)
	if err != nil {
		return nil, err
	}
	if st.Self == nil {
		return nil, nil
	}

	// Dialled through the embedded tailnet network stack, not the ordinary
	// internet - the whole reason this needs no new port and no firewall
	// rule on either side is that both instances already speak to each
	// other over the same private mesh their Funnel address rides on top
	// of.
	client := srv.HTTPClient()
	var out []PeerInstance
	for _, p := range st.Peer {
		if p == nil || !p.Online || p.UserID != st.Self.UserID {
			continue
		}
		host := strings.TrimSuffix(p.DNSName, ".")
		if host == "" {
			continue
		}
		if !looksLikeKnightLoader(ctx, client, host) {
			continue
		}
		out = append(out, PeerInstance{Hostname: p.HostName, URL: "https://" + host})
	}
	return out, nil
}

// looksLikeKnightLoader probes a candidate peer's own /api/health the same
// way any KnightLoader client already does, entirely over the tailnet. A
// short per-peer timeout keeps one slow or unreachable device from
// stalling the whole list; a non-KnightLoader device failing this check is
// the expected, silent case; it is skipped, not reported as an error.
func looksLikeKnightLoader(ctx context.Context, client *http.Client, host string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/api/health", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Status string `json:"status"`
	}
	// Capped, not because a real /api/health response is ever large, but
	// because this reads whatever the OTHER end of a probe this package
	// initiated sends back - the same reasoning any response body read from
	// a peer, not a request this instance itself validated, gets elsewhere
	// in this codebase.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body); err != nil {
		return false
	}
	return body.Status == "ok"
}
