package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// readdOf stages a fresh copy of a link that an earlier batch already
// downloaded into a package of its own, the way pasting it again does.
func readdOf(t *testing.T) (a *App, done, again *core.Task) {
	t.Helper()
	a, _ = newRuleApp(t, func(*settings.Settings, string) {})
	const url = "https://host.example/files/movie.mkv"
	done = putTask(t, a, core.Task{URL: url, Name: "movie.mkv", Package: "Earlier batch", ManualPackage: true,
		Status: core.StatusDone, CreatedAt: time.Now().Add(-time.Hour)})
	again = putTask(t, a, core.Task{URL: url, Name: url, Status: core.StatusCollected, Enabled: true})
	return a, done, again
}

func TestNamingARepastedLinkLeavesTheFinishedCopyInItsPackage(t *testing.T) {
	a, done, again := readdOf(t)

	a.setTaskName(again.ID, "movie.mkv")

	if got := snapshot(t, a, again.ID).Package; got != "movie.mkv" {
		t.Errorf("re-pasted link's package = %q, want it filed under its probed name", got)
	}
	if got := snapshot(t, a, done.ID).Package; got != "Earlier batch" {
		t.Errorf("finished copy's package = %q, want it left in %q, where its file is", got, "Earlier batch")
	}
}

func TestMovingARepastedLinkLeavesTheFinishedCopyInItsPackage(t *testing.T) {
	a, done, again := readdOf(t)

	a.SetPackage([]string{again.ID}, "Elsewhere")

	if got := snapshot(t, a, again.ID).Package; got != "Elsewhere" {
		t.Errorf("re-pasted link's package = %q, want %q", got, "Elsewhere")
	}
	if got := snapshot(t, a, done.ID).Package; got != "Earlier batch" {
		t.Errorf("finished copy's package = %q, want it left in %q", got, "Earlier batch")
	}
}
