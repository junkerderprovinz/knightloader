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

// The body every test in this file uses, and the hash of a DIFFERENT body of
// exactly the same length. Same length is the point: on this build a
// half-finished engine download is already at its full final size, so length
// cannot tell the two apart and only the hash can.
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

// TestAFileThisInstanceAlreadyFetchedIsNotFetchedAgain is the whole feature in
// one test, and it is the situation it was built for: the list was emptied,
// the links went back in, and the finished file is still sitting in the
// download folder. Nothing on this side ever watched it arrive, so before this
// pass existed the box spent the whole file again over somebody's line.
//
// The witness is this instance's own history, which is the one record that
// survives the list being cleared - that is the entire reason the table
// exists.
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
	// The emptied list: the row goes, the history keeps the record, the file is
	// never touched. That is what RemoveTasks with deleteFiles false promises.
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
	// Out of the wait queue, or the dispatcher would hand a finished task to a
	// backend on its next pass: its loop reads the flags on a queued task and
	// never its status.
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
	// The file is what this is all about.
	if b, err := os.ReadFile(filepath.Join(dl, "movie.mkv")); err != nil || string(b) != realBody {
		t.Errorf("the file was changed by a pass that only reads: %q, %v", b, err)
	}
}

// TestARightSizedFileWithTheWrongBytesIsStillDownloaded is the rule that keeps
// the whole pass honest, at the level where it costs something: a checksum
// exists, it disagrees, and the answer is to fetch the file again. Better one
// transfer nobody needed than a row that reads "done" over a file that will
// not open.
//
// The task is left exactly where it was and the stale file is left exactly
// where it is: the collision policy already owns what happens to a file in the
// way, and it owns it at the moment the transfer starts rather than minutes
// earlier from over here.
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
		t.Errorf("checksum = %q, want it empty: that column says what verifying THIS task's own download found, and it has not downloaded anything", got.Checksum)
	}
	if rep.Settled != 0 {
		t.Errorf("report says %d settled, want 0", rep.Settled)
	}
	// Neither the file nor the queue was touched. A pass that reads the disk
	// must not delete what it disagrees with, and must not start downloads
	// because somebody pressed "look at the disk".
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

// TestTheStrictTierNeedsAChecksumAndNotARecord proves the setting reaches the
// decision. An instance switched to the strict tier has said that only a
// verified hash may stop a download happening, and a record of having fetched
// the same name and length before is exactly what it has declined to accept.
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

// TestTheCollectorIsReportedAndNotDecidedFor. The collector is where a person
// decides what to download. Telling them eleven of these forty links are
// already in their folder is useful; walking one of them out of the collector
// as "finished" is the app confirming a batch on their behalf.
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

// TestAnOrphanPartFileIsReportedAndKept covers the half-finished .klpart with
// no task behind it. It is thirty gigabytes somebody either wants back or
// wants gone and nothing here can tell which, so it is counted, named and left
// alone. The part file of a task that IS in the list is not an orphan, which
// is the half a naive sweep of the folder would get wrong.
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
	// And the live task's own part file is measured rather than ignored: a row
	// that says 0 of 1000 bytes when four are on the disk is a row that lies
	// about how much a restart would cost.
	if got := taskOf(t, a, "live"); got.Loaded != 4 {
		t.Errorf("loaded = %d, want the 4 bytes in the part file", got.Loaded)
	}
}

// TestATorrentIsNeverSettledFromWhatIsInItsFolder. A torrent client lays out
// the whole file set at full length before it fetches a piece, so a folder
// full of right-sized files is what a torrent that has downloaded NOTHING
// looks like. The only thing that can answer for one is the download library's
// own piece pass, which is what starting the torrent against its folder
// already runs.
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

// TestAReclaimedDownloadReachesTheStoreAndTheHistory. Settling a task in
// memory alone would be undone by the next restart, and the record of what
// this instance holds would be missing a download it is showing as finished.
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

	// And it comes back finished, which is the half that a write only to the
	// task map would lose.
	again := f.boot(t)
	if got := taskOf(t, again, "already-here"); got.Status != core.StatusDone {
		t.Errorf("after a restart status = %q, want done", got.Status)
	}
}
