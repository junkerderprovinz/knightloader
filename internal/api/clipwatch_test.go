package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/clipwatch"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// watchingMember is a group member reached through the relay that holds one
// clipboard watcher and records what it is asked.
type watchingMember struct {
	mu      sync.Mutex
	watcher clipwatch.Watcher
	stopped []string
}

func (m *watchingMember) Siblings() []relay.Announce {
	return []relay.Announce{{InstanceID: desktopID, Name: "Workshop laptop", Deployment: "desktop"}}
}
func (m *watchingMember) Connected() bool { return true }
func (m *watchingMember) Close() error    { return nil }
func (m *watchingMember) Proxy(_ context.Context, target, method, path string, _ []byte, _ string) ([]byte, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case method == http.MethodGet && path == "/api/clipboard-watchers?local=1":
		b, _ := json.Marshal([]clipwatch.Watcher{m.watcher})
		return b, http.StatusOK, nil
	case method == http.MethodPost && path == "/api/clipboard-watchers/"+m.watcher.ID+"/stop?local=1":
		m.stopped = append(m.stopped, target)
		return nil, http.StatusNoContent, nil
	}
	return nil, http.StatusNotFound, nil
}

func (m *watchingMember) stops() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.stopped...)
}

func listWatchers(t *testing.T, url string) []clipwatch.Watcher {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []clipwatch.Watcher
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	return list
}

func TestAWatcherRenewsAndLeaves(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	code, body := postJSON(t, http.MethodPut, srv.URL+"/api/clipboard-watchers/tab-1", map[string]string{"name": "Firefox, Linux", "kind": "web"})
	if code != http.StatusOK || string(body) != "{\"stop\":false}\n" {
		t.Fatalf("renewing = %d %s, want 200 with stop false", code, body)
	}
	list := listWatchers(t, srv.URL+"/api/clipboard-watchers?local=1")
	if len(list) != 1 || list[0].ID != "tab-1" || list[0].Name != "Firefox, Linux" {
		t.Fatalf("listed %+v, want the one watcher", list)
	}

	if code, _ := postJSON(t, http.MethodDelete, srv.URL+"/api/clipboard-watchers/tab-1", nil); code != http.StatusNoContent {
		t.Fatalf("leaving = %d, want 204", code)
	}
	if list := listWatchers(t, srv.URL+"/api/clipboard-watchers?local=1"); len(list) != 0 {
		t.Fatalf("after leaving the list is %+v", list)
	}
}

func TestAWatcherOfAnUnknownKindIsRefused(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	if code, _ := postJSON(t, http.MethodPut, srv.URL+"/api/clipboard-watchers/x", map[string]string{"kind": "toaster"}); code != http.StatusBadRequest {
		t.Fatalf("an unknown kind = %d, want 400", code)
	}
}

func TestTheGroupListNamesEachWatchersInstance(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	a.Federation.SetRelay(&watchingMember{watcher: clipwatch.Watcher{ID: "desk", Name: "Workshop laptop", Kind: "desktop"}})
	if _, err := a.ClipWatch.Renew(clipwatch.Watcher{ID: "tab", Name: "Chrome, Windows", Kind: "web"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	list := listWatchers(t, srv.URL+"/api/clipboard-watchers")
	if len(list) != 2 {
		t.Fatalf("listed %+v, want this instance's watcher and the member's", list)
	}
	byID := map[string]clipwatch.Watcher{}
	for _, w := range list {
		byID[w.ID] = w
	}
	if byID["tab"].Instance != instanceDisplayName(a) {
		t.Errorf("this instance's watcher sends to %q, want %q", byID["tab"].Instance, instanceDisplayName(a))
	}
	if byID["desk"].Instance != "Workshop laptop" {
		t.Errorf("the member's watcher sends to %q, want the member's name", byID["desk"].Instance)
	}

	if local := listWatchers(t, srv.URL+"/api/clipboard-watchers?local=1"); len(local) != 1 || local[0].ID != "tab" {
		t.Fatalf("?local=1 listed %+v, want this instance's watcher alone", local)
	}
}

func TestStoppingAWatcherReachesTheMemberThatHoldsIt(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	member := &watchingMember{watcher: clipwatch.Watcher{ID: "desk", Kind: "desktop"}}
	a.Federation.SetRelay(member)

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/desk/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stopping the member's watcher = %d, want 204", code)
	}
	if got := member.stops(); len(got) != 1 || got[0] != desktopID {
		t.Fatalf("the member was asked %v, want one stop", got)
	}
	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/nobody/stop", nil); code != http.StatusNotFound {
		t.Fatalf("stopping a watcher nobody holds = %d, want 404", code)
	}
}

