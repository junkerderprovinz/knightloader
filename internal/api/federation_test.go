package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
)

// TestFederationProxy runs two real instances and drives instance B entirely
// through instance A's proxy routes: list tasks, add a link, delete one.
func TestFederationProxy(t *testing.T) {
	t.Parallel()
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

	aApp.Federation.SetGroup(&servedMember{url: bSrv.URL})

	// A task added directly on B must be visible through A's proxy.
	seed := bApp.AddLinks([]string{"https://example.com/direct-file.zip"}, "fed")
	if len(seed) != 1 {
		t.Fatalf("seed task not created on B")
	}
	resp, err := http.Get(aSrv.URL + "/api/instances/" + nasID + "/tasks")
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
	body, _ := json.Marshal(map[string]string{"links": "https://example.com/second.zip", "package": "fed"})
	resp, err = http.Post(aSrv.URL+"/api/instances/"+nasID+"/links", "application/json", bytes.NewReader(body))
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
	req, _ := http.NewRequest(http.MethodDelete, aSrv.URL+"/api/instances/"+nasID+"/tasks/"+seed[0].ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := len(bApp.Tasks()); got != 1 {
		t.Fatalf("B has %d tasks after proxied delete, want 1", got)
	}

	// Non-task routes must not be proxied.
	resp, err = http.Get(aSrv.URL + "/api/instances/" + nasID + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("settings proxy = HTTP %d, want 403", resp.StatusCode)
	}

	resp, _ = http.Get(aSrv.URL + "/api/instances")
	b, _ := readAll(resp)
	if !strings.Contains(string(b), nasID) {
		t.Fatalf("instances list %s missing the member", b)
	}
}

// TestLinksHandedOverAreAnnouncedToTheOpenPages checks both ends of a
// handover: the instance that forwards links says who they went to, and the
// one a group member hands links to says who they came from. Reading a peer's
// list announces nothing.
func TestLinksHandedOverAreAnnouncedToTheOpenPages(t *testing.T) {
	t.Parallel()
	aSrv, aApp := testServer(t)
	defer aSrv.Close()
	bSrv, bApp := testServer(t)
	defer bSrv.Close()
	aApp.Federation.SetGroup(&servedMember{url: bSrv.URL})

	// The server adds a stream to the hub only after the handshake Dial waits
	// for, so a broadcast sent in between misses it. The snapshot is queued
	// right after the add.
	sent := dialWS(t, "ws"+strings.TrimPrefix(aSrv.URL, "http"))
	readOneOfType(t, sent, "snapshot")
	handed := func(msg map[string]any) (string, bool) {
		data, _ := msg["data"].(map[string]any)
		peer, _ := data["peer"].(string)
		out, _ := data["out"].(bool)
		return peer, out
	}

	resp, err := http.Get(aSrv.URL + "/api/instances/" + nasID + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	links, _ := json.Marshal(map[string]string{"links": "https://example.com/one.zip"})
	resp, err = http.Post(aSrv.URL+"/api/instances/"+nasID+"/links", "application/json", bytes.NewReader(links))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// The read went first, so a handover it had announced would be read here.
	if peer, out := handed(readOneOfType(t, sent, "handover")); peer != nasID || !out {
		t.Errorf("forwarded links announced as peer %q, out %v, want the member going out", peer, out)
	}

	arrived := dialWS(t, "ws"+strings.TrimPrefix(bSrv.URL, "http"))
	readOneOfType(t, arrived, "snapshot")
	w := httptest.NewRecorder()
	add := httptest.NewRequest(http.MethodPost, "/api/links", bytes.NewReader(links))
	add.Header.Set("Content-Type", "application/json")
	add.Host = "127.0.0.1:8080"
	serveMember(bApp, bApp.SelfServeHandler(), group.Caller{ID: "pixel", Kind: member.KindMobile}, w, add)
	if w.Code/100 != 2 {
		t.Fatalf("links from the phone answered %d: %s", w.Code, w.Body.Bytes())
	}
	if peer, out := handed(readOneOfType(t, arrived, "handover")); peer != "pixel" || out {
		t.Errorf("arriving links announced as peer %q, out %v, want pixel coming in", peer, out)
	}
	if got := len(aApp.Tasks()); got != 0 {
		t.Errorf("the forwarding instance holds %d tasks, want none", got)
	}
}

// The forwarded path arrives unescaped, so tasks%2F..%2Fsettings reads
// tasks/../settings. It must not pass for a task route: the peer's mux would
// redirect it to /api/settings and the client follow with the peer token.
func TestAForwardedPathCannotClimbOutOfTheTaskRoutes(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var asked []string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if strings.Contains(r.URL.Path, "..") {
			http.Redirect(w, r, "/api/settings", http.StatusMovedPermanently)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer peer.Close()

	srv, a := testServer(t)
	defer srv.Close()
	a.Federation.SetGroup(&servedMember{url: peer.URL})

	for _, p := range []string{"tasks%2F..%2Fsettings", "tasks%2F%2E%2E%2Fsettings", "tasks%2F.%2F..%2Fsettings", "queue%2F..%2F..%2Fapi%2Fsettings"} {
		resp, err := http.Get(srv.URL + "/api/instances/" + nasID + "/" + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s answered %d, want 403", p, resp.StatusCode)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range asked {
		if p != "/api/tasks" {
			t.Errorf("the peer was asked for %s", p)
		}
	}
}

func readAll(r *http.Response) ([]byte, error) {
	defer r.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

// relayOnlyPeer is a group with members reached through the relay alone,
// such as a desktop build. It answers one path so the proxy below has
// something real to reach.
type relayOnlyPeer struct {
	members []group.Member
	gotPath string
	gotBody []byte
	gotBoth bool
}

func (r *relayOnlyPeer) Members() []group.Member { return r.members }
func (r *relayOnlyPeer) RelayConnected() bool    { return true }
func (r *relayOnlyPeer) Close() error            { return nil }
func (r *relayOnlyPeer) Call(_ context.Context, target, method, path string, body []byte) (int, []byte, error) {
	r.gotPath = path
	r.gotBody = body
	r.gotBoth = target == desktopID && method == http.MethodPost
	return http.StatusOK, []byte(`[{"id":"t1","name":"file.zip"}]`), nil
}

// onTheRelay is an instance of the group the relay reaches.
func onTheRelay(id, name, deployment string) group.Member {
	return group.Member{ID: id, Kind: member.KindInstance, Name: name, Deployment: deployment, State: group.StateApproved, Relay: true}
}

// TestRelayOnlyPeerIsListedAndReachable pins the two facts the browser
// extension's relay support rests on (issue #27). The extension can only open
// an HTTP connection to an address, so a peer reachable through a relay alone
// is routed to through an instance that can be opened:
//
//  1. GET /api/instances lists it with an empty url, which is the signal the
//     extension keys on and part of the contract.
//  2. POST /api/instances/{name}/links reaches it over the relay, so the
//     extension does not show a peer it cannot send to.
func TestRelayOnlyPeerIsListedAndReachable(t *testing.T) {
	t.Parallel()
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	srv := httptest.NewServer(Handler(a))
	defer srv.Close()

	// After Handler, not before: Handler calls applyRelay, which opens the
	// group from the saved settings and would drop one installed ahead of it.
	rt := &relayOnlyPeer{members: []group.Member{onTheRelay(desktopID, "Workshop laptop", "desktop")}}
	a.Federation.SetGroup(rt)

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
	// extension stores.
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

// A peer's list reads its unpackings through the forward, and nothing more of
// them: starting or stopping one stays on the peer.
func TestAPeersUnpackingsAreForwardedReadOnly(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var asked []string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`[]`))
	}))
	defer peer.Close()

	srv, a := testServer(t)
	defer srv.Close()
	a.Federation.SetGroup(&servedMember{url: peer.URL})

	call := func(method, rest string) int {
		req, _ := http.NewRequest(method, srv.URL+"/api/instances/"+nasID+"/"+rest, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := call(http.MethodGet, "extract"); code != http.StatusOK {
		t.Errorf("GET extract answered %d, want 200", code)
	}
	for _, c := range []struct{ method, rest string }{
		{http.MethodPost, "extract"},
		{http.MethodDelete, "extract"},
		{http.MethodPost, "extract/start"},
		{http.MethodGet, "extract/start"},
		{http.MethodPost, "extract/j1/abort"},
	} {
		if code := call(c.method, c.rest); code != http.StatusForbidden {
			t.Errorf("%s %s answered %d, want 403", c.method, c.rest, code)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	var jobs []string
	for _, a := range asked {
		if strings.Contains(a, "/extract") {
			jobs = append(jobs, a)
		}
	}
	if len(jobs) != 1 || jobs[0] != "GET /api/extract" {
		t.Errorf("the peer was asked %v about unpackings, want only GET /api/extract", jobs)
	}
}
