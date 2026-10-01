package federation

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/discovery"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// fakeRelay stands in for *relay.Client and records the last call it was asked
// to make.
type fakeRelay struct {
	sibs []relay.Announce

	target, method, path string
	body                 []byte
	auth                 string

	resp   []byte
	status int
	err    error

	// down makes Connected report false: a relay that is configured but
	// unreachable.
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

// Member ids in the shape instances mint them, the only one a member is
// listed under.
var (
	bravoID  = strings.Repeat("b", 40)
	nasID    = strings.Repeat("d", 40)
	laptopID = strings.Repeat("e", 40)
	firstID  = strings.Repeat("1", 40)
	secondID = strings.Repeat("2", 40)
	officeID = strings.Repeat("0", 40)
)

// fakeLocal stands in for discovery: members announcing on this network.
type fakeLocal struct{ members []discovery.Peer }

func (f *fakeLocal) Members() []discovery.Peer { return f.members }

func (f *fakeLocal) SetGroup(func(discovery.Peer) string, func(discovery.Peer) bool) {}

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m
}

// TestManualPeersAreUntouchedByRelaySupport checks the stored-peer path end to
// end: the list, the HTTP call and the file on disk.
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

// TestAPeerUnderABasePathIsCalledThere covers a peer behind a reverse proxy at
// https://example.com/kl/, added with the trailing slash a browser shows.
func TestAPeerUnderABasePathIsCalledThere(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/kl/api/tasks" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"t1"}]`))
	}))
	defer peer.Close()

	m := newManager(t)
	if err := m.Add(Instance{Name: "NAS", URL: peer.URL + "/kl/"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	body, code, err := m.Proxy(context.Background(), "NAS", http.MethodGet, "/api/tasks", nil)
	if err != nil || code != http.StatusOK || string(body) != `[{"id":"t1"}]` {
		t.Errorf("got %s %d %v, want the answer from /kl/api/tasks", body, code, err)
	}
}

func TestRelayPeersAppearWithoutBeingStored(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rt := &fakeRelay{sibs: []relay.Announce{
		{InstanceID: bravoID, Name: "Laptop", Deployment: "desktop"},
	}}
	m.SetRelay(rt)

	list := m.List()
	if len(list) != 1 || list[0].Name != bravoID || list[0].DisplayName != "Laptop" || list[0].RelayID != bravoID || list[0].URL != "" {
		t.Fatalf("got %+v, want one relay peer addressed as id-bravo, displayed as Laptop", list)
	}
	if _, err := os.Stat(filepath.Join(dir, "instances.json")); !os.IsNotExist(err) {
		t.Errorf("instances.json exists, want a relay peer never written to disk")
	}

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

func TestAMemberCarriesTheAddressItAnnounced(t *testing.T) {
	m := newManager(t)
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: nasID, Name: "NAS", Address: "https://kl.example.org"},
		{InstanceID: laptopID, Name: "Laptop"},
	}})
	got := map[string]string{}
	for _, in := range m.List() {
		got[in.Name] = in.Address
	}
	if got[nasID] != "https://kl.example.org" || got[laptopID] != "" {
		t.Fatalf("addresses = %v, want the NAS at its domain and the laptop without one", got)
	}
}