func TestAStopAskedOfThisInstanceAloneIsNotPassedOn(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	member := &watchingMember{watcher: clipwatch.Watcher{ID: "desk", Kind: "desktop"}}
	a.Federation.SetRelay(member)

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/desk/stop?local=1", nil); code != http.StatusNotFound {
		t.Fatalf("a local stop for a watcher held elsewhere = %d, want 404", code)
	}
	if len(member.stops()) != 0 {
		t.Fatal("a stop asked of this instance alone went on to the group")
	}
}

func TestAStoppedWatcherLearnsItFromItsNextRenewal(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	watcher := map[string]string{"name": "Edge, Windows", "kind": "extension"}
	postJSON(t, http.MethodPut, srv.URL+"/api/clipboard-watchers/ext", watcher)
	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/ext/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stopping = %d, want 204", code)
	}
	if code, body := postJSON(t, http.MethodPut, srv.URL+"/api/clipboard-watchers/ext", watcher); code != http.StatusOK || string(body) != "{\"stop\":true}\n" {
		t.Fatalf("the next renewal = %d %s, want stop true", code, body)
	}
}

func TestTheRelayForwardsTheClipboardWatcherCalls(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/api/clipboard-watchers", true},
		{http.MethodGet, "/api/clipboard-watchers?local=1", true},
		{http.MethodPut, "/api/clipboard-watchers/abc", true},
		{http.MethodDelete, "/api/clipboard-watchers/abc", true},
		{http.MethodPost, "/api/clipboard-watchers/abc/stop", true},
		{http.MethodPost, "/api/clipboard-watchers", false},
		{http.MethodGet, "/api/clipboard-watchers/abc", false},
		{http.MethodPut, "/api/clipboard-watchers/", false},
		{http.MethodPost, "/api/clipboard-watchers/abc/other", false},
		{http.MethodPost, "/api/clipboard-watchers/abc/stop/more", false},
	} {
		if got := relayForwardable(c.method, c.path); got != c.want {
			t.Errorf("relayForwardable(%s %s) = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

// twoMembers is a group of two members reached through the relay: Aardvark,
// which never answers, and Zebra, which holds watcher "desk".
type twoMembers struct {
	mu        sync.Mutex
	zebraLive bool
}

var (
	aardvarkID = strings.Repeat("a", 40)
	zebraID    = strings.Repeat("e", 40)
)

func (m *twoMembers) Siblings() []relay.Announce {
	return []relay.Announce{{InstanceID: aardvarkID, Name: "Aardvark"}, {InstanceID: zebraID, Name: "Zebra"}}
}
func (m *twoMembers) Connected() bool { return true }
func (m *twoMembers) Close() error    { return nil }
func (m *twoMembers) Proxy(ctx context.Context, target, method, path string, _ []byte, _ string) ([]byte, int, error) {
	if target == aardvarkID {
		<-ctx.Done()
		return nil, http.StatusBadGateway, ctx.Err()
	}
	if ctx.Err() != nil {
		return nil, http.StatusBadGateway, ctx.Err()
	}
	if method == http.MethodPost && path == "/api/clipboard-watchers/desk/stop?local=1" {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.zebraLive = false
		return nil, http.StatusNoContent, nil
	}
	return nil, http.StatusNotFound, nil
}

func TestAMemberThatDoesNotAnswerDoesNotHideTheOneHoldingTheWatcher(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	members := &twoMembers{zebraLive: true}
	a.Federation.SetRelay(members)

	if code, body := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/desk/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stopping Zebra's watcher behind a silent Aardvark = %d %s, want 204", code, body)
	}
	members.mu.Lock()
	defer members.mu.Unlock()
	if members.zebraLive {
		t.Fatal("Zebra was never asked to stop its watcher")
	}
}

func TestAStopReachesTheMemberEvenWhenThisInstanceHoldsAStaleLease(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	member := &watchingMember{watcher: clipwatch.Watcher{ID: "ext", Kind: "extension"}}
	a.Federation.SetRelay(member)
	// The extension renewed here before its default instance moved to the
	// member.
	if _, err := a.ClipWatch.Renew(clipwatch.Watcher{ID: "ext", Kind: "extension"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/ext/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stopping = %d, want 204", code)
	}
	waitUntil(t, "the member holding the live lease to be asked to stop", func() bool { return len(member.stops()) == 1 })
}

func TestAWatcherLeasesWithThePeerItSendsTo(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	nasSrv, nas := testServer(t)
	defer nasSrv.Close()
	if err := a.Federation.Add(federation.Instance{Name: "nas", URL: nasSrv.URL}); err != nil {
		t.Fatal(err)
	}
	watcher := map[string]string{"name": "Firefox, Windows", "kind": "web"}
	renew := func() string {
		t.Helper()
		code, body := postJSON(t, http.MethodPut, srv.URL+"/api/instances/nas/clipboard-watchers/web-1", watcher)
		if code != http.StatusOK {
			t.Fatalf("renewing at the peer = %d %s, want 200", code, body)
		}
		return string(body)
	}

	if got := renew(); got != "{\"stop\":false}\n" {
		t.Fatalf("the first renewal at the peer = %s, want stop false", got)
	}
	if list := nas.ClipWatch.List(time.Now()); len(list) != 1 || list[0].ID != "web-1" {
		t.Fatalf("the peer the links go to holds %+v, want the watcher", list)
	}
	if list := a.ClipWatch.List(time.Now()); len(list) != 0 {
		t.Fatalf("the instance serving the page holds %+v, want nothing", list)
	}
	list := listWatchers(t, srv.URL+"/api/clipboard-watchers")
	if len(list) != 1 || list[0].Instance != "nas" {
		t.Fatalf("the group list is %+v, want the watcher sending to nas", list)
	}

	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/web-1/stop", nil); code != http.StatusNoContent {
		t.Fatalf("stopping the watcher held by the peer = %d, want 204", code)
	}
	if got := renew(); got != "{\"stop\":true}\n" {
		t.Fatalf("the renewal after the stop = %s, want stop true", got)
	}

	renew()
	if code, _ := postJSON(t, http.MethodDelete, srv.URL+"/api/instances/nas/clipboard-watchers/web-1", nil); code != http.StatusNoContent {
		t.Fatalf("leaving at the peer = %d, want 204", code)
	}
	if list := nas.ClipWatch.List(time.Now()); len(list) != 0 {
		t.Fatalf("after leaving the peer still holds %+v", list)
	}
}

func TestOnlyTheLeaseCallsOfTheWatchersAreForwardedToAPeer(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	nasSrv, _ := testServer(t)
	defer nasSrv.Close()
	if err := a.Federation.Add(federation.Instance{Name: "nas", URL: nasSrv.URL}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, rest string }{
		{http.MethodGet, "clipboard-watchers"},
		{http.MethodPost, "clipboard-watchers/web-1/stop"},
		{http.MethodPut, "clipboard-watchers/web-1/more"},
		{http.MethodGet, "clipboard-watchers/web-1"},
	} {
		if code, _ := postJSON(t, c.method, srv.URL+"/api/instances/nas/"+c.rest, map[string]string{"kind": "web"}); code != http.StatusForbidden {
			t.Errorf("%s %s through the peer forward = %d, want 403", c.method, c.rest, code)
		}
	}
}

func TestTheWatcherListAndStopLeaveThePeersAloneWhileInstancesIsOff(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	member := &watchingMember{watcher: clipwatch.Watcher{ID: "desk", Kind: "desktop"}}
	a.Federation.SetRelay(member)
	if err := setFeature(a, "federation", false); err != nil {
		t.Fatal(err)
	}

	if list := listWatchers(t, srv.URL+"/api/clipboard-watchers"); len(list) != 0 {
		t.Fatalf("listed %+v while the Instances module is off, want no peer's watcher", list)
	}
	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/desk/stop", nil); code != http.StatusNotFound {
		t.Fatalf("stopping a peer's watcher while the Instances module is off = %d, want 404", code)
	}
	if got := member.stops(); len(got) != 0 {
		t.Fatalf("the peer was asked to stop %v while the Instances module is off", got)
	}
}

func TestAStopAnswersWithoutWaitingForAPeerThatDoesNotAnswer(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	a.Federation.SetRelay(&twoMembers{zebraLive: true})
	if _, err := a.ClipWatch.Renew(clipwatch.Watcher{ID: "tab", Kind: "web"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"tab", "desk"} {
		start := time.Now()
		if code, body := postJSON(t, http.MethodPost, srv.URL+"/api/clipboard-watchers/"+id+"/stop", nil); code != http.StatusNoContent {
			t.Fatalf("stopping %s = %d %s, want 204", id, code, body)
		}
		if took := time.Since(start); took >= peerWatchersWait/2 {
			t.Errorf("stopping %s took %v, waiting on the member that does not answer", id, took)
		}
	}
}
