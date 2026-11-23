package federation

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
)

// fakeGroup stands in for *group.Group and records the last call it was asked
// to make.
type fakeGroup struct {
	members []group.Member

	target, method, path string
	body                 []byte

	resp   []byte
	status int
	err    error

	// down makes RelayConnected report false: a relay that is configured but
	// unreachable.
	down   bool
	closed bool
}

func (f *fakeGroup) Members() []group.Member { return f.members }

func (f *fakeGroup) RelayConnected() bool { return !f.down }

func (f *fakeGroup) Call(_ context.Context, target, method, path string, body []byte) (int, []byte, error) {
	f.target, f.method, f.path, f.body = target, method, path, body
	return f.status, f.resp, f.err
}

func (f *fakeGroup) Close() error {
	f.closed = true
	return nil
}

// onRelay is an instance of the group the relay reaches.
func onRelay(id, name string) group.Member {
	return group.Member{ID: id, Kind: member.KindInstance, Name: name, Relay: true}
}

// Member ids in the shape instances mint them, the only one a member is
// listed under.
var (
	bravoID  = strings.Repeat("b", 40)
	nasID    = strings.Repeat("d", 40)
	laptopID = strings.Repeat("e", 40)
	firstID  = strings.Repeat("1", 40)
	secondID = strings.Repeat("2", 40)
)

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m
}

func TestMembersAppearWithoutBeingStored(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	g := &fakeGroup{members: []group.Member{onRelay(bravoID, "Laptop")}}
	m.SetGroup(g)

	list := m.List()
	if len(list) != 1 || list[0].Name != bravoID || list[0].DisplayName != "Laptop" || list[0].RelayID != bravoID {
		t.Fatalf("got %+v, want one member addressed by its id, displayed as Laptop", list)
	}

	g.members = nil
	if list := m.List(); len(list) != 0 {
		t.Errorf("got %+v, want no peers once the group reaches none", list)
	}
	m.SetGroup(nil)
	if list := m.List(); len(list) != 0 {
		t.Errorf("got %+v, want no peers outside a group", list)
	}
	if !g.closed {
		t.Error("leaving the group never closed it")
	}
}

func TestAnAwayMemberIsNoPlaceToGo(t *testing.T) {
	m := newManager(t)
	m.SetGroup(&fakeGroup{members: []group.Member{
		{ID: nasID, Kind: member.KindInstance, Name: "NAS", State: group.StateApproved},
		onRelay(laptopID, "Laptop"),
	}})
	if list := m.List(); len(list) != 1 || list[0].Name != laptopID {
		t.Fatalf("got %+v, want only the member reachable now", list)
	}
}

func TestAMemberCarriesTheAddressAndDeploymentItAnnounced(t *testing.T) {
	m := newManager(t)
	nas := onRelay(nasID, "NAS")
	nas.Address, nas.Deployment = "https://kl.example.org", "container"
	laptop := group.Member{ID: laptopID, Kind: member.KindInstance, Name: "Laptop", Deployment: "desktop", Direct: true}
	m.SetGroup(&fakeGroup{members: []group.Member{nas, laptop}})
	addresses, deployments := map[string]string{}, map[string]string{}
	for _, in := range m.List() {
		addresses[in.Name], deployments[in.Name] = in.Address, in.Deployment
	}
	if addresses[nasID] != "https://kl.example.org" || addresses[laptopID] != "" {
		t.Errorf("addresses = %v, want the NAS at its domain and the laptop without one", addresses)
	}
	want := map[string]string{nasID: "container", laptopID: "desktop"}
	if !maps.Equal(deployments, want) {
		t.Errorf("deployments = %v, want %v", deployments, want)
	}
}

func TestAMemberSaysHowItIsReached(t *testing.T) {
	m := newManager(t)
	both := onRelay(nasID, "NAS")
	both.Direct = true
	m.SetGroup(&fakeGroup{members: []group.Member{both, onRelay(laptopID, "Laptop")}})
	for _, in := range m.List() {
		switch in.Name {
		case nasID:
			if !in.Direct() || !in.ViaRelay() {
				t.Errorf("the NAS is direct %v, relay %v, want both", in.Direct(), in.ViaRelay())
			}
		case laptopID:
			if in.Direct() || !in.ViaRelay() {
				t.Errorf("the laptop is direct %v, relay %v, want the relay alone", in.Direct(), in.ViaRelay())
			}
		}
	}
}

func TestSetGroupClosesTheGroupItReplaces(t *testing.T) {
	m := newManager(t)
	first := &fakeGroup{}
	second := &fakeGroup{}

	m.SetGroup(first)
	if first.closed {
		t.Fatal("the group was closed before anything replaced it")
	}
	m.SetGroup(second)
	if !first.closed {
		t.Error("the old group was never closed when a new one replaced it")
	}
	if second.closed {
		t.Error("the new group was closed immediately after being installed")
	}
}

