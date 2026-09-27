package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
)

var twoFileTorrent = []metainfo.FileInfo{
	{Length: 900, Path: []string{"one.mkv"}},
	{Length: 12, Path: []string{"two.srt"}},
}

func stagedTorrent(t *testing.T, a *App, files []core.TorrentFile) string {
	t.Helper()
	task, err := a.AddTorrent(testTorrentURI(t, "Pack", twoFileTorrent), files, "Pack", OriginPaste)
	if err != nil || task == nil {
		t.Fatalf("AddTorrent = %+v, %v", task, err)
	}
	return task.ID
}

func selectedPaths(files []TorrentFileView) []string {
	var out []string
	for _, f := range files {
		if f.Selected {
			out = append(out, f.Path)
		}
	}
	return out
}

func TestAStagedTorrentsFilesCanBeChosenAgainFromItsRow(t *testing.T) {
	a := newTorrentTestApp(t)
	id := stagedTorrent(t, a, []core.TorrentFile{
		{Path: "one.mkv", Size: 900, Selected: true},
		{Path: "two.srt", Size: 12},
	})

	got, err := a.SelectTorrentFiles(id, []string{"two.srt"})
	if err != nil {
		t.Fatalf("SelectTorrentFiles = %v", err)
	}
	if !slices.Equal(selectedPaths(got), []string{"two.srt"}) {
		t.Fatalf("the answer selects %v, want only two.srt", selectedPaths(got))
	}
	// Nothing of a collected torrent is here yet, which is a known zero.
	if got[1].Done == nil || *got[1].Done != 0 || got[0].Done != nil {
		t.Errorf("progress = %v / %v, want 0 for the selected file and nothing for the other", got[0].Done, got[1].Done)
	}
	task := liveTask(a, id)
	if task.Size != 12 || task.TorrentFileCount != 2 {
		t.Errorf("task size %d, file count %d; want 12 and 2", task.Size, task.TorrentFileCount)
	}
	if !slices.Equal(core.SelectedTorrentIndices(task.TorrentFiles), []int{1}) {
		t.Errorf("the task keeps the selection %+v, want file 1 alone", task.TorrentFiles)
	}
}

func TestAPathTheTorrentDoesNotHaveSelectsNothing(t *testing.T) {
	a := newTorrentTestApp(t)
	id := stagedTorrent(t, a, nil)
	got, err := a.SelectTorrentFiles(id, []string{"one.mkv", "../../etc/passwd"})
	if err != nil {
		t.Fatalf("SelectTorrentFiles = %v", err)
	}
	if len(got) != 2 || !slices.Equal(selectedPaths(got), []string{"one.mkv"}) {
		t.Fatalf("files = %+v, want the torrent's own two with one.mkv selected", got)
	}
}

func TestAnUntouchedUploadShowsTheFilesItsRulesWouldFetch(t *testing.T) {
	a := newTorrentTestApp(t)
	s := a.Settings.Get()
	s.Torrent.ExcludeFiles = []string{`\.srt$`}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	id := stagedTorrent(t, a, nil)

	got, err := a.TorrentFiles(id)
	if err != nil {
		t.Fatalf("TorrentFiles = %v", err)
	}
	if !slices.Equal(selectedPaths(got), []string{"one.mkv"}) {
		t.Fatalf("the row offers %v, want what the rules keep", selectedPaths(got))
	}
	task := liveTask(a, id)
	if task.TorrentFiles != nil {
		t.Errorf("looking at the files made a selection %+v; the rules still choose at the start", task.TorrentFiles)
	}
	if task.TorrentFileCount != 2 {
		t.Errorf("file count = %d, want 2", task.TorrentFileCount)
	}
}

func TestLeavingOutEveryFileIsRefused(t *testing.T) {
	a := newTorrentTestApp(t)
	id := stagedTorrent(t, a, nil)
	if _, err := a.SelectTorrentFiles(id, nil); !errors.Is(err, ErrNoFileSelected) {
		t.Fatalf("SelectTorrentFiles(none) = %v, want ErrNoFileSelected", err)
	}
	if task := liveTask(a, id); task.TorrentFiles != nil {
		t.Errorf("a refused change left the selection %+v", task.TorrentFiles)
	}
}

func TestAFinishedTorrentKeepsItsFiles(t *testing.T) {
	a := newTorrentTestApp(t)
	id := stagedTorrent(t, a, nil)
	a.mu.Lock()
	a.tasks[id].Status = core.StatusDone
	a.mu.Unlock()
	if _, err := a.SelectTorrentFiles(id, []string{"one.mkv"}); !errors.Is(err, engine.ErrTorrentFinished) {
		t.Fatalf("SelectTorrentFiles on a finished torrent = %v, want ErrTorrentFinished", err)
	}
	got, err := a.TorrentFiles(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		if f.Selected && (f.Done == nil || *f.Done != f.Size) {
			t.Errorf("%s of a finished torrent shows %v bytes, want all %d", f.Path, f.Done, f.Size)
		}
	}
}

func TestAMagnetShowsItsFilesOnceTheSwarmHasListedThem(t *testing.T) {
	a := newTorrentTestApp(t)
	created := a.AddLinksFrom([]string{"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"}, "M", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(created))
	}
	id := created[0].ID
	if got, err := a.TorrentFiles(id); err != nil || len(got) != 0 {
		t.Fatalf("before the swarm answered: %+v, %v; want no files", got, err)
	}
	if _, err := a.SelectTorrentFiles(id, []string{"a.mkv"}); !errors.Is(err, ErrTorrentFilesUnknown) {
		t.Fatalf("a change before the list came = %v, want ErrTorrentFilesUnknown", err)
	}

	listed := []core.TorrentFile{{Path: "a.mkv", Size: 5, Selected: true}, {Path: "b.nfo", Size: 1}}
	a.onUpdate(id, core.Update{TorrentFiles: listed})
	task := liveTask(a, id)
	if task.TorrentFileCount != 2 || !slices.Equal(core.SelectedTorrentIndices(task.TorrentFiles), []int{0}) {
		t.Fatalf("after the list came: count %d, files %+v", task.TorrentFileCount, task.TorrentFiles)
	}

	if _, err := a.SelectTorrentFiles(id, []string{"a.mkv", "b.nfo"}); err != nil {
		t.Fatal(err)
	}
	// A later start reports its list again; the choice made since stands.
	a.onUpdate(id, core.Update{TorrentFiles: listed})
	if task := liveTask(a, id); core.SelectedTorrentIndices(task.TorrentFiles) != nil {
		t.Errorf("the reported list overwrote the choice made since: %+v", task.TorrentFiles)
	}
}
