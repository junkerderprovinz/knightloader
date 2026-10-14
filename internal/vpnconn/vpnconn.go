// Package vpnconn runs the one VPN connection the built-in torrent client goes
// through: the providers of the VPN page, their sealed logins, the tunnel and
// the port the provider forwards. Everything else of KnightLoader stays on the
// machine's own connection.
package vpnconn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/GopeedLab/gopeed/pkg/netbind"
	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

// NetworkName is the name the tunnel is registered under with netbind, and so
// the torrent client's interface while a provider is in use.
const NetworkName = "knightloader-vpn"

// Service is the credential store's service the logins are sealed under, one
// account per provider.
const Service = "vpn"

// Store seals the logins; accounts.Store is one.
type Store interface {
	GetCredential(service, account string) (accounts.Credential, error)
	SetCredential(service, account string, cred accounts.Credential) error
	AccountIDs(service string) []string
}

// Options configure a Manager.
type Options struct {
	Store Store
	// StatePath keeps which provider is in use across restarts.
	StatePath string
	// ManualPort is the torrent settings' port, which a provider that has the
	// user forward a port in its account forwards.
	ManualPort func() int
	// OnChange runs whenever the provider in use, the tunnel or the forwarded
	// port changes, so the torrent client's configuration can follow.
	OnChange func()
	Logf     func(format string, args ...any)
	// ExitIPURL answers with the address a request through the tunnel comes
	// from.
	ExitIPURL string
	// ConnectTimeout bounds opening a session and the first handshake before
	// the page says the server did not answer; the tunnel keeps trying.
	ConnectTimeout time.Duration
	// RenewAfter is how long the tunnel of a provider whose sessions can end
	// may stay down before the session is opened again.
	RenewAfter time.Duration
	// Tunnel tunes the tunnel, for tests.
	Tunnel vpn.Options
}

// ProviderState is one provider as the VPN page shows it
// (web/src/lib/api.ts, VpnProvider).
type ProviderState struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Fields         []string `json:"fields"`
	PortForwarding bool     `json:"portForwarding"`
	Configured     bool     `json:"configured"`
	State          string   `json:"state"`
	ExitIP         string   `json:"exitIp,omitempty"`
	ForwardedPort  int      `json:"forwardedPort,omitempty"`
	Error          string   `json:"error,omitempty"`
	ErrorCode      string   `json:"errorCode,omitempty"`
}

// State is the VPN page's whole state (web/src/lib/api.ts, VpnState).
type State struct {
	Active    string          `json:"active"`
	Providers []ProviderState `json:"providers"`
}

// Refusal is a call the routes answer with an error status, a code the page
// words and, for a missing field, the field.
type Refusal struct {
	Status int
	Code   string
	Field  string
	Text   string
}

func (r *Refusal) Error() string { return r.Text }

// Manager owns the tunnel and the provider in use.
type Manager struct {
	opts      Options
	tun       *vpn.Tunnel
	providers []Provider

	mu      sync.Mutex
	active  string
	fail    *failure
	exitIP  string
	port    uint16
	sess    *session
	stopRun context.CancelFunc
	runDone chan struct{}
}

// New returns a Manager with no provider in use; Resume takes up the one in
// use before.
func New(opts Options) (*Manager, error) {
	if opts.ManualPort == nil {
		opts.ManualPort = func() int { return 0 }
	}
	if opts.OnChange == nil {
		opts.OnChange = func() {}
	}
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	if opts.ExitIPURL == "" {
		opts.ExitIPURL = "https://api.ipify.org"
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 45 * time.Second
	}
	if opts.RenewAfter <= 0 {
		opts.RenewAfter = 90 * time.Second
	}
	if opts.Tunnel.Logf == nil {
		opts.Tunnel.Logf = opts.Logf
	}
	m := &Manager{opts: opts, tun: vpn.New(opts.Tunnel), providers: catalogue()}
	m.tun.Watch(func(vpn.State) {
		netbind.Default.Refresh()
		m.opts.OnChange()
	})
	return m, nil
}

