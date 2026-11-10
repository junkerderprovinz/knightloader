package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/lan"
)

// A rename refreshes the announce of the handler that saved it. A second
// handler built in the same process keeps its own, so neither can end up
// announcing the other's name.
func TestARenameRefreshesOnlyItsOwnHandlersAnnounce(t *testing.T) {
	t.Parallel()
	first := newRegistry()
	registerAll(first, testApp(t))
	var firstCalls, secondCalls int
	first.refreshDiscovery = func() { firstCalls++ }

	second := newRegistry()
	registerAll(second, testApp(t))
	second.refreshDiscovery = func() { secondCalls++ }

	mux := http.NewServeMux()
	first.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if code, _, msg := patchSettings(t, srv.URL, `{"instanceName":"Keller"}`); code != http.StatusOK {
		t.Fatalf("renaming answered %d: %s", code, msg)
	}
	if firstCalls != 1 || secondCalls != 0 {
		t.Errorf("the rename refreshed the saving handler %d times and the other one %d times, want 1 and 0",
			firstCalls, secondCalls)
	}
}

// fakeAnnouncer keeps what a localNetwork last handed to the network.
type fakeAnnouncer struct {
	self   lan.Peer
	sign   func(lan.Peer) string
	accept func(lan.Peer) bool
}

func (f *fakeAnnouncer) SetSelf(self lan.Peer, sign func(lan.Peer) string) {
	f.self, f.sign = self, sign
}
func (f *fakeAnnouncer) SetAccept(accept func(lan.Peer) bool) { f.accept = accept }
func (f *fakeAnnouncer) Members() []lan.Peer                  { return nil }

func TestAnInstanceAnnouncesUnderItsGroupIDOnlyInAGroup(t *testing.T) {
	const ownID, groupID = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	fake := &fakeAnnouncer{}
	n := newLocalNetwork(fake, lan.Peer{ID: ownID, Name: "Keller", URL: "http://192.168.1.5:8749"})
	if fake.self.ID != ownID || fake.sign != nil {
		t.Fatalf("outside a group it announces %+v, signed %v; want its own id, untagged", fake.self, fake.sign != nil)
	}

	sign := func(lan.Peer) string { return "tag" }
	member := func(lan.Peer) bool { return true }
	n.SetGroup(groupID, sign, member)
	if fake.self.ID != groupID || fake.sign == nil || fake.accept == nil {
		t.Fatalf("in a group it announces %+v; want the group id, tagged, and members accepted", fake.self)
	}

	n.SetSelf(lan.Peer{ID: ownID, Name: "Dachboden", URL: "http://192.168.1.5:8749"})
	if fake.self.ID != groupID || fake.self.Name != "Dachboden" || fake.sign == nil {
		t.Fatalf("after a rename in a group it announces %+v; want the new name under the group id, tagged", fake.self)
	}

	n.SetGroup("", nil, nil)
	if fake.self.ID != ownID || fake.self.Name != "Dachboden" || fake.sign != nil || fake.accept != nil {
		t.Fatalf("after leaving the group it announces %+v; want its own id, untagged, nobody a member", fake.self)
	}
}
