package engine

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// sintelMagnet is the Blender Foundation's Sintel: public domain and heavily
// seeded. A run that finds no swarm here cannot reach one at all.
const sintelMagnet = "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Ftracker.openbittorrent.com%3A6969%2Fannounce&tr=udp%3A%2F%2Fexplodie.org%3A6969&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce"

// unsharedMagnet is a valid magnet for a torrent that does not exist. An
// all-zero hash would be refused before any wait (see torrent.checkMagnet).
const unsharedMagnet = "magnet:?xt=urn:btih:1111111111111111111111111111111111111111"

// taskSink is a core.Task behind a lock, since updates arrive on the engine's
// goroutines while the test reads on its own.
type taskSink struct {
	mu sync.Mutex
	t  core.Task
}

func (s *taskSink) apply(_ string, u core.Update) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.Name != "" {
		s.t.Name = u.Name
	}
	if u.Size > 0 {
		s.t.Size = u.Size
	}
	if u.Status != "" {
		s.t.Status = u.Status
	}
	if u.Loaded > 0 {
		s.t.Loaded = u.Loaded
	}
	if u.Err != "" {
		s.t.Error = u.Err
	}
	if u.Torrent != nil {
		u.Torrent.ApplyTo(&s.t)
	}
}

func (s *taskSink) snapshot() core.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.t
}

// TestARealMagnetPutsRealSwarmNumbersOnTheTask runs a real magnet through the
// real engine and checks that swarm numbers reach the task by the ordinary
// update path.
func TestARealMagnetPutsRealSwarmNumbersOnTheTask(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this joins a real BitTorrent swarm")
	}
	if raceEnabled {
		// gopeed v1.9.3's bt.Fetcher updates its upload counter from two
		// goroutines without a lock (doUpload against UploadedBytes), which
		// only real swarm traffic reaches. The fix belongs in gopeed; the
		// non-race test run still covers this.
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity; see comment")
	}
	// Not t.TempDir: the torrent client can still hold a .part file when the
	// test returns, which fails TempDir's cleanup.
	dir, err := os.MkdirTemp("", "kl-bt-live-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sink := &taskSink{}
	e, err := New(dir, sink.apply)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()
	e.SetMetadataTimeout(60 * time.Second)

	const id = "live-1"
	e.DownloadTorrent(id, sintelMagnet, dir, nil)

	deadline := time.Now().Add(90 * time.Second)
	var got core.Task
	paused := false
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		got = sink.snapshot()
		if got.Status == core.StatusError {
			t.Skipf("could not reach a swarm from this machine: %s", got.Error)
		}
		if !paused && got.Peers > 0 {
			// The peer count is the proof; no need to fetch the film.
			e.Pause(id)
			paused = true
		}
		if got.Peers > 0 && got.Name != "" {
			break
		}
	}
	defer e.Remove(id, true)

	if got.Name != "Sintel" {
		t.Fatalf("Name = %q, want the torrent's own name from the swarm's metadata", got.Name)
	}
	if got.Size <= 0 {
		t.Fatalf("Size = %d, want the torrent's real total", got.Size)
	}
	if got.Peers <= 0 {
		t.Fatalf("Peers = %d after 90s; the swarm numbers never reached the task", got.Peers)
	}
	// Seeding must be false while the download is incomplete, since
	// Task.Uploading is true from creation. On a fast runner the whole film
	// can arrive before the pause, and a finished torrent seeding is correct.
	done := got.Size > 0 && got.Loaded >= got.Size
	if !done && got.Seeding {
		t.Fatalf("a torrent that is still downloading reported itself as seeding (%d of %d bytes)", got.Loaded, got.Size)
	}
	switch {
	case done && got.Status != core.StatusDone:
		t.Fatalf("Status = %q with every byte in, want %q", got.Status, core.StatusDone)
	case !done && got.Status != core.StatusRunning && got.Status != core.StatusPaused:
		t.Fatalf("Status = %q, want the task to be running or paused", got.Status)
	}
	t.Logf("live swarm: peers=%d seeds=%d uploaded=%d ratio=%.4f size=%d",
		got.Peers, got.Seeds, got.Uploaded, got.Ratio, got.Size)
}