func TestProxyReachesAMemberThroughTheGroup(t *testing.T) {
	m := newManager(t)
	g := &fakeGroup{
		members: []group.Member{onRelay(bravoID, "Laptop")},
		resp:    []byte(`{"added":1}`),
		status:  http.StatusCreated,
	}
	m.SetGroup(g)

	body, code, err := m.Proxy(context.Background(), bravoID, http.MethodPost, "/api/links", []byte(`{"url":"x"}`))
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if code != http.StatusCreated || string(body) != `{"added":1}` {
		t.Errorf("got %d %s, want the peer's own answer", code, body)
	}
	if g.target != bravoID || g.method != http.MethodPost || g.path != "/api/links" || string(g.body) != `{"url":"x"}` {
		t.Errorf("the group was asked for %s %s %s %s, want the call unchanged and addressed by id",
			g.target, g.method, g.path, g.body)
	}
	if _, code, err := m.Proxy(context.Background(), "Laptop", http.MethodGet, "/api/tasks", nil); err == nil || code != http.StatusNotFound {
		t.Errorf("got %d %v proxying by the display name, want a 404; display names never route", code, err)
	}
}

// TestPeerNamesCollide covers shared display names (two containers from one
// image announce the same hostname): every peer stays listed and reachable,
// under its own id.
func TestPeerNamesCollide(t *testing.T) {
	tests := []struct {
		name    string
		members []group.Member
		want    map[string]string // address (Name) -> relay ID
	}{
		{
			name:    "two members with one hostname",
			members: []group.Member{onRelay(firstID, "knightloader"), onRelay(secondID, "knightloader")},
			want:    map[string]string{firstID: firstID, secondID: secondID},
		},
		{
			name:    "a member that announced no name at all",
			members: []group.Member{onRelay(firstID, "")},
			want:    map[string]string{firstID: firstID},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(t)
			m.SetGroup(&fakeGroup{members: tc.members, status: http.StatusOK})

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

func TestUnknownInstanceIsStill404(t *testing.T) {
	m := newManager(t)
	g := &fakeGroup{members: []group.Member{onRelay(bravoID, "Laptop")}}
	m.SetGroup(g)

	_, code, err := m.Proxy(context.Background(), "Nowhere", http.MethodGet, "/api/tasks", nil)
	if err == nil || code != http.StatusNotFound {
		t.Errorf("got %d %v, want a 404", code, err)
	}
	if g.target != "" {
		t.Errorf("the group was called with %q, want an unknown name never routed", g.target)
	}
}

// TestPhonesAndBrowsersAreNotListedAsInstances: the mobile app and the
// extension join the group to call instances and serve no API. Listed, they
// would appear as broken peers on the Instances page.
func TestPhonesAndBrowsersAreNotListedAsInstances(t *testing.T) {
	m := newManager(t)
	m.SetGroup(&fakeGroup{members: []group.Member{
		onRelay(nasID, "Cellar"),
		{ID: "phone-" + strings.Repeat("a", 64), Kind: member.KindMobile, Name: "Pixel", Relay: true},
		{ID: "browser-" + strings.Repeat("a", 64), Kind: member.KindExtension, Name: "Firefox", Relay: true},
	}})

	list := m.List()
	if len(list) != 1 || list[0].Name != nasID {
		t.Fatalf("got %+v, want only the real instance", list)
	}
}

// TestRelayConnectedDistinguishesUnreachableFromAbsent: an empty member list
// means either "nobody else in the group" or "relay unreachable".
func TestRelayConnectedDistinguishesUnreachableFromAbsent(t *testing.T) {
	m := newManager(t)
	if m.RelayConnected() {
		t.Error("in no group at all, want RelayConnected false")
	}
	m.SetGroup(&fakeGroup{})
	if !m.RelayConnected() {
		t.Error("relay up with nobody else, want RelayConnected true")
	}
	m.SetGroup(&fakeGroup{down: true})
	if m.RelayConnected() {
		t.Error("relay configured but down, want RelayConnected false")
	}
}

func TestAMemberIsListedOnlyUnderAnInstanceID(t *testing.T) {
	m := newManager(t)
	m.SetGroup(&fakeGroup{members: []group.Member{
		onRelay("cellar", "Cellar"),
		onRelay(nasID, "NAS"),
		onRelay(strings.ToUpper(bravoID), "Laptop"),
		onRelay(bravoID, "Laptop"),
	}})

	got := map[string]bool{}
	for _, in := range m.List() {
		got[in.Name] = true
	}
	if len(got) != 2 || !got[nasID] || !got[bravoID] {
		t.Fatalf("listed %v, want only the two members whose ids are shaped like an instance's", got)
	}
}
