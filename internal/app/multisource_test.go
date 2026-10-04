package app

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// slotDebrid is one AllDebrid account that unlocks every link to url and
// counts its unlocks.
type slotDebrid struct {
	url     string
	unlocks *atomic.Int32
}

func (slotDebrid) ID() string    { return "alldebrid" }
func (slotDebrid) Label() string { return "AllDebrid" }

func (slotDebrid) Hosts(context.Context) (map[string]bool, error) {
	return map[string]bool{"hoster.example": true}, nil
}

func (s slotDebrid) Unlock(context.Context, string) (debrid.Direct, error) {
	s.unlocks.Add(1)
	return debrid.Direct{URL: s.url}, nil
}

// wireSlot registers an AllDebrid account under account and returns its
// unlock count.
func wireSlot(a *App, account, url string) *atomic.Int32 {
	n := &atomic.Int32{}
	a.Registry.Register(debrid.Resolver{ServiceID: "alldebrid", Account: account, Prio: 90,
		Hosts: map[string]bool{"hoster.example": true}, Svc: slotDebrid{url: url, unlocks: n}})
	return n
}

func multiSourceApp(t *testing.T, on bool) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { s.MultiSource = on })
	return a
}

// sourcesOf is what the engine would be handed for t as its further links.
func sourcesOf(a *App, t *core.Task) []string {
	a.mu.Lock()
	ask := a.sourcesLocked(t, a.Settings.Get())
	a.mu.Unlock()
	if ask == nil {
		return nil
	}
	return slices.DeleteFunc(ask(context.Background()), func(s string) bool { return s == "" })
}

func TestASecondAccountUnlocksTheSameLinkAsAFurtherSource(t *testing.T) {
	a := multiSourceApp(t, true)
	first := wireSlot(a, "", "https://cdn1.example/first")
	wireSlot(a, "two", "https://cdn2.example/second")
	task := &core.Task{ID: "1", URL: "https://hoster.example/file/abc", Resolver: "alldebrid"}

	got := sourcesOf(a, task)

	if !slices.Equal(got, []string{"https://cdn2.example/second"}) {
		t.Fatalf("sources = %v, want the second account's link", got)
	}
	if first.Load() != 0 {
		t.Error("the account doing the transfer was asked to unlock again")
	}
}

func TestNoFurtherSourceIsAskedForWhileTheSettingIsOff(t *testing.T) {
	a := multiSourceApp(t, false)
	wireSlot(a, "", "https://cdn1.example/first")
	second := wireSlot(a, "two", "https://cdn2.example/second")
	task := &core.Task{ID: "1", URL: "https://hoster.example/file/abc", Resolver: "alldebrid"}

	if got := sourcesOf(a, task); got != nil {
		t.Fatalf("sources = %v, want none", got)
	}
	if second.Load() != 0 {
		t.Error("the second account unlocked the link with the setting off")
	}
}

// A parked copy from another hoster that is a plain file link joins as it is.
func TestAParkedCopyIsAFurtherSource(t *testing.T) {
	a := multiSourceApp(t, true)
	wireSlot(a, "", "https://cdn1.example/first")
	task := &core.Task{ID: "1", URL: "https://hoster.example/file/abc", Resolver: "alldebrid", Status: core.StatusRunning, Enabled: true}
	parked := &core.Task{ID: "2", URL: "https://files.example/film.rar", MirrorOf: "1", Status: core.StatusCollected, CreatedAt: time.Now()}
	a.mu.Lock()
	a.tasks["1"], a.tasks["2"] = task, parked
	a.mu.Unlock()

	if got := sourcesOf(a, task); !slices.Equal(got, []string{"https://files.example/film.rar"}) {
		t.Fatalf("sources = %v, want the parked copy's link", got)
	}
}

// A copy somebody enabled is a download of its own and not spare.
func TestAnEnabledCopyIsNoFurtherSource(t *testing.T) {
	a := multiSourceApp(t, true)
	task := &core.Task{ID: "1", URL: "https://files.example/a/film.rar", Resolver: "direct", Status: core.StatusRunning, Enabled: true}
	copied := &core.Task{ID: "2", URL: "https://files.example/b/film.rar", MirrorOf: "1", Status: core.StatusQueued, Enabled: true}
	a.mu.Lock()
	a.tasks["1"], a.tasks["2"] = task, copied
	a.mu.Unlock()

	if got := sourcesOf(a, task); got != nil {
		t.Fatalf("sources = %v, want none", got)
	}
}

// JD and yt-dlp fetch for themselves, so there is no link of theirs to stand
// further ones beside.
func TestATaskJDFetchesAsksForNoFurtherSource(t *testing.T) {
	a := multiSourceApp(t, true)
	second := wireSlot(a, "two", "https://cdn2.example/second")
	task := &core.Task{ID: "1", URL: "https://hoster.example/file/abc", Resolver: "jd"}

	if got := sourcesOf(a, task); got != nil {
		t.Fatalf("sources = %v, want none", got)
	}
	if second.Load() != 0 {
		t.Error("an account unlocked a link JD is fetching")
	}
}
