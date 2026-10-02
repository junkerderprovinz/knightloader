package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// downloadedBefore files a finished download in the history without putting it
// in the list, as after the list was cleared or trimmed.
func downloadedBefore(t *testing.T, a *App, id, url, name string, size int64) time.Time {
	t.Helper()
	at := time.UnixMilli(time.Date(2026, 9, 14, 18, 30, 0, 0, time.Local).UnixMilli())
	if err := a.Store.Save(&core.Task{
		ID: id, URL: url, Name: name, Size: size, Loaded: size,
		Status: core.StatusDone, CreatedAt: at.Add(-time.Minute), FinishedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	return at
}

// historyApp is an app with the history check at its default and mirrors
// matched on name and size.
func historyApp(t *testing.T, mutate func(s *settings.Settings)) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.MirrorPolicy = string(dedupe.PolicyFilenameAndSize)
		if mutate != nil {
			mutate(s)
		}
	})
	return a
}

func onlyTask(t *testing.T, created []*core.Task) *core.Task {
	t.Helper()
	if len(created) != 1 {
		t.Fatalf("staging made %d tasks, want 1", len(created))
	}
	return created[0]
}

func TestALinkFromTheHistoryIsRejectedWithTheDownloadItRepeats(t *testing.T) {
	a := historyApp(t, nil)
	at := downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddLinksFrom([]string{"https://host.example/film.mkv"}, "", OriginFeed))
	if !got.Skipped {
		t.Fatalf("the link was staged as %q, want it among the rejected links", got.Status)
	}
	if got.SkipCode != skipDownloaded {
		t.Errorf("skip code = %q, want %q", got.SkipCode, skipDownloaded)
	}
	if got.SkipParams["name"] != "film.mkv" {
		t.Errorf("the reason names %q, want the downloaded file", got.SkipParams["name"])
	}
	if got.SkipParams["finished"] != at.Format(time.RFC3339) {
		t.Errorf("the reason dates the download %q, want %q", got.SkipParams["finished"], at.Format(time.RFC3339))
	}
	if held := a.FilteredLinks(); len(held) != 1 || held[0].ID != got.ID {
		t.Errorf("the rejected links are %+v, want the one link", held)
	}
}

// Host case and a default port do not make a second download of one link.
func TestTheHistoryMatchesALinkWrittenDifferently(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddLinks([]string{"https://HOST.example:443/film.mkv"}, ""))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the link was staged (skipped=%v, code=%q), want it rejected as downloaded", got.Skipped, got.SkipCode)
	}
}

func TestTheSameFileAtAnotherLinkIsRejectedUnderTheMirrorPolicy(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddResolvedLinksFrom([]resolver.Result{
		{DirectURL: "https://two.example/f/abc", Name: "film.mkv", Size: 4096},
	}, "", OriginPaste))
	if !got.Skipped || got.SkipCode != skipDownloaded {
		t.Errorf("the mirror was staged (skipped=%v, code=%q), want it rejected as downloaded", got.Skipped, got.SkipCode)
	}
}

func TestAMirrorOfADownloadIsAddedWhenThePolicyIsOff(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.MirrorPolicy = string(dedupe.PolicyOff) })
	downloadedBefore(t, a, "old", "https://one.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddResolvedLinksFrom([]resolver.Result{
		{DirectURL: "https://two.example/f/abc", Name: "film.mkv", Size: 4096},
	}, "", OriginPaste))
	if got.Skipped {
		t.Errorf("the link was rejected (%s), want it staged: with mirrors off only the same link counts", got.SkipReason)
	}
}

func TestALinkFromTheHistoryIsAddedWhenTheCheckIsOff(t *testing.T) {
	a := historyApp(t, func(s *settings.Settings) { s.RejectDownloaded = false })
	downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)

	got := onlyTask(t, a.AddLinks([]string{"https://host.example/film.mkv"}, ""))
	if got.Skipped {
		t.Errorf("the link was rejected (%s), want it staged with the check switched off", got.SkipReason)
	}
}

func TestRestoringALinkTheHistoryRejectedAddsItAnyway(t *testing.T) {
	a := historyApp(t, nil)
	downloadedBefore(t, a, "old", "https://host.example/film.mkv", "film.mkv", 4096)
	held := onlyTask(t, a.AddLinks([]string{"https://host.example/film.mkv"}, ""))

	restored := onlyTask(t, a.RestoreFiltered([]string{held.ID}))
	if restored.Skipped || restored.Status != core.StatusCollected {
		t.Errorf("restored as skipped=%v status=%q, want it in the collector", restored.Skipped, restored.Status)
	}
	if !filterWaived(restored) {
		t.Error("the restored link carries no waiver, so the queue could refuse it again")
	}
}

// A download that finishes while the app runs counts from then on, and a
// cleared history stops counting.
func TestTheCheckFollowsTheHistoryAsItChanges(t *testing.T) {
	a := historyApp(t, nil)
	first := onlyTask(t, a.AddLinks([]string{"https://host.example/a.bin"}, ""))
	if first.Skipped {
		t.Fatalf("a link the history does not have was rejected: %s", first.SkipReason)
	}

	downloadedBefore(t, a, "b", "https://host.example/b.bin", "b.bin", 10)
	if got := onlyTask(t, a.AddLinks([]string{"https://host.example/b.bin"}, "")); !got.Skipped {
		t.Error("a download finished after the first check was not seen by the next one")
	}

	if err := a.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if got := onlyTask(t, a.AddLinks([]string{"https://host.example/c.bin"}, "")); got.Skipped {
		t.Errorf("a link was rejected after the history was cleared: %s", got.SkipReason)
	}
	downloadedBefore(t, a, "d", "https://host.example/d.bin", "d.bin", 10)
	if err := a.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if got := onlyTask(t, a.AddLinks([]string{"https://host.example/d.bin"}, "")); got.Skipped {
		t.Errorf("a cleared history still rejected a link: %s", got.SkipReason)
	}
}
