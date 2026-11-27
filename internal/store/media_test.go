package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// finishVideo saves a yt-dlp download of the video key as finished at at.
func finishVideo(t *testing.T, s *Store, id, key string, at time.Time) {
	t.Helper()
	if err := s.Save(&core.Task{
		ID: id, URL: "https://www.youtube.com/watch?v=" + id, Name: "Clip", Media: key,
		File:   filepath.Join("downloads", "Channel", "Clip [1080p].mp4"),
		Status: core.StatusDone, CreatedAt: at.Add(-time.Minute), FinishedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAFinishedVideoIsFiledUnderItsIdentity(t *testing.T) {
	s := openStore(t)
	at := time.UnixMilli(time.Date(2026, 10, 3, 20, 15, 0, 0, time.Local).UnixMilli())
	finishVideo(t, s, "one", "youtube dQw4w9WgXcQ", at)

	got, ok, err := s.DownloadedMedia("youtube dQw4w9WgXcQ")
	if err != nil || !ok {
		t.Fatalf("the archive has no record of the video (%v)", err)
	}
	if got.TaskID != "one" || got.Name != "Clip [1080p].mp4" || !got.FinishedAt.Equal(at) {
		t.Errorf("the record is %+v, want the download's task, file name and finish time", got)
	}
	if got.Folder != filepath.Join("downloads", "Channel") {
		t.Errorf("the record names the folder %q, want the one the file is in", got.Folder)
	}
}

func TestAVideoNotDownloadedYetIsNotInTheArchive(t *testing.T) {
	s := openStore(t)
	if err := s.Save(&core.Task{
		ID: "waiting", URL: "https://youtu.be/dQw4w9WgXcQ", Media: "youtube dQw4w9WgXcQ",
		Status: core.StatusCollected, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.DownloadedMedia("youtube dQw4w9WgXcQ"); err != nil || ok {
		t.Errorf("a collected video counts as downloaded (ok=%v, err=%v)", ok, err)
	}
}

// The identity is learned by a probe long before the download finishes, and
// a restart in between must not lose it.
func TestATasksIdentitySurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(&core.Task{
		ID: "queued", URL: "https://youtu.be/dQw4w9WgXcQ", Media: "youtube dQw4w9WgXcQ",
		Status: core.StatusQueued, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	all, err := again.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Media != "youtube dQw4w9WgXcQ" {
		t.Errorf("the reloaded tasks are %+v, want the queued video with its identity", all)
	}
}

func TestTheArchiveOutlivesATrimmedHistoryAndTheTask(t *testing.T) {
	s := openStore(t)
	at := time.Now().Add(-time.Hour)
	finishVideo(t, s, "old", "youtube aaaaaaaaaaa", at)
	finishVideo(t, s, "new", "youtube bbbbbbbbbbb", at.Add(time.Minute))
	if _, err := s.TrimHistory(1); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("old"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.DownloadedMedia("youtube aaaaaaaaaaa"); err != nil || !ok {
		t.Errorf("the trimmed and deleted download left the archive (ok=%v, err=%v)", ok, err)
	}
}

// Clearing the history is somebody asking this box to forget what it
// downloaded, and the duplicate check would otherwise go on rejecting links
// with nothing left on screen to say why.
func TestClearingTheHistoryEmptiesTheArchive(t *testing.T) {
	s := openStore(t)
	finishVideo(t, s, "one", "youtube dQw4w9WgXcQ", time.Now())
	if err := s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.DownloadedMedia("youtube dQw4w9WgXcQ"); err != nil || ok {
		t.Errorf("the video is still in the archive after the history was cleared (ok=%v, err=%v)", ok, err)
	}
}

func TestADownloadRemovedByHandLeavesTheArchiveUntilAnUndo(t *testing.T) {
	s := openStore(t)
	finishVideo(t, s, "one", "youtube dQw4w9WgXcQ", time.Now())

	if err := s.MarkHistoryRemoved([]string{"one"}, true); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.DownloadedMedia("youtube dQw4w9WgXcQ"); ok {
		t.Error("a download removed by hand still counts as downloaded")
	}
	if err := s.MarkHistoryRemoved([]string{"one"}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.DownloadedMedia("youtube dQw4w9WgXcQ"); !ok {
		t.Error("an undone removal left the video out of the archive")
	}
}

func TestTheArchiveNamesTheLatestDownloadOfAVideo(t *testing.T) {
	s := openStore(t)
	at := time.Now().Add(-time.Hour)
	finishVideo(t, s, "first", "youtube dQw4w9WgXcQ", at)
	finishVideo(t, s, "second", "youtube dQw4w9WgXcQ", at.Add(time.Minute))

	got, ok, err := s.DownloadedMedia("youtube dQw4w9WgXcQ")
	if err != nil || !ok || got.TaskID != "second" {
		t.Errorf("the archive answers with %+v (ok=%v, err=%v), want the second download", got, ok, err)
	}
}

// The archive is a table beside the numbered migrations, so the build before
// it still opens the database and a database that build wrote gets the table.
func TestTheArchiveLeavesTheSchemaVersionAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db, migrations); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v := versionOf(t, s.db); v != len(migrations) {
		t.Errorf("the schema version is %d, want %d as the numbered migrations leave it", v, len(migrations))
	}
	if err := migrate(s.db, migrations); err != nil {
		t.Errorf("the numbered migrations refuse the database: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, mediaTable).Scan(&n); err != nil || n != 1 {
		t.Errorf("found %d tables named %s (%v), want the archive", n, mediaTable, err)
	}
	finishVideo(t, s, "one", "youtube dQw4w9WgXcQ", time.Now())
	if _, ok, err := s.DownloadedMedia("youtube dQw4w9WgXcQ"); err != nil || !ok {
		t.Errorf("the archive does not work on a database the build before it wrote (ok=%v, err=%v)", ok, err)
	}
}
