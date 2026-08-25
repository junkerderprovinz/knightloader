// Package federation lets one KnightLoader act as the dashboard for others:
// peer instances are stored locally, and their REST APIs are proxied so the UI
// can view and control every instance from one place.
//
// A peer is reached one of two ways, and List/Proxy hide which: over plain
// HTTP to an address somebody stored (the original path - a LAN IP, or a
// domain behind a reverse proxy), or through a self-hosted relay both sides
// dial out to when neither can accept an inbound connection. The relay is
// additive and optional; with none configured this package behaves exactly as
// it did before one existed.
package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// Instance is a peer KnightLoader, either reachable over HTTP at URL or
// visible through the relay under RelayID.
type Instance struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// RelayID is the instance ID a call is addressed to when this peer is only
	// reachable through the relay, and is empty for every stored peer. It is
	// never written to instances.json, because it is never true for longer
	// than the relay connection that produced it: a relay peer comes and goes
	// live, and one remembered across a restart would be a peer this instance
	// cannot reach and cannot explain.
	//
	// The UI reads it to say how a peer is reached; the API layer does not
	// need to, which is the point of it being on the same struct.
	RelayID string `json:"relayId,omitempty"`
}

// RelayTransport is the relay client seen from here: the sibling list it keeps
// live, one call over it, and the ability to shut the connection down.
// Declared as an interface rather than taking *relay.Client so this package's
// tests can exercise a relay peer without a socket - *relay.Client satisfies
// it as it stands, with no adapter.
type RelayTransport interface {
	// Siblings is the instances the relay makes visible right now. It is empty
	// while the relay is unreachable, which is the whole of the outage
	// handling here: no relay, no relay peers, nothing else affected.
	Siblings() []relay.Announce
	// Proxy calls one sibling, addressed by its instance ID.
	Proxy(ctx context.Context, target, method, path string, body []byte) ([]byte, int, error)
	// Close stops the connection for good. SetRelay calls it on whatever
	// transport it is replacing, which is what makes reconfiguring the relay
	// (a new address, a new key, or switching it off) safe to call as often
	// as a settings save happens, rather than leaking one socket per save.
	Close() error
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,31}$`)

// peerTimeout bounds one call to another instance. A peer that is switched off
// answers instantly; one that is reachable but wedged is the case this exists
// for, and the dashboard polls every peer, so a generous ceiling here is paid
// once per peer per tick.
const peerTimeout = 15 * time.Second

// Manager persists the peer list and talks to peers.
type Manager struct {
	path string
	hc   *http.Client

	mu   sync.Mutex
	list map[string]Instance // by name
	rt   RelayTransport      // nil until SetRelay, and again after it is cleared
}

// Load reads instances.json from dir (missing file = empty list).
func Load(dir string) (*Manager, error) {
	m := &Manager{
		path: filepath.Join(dir, "instances.json"),
		hc:   httpx.New(httpx.Options{Timeout: peerTimeout}),
		list: map[string]Instance{},
	}
	if b, err := os.ReadFile(m.path); err == nil {
		var arr []Instance
		if json.Unmarshal(b, &arr) == nil {
			for _, in := range arr {
				m.list[in.Name] = in
			}
		}
	}
	return m, nil
}

// SetRelay installs the transport that reaches relay-visible peers, or clears
// it with nil when relay mode is switched off - and closes whatever transport
// it is replacing, so calling this again (a new address, a new key, an
// application restart's own first call) never leaks the previous connection.
// Peers the relay reports are not stored and not persisted: they appear and
// disappear with the relay connection itself, so there is nothing here to
// save and nothing to load back.
func (m *Manager) SetRelay(rt RelayTransport) {
	m.mu.Lock()
	prev := m.rt
	m.rt = rt
	m.mu.Unlock()
	if prev != nil {
		_ = prev.Close()
	}
}

// List returns the peers sorted by name: the stored ones plus whatever the
// relay currently makes visible, in one list, because the Instances page shows
// one list.
func (m *Manager) List() []Instance {
	all, _ := m.reachable()
	out := make([]Instance, 0, len(all))
	for _, in := range all {
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// reachable is every peer addressable right now, keyed by the name the API
// layer addresses it as, together with the transport the relay ones need. Both
// come out of one call so a relay that disconnects between resolving a name
// and using it cannot leave a relay peer with no way to reach it.
//
// A stored peer always wins a name: it is the one somebody configured
// deliberately, and the one that keeps working when the relay does not. A
// relay peer whose name is taken - by a stored peer, or by another relay peer
// that sorted ahead of it - is listed under its instance ID instead. That is
// not a rare edge: an instance nobody has named announces its hostname, and
// two containers from the same image routinely have the same one.
func (m *Manager) reachable() (map[string]Instance, RelayTransport) {
	m.mu.Lock()
	rt := m.rt
	out := make(map[string]Instance, len(m.list))
	for name, in := range m.list {
		out[name] = in
	}
	m.mu.Unlock()
	if rt == nil {
		return out, nil
	}
	// Siblings is sorted by instance ID, so which of two same-named peers
	// keeps the name is the same on every call rather than whichever the map
	// happened to yield first.
	for _, sib := range rt.Siblings() {
		name := sib.Name
		if _, taken := out[name]; taken || name == "" {
			name = sib.InstanceID
		}
		if _, taken := out[name]; taken {
			continue
		}
		out[name] = Instance{Name: name, RelayID: sib.InstanceID}
	}
	return out, rt
}

// Add validates and stores a peer (overwrites the same name).
func (m *Manager) Add(in Instance) error {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	// A stored peer is an HTTP peer by definition. Dropping this rather than
	// rejecting it keeps the route that decodes an Instance straight off a
	// request body (routes_federation.go) from turning a field the UI only
	// reads into a way to store a peer that claims a relay identity.
	in.RelayID = ""
	if !nameRe.MatchString(in.Name) {
		return errors.New("federation: invalid instance name")
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("federation: instance URL must be http(s)")
	}
	m.mu.Lock()
	m.list[in.Name] = in
	err = m.flushLocked()
	m.mu.Unlock()
	return err
}

// Remove deletes a peer by name.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	delete(m.list, name)
	err := m.flushLocked()
	m.mu.Unlock()
	return err
}

func (m *Manager) flushLocked() error {
	arr := make([]Instance, 0, len(m.list))
	for _, in := range m.list {
		arr = append(arr, in)
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Name < arr[j].Name })
	b, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0o600)
}

// Proxy forwards an API call to a peer and returns its response body.
// method + path are the peer-local API route (e.g. GET /api/tasks).
//
// The transport is chosen from the peer, not from the caller: a relay-visible
// peer goes over the relay, a stored one over HTTP, and the route that calls
// this (and the page behind it) never learns which.
func (m *Manager) Proxy(ctx context.Context, name, method, path string, body []byte) ([]byte, int, error) {
	all, rt := m.reachable()
	in, ok := all[name]
	if !ok {
		return nil, http.StatusNotFound, fmt.Errorf("federation: unknown instance %q", name)
	}
	// rt is never nil here when RelayID is set: the entry came from that same
	// transport in the same call, above.
	if in.RelayID != "" {
		return rt.Proxy(ctx, in.RelayID, method, path, body)
	}
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, in.URL+path, rd)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s unreachable: %w", in.Name, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return b, resp.StatusCode, nil
}

// Ping checks a peer by listing its tasks.
func (m *Manager) Ping(ctx context.Context, name string) error {
	_, code, err := m.Proxy(ctx, name, http.MethodGet, "/api/tasks", nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("federation: peer answered HTTP %d", code)
	}
	return nil
}