func (m *Manager) provider(id string) *Provider {
	i := slices.IndexFunc(m.providers, func(p Provider) bool { return p.ID == id })
	if i < 0 {
		return nil
	}
	return &m.providers[i]
}

// Active is the provider torrents go through, "" for none.
func (m *Manager) Active() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// ForwardedPort is the port the provider in use forwards through NAT-PMP, 0
// while there is none. A port the user forwards in the provider's account is
// the torrent settings' own and is not repeated here.
func (m *Manager) ForwardedPort() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int(m.port)
}

// State is the VPN page's state.
func (m *Manager) State() State {
	stored := map[string]bool{}
	for _, id := range m.opts.Store.AccountIDs(Service) {
		stored[id] = true
	}
	ts := m.tun.State()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := State{Active: m.active, Providers: make([]ProviderState, 0, len(m.providers))}
	for _, p := range m.providers {
		ps := ProviderState{
			ID: p.ID, Name: p.Name, Fields: slices.Clone(p.Fields), PortForwarding: p.PortForwarding,
			Configured: stored[p.ID], State: "idle",
		}
		if p.ID == m.active {
			m.fillActive(&ps, p, ts)
		}
		out.Providers = append(out.Providers, ps)
	}
	return out
}

func (m *Manager) fillActive(ps *ProviderState, p Provider, ts vpn.State) {
	switch {
	case ts.Status == vpn.Connected:
		ps.State = "connected"
		ps.ExitIP = m.exitIP
		switch p.port {
		case natpmpPort:
			ps.ForwardedPort = int(m.port)
		case manualPort:
			ps.ForwardedPort = m.opts.ManualPort()
		}
	case m.fail != nil:
		ps.State = "failed"
		ps.Error, ps.ErrorCode = m.fail.Error(), m.fail.code
	case ts.Status == vpn.Reconnecting:
		ps.State = "failed"
		ps.Error, ps.ErrorCode = "The connection dropped; KnightLoader keeps trying.", CodeDropped
	default:
		ps.State = "connecting"
	}
}

// Connect makes id the provider torrents go through, ending any other. Fields
// left empty keep the stored login. It returns once the login is stored; the
// connection follows, and State tells how it goes.
func (m *Manager) Connect(id string, fields map[string]string) (State, error) {
	p := m.provider(id)
	if p == nil {
		return State{}, &Refusal{Status: http.StatusNotFound, Code: "vpn-unknown", Text: fmt.Sprintf("no VPN provider %q", id)}
	}
	if p.open == nil {
		return State{}, &Refusal{Status: http.StatusNotImplemented, Code: "vpn-unsupported",
			Text: fmt.Sprintf("KnightLoader cannot log in to %s yet", p.Name)}
	}
	login := m.login(id)
	for k, v := range fields {
		if !slices.Contains(p.Fields, k) {
			return State{}, &Refusal{Status: http.StatusBadRequest, Code: "vpn-field-unknown", Field: k,
				Text: fmt.Sprintf("%s asks for no %s", p.Name, k)}
		}
		if strings.TrimSpace(v) != "" && login[k] != v {
			login[k] = v
			// A token belongs to the login it was fetched with.
			delete(login, accessTokenField)
		}
	}
	for _, f := range p.Fields {
		if f != "location" && strings.TrimSpace(login[f]) == "" {
			return State{}, &Refusal{Status: http.StatusBadRequest, Code: "vpn-field-missing", Field: f, Text: f + " is missing"}
		}
	}
	if err := m.seal(id, login); err != nil {
		return State{}, err
	}
	// Registered while a provider is in use, so the torrent client's binder
	// finds the tunnel under the name its interface is set to.
	if err := netbind.Register(NetworkName, m.tun); err != nil {
		return State{}, err
	}
	m.stop()
	m.mu.Lock()
	m.active, m.fail, m.exitIP, m.port = id, nil, "", 0
	ctx, cancel := context.WithCancel(context.Background())
	m.stopRun, m.runDone = cancel, make(chan struct{})
	done := m.runDone
	m.mu.Unlock()
	m.saveActive(id)
	go m.run(ctx, done, *p, login)
	m.opts.OnChange()
	return m.State(), nil
}