// TestAFinishedTorrentIsDoneWithASeedingFlagBesideIt checks against a real
// swarm that a finished torrent is StatusDone with Seeding set, not a status
// of its own. It fetches one subtitle file; the index is stable because the
// file list is part of the info hash.
func TestAFinishedTorrentIsDoneWithASeedingFlagBesideIt(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this joins a real BitTorrent swarm")
	}
	if raceEnabled {
		// Waiting for Seeding means waiting for the upload activity that
		// triggers gopeed's race; see TestARealMagnetPutsRealSwarmNumbersOnTheTask.
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	dir, err := os.MkdirTemp("", "kl-bt-seed-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sink := &taskSink{}
	e, err := New(dir, sink.apply)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()
	e.SetMetadataTimeout(60 * time.Second)

	const id = "seeding-1"
	const enSubtitle = 1 // Sintel.en.srt, 1514 bytes
	e.DownloadTorrent(id, sintelMagnet, dir, []int{enSubtitle})
	defer e.Remove(id, true)

	deadline := time.Now().Add(150 * time.Second)
	var got core.Task
	next := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		got = sink.snapshot()
		if got.Status == core.StatusError {
			t.Skipf("could not reach a swarm from this machine: %s", got.Error)
		}
		if time.Now().After(next) {
			next = time.Now().Add(10 * time.Second)
			t.Logf("status=%s loaded=%d size=%d peers=%d seeds=%d seeding=%v", got.Status, got.Loaded, got.Size, got.Peers, got.Seeds, got.Seeding)
		}
		if got.Status == core.StatusDone && got.Seeding {
			break
		}
	}
	if got.Status != core.StatusDone {
		t.Fatalf("Status = %q after 150s, want done", got.Status)
	}
	if !got.Seeding {
		t.Fatal("a finished torrent is not seeding; the flag never reached the task")
	}
	if got.Size != 1514 {
		t.Fatalf("Size = %d, want the 1514 bytes actually asked for", got.Size)
	}
	for _, s := range []core.Status{core.StatusCollected, core.StatusQueued, core.StatusRunning, core.StatusPaused, core.StatusExtracting, core.StatusDone, core.StatusError} {
		if got.Status == s {
			return
		}
	}
	t.Fatalf("Status = %q, which is not one of the seven", got.Status)
}

// TestAMagnetNobodyIsSharingFailsWithAReason: the metadata deadline is the
// only thing that ends the wait for an unshared magnet.
func TestAMagnetNobodyIsSharingFailsWithAReason(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	dir, err := os.MkdirTemp("", "kl-bt-dead-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sink := &taskSink{}
	e, err := New(dir, sink.apply)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()
	e.SetMetadataTimeout(2 * time.Second)

	e.DownloadTorrent("dead-1", unsharedMagnet, dir, nil)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if got := sink.snapshot(); got.Status == core.StatusError {
			if strings.TrimSpace(got.Error) == "" {
				t.Fatal("the task failed with no sentence to show")
			}
			return
		}
	}
	t.Fatal("a magnet nobody is sharing never settled; it would sit in the list forever")
}

func TestCloseWaitsForTheTorrentGoroutinesItStarted(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	dir, err := os.MkdirTemp("", "kl-bt-close-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	e, err := New(dir, func(string, core.Update) {})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.SetMetadataTimeout(time.Hour)
	e.DownloadTorrent("closing-1", unsharedMagnet, dir, nil)
	time.Sleep(500 * time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- e.Close() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close hung; the resolve wait does not observe shutdown")
	}
	// A second Close must not panic on an already-closed channel.
	_ = e.Close()
}
