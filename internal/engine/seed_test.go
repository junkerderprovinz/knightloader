package engine

import (
	cryptorand "crypto/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// updateLog keeps every update the engine sends, for a test that has to know
// what was never said.
type updateLog struct {
	mu  sync.Mutex
	all []core.Update
}

func (l *updateLog) add(_ string, u core.Update) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, u)
}

func (l *updateLog) snapshot() []core.Update {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]core.Update(nil), l.all...)
}

// finishedTorrent writes a two-file torrent's files into dir and returns the
// .torrent as the URI the app carries it as, and where it landed.
func finishedTorrent(t *testing.T, dir string) (uri, root string) {
	t.Helper()
	root = filepath.Join(dir, "Pack")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.bin", "b.bin"} {
		data := make([]byte, 80<<10)
		_, _ = cryptorand.Read(data)
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(root); err != nil {
		t.Fatal(err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bencode.Marshal(metainfo.MetaInfo{InfoBytes: ib})
	if err != nil {
		t.Fatal(err)
	}
	return torrent.EncodeBytes(b), root
}

// A finished torrent taken up again where its files moved to finds them
// complete and seeds, and says nothing but that: no start, no progress, no
// finish, which are a download's. Its upload goes on from what it had.
func TestATorrentStartedOnlyToSeedReportsNothingButTheSeeding(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	dir := t.TempDir()
	uri, root := finishedTorrent(t, dir)
	log := &updateLog{}
	e, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	e.Start(Job{TaskID: "s", URL: uri, Dir: dir, TorrentRoot: root, TorrentName: "Pack",
		Seed: true, SeedFrom: core.TorrentStats{Uploaded: 5000, Ratio: 0.5}})
	defer e.Remove("s", false)

	var seeding *core.TorrentStats
	deadline := time.Now().Add(60 * time.Second)
	for seeding == nil && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		for _, u := range log.snapshot() {
			if u.Torrent != nil && u.Torrent.Seeding {
				seeding = u.Torrent
			}
		}
	}
	if seeding == nil {
		t.Fatalf("the torrent never went back to seeding; updates: %+v", log.snapshot())
	}
	if seeding.Uploaded < 5000 || seeding.Ratio < 0.5 {
		t.Errorf("seeding reads uploaded %d, ratio %.2f; want it to go on from 5000 and 0.5", seeding.Uploaded, seeding.Ratio)
	}
	for _, u := range log.snapshot() {
		if u.Status != "" {
			t.Errorf("a start only to seed reported the status %q (%+v)", u.Status, u)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Pack.1")); err == nil {
		t.Error("the torrent took a place of its own beside its files instead of taking them up")
	}
}

// A start only to seed that fails ends the seeding and leaves the task done:
// it reports no error, and keeps what had been uploaded.
func TestASeedStartThatFailsEndsTheSeedingQuietly(t *testing.T) {
	log := &updateLog{}
	e, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	e.Start(Job{TaskID: "s", URL: "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a", Dir: t.TempDir(),
		FileRules: torrent.FileRules{Include: []string{"("}},
		Seed:      true, SeedFrom: core.TorrentStats{Uploaded: 5000, Ratio: 0.5, Seeding: true}})

	deadline := time.Now().Add(5 * time.Second)
	for len(log.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := log.snapshot()
	if len(got) != 1 {
		t.Fatalf("updates = %+v, want the one that ends the seeding", got)
	}
	u := got[0]
	if u.Status != "" || u.Err != "" {
		t.Errorf("the failed seed start reported status %q, error %q; the download itself is still done", u.Status, u.Err)
	}
	if u.Torrent == nil || u.Torrent.Seeding || u.Torrent.Uploaded != 5000 || u.Torrent.Ratio != 0.5 {
		t.Errorf("the failed seed start reported %+v, want seeding ended with the upload kept", u.Torrent)
	}
}
