package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
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
}

func newWatchRig() *watchRig {
	r := &watchRig{clip: &fakeClipboard{}, background: true, created: 1}
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
	if r.opened != 0 || len(r.sent) != 0 || r.w.watching() {
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
	if !r.w.watching() {
		t.Fatal("watching() is false while the switch is on")
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
