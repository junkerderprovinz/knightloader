package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

func TestClipboardLinksMatchThePage(t *testing.T) {
	raw, err := os.ReadFile("../web/src/lib/clipboardWatch.cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string   `json:"name"`
		Text  string   `json:"text"`
		Links []string `json:"links"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got := clipboardLinks(c.Text)
		if len(got) == 0 && len(c.Links) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.Links) {
			t.Errorf("%s: got %q, want %q", c.Name, got, c.Links)
		}
	}
}

func TestPolledClipboardReadsOnlyAfterTheStampMoves(t *testing.T) {
	stamp, reads := uint64(1), 0
	p := &polledClipboard{
		read:  func() (string, bool) { reads++; return "text", true },
		stamp: func() (uint64, bool) { return stamp, true },
	}
	if _, fresh := p.latest(); !fresh || reads != 1 {
		t.Fatalf("first call: fresh %v after %d reads", fresh, reads)
	}
	if _, fresh := p.latest(); fresh || reads != 1 {
		t.Fatalf("unchanged stamp: fresh %v after %d reads", fresh, reads)
	}
	stamp = 2
	if _, fresh := p.latest(); !fresh || reads != 2 {
		t.Fatalf("moved stamp: fresh %v after %d reads", fresh, reads)
	}
}

func TestPolledClipboardRetriesAFailedRead(t *testing.T) {
	ok := false
	p := &polledClipboard{
		read:  func() (string, bool) { return "text", ok },
		stamp: func() (uint64, bool) { return 7, true },
	}
	if _, fresh := p.latest(); fresh {
		t.Fatal("a failed read counted as fresh")
	}
	ok = true
	if _, fresh := p.latest(); !fresh {
		t.Fatal("the change behind a failed read was lost")
	}
}

// fakeClipboard hands out queued texts, one per call.
type fakeClipboard struct {
	texts  []string
	closed bool
}

func (f *fakeClipboard) latest() (string, bool) {
	if len(f.texts) == 0 {
		return "", false
	}
	text := f.texts[0]
	f.texts = f.texts[1:]
	return text, true
}

func (f *fakeClipboard) close() { f.closed = true }

type delivery struct {
	target string
	links  []string
}

type watchRig struct {
	w          *clipWatch
	settings   clipSettings
	clip       *fakeClipboard
	opened     int
	background bool
	sent       []delivery
	created    int
	err        error
	outcomes   []clipOutcome
	// leases and leaves are the targets each lease call went to.
	leases []string
	leaves []string
	// stops are the targets where another device asked the watch to stop. A
	// renewal there hears it and a leave drops it, as in clipwatch.Registry.
	stops       map[string]bool
	switchedOff bool
}

func newWatchRig() *watchRig {
	r := &watchRig{clip: &fakeClipboard{}, background: true, created: 1, stops: map[string]bool{}}
	r.w = &clipWatch{
		settings: func() clipSettings { return r.settings },
		open: func() (clipboardReader, bool) {
			r.opened++
			r.clip.closed = false
			return r.clip, r.background
		},
		deliver: func(_ context.Context, target string, links []string) (int, error) {
			r.sent = append(r.sent, delivery{target, links})
			return r.created, r.err
		},
		report: func(o clipOutcome) { r.outcomes = append(r.outcomes, o) },
		lease: func(_ context.Context, target string) bool {
			r.leases = append(r.leases, target)
			stop := r.stops[target]
			delete(r.stops, target)
			return stop
		},
		leave: func(_ context.Context, target string) {
			r.leaves = append(r.leaves, target)
			delete(r.stops, target)
		},
		switchOff: func() {
			r.switchedOff = true
			r.settings.On = false
		},
	}
	return r
}

// copy puts text on the clipboard and runs one round.
func (r *watchRig) copy(text string) {
	r.clip.texts = append(r.clip.texts, text)
	r.w.step(context.Background())
}

func TestClipWatchLeavesTheClipboardAloneWhileOff(t *testing.T) {
	r := newWatchRig()
	r.copy("https://host.example/a")
	if r.opened != 0 || len(r.sent) != 0 {
		t.Fatalf("switched off, it opened the clipboard %d times and sent %v", r.opened, r.sent)
	}
}

func TestClipWatchSendsOnlyNewLinks(t *testing.T) {
	r := newWatchRig()
	r.settings = clipSettings{On: true, Target: "nas"}
	r.copy("https://host.example/old")
	if len(r.sent) != 0 {
		t.Fatalf("what was on the clipboard before the switch went on was sent: %v", r.sent)
	}
	r.copy("the file is at https://host.example/new, my pin is 1234")
	r.copy("the file is at https://host.example/new, my pin is 1234")
	r.copy("no link here")
	want := []delivery{{"nas", []string{"https://host.example/new,"}}}
	if !reflect.DeepEqual(r.sent, want) {
		t.Fatalf("sent %v, want %v", r.sent, want)
	}
}

func TestClipWatchReseedsAfterBeingSwitchedOff(t *testing.T) {
	r := newWatchRig()
	r.settings.On = true
	r.copy("")
	r.settings.On = false
	r.copy("https://host.example/while-off")
	if !r.clip.closed {
		t.Fatal("switching off left the clipboard reader open")
	}
	r.settings.On = true
	r.w.step(context.Background())
	if len(r.sent) != 0 {
		t.Fatalf("a link copied while off was sent: %v", r.sent)
	}
	if r.opened != 2 {
		t.Fatalf("opened the clipboard %d times, want 2", r.opened)
	}
}

func TestClipWatchReportsEachOutcome(t *testing.T) {
	r := newWatchRig()
	r.settings.On = true
	r.copy("")
	r.copy("https://host.example/a")
	r.created = 0
	r.copy("https://host.example/b")
	r.err = errors.New("nas unreachable")
	r.copy("https://host.example/c")
	want := []clipOutcome{
		{Kind: "staged", N: 1},
		{Kind: "none"},
		{Kind: "failed", Reason: "nas unreachable"},
	}
	if !reflect.DeepEqual(r.outcomes, want) {
		t.Fatalf("reported %v, want %v", r.outcomes, want)
	}
}

func TestClipWatchSaysWhenOnlyAFocusedWindowSeesTheClipboard(t *testing.T) {
	r := newWatchRig()
	r.background = false
	r.settings.On = true
	r.copy("")
	if want := []clipOutcome{{Kind: "limited"}}; !reflect.DeepEqual(r.outcomes, want) {
		t.Fatalf("reported %v, want %v", r.outcomes, want)
	}
}

func TestClipWatchSendsNothingAfterStop(t *testing.T) {
	r := newWatchRig()
	r.settings.On = true
	r.copy("")
	r.w.stop()
	r.copy("https://host.example/a")
	if len(r.sent) != 0 {
		t.Fatalf("sent %v after stop", r.sent)
	}
}

func TestClipWatchHoldsALeaseWhileOn(t *testing.T) {
	r := newWatchRig()
	r.copy("https://host.example/a")
	if len(r.leases) != 0 {
		t.Fatalf("switched off, it leased %v", r.leases)
	}
	r.settings.On = true
	r.copy("")
	r.copy("https://host.example/b")
	if !reflect.DeepEqual(r.leases, []string{""}) {
		t.Fatalf("leased %v within a minute, want once with this instance", r.leases)
	}
	r.settings.On = false
	r.copy("")
	r.copy("")
	if !reflect.DeepEqual(r.leaves, []string{""}) {
		t.Fatalf("switching off left %v, want this instance once", r.leaves)
	}
	r.settings.On = true
	r.copy("")
	if len(r.leases) != 2 {
		t.Fatalf("switching on again leased %v in all, want two", r.leases)
	}
}

func TestClipWatchMovesItsLeaseWithTheTarget(t *testing.T) {
	r := newWatchRig()
	r.settings = clipSettings{On: true}
	r.copy("")
	r.settings.Target = "nas"
	r.copy("")
	if !reflect.DeepEqual(r.leaves, []string{""}) || !reflect.DeepEqual(r.leases, []string{"", "", "nas"}) {
		t.Fatalf("left %v and leased %v, want a last renewal here, then to leave and lease with nas", r.leaves, r.leases)
	}
	r.settings.On = false
	r.copy("")
	if !reflect.DeepEqual(r.leaves, []string{"", "nas"}) {
		t.Fatalf("switching off left %v, want nas last", r.leaves)
	}
}

func TestClipWatchStopsWhenAnotherDeviceAsks(t *testing.T) {
	r := newWatchRig()
	r.stops[""] = true
	r.settings.On = true
	r.copy("")
	r.copy("https://host.example/a")
	if !r.switchedOff {
		t.Fatal("the page's switch stayed on")
	}
	if len(r.sent) != 0 || r.opened != 0 {
		t.Fatalf("after the stop it opened the clipboard %d times and sent %v", r.opened, r.sent)
	}
	if want := []clipOutcome{{Kind: "stopped"}}; !reflect.DeepEqual(r.outcomes, want) {
		t.Fatalf("reported %v, want %v", r.outcomes, want)
	}
	if len(r.leaves) != 0 {
		t.Fatalf("a watch the list already dropped left it again: %v", r.leaves)
	}
}

func TestClipWatchHearsAStopAskedBeforeTheTargetChanged(t *testing.T) {
	r := newWatchRig()
	r.settings.On = true
	r.copy("")
	r.stops[""] = true
	r.settings.Target = "nas"
	r.copy("https://host.example/a")
	if !r.switchedOff {
		t.Fatal("moving the lease to nas lost the stop waiting at this instance")
	}
	if len(r.sent) != 0 {
		t.Fatalf("sent %v after the stop", r.sent)
	}
	if want := []clipOutcome{{Kind: "stopped"}}; !reflect.DeepEqual(r.outcomes, want) {
		t.Fatalf("reported %v, want %v", r.outcomes, want)
	}
}

func TestClipWatchLeavesTheListWhenTheAppQuits(t *testing.T) {
	r := newWatchRig()
	r.settings = clipSettings{On: true, Target: "nas"}
	r.copy("")
	r.w.stop()
	if !reflect.DeepEqual(r.leaves, []string{"nas"}) {
		t.Fatalf("quitting left %v, want nas", r.leaves)
	}
	// A round already under way when the app quits.
	r.copy("")
	if !reflect.DeepEqual(r.leases, []string{"nas", "nas"}) || len(r.leaves) != 1 {
		t.Fatalf("leased %v and left %v, want a lease, one last renewal on quitting and nothing after", r.leases, r.leaves)
	}
}

func TestClipWatchHearsAStopAskedBeforeTheAppQuits(t *testing.T) {
	r := newWatchRig()
	r.settings = clipSettings{On: true, Target: "nas"}
	r.copy("")
	r.stops["nas"] = true
	r.w.stop()
	if !r.switchedOff {
		t.Fatal("quitting lost the stop waiting at nas, so the next start watches again")
	}
	if len(r.leaves) != 0 {
		t.Fatalf("left %v after the renewal had already taken the watch off the list", r.leaves)
	}
}

func TestClipWatchQuitWhileOffLeavesNothing(t *testing.T) {
	r := newWatchRig()
	r.copy("")
	r.w.stop()
	if len(r.leaves) != 0 {
		t.Fatalf("a watch that held no lease left %v", r.leaves)
	}
}

func TestSwitchClipOffKeepsThePagesOtherFields(t *testing.T) {
	a := newClipApp(t)
	doc := `{"columns":{"name":240},"clipboardWatch":true,"clipboardWatchTarget":"nas"}`
	if err := a.SetUIState(store.UIStateKey, doc); err != nil {
		t.Fatal(err)
	}
	if err := switchClipOff(a); err != nil {
		t.Fatal(err)
	}
	if s := readClipSettings(a); s != (clipSettings{Target: "nas"}) {
		t.Fatalf("read %+v", s)
	}
	raw, _ := a.UIState(store.UIStateKey)
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &got); err != nil || string(got["columns"]) != `{"name":240}` {
		t.Fatalf("the other fields became %s", raw)
	}
}

func TestClipWatchLeasesWithThisInstance(t *testing.T) {
	a := newClipApp(t)
	if leaseClip(context.Background(), a, "") {
		t.Fatal("a fresh lease was told to stop")
	}
	list := a.ClipWatch.List(time.Now())
	if len(list) != 1 || list[0].Kind != "desktop" || list[0].ID != clipWatcher(a).ID {
		t.Fatalf("listed %+v", list)
	}
	a.ClipWatch.Stop(list[0].ID, time.Now())
	if !leaseClip(context.Background(), a, "") {
		t.Fatal("the renewal after a stop did not say so")
	}
	leaseClip(context.Background(), a, "")
	leaveClip(context.Background(), a, "")
	if list := a.ClipWatch.List(time.Now()); len(list) != 0 {
		t.Fatalf("after leaving, listed %+v", list)
	}
}

func TestClipWatchLeasesWithThePeerItSendsTo(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var kind string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var b struct{ Kind string }
		_ = json.Unmarshal(body, &b)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPut {
			kind = b.Kind
		}
		mu.Unlock()
		if r.Method == http.MethodPut {
			_, _ = io.WriteString(w, `{"stop":true}`)
		}
	}))
	defer peer.Close()
	a := newClipApp(t)
	if err := a.Federation.Add(federation.Instance{Name: "nas", URL: peer.URL}); err != nil {
		t.Fatal(err)
	}

	if !leaseClip(context.Background(), a, "nas") {
		t.Fatal("the peer's stop did not come through")
	}
	leaveClip(context.Background(), a, "nas")
	path := "/api/clipboard-watchers/" + clipWatcher(a).ID
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"PUT " + path, "DELETE " + path}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("the peer was asked %v, want %v", calls, want)
	}
	if kind != "desktop" {
		t.Fatalf("leased as %q, want desktop", kind)
	}
	if list := a.ClipWatch.List(time.Now()); len(list) != 0 {
		t.Fatalf("this instance holds %+v, want nothing", list)
	}
}

func newClipApp(t *testing.T) *app.App {
	t.Helper()
	t.Setenv("KL_JD", "")
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestReadClipSettingsReadsThePagesFields(t *testing.T) {
	a := newClipApp(t)
	if s := readClipSettings(a); s.On {
		t.Fatal("a fresh instance reads as watching")
	}
	doc := `{"columns":{"name":240},"clipboardWatch":true,"clipboardWatchTarget":"nas"}`
	if err := a.SetUIState(store.UIStateKey, doc); err != nil {
		t.Fatal(err)
	}
	if s := readClipSettings(a); s != (clipSettings{On: true, Target: "nas"}) {
		t.Fatalf("read %+v", s)
	}
}

func TestDeliverLinksStagesOnThisInstance(t *testing.T) {
	a := newClipApp(t)
	n, err := deliverLinks(context.Background(), a, "", []string{"https://host.example/file.bin"})
	if err != nil || n != 1 {
		t.Fatalf("staged %d, err %v", n, err)
	}
	n, err = deliverLinks(context.Background(), a, "", []string{"https://host.example/file.bin"})
	if err != nil || n != 0 {
		t.Fatalf("the same link again staged %d, err %v", n, err)
	}
}

func TestDeliverLinksRefusesAnUnknownPeer(t *testing.T) {
	a := newClipApp(t)
	if _, err := deliverLinks(context.Background(), a, "nowhere", []string{"https://host.example/a"}); err == nil {
		t.Fatal("an unknown peer took the links")
	}
}
