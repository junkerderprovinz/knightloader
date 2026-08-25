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
	// auth is what the credential hook produced for this call, so a test
	// can prove a peer token reaches the relay transport too.
	auth string

	resp   []byte
	status int
	err    error

	// down makes Connected() report false, for the case a relay is configured
	// but unreachable - which must NOT look the same as no relay at all.
	down   bool
	closed bool
}

func (f *fakeRelay) Siblings() []relay.Announce { return f.sibs }

func (f *fakeRelay) Connected() bool { return !f.down }

func (f *fakeRelay) Proxy(_ context.Context, target, method, path string, body []byte, authorization string) ([]byte, int, error) {
	f.target, f.method, f.path, f.body = target, method, path, body
	f.auth = authorization
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

	// Name is the address (always the InstanceID for a relay peer, see
	// reachable's own doc comment) - DisplayName is what carries "Laptop".
	list := m.List()
	if len(list) != 1 || list[0].Name != "id-bravo" || list[0].DisplayName != "Laptop" || list[0].RelayID != "id-bravo" || list[0].URL != "" {
		t.Fatalf("got %+v, want one relay peer addressed as id-bravo, displayed as Laptop", list)
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

	// Addressed by instance ID, the peer's Name - "Laptop" is only its
	// DisplayName, and DisplayName is never an address (see the assertion
	// below, and reachable's own doc comment for why).
	body, code, err := m.Proxy(context.Background(), "id-bravo", http.MethodPost, "/api/links", []byte(`{"url":"x"}`))
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if code != http.StatusCreated || string(body) != `{"added":1}` {
		t.Errorf("got %d %s, want the peer's own answer", code, body)
	}
	if rt.target != "id-bravo" || rt.method != http.MethodPost || rt.path != "/api/links" || string(rt.body) != `{"url":"x"}` {
		t.Errorf("the transport was asked for %s %s %s %s, want the call unchanged and addressed by ID",
			rt.target, rt.method, rt.path, rt.body)
	}
	if _, code, err := m.Proxy(context.Background(), "Laptop", http.MethodGet, "/api/tasks", nil); err == nil || code != http.StatusNotFound {
		t.Errorf("got %d %v proxying by the display name, want a 404 - display names never route", code, err)
	}
}

// TestPeerNamesCollide: an instance nobody has named announces its hostname,
// and two containers from one image routinely share it, so a shared
// DisplayName must never keep a peer from being listed or reached - and a
// stored peer must never lose its own address to a relay peer sharing its
// name, because a relay peer no longer competes for one at all.
func TestPeerNamesCollide(t *testing.T) {
	tests := []struct {
		name   string
		stored []Instance
		sibs   []relay.Announce
		want   map[string]string // address (Name) -> relay ID ("" for a stored peer)
	}{
		{
			name: "a stored peer keeps its name even when a relay peer announces the same one",
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
			want: map[string]string{"id-a": "id-a", "id-b": "id-b"},
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
			// Every address in the list has to actually resolve, which is the
			// whole point of handing them out. 404 is the one status Proxy
			// gives an address it could not place; whether the peer behind it
			// then answers is a different question, and not this one.
			for name := range got {
				if _, code, _ := m.Proxy(context.Background(), name, http.MethodGet, "/api/tasks", nil); code == http.StatusNotFound {
					t.Errorf("%q is listed but does not resolve to a peer", name)
				}
			}
		})
	}
}