func TestAMemberCarriesTheDeploymentItAnnounced(t *testing.T) {
	m := newManager(t)
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: nasID, Name: "NAS", Deployment: "container"},
		{InstanceID: laptopID, Name: "Laptop", Deployment: "desktop"},
	}})
	if err := m.Add(Instance{Name: "cellar", URL: "http://192.168.1.9:8749", Deployment: "desktop"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := map[string]string{}
	for _, in := range m.List() {
		got[in.Name] = in.Deployment
	}
	want := map[string]string{nasID: "container", laptopID: "desktop", "cellar": ""}
	if !maps.Equal(got, want) {
		t.Fatalf("deployments = %v, want %v", got, want)
	}
}

func TestAddedPeerCannotClaimAnAddress(t *testing.T) {
	m := newManager(t)
	if err := m.Add(Instance{Name: "cellar", URL: "http://192.168.1.9:8749", Address: "https://evil.example"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if list := m.List(); len(list) != 1 || list[0].Address != "" {
		t.Fatalf("got %+v, want the stored peer without an address of its own", list)
	}
}

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

func TestProxyReachesARelayPeerThroughTheTransport(t *testing.T) {
	m := newManager(t)
	rt := &fakeRelay{
		sibs:   []relay.Announce{{InstanceID: bravoID, Name: "Laptop"}},
		resp:   []byte(`{"added":1}`),
		status: http.StatusCreated,
	}
	m.SetRelay(rt)

	body, code, err := m.Proxy(context.Background(), bravoID, http.MethodPost, "/api/links", []byte(`{"url":"x"}`))
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if code != http.StatusCreated || string(body) != `{"added":1}` {
		t.Errorf("got %d %s, want the peer's own answer", code, body)
	}
	if rt.target != bravoID || rt.method != http.MethodPost || rt.path != "/api/links" || string(rt.body) != `{"url":"x"}` {
		t.Errorf("the transport was asked for %s %s %s %s, want the call unchanged and addressed by ID",
			rt.target, rt.method, rt.path, rt.body)
	}
	if _, code, err := m.Proxy(context.Background(), "Laptop", http.MethodGet, "/api/tasks", nil); err == nil || code != http.StatusNotFound {
		t.Errorf("got %d %v proxying by the display name, want a 404; display names never route", code, err)
	}
}

// TestPeerNamesCollide covers shared display names (two containers from one
// image announce the same hostname): every peer stays listed and reachable,
// and a stored peer keeps its name.
func TestPeerNamesCollide(t *testing.T) {
	tests := []struct {
		name   string
		stored []Instance
		sibs   []relay.Announce
		want   map[string]string // address (Name) -> relay ID ("" for a stored peer)
	}{
		{
			name: "a stored peer keeps its name even when a relay peer announces the same one",
			// Nothing listens on this port, so the resolve check below fails
			// fast instead of waiting out peerTimeout.
			stored: []Instance{{Name: "NAS", URL: "http://127.0.0.1:1"}},
			sibs:   []relay.Announce{{InstanceID: nasID, Name: "NAS"}},
			want:   map[string]string{"NAS": "", nasID: nasID},
		},
		{
			name: "two relay peers with one hostname",
			sibs: []relay.Announce{
				{InstanceID: firstID, Name: "knightloader"},
				{InstanceID: secondID, Name: "knightloader"},
			},
			want: map[string]string{firstID: firstID, secondID: secondID},
		},
		{
			name: "a peer that announced no name at all",
			sibs: []relay.Announce{{InstanceID: firstID}},
			want: map[string]string{firstID: firstID},
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
			// Every listed address must resolve; 404 is what Proxy returns for
			// one it cannot place.
			for name := range got {
				if _, code, _ := m.Proxy(context.Background(), name, http.MethodGet, "/api/tasks", nil); code == http.StatusNotFound {
					t.Errorf("%q is listed but does not resolve to a peer", name)
				}
			}
		})
	}
}

// TestRelayPeerAddressSurvivesUnrelatedChanges checks that a relay peer's
// address stays put while other stored peers and siblings come and go.
func TestRelayPeerAddressSurvivesUnrelatedChanges(t *testing.T) {
	m := newManager(t)
	rt := &fakeRelay{sibs: []relay.Announce{{InstanceID: firstID, Name: "Cellar"}}}
	m.SetRelay(rt)
	addressBefore := m.List()[0].Name

	if err := m.Add(Instance{Name: "Cellar", URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	rt.sibs = append(rt.sibs, relay.Announce{InstanceID: secondID, Name: "Other"})
	rt.sibs = rt.sibs[:1]
	if err := m.Remove("Cellar"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	list := m.List()
	if len(list) != 1 || list[0].Name != addressBefore {
		t.Fatalf("relay peer's address is %+v after unrelated churn, want it unchanged at %q", list, addressBefore)
	}
	if _, code, _ := m.Proxy(context.Background(), addressBefore, http.MethodGet, "/api/tasks", nil); code == http.StatusNotFound {
		t.Errorf("the original address no longer resolves after unrelated churn")
	}
}

func TestUnknownInstanceIsStill404(t *testing.T) {
	m := newManager(t)
	rt := &fakeRelay{sibs: []relay.Announce{{InstanceID: bravoID, Name: "Laptop"}}}
	m.SetRelay(rt)

	_, code, err := m.Proxy(context.Background(), "Nowhere", http.MethodGet, "/api/tasks", nil)
	if err == nil || code != http.StatusNotFound {
		t.Errorf("got %d %v, want a 404", code, err)
	}
	if rt.target != "" {
		t.Errorf("the transport was called with %q, want an unknown name never routed", rt.target)
	}
}

// TestAddNeverStoresARelayIdentity: the route decodes an Instance from the
// request body, so RelayID must not be a way to store a relay identity.
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

// TestClientOnlySiblingsAreNotListedAsInstances: the mobile app joins the
// relay key to call instances and serves no API. Listed, it would appear as a
// broken peer on other people's Instances pages.
func TestClientOnlySiblingsAreNotListedAsInstances(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: nasID, Name: "Cellar", Deployment: "container"},
		{InstanceID: "id-phone", Name: "Pixel", Deployment: "mobile", Client: true},
	}})

	list := m.List()
	if len(list) != 1 || list[0].Name != nasID {
		t.Fatalf("got %+v, want only the real instance; a client-only sibling is not a place to go", list)
	}
}

// TestRelayConnectedDistinguishesUnreachableFromAbsent: an empty sibling list
// means either "nobody else on the key" or "relay unreachable".
func TestRelayConnectedDistinguishesUnreachableFromAbsent(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if m.RelayConnected() {
		t.Error("no relay configured at all, want RelayConnected false")
	}

	up := &fakeRelay{}
	m.SetRelay(up)
	if !m.RelayConnected() {
		t.Error("relay up with no siblings, want RelayConnected true")
	}
	if len(m.List()) != 0 {
		t.Error("want no peers from an empty relay")
	}

	m.SetRelay(&fakeRelay{down: true})
	if m.RelayConnected() {
		t.Error("relay configured but down, want RelayConnected false")
	}
}

// TestBothTransportsCarryTheirPeerCredential checks that each transport looks
// the token up under the key it addresses the peer by: the pairing name over
// HTTP, the InstanceID over the relay. Without the latter, a
// password-protected relay peer refuses every call (#26).
func TestBothTransportsCarryTheirPeerCredential(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Filed as the pairing exchange files them. The display name entry must
	// never be used.
	m.SetPeerTokens(staticTokens{
		"cellar": "secret-for-cellar",
		bravoID:  "secret-for-the-relay-peer",
		"Laptop": "a-display-name-is-not-an-address",
	})

	rt := &fakeRelay{sibs: []relay.Announce{{InstanceID: bravoID, Name: "Laptop"}}}
	m.SetRelay(rt)

	if _, _, err := m.Proxy(context.Background(), bravoID, http.MethodGet, "/api/tasks", nil); err != nil {
		t.Fatalf("relay proxy: %v", err)
	}
	if rt.auth != "Bearer secret-for-the-relay-peer" {
		t.Errorf("relay call carried %q, want the credential filed under the instance id; without it a password-protected relay peer answers 401 forever", rt.auth)
	}

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

type staticTokens map[string]string

func (s staticTokens) TokenFor(peer string) string { return s[peer] }

// TestAMemberCannotTakeAStoredPeersName: a member announcing a stored peer's
// name as its id would otherwise receive that peer's calls and the
// credential the peer issued, though the peer may be in no group at all.
func TestAMemberCannotTakeAStoredPeersName(t *testing.T) {
	var seen string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("[]"))
	}))
	defer peer.Close()

	m := newManager(t)
	if err := m.Add(Instance{Name: "nas", URL: peer.URL}); err != nil {
		t.Fatal(err)
	}
	m.SetPeerTokens(staticTokens{"nas": "secret-the-nas-issued"})
	m.SetGroup(make([]byte, 16), officeID)
	m.SetDiscovery(&fakeLocal{members: []discovery.Peer{{ID: "nas", Name: "nas", URL: "http://192.168.1.66:8749"}}})
	rt := &fakeRelay{sibs: []relay.Announce{{InstanceID: "nas", Name: "nas"}}}
	m.SetRelay(rt)

	if list := m.List(); len(list) != 1 || list[0].RelayID != "" || list[0].URL != peer.URL {
		t.Fatalf("got %+v, want only the stored peer", list)
	}
	if _, _, err := m.Proxy(context.Background(), "nas", http.MethodGet, "/api/tasks", nil); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if rt.target != "" {
		t.Errorf("the call went over the relay to %q", rt.target)
	}
	if seen != "Bearer secret-the-nas-issued" {
		t.Errorf("the stored peer got %q, want its own credential", seen)
	}
}