// Disconnect ends the connection; torrents go back to the machine's own.
func (m *Manager) Disconnect() State {
	m.stop()
	m.tun.Disconnect()
	netbind.Register(NetworkName, nil)
	m.mu.Lock()
	m.active, m.fail, m.exitIP, m.port = "", nil, "", 0
	m.mu.Unlock()
	m.saveActive("")
	m.opts.OnChange()
	return m.State()
}

// Forget deletes a provider's stored login, disconnecting it first.
func (m *Manager) Forget(id string) (State, error) {
	if m.provider(id) == nil {
		return State{}, &Refusal{Status: http.StatusNotFound, Code: "vpn-unknown", Text: fmt.Sprintf("no VPN provider %q", id)}
	}
	if m.Active() == id {
		m.Disconnect()
	}
	if err := m.opts.Store.SetCredential(Service, id, accounts.Credential{}); err != nil {
		return State{}, err
	}
	return m.State(), nil
}

// Resume connects the provider that was in use when the process last ended.
func (m *Manager) Resume() {
	b, err := os.ReadFile(m.opts.StatePath)
	if err != nil {
		return
	}
	var st struct{ Active string }
	if json.Unmarshal(b, &st) != nil || st.Active == "" {
		return
	}
	if _, err := m.Connect(st.Active, nil); err != nil {
		m.opts.Logf("vpn: %s not taken up again: %v", st.Active, err)
	}
}

// Close ends the connection and keeps which provider was in use, for Resume.
func (m *Manager) Close() {
	m.stop()
	m.tun.Disconnect()
	if m.Active() != "" {
		netbind.Register(NetworkName, nil)
	}
}

// stop ends the run and the session of the provider in use.
func (m *Manager) stop() {
	m.mu.Lock()
	cancel, done, sess := m.stopRun, m.runDone, m.sess
	m.stopRun, m.runDone, m.sess = nil, nil, nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	endSoon(sess)
}

func (m *Manager) login(id string) Login {
	login := Login{}
	cred, err := m.opts.Store.GetCredential(Service, id)
	if err == nil && cred.APIKey != "" {
		json.Unmarshal([]byte(cred.APIKey), &login)
	}
	return login
}

func (m *Manager) seal(id string, login Login) error {
	b, err := json.Marshal(login)
	if err != nil {
		return err
	}
	return m.opts.Store.SetCredential(Service, id, accounts.Credential{APIKey: string(b)})
}

func (m *Manager) saveActive(id string) {
	if m.opts.StatePath == "" {
		return
	}
	b, _ := json.Marshal(struct {
		Active string `json:"active"`
	}{id})
	if err := os.WriteFile(m.opts.StatePath, b, 0o600); err != nil {
		m.opts.Logf("vpn: keeping the provider in use: %v", err)
	}
}

func (m *Manager) setFail(f *failure) {
	m.mu.Lock()
	m.fail = f
	m.mu.Unlock()
	if f != nil {
		m.opts.Logf("vpn: %v", f.err)
	}
	m.opts.OnChange()
}

func (m *Manager) setPort(p uint16) {
	m.mu.Lock()
	changed := m.port != p
	m.port = p
	m.mu.Unlock()
	if changed {
		m.opts.OnChange()
	}
}

