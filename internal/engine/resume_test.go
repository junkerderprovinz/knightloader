package engine

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// pausedBeforeRestart downloads part of a file from a fresh slowOrigin, pauses
// it and shuts the engine down. It returns the job, the file it was writing,
// how many requests the origin had answered by then, and the folders a new
// engine boots from. A picky origin hangs up on the library's browser agent
// from the first request on.
func pausedBeforeRestart(t *testing.T, picky bool) (o *slowOrigin, j Job, file string, asked int, dir, state string) {
	t.Helper()
	if raceEnabled {
		// See startSlow.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
	}
	o = newSlowOrigin(t, 16<<20)
	o.picky.Store(picky)
	dir, state = t.TempDir(), t.TempDir()
	j = Job{TaskID: "t1", URL: o.srv.URL + "/big.bin", Conns: 4}
	u := &updates{}
	e, err := Open(dir, state, u.add)
	if err != nil {
		t.Fatal(err)
	}
	e.Start(j)
	waitUntil(t, "the first megabyte", func() bool { return u.loaded() >= 1<<20 })
	e.Pause(j.TaskID)
	waitUntil(t, "the pause", func() bool { return u.last().Status == core.StatusPaused })
	file = u.last().File
	if u.loaded() >= int64(len(o.data)) {
		t.Fatal("the download finished before it was paused")
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return o, j, file, o.requests(), dir, state
}

func restarted(t *testing.T, dir, state string) (*Engine, *updates) {
	t.Helper()
	u := &updates{}
	e, err := Open(dir, state, u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e, u
}

// A download paused before a restart carries on with the bytes it has: the
// engine booted on the same folders asks the server only for the rest.
func TestAPausedDownloadCarriesOnAfterARestart(t *testing.T) {
	t.Parallel()
	o, j, file, asked, dir, state := pausedBeforeRestart(t, false)
	e, u := restarted(t, dir, state)
	if !e.Resumes(j.TaskID, file) {
		t.Fatal("the restarted engine does not carry on with the paused download's file")
	}
	e.Start(j)
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })

	o.mu.Lock()
	later := slices.Clone(o.ranges[asked:])
	o.mu.Unlock()
	if whole := fromTheStart(later); len(whole) > 0 {
		t.Errorf("after the restart the file was asked for from the start (Range %q)", whole)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
}

// A download still running when the engine shuts down carries on after the
// restart as a paused one does, and the app is not told it was paused, so it
// comes back as running.
func TestADownloadRunningAtShutdownCarriesOnAfterARestart(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer; see startSlow")
	}
	o := newSlowOrigin(t, 16<<20)
	dir, state := t.TempDir(), t.TempDir()
	j := Job{TaskID: "t1", URL: o.srv.URL + "/big.bin", Conns: 4}
	u := &updates{}
	e, err := Open(dir, state, u.add)
	if err != nil {
		t.Fatal(err)
	}
	e.Start(j)
	waitUntil(t, "the first megabyte", func() bool { return u.loaded() >= 1<<20 })
	file := u.last().File
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if u.loaded() >= int64(len(o.data)) {
		t.Fatal("the download finished before the shutdown")
	}
	if got := u.last().Status; got == core.StatusPaused {
		t.Error("the shutdown reported the download as paused")
	}
	asked := o.requests()

	e, u = restarted(t, dir, state)
	if !e.Resumes(j.TaskID, file) {
		t.Fatal("the restarted engine does not carry on with the download that was running")
	}
	e.Start(j)
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })
	o.mu.Lock()
	later := slices.Clone(o.ranges[asked:])
	o.mu.Unlock()
	if whole := fromTheStart(later); len(whole) > 0 {
		t.Errorf("after the restart the file was asked for from the start (Range %q)", whole)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
}

// A server that sends only the whole file after the restart is asked
// for all of it, and the file that comes out is that file rather than the new
// bytes written over the old ones at the wrong place.
func TestADownloadStartsOverCleanlyWhenTheServerStoppedSendingParts(t *testing.T) {
	t.Parallel()
	o, j, file, _, dir, state := pausedBeforeRestart(t, false)
	o.whole.Store(true)
	e, u := restarted(t, dir, state)
	e.Start(j)
	waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })

	got, err := os.ReadFile(u.last().File)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
	if u.last().File != file {
		t.Errorf("the download landed in %s rather than in place of its old file %s", u.last().File, file)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 1 {
		t.Errorf("the folder holds %v; the old file should have made way", left)
	}
}

// Removing a download after the restart, before it ever started again, takes
// its file and its record with it.
func TestRemovingAPausedDownloadAfterARestartDeletesItsFile(t *testing.T) {
	t.Parallel()
	_, j, file, _, dir, state := pausedBeforeRestart(t, false)
	e, _ := restarted(t, dir, state)
	e.Remove(j.TaskID, true)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the file of the removed download is still there (%v)", err)
	}
	if left, _ := os.ReadDir(state); len(left) != 0 {
		t.Errorf("the state folder still holds %d entries", len(left))
	}
	e.Close()
	again, _ := restarted(t, dir, state)
	if again.Resumes(j.TaskID, file) {
		t.Error("the removed download came back after the next restart")
	}
}

// fromTheStart reports the requests in ranges that asked for the file from its
// first byte, other than the one-byte probe.
func fromTheStart(ranges []string) []string {
	var whole []string
	for _, rg := range ranges {
		if rg == "" || strings.HasPrefix(rg, "bytes=0-") && rg != "bytes=0-0" {
			whole = append(whole, rg)
		}
	}
	return whole
}