// TestRelayPeerAddressSurvivesUnrelatedChanges is the regression this whole
// scheme exists for: a relay peer's address (its Name/InstanceID) must never
// change because something ELSE about the reachable set changed - not a
// same-named stored peer coming or going, and not another sibling coming or
// going. The older, name-first scheme flipped a peer between its friendly
// name and its ID exactly on events like these, silently breaking anything
// that had cached the address from before.
func TestRelayPeerAddressSurvivesUnrelatedChanges(t *testing.T) {
	m := newManager(t)
	rt := &fakeRelay{sibs: []relay.Announce{{InstanceID: "id-a", Name: "Cellar"}}}
	m.SetRelay(rt)
	addressBefore := m.List()[0].Name

	// A stored peer sharing the relay peer's DisplayName arrives...
	if err := m.Add(Instance{Name: "Cellar", URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// ...and a second sibling joins, then leaves again.
	rt.sibs = append(rt.sibs, relay.Announce{InstanceID: "id-b", Name: "Other"})
	rt.sibs = rt.sibs[:1]
	// ...and the stored peer is removed again.
	if err := m.Remove("Cellar"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	list := m.List()
	if len(list) != 1 || list[0].Name != addressBefore {
		t.Fatalf("relay peer's address is %q after unrelated churn, want it unchanged at %q", list, addressBefore)
	}
	if _, code, _ := m.Proxy(context.Background(), addressBefore, http.MethodGet, "/api/tasks", nil); code == http.StatusNotFound {
		t.Errorf("the original address no longer resolves after unrelated churn")
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

// TestClientOnlySiblingsAreNotListedAsInstances: the mobile companion app
// joins the relay key to CALL instances, not to be one - it serves no API, so
// an entry for it in this list would be somewhere the UI offers to open and
// which then answers 501 to every route. Every connection must announce
// before the relay will join it to a key, so "just don't announce" is not
// available and the announce carries a flag instead.
//
// Pinned as its own test because the failure is silent and remote: it would
// show up as a stray, broken peer on OTHER people's Instances pages, not
// anywhere the phone's own owner would look.
func TestClientOnlySiblingsAreNotListedAsInstances(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: "id-nas", Name: "Cellar", Deployment: "container"},
		{InstanceID: "id-phone", Name: "Pixel", Deployment: "mobile", Client: true},
	}})

	list := m.List()
	if len(list) != 1 || list[0].Name != "id-nas" {
		t.Fatalf("got %+v, want only the real instance - a client-only sibling is not a place to go", list)
	}
}

// TestRelayConnectedDistinguishesUnreachableFromAbsent: an empty sibling list
// is ambiguous - it means both "the relay is fine, nobody else is on the key"
// and "the relay cannot be reached at all". Those want different reactions
// from the user, and nothing above this layer could tell them apart, because
// relay.Client.Connected() had no callers even though its own doc comment
// calls it the honest answer to whether relay pairing is working.
func TestRelayConnectedDistinguishesUnreachableFromAbsent(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if m.RelayConnected() {
		t.Error("no relay configured at all, want RelayConnected false")
	}

	// Configured and up, but nobody else on the key: no peers, yet connected.
	up := &fakeRelay{}
	m.SetRelay(up)
	if !m.RelayConnected() {
		t.Error("relay up with no siblings, want RelayConnected true")
	}
	if len(m.List()) != 0 {
		t.Error("want no peers from an empty relay")
	}

	// Configured but unreachable: also no peers - and this is the case that
	// used to be indistinguishable from the one above.
	m.SetRelay(&fakeRelay{down: true})
	if m.RelayConnected() {
		t.Error("relay configured but down, want RelayConnected false")
	}
}

// TestARelayPeerCarriesNoCredential pins what is actually true today, because
// a comment here once claimed the opposite and nothing tested it.
//
// A stored peer is addressed by its pairing name, which is the key a peer
// token is filed under, so an HTTP call carries one. A relay peer is addressed
// by its 40-hex InstanceID, and no credential is ever filed under that -
// pairing is an HTTP POST to the other side, which two instances that can only
// meet over a relay cannot make.
//
// So this asserts an EMPTY Authorization for the relay transport and a
// populated one for HTTP. If relay pairing is ever built, this test failing is
// the correct and intended signal to update it.
func TestARelayPeerCarriesNoCredential(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Filed the way production files them: under PAIRING NAMES only. Nothing
	// ever writes a credential under an InstanceID, because the only thing
	// that writes one is the pairing exchange, and that names its peer.
	m.SetPeerTokens(staticTokens{"cellar": "secret-for-cellar", "Laptop": "the-relay-peer-s-display-name"})

	rt := &fakeRelay{sibs: []relay.Announce{{InstanceID: "id-bravo", Name: "Laptop"}}}
	m.SetRelay(rt)

	if _, _, err := m.Proxy(context.Background(), "id-bravo", http.MethodGet, "/api/tasks", nil); err != nil {
		t.Fatalf("relay proxy: %v", err)
	}
	// Empty, and note the second entry above: not even the peer's DISPLAY name
	// helps, because a relay peer is addressed by its id and that is the key
	// the lookup uses. Both halves of the gap in one assertion.
	if rt.auth != "" {
		t.Errorf("relay call carried Authorization %q, want none - no credential is filed under an InstanceID", rt.auth)
	}

	// The HTTP half, for contrast: same manager, same hook, a credential does
	// travel - so an empty one above is about the key, not about the hook
	// being unwired.
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	if err := m.Add(Instance{Name: "cellar", URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Proxy(context.Background(), "cellar", http.MethodGet, "/api/tasks", nil); err != nil {
		t.Fatalf("http proxy: %v", err)
	}
	if seen != "Bearer secret-for-cellar" {
		t.Errorf("http call carried %q, want the stored peer token", seen)
	}
}

// staticTokens is a PeerTokens hook backed by a plain map.
type staticTokens map[string]string

func (s staticTokens) TokenFor(peer string) string { return s[peer] }
