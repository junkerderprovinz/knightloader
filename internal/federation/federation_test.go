package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// fakeRelay stands in for *relay.Client, which satisfies RelayTransport as it
// stands. Siblings are returned in the order given, sorted by instance ID the
// way the real client sorts them, so the collision rule below is exercised
// against the same input order production sees.
type fakeRelay struct {
	sibs []relay.Announce

	// The last call this transport was asked to make, so a test can prove the
	// instance ID was used as the address rather than the display name.
	target, method, path string
	body                 []byte

	resp   []byte
	status int
	err    error

	closed bool
}

func (f *fakeRelay) Siblings() []relay.Announce { return f.sibs }

func (f *fakeRelay) Proxy(_ context.Context, target, method, path string, body []byte) ([]byte, int, error) {
	f.target, f.method, f.path, f.body = target, method, path, body
	return f.resp, f.status, f.err
}

func (f *fakeRelay) Close() error {
	f.closed = true
	return nil
}

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m
}

// TestManualPeersAreUntouchedByRelaySupport: the stored-peer path is the one
// that already worked, and adding a second transport must not have moved any
// of it - the list, the file it is written to, and the HTTP call all behave as
// they did before a relay existed.
func TestManualPeersAreUntouchedByRelaySupport(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks" {
			t.Errorf("peer was asked for %s, want /api/tasks", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"t1"}]`))
	}))
	defer peer.Close()

	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.Add(Instance{Name: "NAS", URL: peer.URL}); err != nil {
		t.Fatalf("add: %v", err)
	}

	list := m.List()
	if len(list) != 1 || list[0].Name != "NAS" || list[0].URL != peer.URL || list[0].RelayID != "" {
		t.Fatalf("got %+v, want one stored HTTP peer", list)
	}

	body, code, err := m.Proxy(context.Background(), "NAS", http.MethodGet, "/api/tasks", nil)
	if err != nil || code != http.StatusOK || string(body) != `[{"id":"t1"}]` {
		t.Errorf("got %s %d %v, want the peer's own answer", body, code, err)
	}
	if err := m.Ping(context.Background(), "NAS"); err != nil {
		t.Errorf("ping: %v", err)
	}

	// The stored file is the other half of "unchanged": a reloaded manager has
	// to find the same peer, with no relay field written into it.
	saved, err := os.ReadFile(filepath.Join(dir, "instances.json"))
	if err != nil {
		t.Fatalf("read instances.json: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(saved, &arr); err != nil {
		t.Fatalf("instances.json: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("instances.json holds %v, want one entry", arr)
	}
	if _, ok := arr[0]["relayId"]; ok {
		t.Errorf("instances.json holds %v, want no relay field on a stored peer", arr[0])
	}
}

// TestRelayPeersAppearWithoutBeingStored: relay peers come and go with the
// connection, so they must show up in List and never reach instances.json.
func TestRelayPeersAppearWithoutBeingStored(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rt := &fakeRelay{sibs: []relay.Announce{
		{InstanceID: "id-bravo", Name: "Laptop", Deployment: "desktop"},
	}}
	m.SetRelay(rt)

	list := m.List()
	if len(list) != 1 || list[0].Name != "Laptop" || list[0].RelayID != "id-bravo" || list[0].URL != "" {
		t.Fatalf("got %+v, want one relay peer named Laptop", list)
	}
	if _, err := os.Stat(filepath.Join(dir, "instances.json")); !os.IsNotExist(err) {
		t.Errorf("instances.json exists, want a relay peer never written to disk")
	}

	// The relay going away takes its peers with it, and nothing else.
	rt.sibs = nil
	if list := m.List(); len(list) != 0 {
		t.Errorf("got %+v, want no peers once the relay sees none", list)
	}
	m.SetRelay(nil)
	if list := m.List(); len(list) != 0 {
		t.Errorf("got %+v, want no peers with relay mode switched off", list)
	}
	if !rt.closed {
		t.Error("SetRelay(nil) never closed the transport it replaced")
	}
}

// TestSetRelayClosesTheTransportItReplaces: reconfiguring the relay (a new
// address, a new key) has to close the old connection rather than leaking it
// - this is what makes SetRelay safe to call on every settings save instead
// of only once at boot.
func TestSetRelayClosesTheTransportItReplaces(t *testing.T) {
	m := newManager(t)
	first := &fakeRelay{}
	second := &fakeRelay{}

	m.SetRelay(first)
	if first.closed {
		t.Fatal("the transport was closed before anything replaced it")
	}
	m.SetRelay(second)
	if !first.closed {
		t.Error("the old transport was never closed when a new one replaced it")
	}
	if second.closed {
		t.Error("the new transport was closed immediately after being installed")
	}
}

// TestProxyReachesARelayPeerThroughTheTransport: the API layer calls one Proxy
// with a name and never learns which transport carried it.
func TestProxyReachesARelayPeerThroughTheTransport(t *testing.T) {
	m := newManager(t)
	rt := &fakeRelay{
		sibs:   []relay.Announce{{InstanceID: "id-bravo", Name: "Laptop"}},
		resp:   []byte(`{"added":1}`),
		status: http.StatusCreated,
	}
	m.SetRelay(rt)

	body, code, err := m.Proxy(context.Background(), "Laptop", http.MethodPost, "/api/links", []byte(`{"url":"x"}`))
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if code != http.StatusCreated || string(body) != `{"added":1}` {
		t.Errorf("got %d %s, want the peer's own answer", code, body)
	}
	// Addressed by instance ID, not by the name the page shows: two peers can
	// share a name, and only the ID routes.
	if rt.target != "id-bravo" || rt.method != http.MethodPost || rt.path != "/api/links" || string(rt.body) != `{"url":"x"}` {
		t.Errorf("the transport was asked for %s %s %s %s, want the call unchanged and addressed by ID",
			rt.target, rt.method, rt.path, rt.body)
	}
}

// TestPeerNamesCollide: an instance nobody has named announces its hostname,
// and two containers from one image routinely share it - so every peer has to
// stay addressable under some name, and a stored peer must never lose its own.
func TestPeerNamesCollide(t *testing.T) {
	tests := []struct {
		name   string
		stored []Instance
		sibs   []relay.Announce
		want   map[string]string // name -> relay ID ("" for a stored peer)
	}{
		{
			name: "a stored peer keeps its name",
			// A port nothing listens on: this peer is only ever resolved
			// here, never called, and refusing instantly keeps the resolve
			// check below from waiting out a real peerTimeout.
			stored: []Instance{{Name: "NAS", URL: "http://127.0.0.1:1"}},
			sibs:   []relay.Announce{{InstanceID: "id-nas", Name: "NAS"}},
			want:   map[string]string{"NAS": "", "id-nas": "id-nas"},
		},
		{
			name: "two relay peers with one hostname",
			sibs: []relay.Announce{
				{InstanceID: "id-a", Name: "knightloader"},
				{InstanceID: "id-b", Name: "knightloader"},
			},
			want: map[string]string{"knightloader": "id-a", "id-b": "id-b"},
		},
		{
			name: "a peer that announced no name at all",
			sibs: []relay.Announce{{InstanceID: "id-a"}},
			want: map[string]string{"id-a": "id-a"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(t)
			for _, in := range tc.stored {
				if err := m.Add(in); err != nil {
					t.Fatalf("add %s: %v", in.Name, err)
				}
			}
			m.SetRelay(&fakeRelay{sibs: tc.sibs})

			got := map[string]string{}
			for _, in := range m.List() {
				got[in.Name] = in.RelayID
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for name, relayID := range tc.want {
				if got[name] != relayID {
					t.Errorf("%q is reached as %q, want %q", name, got[name], relayID)
				}
			}
			// Every name in the list has to actually resolve, which is the
			// whole point of handing them out. 404 is the one status Proxy
			// gives a name it could not place; whether the peer behind it
			// then answers is a different question, and not this one.
			for name := range got {
				if _, code, _ := m.Proxy(context.Background(), name, http.MethodGet, "/api/tasks", nil); code == http.StatusNotFound {
					t.Errorf("%q is listed but does not resolve to a peer", name)
				}
			}
		})
	}
}

// TestUnknownInstanceIsStill404: a name nobody serves must not become a relay
// call to an empty target now that there are two transports to pick from.
func TestUnknownInstanceIsStill404(t *testing.T) {
	m := newManager(t)
	rt := &fakeRelay{sibs: []relay.Announce{{InstanceID: "id-bravo", Name: "Laptop"}}}
	m.SetRelay(rt)

	_, code, err := m.Proxy(context.Background(), "Nowhere", http.MethodGet, "/api/tasks", nil)
	if err == nil || code != http.StatusNotFound {
		t.Errorf("got %d %v, want a 404", code, err)
	}
	if rt.target != "" {
		t.Errorf("the transport was called with %q, want an unknown name never routed", rt.target)
	}
}

// TestAddNeverStoresARelayIdentity: routes_federation decodes an Instance
// straight off a request body, so the field the UI only reads must not be a
// way to store a peer that claims to be relay-reachable.
func TestAddNeverStoresARelayIdentity(t *testing.T) {
	m := newManager(t)
	if err := m.Add(Instance{Name: "NAS", URL: "http://192.168.20.30:8749", RelayID: "id-somebody-else"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	list := m.List()
	if len(list) != 1 || list[0].RelayID != "" {
		t.Fatalf("got %+v, want the relay identity dropped", list)
	}
}
