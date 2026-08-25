package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// TestFederationProxy runs two real instances and drives instance B entirely
// through instance A's proxy routes: register, list tasks, add a link, remove.
func TestFederationProxy(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()

	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	// Register B as a peer of A; the add response reports it online.
	body, _ := json.Marshal(map[string]string{"name": "cellar", "url": bSrv.URL})
	resp, err := http.Post(aSrv.URL+"/api/instances", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var added struct {
		Online bool `json:"online"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&added)
	resp.Body.Close()
	if !added.Online {
		t.Fatal("peer not reported online")
	}

	// A task added directly on B must be visible through A's proxy.
	seed := bApp.AddLinks([]string{"https://example.com/direct-file.zip"}, "fed")
	if len(seed) != 1 {
		t.Fatalf("seed task not created on B")
	}
	resp, err = http.Get(aSrv.URL + "/api/instances/cellar/tasks")
	if err != nil {
		t.Fatal(err)
	}
	var tasks []core.Task
	_ = json.NewDecoder(resp.Body).Decode(&tasks)
	resp.Body.Close()
	if len(tasks) != 1 || tasks[0].Name != "direct-file.zip" {
		t.Fatalf("proxied tasks = %+v, want the seeded B task", tasks)
	}

	// Adding links through the proxy lands on B, not on A.
	body, _ = json.Marshal(map[string]string{"links": "https://example.com/second.zip", "package": "fed"})
	resp, err = http.Post(aSrv.URL+"/api/instances/cellar/links", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := len(bApp.Tasks()); got != 2 {
		t.Fatalf("B has %d tasks after proxied add, want 2", got)
	}
	if got := len(aApp.Tasks()); got != 0 {
		t.Fatalf("A has %d tasks, proxied add must not create local tasks", got)
	}

	// Deleting through the proxy removes on B.
	req, _ := http.NewRequest(http.MethodDelete, aSrv.URL+"/api/instances/cellar/tasks/"+seed[0].ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := len(bApp.Tasks()); got != 1 {
		t.Fatalf("B has %d tasks after proxied delete, want 1", got)
	}

	// Non-task routes must not be proxied.
	resp, err = http.Get(aSrv.URL + "/api/instances/cellar/settings")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("settings proxy = HTTP %d, want 403", resp.StatusCode)
	}

	// Peer list is persisted + removable.
	resp, _ = http.Get(aSrv.URL + "/api/instances")
	b, _ := readAll(resp)
	if !strings.Contains(string(b), "cellar") {
		t.Fatalf("instances list %s missing peer", b)
	}
	req, _ = http.NewRequest(http.MethodDelete, aSrv.URL+"/api/instances/cellar", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	resp, _ = http.Get(aSrv.URL + "/api/instances")
	b, _ = readAll(resp)
	if strings.Contains(string(b), "cellar") {
		t.Fatalf("peer still listed after delete: %s", b)
	}
}

func readAll(r *http.Response) ([]byte, error) {
	defer r.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

// relayOnlyPeer is a relay transport carrying exactly one sibling with no
// address of its own - a desktop build, or anything behind a relay. It answers
// one path so the proxy below has something real to reach.
type relayOnlyPeer struct {
	sibs    []relay.Announce
	gotPath string
	gotBody []byte
	gotBoth bool
}

func (r *relayOnlyPeer) Siblings() []relay.Announce { return r.sibs }
func (r *relayOnlyPeer) Connected() bool            { return true }
func (r *relayOnlyPeer) Close() error               { return nil }
func (r *relayOnlyPeer) Proxy(_ context.Context, target, method, path string, body []byte, _ string) ([]byte, int, error) {
	r.gotPath = path
	r.gotBody = body
	r.gotBoth = target == "id-desktop" && method == http.MethodPost
	return []byte(`[{"id":"t1","name":"file.zip"}]`), http.StatusOK, nil
}

// TestRelayOnlyPeerIsListedAndReachable pins the two facts the browser
// extension's relay support rests on (issue #27).
//
// The extension can only open an HTTP connection to an address, so a peer
// reachable ONLY through a relay used to be dropped from its sync - silently,
// and then reported as "No new instances found". The fix keeps such a peer and
// routes to it through an instance that CAN be opened, using the very route
// this test drives. Both halves have to hold:
//
//  1. GET /api/instances lists it, with an EMPTY url - that emptiness is the
//     signal the extension keys on, so it is part of the contract, not an
//     accident of serialisation.
//  2. POST /api/instances/{name}/links actually reaches it over the relay -
//     otherwise the extension would show a peer it still cannot send to,
//     which is the original bug wearing a different hat.
func TestRelayOnlyPeerIsListedAndReachable(t *testing.T) {
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	srv := httptest.NewServer(Handler(a))
	defer srv.Close()

	// After Handler, not before: Handler calls applyRelay, which reconciles
	// the transport against the saved settings and would drop one installed
	// ahead of it.
	rt := &relayOnlyPeer{sibs: []relay.Announce{
		{InstanceID: "id-desktop", Name: "Workshop laptop", Deployment: "desktop"},
	}}
	a.Federation.SetRelay(rt)

	resp, err := http.Get(srv.URL + "/api/instances")
	if err != nil {
		t.Fatal(err)
	}
	var listed []struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		RelayID string `json:"relayId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(listed) != 1 {
		t.Fatalf("got %d instances, want the one relay peer", len(listed))
	}
	if listed[0].URL != "" {
		t.Errorf("url = %q, want empty: an address here would tell the extension to open a connection that cannot exist", listed[0].URL)
	}
	if listed[0].RelayID == "" {
		t.Error("relayId is empty, so nothing distinguishes this from a peer that simply has no address configured")
	}

	// The peer is addressed by the name the list gave, which is what the
	// extension stores - no second naming scheme.
	body := bytes.NewReader([]byte(`{"links":"https://example.com/file.zip"}`))
	pr, err := http.Post(srv.URL+"/api/instances/"+listed[0].Name+"/links", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Body.Close()
	if pr.StatusCode != http.StatusOK {
		t.Fatalf("proxy answered %d, want 200 - a listed peer that cannot be sent to is the bug this fixes", pr.StatusCode)
	}
	if rt.gotPath != "/api/links" {
		t.Errorf("relay was asked for %q, want /api/links", rt.gotPath)
	}
	if !rt.gotBoth {
		t.Error("the relay call did not carry the peer id and the POST method through")
	}
	if !strings.Contains(string(rt.gotBody), "example.com/file.zip") {
		t.Errorf("body reached the peer as %q, want the link intact", rt.gotBody)
	}
}

// TestAddingAPasswordProtectedPeerSaysWhy pins the honest half of what
// "adding" actually does.
//
// Adding a peer by address - typed, or one click from the discovery card -
// stores an address and exchanges nothing. So a peer with a password set
// refuses the very next call, and for a while the page reported that as
// "offline": the same word it uses for a machine that is switched off, with a
// completely different fix behind it. Worse, the hint above the discovery card
// claimed in 42 languages that adding was what exchanged credentials, which it
// never did.
func TestAddingAPasswordProtectedPeerSaysWhy(t *testing.T) {
	locked, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Close()
	lockedSrv := httptest.NewServer(Handler(locked))
	defer lockedSrv.Close()
	if err := locked.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	me, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer me.Close()
	meSrv := httptest.NewServer(Handler(me))
	defer meSrv.Close()

	add := func(name, url string) (online, needsPairing bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"name": name, "url": url})
		resp, err := http.Post(meSrv.URL+"/api/instances", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var got struct {
			Online       bool `json:"online"`
			NeedsPairing bool `json:"needsPairing"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got.Online, got.NeedsPairing
	}

	// Reached, and refused: that is a pairing problem, not a reachability one.
	online, needsPairing := add("locked", lockedSrv.URL)
	if online {
		t.Error("online = true for a peer that answered 401")
	}
	if !needsPairing {
		t.Error("needsPairing = false for a peer that was reached and refused us - the page can only say \"offline\", which points at the wrong fix")
	}

	// Not reached at all: a different problem, and it must not be reported as
	// the pairing one.
	online, needsPairing = add("gone", "http://127.0.0.1:1")
	if online {
		t.Error("online = true for an address nothing listens on")
	}
	if needsPairing {
		t.Error("needsPairing = true for an unreachable peer - that sends somebody to fetch a pairing code for a machine that is switched off")
	}
}