func TestAMemberIsListedOnlyUnderAnInstanceID(t *testing.T) {
	m := newManager(t)
	m.SetGroup(make([]byte, 16), officeID)
	m.SetDiscovery(&fakeLocal{members: []discovery.Peer{
		{ID: "cellar", Name: "Cellar", URL: "http://192.168.1.66:8749"},
		{ID: nasID, Name: "NAS", URL: "http://192.168.1.5:8749"},
	}})
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{
		{InstanceID: "Laptop", Name: "Laptop"},
		{InstanceID: strings.ToUpper(bravoID), Name: "Laptop"},
		{InstanceID: bravoID, Name: "Laptop"},
	}})

	got := map[string]bool{}
	for _, in := range m.List() {
		got[in.Name] = true
	}
	if len(got) != 2 || !got[nasID] || !got[bravoID] {
		t.Fatalf("listed %v, want only the two members whose ids are shaped like an InstanceID", got)
	}
}

// TestAStoredPeerIsNeverReplacedByAMember covers a stored name that is
// shaped like an InstanceID, which instances.json can hold when edited by
// hand.
func TestAStoredPeerIsNeverReplacedByAMember(t *testing.T) {
	dir := t.TempDir()
	stored := []Instance{{Name: bravoID, URL: "http://127.0.0.1:1"}}
	b, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instances.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.SetGroup(make([]byte, 16), officeID)
	m.SetDiscovery(&fakeLocal{members: []discovery.Peer{{ID: bravoID, URL: "http://192.168.1.66:8749"}}})
	m.SetRelay(&fakeRelay{sibs: []relay.Announce{{InstanceID: bravoID, Name: "Laptop"}}})

	if list := m.List(); len(list) != 1 || list[0].RelayID != "" || list[0].URL != "http://127.0.0.1:1" {
		t.Fatalf("got %+v, want the stored peer as stored", list)
	}
}
