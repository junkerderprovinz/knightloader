package app

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func TestUndoingTheRemovalOfAnUnpackingDownloadKeepsItFinished(t *testing.T) {
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { noUnpacking(s) })
	be := newOrderBackend()
	withBackend(a, "elsewhere", be)
	a.Registry.Register(elsewhereResolver{})
	path := filepath.Join(dir, "film.rar")
	fileBytes(t, path, 64)
	putTask(t, a, core.Task{ID: "x1", URL: "https://elsewhere.example/film.rar", Name: "film.rar", Resolver: "elsewhere",
		Status: core.StatusExtracting, Enabled: true, Size: 64, Loaded: 64, File: path})

	_, token := a.RemoveTasksUndoable([]string{"x1"}, false)
	if back := a.UndoRemove(token); len(back) != 1 {
		t.Fatalf("undo brought back %v", back)
	}

	if got := liveTask(a, "x1"); got.Status != core.StatusDone {
		t.Errorf("the finished download came back as %s", got.Status)
	}
	if !fileExists(path) {
		t.Error("the archive was deleted")
	}
	if started := be.startedIDs(); len(started) > 0 {
		t.Errorf("the finished download was fetched again: %v", started)
	}
}

func TestAFinishedRowBroughtBackByUndoDoesNotBlockItsLink(t *testing.T) {
	a, dir := newRuleApp(t, func(s *settings.Settings, _ string) { noUnpacking(s) })
	u := "https://host.example/files/report.pdf"
	path := filepath.Join(dir, "report.pdf")
	fileBytes(t, path, 4)
	putTask(t, a, core.Task{ID: "done1", URL: u, Name: "report.pdf", Resolver: "direct",
		Status: core.StatusDone, Enabled: true, Size: 4, Loaded: 4, File: path})

	_, token := a.RemoveTasksUndoable([]string{"done1"}, false)
	if back := a.UndoRemove(token); len(back) != 1 {
		t.Fatalf("undo brought back %v", back)
	}

	if got := a.AddLinks([]string{u}, ""); len(got) != 1 {
		t.Errorf("pasting the finished link again staged %d rows, want 1", len(got))
	}
}

func TestUndoRetriesAFailedRowWhoseRetryFellDueWhileItWasRemoved(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) { noUnpacking(s) })
	be := newOrderBackend()
	withBackend(a, "elsewhere", be)
	a.Registry.Register(elsewhereResolver{})
	wait := 50 * time.Millisecond
	due := time.Now().Add(wait)
	putTask(t, a, core.Task{ID: "e1", URL: "https://elsewhere.example/e1.bin", Name: "e1.bin", Resolver: "elsewhere",
		Status: core.StatusError, Enabled: true, Retries: 1, MaxTries: 5, NextTry: due})
	a.retryAfter("e1", wait, due)

	_, token := a.RemoveTasksUndoable([]string{"e1"}, false)
	time.Sleep(4 * wait)
	if back := a.UndoRemove(token); len(back) != 1 {
		t.Fatalf("undo brought back %v", back)
	}

	waitFor(t, "the retry", func() bool { return slices.Contains(be.startedIDs(), "e1") })
}
