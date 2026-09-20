package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/reclaim"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The body every test in this file uses, and a different body of exactly the
// same length. A half-finished engine download is already at its full final
// size, so length cannot tell the two apart and only the hash can.
const (
	realBody  = "the real film!!!"
	otherBody = "................"
)

func sha256Of(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeBody(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findingFor(t *testing.T, rep ReclaimReport, id string) reclaim.Finding {
	t.Helper()
	for _, f := range rep.Findings {
		if f.TaskID == id {
			return f
		}
	}
	t.Fatalf("task %s is not in the report at all: %+v", id, rep.Findings)
	return reclaim.Finding{}
}

// The situation the pass is for: the list was emptied, the links went back in,
// and the finished file is still in the download folder. The witness is this
// instance's own history, the one record that survives the list being cleared.
func TestAFileThisInstanceAlreadyFetchedIsNotFetchedAgain(t *testing.T) {
	dl := t.TempDir()
	size := int64(len(realBody))
	finished := time.Now().Add(-time.Hour)
	f := newBootFixture(t, nil,
		core.Task{ID: "fetched-before", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusDone, Size: size, Loaded: size,
			CreatedAt: finished, FinishedAt: finished, Enabled: true},
		core.Task{ID: "pasted-again", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusQueued, Size: size, Enabled: true},
	)
	writeBody(t, dl, "movie.mkv", realBody)

	a := f.boot(t)
	// The emptied list: RemoveTasks without deleteFiles takes the row, keeps the
	// history record and leaves the file.
	a.RemoveTasks([]string{"fetched-before"}, false)

	rep, err := a.Reclaim()
	if err != nil {
		t.Fatal(err)
	}

	got := taskOf(t, a, "pasted-again")
	if got.Status != core.StatusDone {
		t.Fatalf("status = %q, want done: the file is in the folder and this instance's history says it fetched it", got.Status)
	}
	if got.Loaded != size {
		t.Errorf("loaded = %d, want the %d bytes that are on the disk", got.Loaded, size)
	}
	if fd := findingFor(t, rep, "pasted-again"); fd.Basis != reclaim.BasisRecord {
		t.Errorf("basis = %q, want %q", fd.Basis, reclaim.BasisRecord)
	}
	if rep.Settled != 1 {
		t.Errorf("report says %d settled, want 1", rep.Settled)
	}
	// Out of the wait queue, or the next dispatch would hand a finished task to
	// a backend: its loop reads the flags on a queued task, not its status.
	a.mu.Lock()
	var stillQueued bool
	for _, id := range a.queue {
		if id == "pasted-again" {
			stillQueued = true
		}
	}
	a.mu.Unlock()
	if stillQueued {
		t.Error("a task settled as finished is still in the wait queue, so the next dispatch would download it")
	}
	if b, err := os.ReadFile(filepath.Join(dl, "movie.mkv")); err != nil || string(b) != realBody {
		t.Errorf("the file was changed by a pass that only reads: %q, %v", b, err)
	}
}

// A checksum that disagrees means the file is fetched again: one transfer
// nobody needed beats a row reading "done" over a file that will not open. Both
// the task and the stale file are left alone, because the collision policy owns
// what happens to a file in the way, at the moment the transfer starts.
func TestARightSizedFileWithTheWrongBytesIsStillDownloaded(t *testing.T) {
	dl := t.TempDir()
	size := int64(len(realBody))
	f := newBootFixture(t, nil,
		core.Task{ID: "wrong-bytes", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusQueued, Size: size, Loaded: size,
			ExpectedHash: sha256Of(realBody), Enabled: true},
	)
	// The right length and the wrong film.
	writeBody(t, dl, "movie.mkv", otherBody)

	a := f.boot(t)
	rep, err := a.Reclaim()
	if err != nil {
		t.Fatal(err)
	}

	if fd := findingFor(t, rep, "wrong-bytes"); fd.Verdict != reclaim.Mismatch {
		t.Fatalf("verdict = %q, want mismatch (%s)", fd.Verdict, fd.Detail)
	}
	got := taskOf(t, a, "wrong-bytes")
	if got.Status != core.StatusQueued {
		t.Errorf("status = %q, want it left queued so the download still happens", got.Status)
	}
	if got.Loaded != 0 {
		t.Errorf("loaded = %d, want 0: those bytes are not this download's", got.Loaded)
	}
	if got.Checksum != "" {
		t.Errorf("checksum = %q, want it empty: the column reports this task's own download, and it has not downloaded anything", got.Checksum)
	}
	if rep.Settled != 0 {
		t.Errorf("report says %d settled, want 0", rep.Settled)
	}
	// A pass that reads the disk does not delete what it disagrees with and
	// starts no downloads.
	if b, err := os.ReadFile(filepath.Join(dl, "movie.mkv")); err != nil || string(b) != otherBody {
		t.Errorf("the file in the way was removed or rewritten: %q, %v", b, err)
	}
	a.mu.Lock()
	active := len(a.active)
	a.mu.Unlock()
	if active != 0 {
		t.Errorf("%d downloads were started by a pass that only looks at the disk", active)
	}
}

// Under the strict tier only a verified hash stops a download, so a record of
// having fetched the same name and length before does not count.
func TestTheStrictTierNeedsAChecksumAndNotARecord(t *testing.T) {
	dl := t.TempDir()
	size := int64(len(realBody))
	finished := time.Now().Add(-time.Hour)
	f := newBootFixture(t,
		func(s *settings.Settings) { s.ReclaimTrust = settings.ReclaimTrustChecksum },
		core.Task{ID: "fetched-before", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusDone, Size: size, Loaded: size,
			CreatedAt: finished, FinishedAt: finished, Enabled: true},
		core.Task{ID: "pasted-again", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusQueued, Size: size, Enabled: true},
	)
	writeBody(t, dl, "movie.mkv", realBody)

	a := f.boot(t)
	rep, err := a.Reclaim()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Trust != settings.ReclaimTrustChecksum {
		t.Fatalf("the pass ran under %q, want the configured %q", rep.Trust, settings.ReclaimTrustChecksum)
	}
	if fd := findingFor(t, rep, "pasted-again"); fd.Verdict != reclaim.Unproven {
		t.Errorf("verdict = %q, want unproven under the strict tier (%s)", fd.Verdict, fd.Detail)
	}
	if got := taskOf(t, a, "pasted-again"); got.Status != core.StatusQueued {
		t.Errorf("status = %q, want it left queued", got.Status)
	}
}

// The collector is where a person decides what to download. Reporting that a
// staged link is already in the folder helps; walking it out of the collector as
// "finished" would confirm a batch on their behalf.
func TestTheCollectorIsReportedAndNotDecidedFor(t *testing.T) {
	dl := t.TempDir()
	size := int64(len(realBody))
	f := newBootFixture(t,
		func(s *settings.Settings) { s.ReclaimTrust = settings.ReclaimTrustSize },
		core.Task{ID: "staged", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusCollected, Size: size, Enabled: true},
	)
	writeBody(t, dl, "movie.mkv", realBody)

	a := f.boot(t)
	rep, err := a.Reclaim()
	if err != nil {
		t.Fatal(err)
	}
	if fd := findingFor(t, rep, "staged"); fd.Verdict != reclaim.Complete {
		t.Fatalf("verdict = %q, want complete: the person still has to be told (%s)", fd.Verdict, fd.Detail)
	}
	if got := taskOf(t, a, "staged"); got.Status != core.StatusCollected {
		t.Errorf("status = %q, want it left in the collector", got.Status)
	}
	if rep.Settled != 0 {
		t.Errorf("report says %d settled, want 0: nothing leaves the collector on its own", rep.Settled)
	}
}

// A half-finished part file with no task behind it is counted, named and left
// alone, since nothing here can tell whether it is wanted back or wanted gone.
// The part file of a task still in the list is not an orphan.
func TestAnOrphanPartFileIsReportedAndKept(t *testing.T) {
	dl := t.TempDir()
	f := newBootFixture(t, nil,
		core.Task{ID: "live", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusQueued, Size: 1000, Enabled: true},
	)
	writeBody(t, dl, "movie.mkv"+reclaim.PartSuffix, "half")
	writeBody(t, dl, "gone.mkv"+reclaim.PartSuffix, "abandoned")

	a := f.boot(t)
	rep, err := a.Reclaim()
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.Orphans) != 1 {
		t.Fatalf("orphans = %+v, want only the part file no task claims", rep.Orphans)
	}
	if want := reclaim.PartPath(dl, "gone.mkv"); rep.Orphans[0].Path != want {
		t.Errorf("orphan = %q, want %q", rep.Orphans[0].Path, want)
	}
	for _, name := range []string{"movie.mkv" + reclaim.PartSuffix, "gone.mkv" + reclaim.PartSuffix} {
		if _, err := os.Stat(filepath.Join(dl, name)); err != nil {
			t.Errorf("%s was deleted by a pass that only reports: %v", name, err)
		}
	}
	// The live task's own part file is measured, or the row would say 0 of 1000
	// bytes with four already on the disk.
	if got := taskOf(t, a, "live"); got.Loaded != 4 {
		t.Errorf("loaded = %d, want the 4 bytes in the part file", got.Loaded)
	}
}

// A torrent client lays out the whole file set at full length before it fetches
// a piece, so a folder of right-sized files is also what a torrent with nothing
// downloaded looks like. Only the library's own piece pass can answer for one.
func TestATorrentIsNeverSettledFromWhatIsInItsFolder(t *testing.T) {
	dl := t.TempDir()
	size := int64(len(realBody))
	f := newBootFixture(t,
		func(s *settings.Settings) { s.ReclaimTrust = settings.ReclaimTrustSize },
		core.Task{ID: "swarm", URL: "magnet:?xt=urn:btih:1111111111111111111111111111111111111111",
			Name: "movie.mkv", Dir: dl, Status: core.StatusQueued, Size: size,
			Resolver: "torrent", Enabled: true},
	)
	writeBody(t, dl, "movie.mkv", realBody)

	a := f.boot(t)
	rep, err := a.Reclaim()
	if err != nil {
		t.Fatal(err)
	}
	if fd := findingFor(t, rep, "swarm"); fd.Verdict != reclaim.Recheck {
		t.Fatalf("verdict = %q, want recheck (%s)", fd.Verdict, fd.Detail)
	}
	if got := taskOf(t, a, "swarm"); got.Status != core.StatusQueued {
		t.Errorf("status = %q, want it left queued for the library to check its pieces", got.Status)
	}
}

// Settling a task in memory alone would be undone by the next restart, and the
// record of what this instance holds would miss a download shown as finished.
func TestAReclaimedDownloadReachesTheStoreAndTheHistory(t *testing.T) {
	dl := t.TempDir()
	size := int64(len(realBody))
	f := newBootFixture(t, nil,
		core.Task{ID: "already-here", URL: "https://host.example/movie.mkv", Name: "movie.mkv",
			Dir: dl, Status: core.StatusQueued, Size: size,
			ExpectedHash: sha256Of(realBody), Enabled: true},
	)
	writeBody(t, dl, "movie.mkv", realBody)

	a := f.boot(t)
	if _, err := a.Reclaim(); err != nil {
		t.Fatal(err)
	}
	if got := taskOf(t, a, "already-here"); got.Checksum != "ok" {
		t.Errorf("checksum = %q, want ok: this one was actually verified", got.Checksum)
	}

	hist, err := a.History(0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range hist {
		if e.TaskID == "already-here" {
			found = true
			if e.FinishedAt.IsZero() {
				t.Error("the reclaimed download reached the history with no finish time")
			}
		}
	}
	if !found {
		t.Fatal("the reclaimed download is not in the history, so nothing records that this instance holds it")
	}

	// And it comes back finished, which a write only to the task map would lose.
	again := f.boot(t)
	if got := taskOf(t, again, "already-here"); got.Status != core.StatusDone {
		t.Errorf("after a restart status = %q, want done", got.Status)
	}
}