// A server that hangs up on the library's browser agent is asked as
// KnightLoader again after the restart, as the first start settled on, both by
// the probe and by the library's own requests for the rest.
func TestAServerThatHangsUpOnTheBrowserAgentSendsTheRestAfterARestart(t *testing.T) {
	t.Parallel()
	o, j, file, asked, dir, state := pausedBeforeRestart(t, true)
	e, u := restarted(t, dir, state)
	e.Start(j)
	waitUntil(t, "the download settling", func() bool {
		s := u.last().Status
		return s == core.StatusDone || s == core.StatusError
	})
	if last := u.last(); last.Status != core.StatusDone {
		t.Fatalf("settled as %s (%s), want done", last.Status, last.Err)
	}

	o.mu.Lock()
	ranges := slices.Clone(o.ranges[asked:])
	agents := slices.Clone(o.agents[asked:])
	o.mu.Unlock()
	if whole := fromTheStart(ranges); len(whole) > 0 {
		t.Errorf("after the restart the file was asked for from the start (Range %q)", whole)
	}
	for i, a := range agents[1:] {
		if !strings.HasPrefix(a, "KnightLoader/") {
			t.Errorf("request %d after the restart went out as %q, which the server hangs up on", i+2, a)
		}
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, o.data) {
		t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
	}
}

// A probe that meets a passing failure learns nothing about the file, so the
// bytes and the record stay, and the next start carries on with them.
func TestAProbeThatFailsForNowKeepsTheBytesForTheNextStart(t *testing.T) {
	t.Parallel()
	for name, fail := range map[string]func(o *slowOrigin, j *Job){
		"HTTP 502": func(o *slowOrigin, _ *Job) { o.status.Store(http.StatusBadGateway) },
		"a refused connection": func(_ *slowOrigin, j *Job) {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			j.URL = "http://" + l.Addr().String() + "/big.bin"
			_ = l.Close()
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			o, j, file, _, dir, state := pausedBeforeRestart(t, false)
			e, u := restarted(t, dir, state)
			failing := j
			fail(o, &failing)
			e.Start(failing)
			waitUntil(t, "the start failing", func() bool { return u.last().Status == core.StatusError })

			if fi, err := os.Stat(file); err != nil || fi.Size() != int64(len(o.data)) {
				t.Fatalf("the paused download's file is gone (%v)", err)
			}
			if kept, _ := filepath.Glob(filepath.Join(state, "*.json")); len(kept) != 1 {
				t.Fatalf("the state folder holds %d records, want the paused download's", len(kept))
			}
			if !e.Resumes(j.TaskID, file) {
				t.Fatal("the download no longer carries on with its file")
			}

			o.status.Store(0)
			asked := o.requests()
			e.Start(j)
			waitUntil(t, "the download finishing", func() bool { return u.last().Status == core.StatusDone })
			o.mu.Lock()
			ranges := slices.Clone(o.ranges[asked:])
			o.mu.Unlock()
			if whole := fromTheStart(ranges); len(whole) > 0 {
				t.Errorf("the next start asked for the file from the start (Range %q)", whole)
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, o.data) {
				t.Errorf("the finished file (%d bytes) is not what the origin served (%d bytes)", len(got), len(o.data))
			}
		})
	}
}

// The library saves a paused task on a goroutine of its own, which can land
// after the task was removed. That save writes no record, or the removed
// download would come back as a paused task on every boot.
func TestAPauseSavedAfterItsDownloadWasRemovedLeavesNoRecord(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	s := &layoutStore{Storage: download.NewMemStorage(), want: map[string][]byte{}, files: map[string][]int64{}, kept: newKeptTransfers(state)}
	if _, err := s.kept.load(s.Storage); err != nil {
		t.Fatal(err)
	}
	var paused download.Task
	if err := json.Unmarshal([]byte(`{"id":"g1","protocol":"http","status":"pause","meta":{
		"req":{"url":"http://origin/big.bin","labels":{"knightloader.task":"t1"}},
		"res":{"size":16,"range":true,"files":[{"name":"big.bin","size":16}]},
		"opts":{"path":"/downloads"}}}`), &paused); err != nil {
		t.Fatal(err)
	}
	saveTask := func() {
		_ = s.Put(savedLayoutBucket, "g1", map[string]any{"connections": 4})
		_ = s.Put(savedTaskBucket, "g1", &paused)
	}
	records := func() []string {
		kept, _ := filepath.Glob(filepath.Join(state, "*.json"))
		return kept
	}

	saveTask()
	if len(records()) != 1 {
		t.Fatal("the pause wrote no record")
	}
	_ = s.Delete(savedTaskBucket, "g1")
	_ = s.Delete(savedLayoutBucket, "g1")
	if kept := records(); len(kept) != 0 {
		t.Fatalf("the remove left %v", kept)
	}
	saveTask()
	if kept := records(); len(kept) != 0 {
		t.Errorf("the late save of the removed download wrote %v", kept)
	}
}

// A record whose task is no longer in the list is let go at boot, with the
// file left where it is.
func TestATransferOfATaskNoLongerListedIsDroppedAtBoot(t *testing.T) {
	t.Parallel()
	_, j, file, _, dir, state := pausedBeforeRestart(t, false)
	e, _ := restarted(t, dir, state)
	e.PruneRestored(func(string) bool { return false })
	if e.Resumes(j.TaskID, file) || e.Holds(j.TaskID) {
		t.Error("the engine still holds the transfer of a task that is gone")
	}
	if left, _ := os.ReadDir(state); len(left) != 0 {
		t.Errorf("the state folder still holds %d entries", len(left))
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("the file went with the transfer (%v)", err)
	}
}