// run opens a session, brings the tunnel up on it and keeps the port
// forwarded, until ctx ends. A session the provider can end is opened anew
// when the tunnel stays down.
func (m *Manager) run(ctx context.Context, done chan struct{}, p Provider, login Login) {
	defer close(done)
	for ctx.Err() == nil {
		sess, err := m.open(ctx, p, login)
		if err != nil {
			if ctx.Err() == nil {
				m.setFail(classify(err))
				// Nothing to retry with; a new connect brings a new login.
				var f *failure
				if errors.As(err, &f) && (f.code == CodeConfigInvalid || f.code == CodeLoginRefused) {
					return
				}
				wait(ctx, m.opts.RenewAfter)
			}
			continue
		}
		if sess.login != nil {
			login = sess.login
			m.seal(p.ID, login)
		}
		m.mu.Lock()
		m.sess = sess
		m.mu.Unlock()
		m.serve(ctx, p, sess)
		// stop takes the session over when it ends the run.
		m.mu.Lock()
		owned := m.sess == sess
		if owned {
			m.sess = nil
		}
		m.mu.Unlock()
		if owned {
			endSoon(sess)
		}
	}
}

func (m *Manager) open(ctx context.Context, p Provider, login Login) (*session, error) {
	octx, cancel := context.WithTimeout(ctx, m.opts.ConnectTimeout)
	defer cancel()
	sess, err := p.open(octx, login)
	if err != nil {
		return nil, err
	}
	if err := m.tun.Connect(octx, sess.cfg); err != nil {
		endSoon(sess)
		return nil, &failure{CodeConfigInvalid, err}
	}
	return sess, nil
}

// serve waits for the handshake, then keeps the port forwarded while the
// tunnel stands. It returns when ctx ends, or when a session that can end has
// had its tunnel down for RenewAfter.
func (m *Manager) serve(ctx context.Context, p Provider, sess *session) {
	wctx, cancel := context.WithTimeout(ctx, m.opts.ConnectTimeout)
	err := m.tun.WaitConnected(wctx)
	cancel()
	if err != nil && ctx.Err() == nil {
		m.setFail(&failure{CodeTimeout, fmt.Errorf("no handshake with %s", sess.cfg.Peer.Endpoint)})
	}
	if err := m.tun.WaitConnected(ctx); err != nil {
		return
	}
	m.setFail(nil)
	go m.lookUpExitIP(ctx)

	pctx, stopPorts := context.WithCancel(ctx)
	defer stopPorts()
	if p.port == natpmpPort {
		go m.forwardPort(pctx, p, sess)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var downSince time.Time
	for {
		select {
		case <-ctx.Done():
			m.setPort(0)
			return
		case <-tick.C:
		}
		if m.tun.State().Status == vpn.Connected {
			downSince = time.Time{}
			continue
		}
		if downSince.IsZero() {
			downSince = time.Now()
		}
		if sess.renews && time.Since(downSince) >= m.opts.RenewAfter {
			m.opts.Logf("vpn: %s stayed down, opening a new session", p.Name)
			stopPorts()
			m.setPort(0)
			return
		}
	}
}

func (m *Manager) forwardPort(ctx context.Context, p Provider, sess *session) {
	gw := p.gateway
	if sess.gateway.IsValid() {
		gw = sess.gateway
	}
	if !gw.IsValid() {
		return
	}
	if sess.forward != nil {
		if err := sess.forward(ctx, m.tun.DialContext); err != nil {
			m.opts.Logf("vpn: %s port forwarding: %v", p.Name, err)
			return
		}
	}
	pf := &vpn.PortForward{
		Client:   &vpn.NATPMP{Gateway: gw, Dial: m.tun.DialContext},
		Prefer:   uint16(m.opts.ManualPort()),
		OnChange: m.setPort,
		Logf:     m.opts.Logf,
	}
	pf.Run(ctx)
}

func (m *Manager) lookUpExitIP(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tr := &http.Transport{DialContext: m.tun.DialContext}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.opts.ExitIPURL, nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		m.opts.Logf("vpn: exit address: %v", err)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	m.mu.Lock()
	m.exitIP = strings.TrimSpace(string(b))
	m.mu.Unlock()
	m.opts.OnChange()
}

func wait(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
