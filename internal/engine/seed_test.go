package engine

import (
	"bytes"
	cryptorand "crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	anacrolix "github.com/anacrolix/torrent"
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
	return finishedTorrentOf(t, dir, 80<<10)
}

// finishedTorrentOf is finishedTorrent with files of size bytes each.
func finishedTorrentOf(t *testing.T, dir string, size int) (uri, root string) {
	t.Helper()
	root = filepath.Join(dir, "Pack")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.bin", "b.bin"} {
		data := make([]byte, size)
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

// A torrent taken up to seed after a restart counts its targets over all of
// its seeding: one whose earlier runs already reached the ratio stops as soon
// as its files are found, with the stop marked as the target's, while one
// short of it seeds on. The stopped one's files stay.
func TestATorrentTakenUpToSeedStopsAtTheTargetsOfItsWholeSeeding(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	doneDir, shortDir := t.TempDir(), t.TempDir()
	doneURI, doneRoot := finishedTorrent(t, doneDir)
	shortURI, shortRoot := finishedTorrent(t, shortDir)
	sinks := map[string]*taskSink{"done": {}, "short": {}}
	e, err := New(t.TempDir(), func(id string, u core.Update) { sinks[id].apply(id, u) })
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.SetTorrentConfig(TorrentConfig{SeedRatio: 1}); err != nil {
		t.Fatal(err)
	}

	e.Start(Job{TaskID: "done", URL: doneURI, Dir: doneDir, TorrentRoot: doneRoot, TorrentName: "Pack",
		Seed: true, SeedFrom: core.TorrentStats{Uploaded: 200 << 10, Ratio: 1.25, SeedSeconds: 600}})
	defer e.Remove("done", false)
	e.Start(Job{TaskID: "short", URL: shortURI, Dir: shortDir, TorrentRoot: shortRoot, TorrentName: "Pack",
		Seed: true, SeedFrom: core.TorrentStats{Uploaded: 16 << 10, Ratio: 0.1}})
	defer e.Remove("short", false)

	var ended core.TorrentStats
	waitFor(t, "the torrent past its ratio to stop", 60*time.Second, func() bool {
		ended = sinks["done"].stats()
		return sinks["done"].seen() && !ended.Seeding
	})
	if !ended.AtTarget || ended.Uploaded != 200<<10 || ended.Ratio < 1.25 || ended.SeedSeconds < 600 {
		t.Errorf("the stop reads %+v, want it at the target with the earlier figures kept", ended)
	}
	e.mu.Lock()
	gid := e.toGopeed["done"]
	e.mu.Unlock()
	if e.d.GetTask(gid) != nil {
		t.Error("the torrent past its ratio is still in the library, so it still uploads")
	}
	if _, err := os.Stat(filepath.Join(doneRoot, "a.bin")); err != nil {
		t.Errorf("stopping the seeding took the files with it: %v", err)
	}

	waitFor(t, "the torrent short of its ratio to seed", 60*time.Second, func() bool {
		return sinks["short"].stats().Seeding
	})
	time.Sleep(2 * torrentStatsInterval)
	if s := sinks["short"].stats(); !s.Seeding || s.AtTarget {
		t.Errorf("the torrent short of its ratio reads %+v, want it seeding on", s)
	}
}

// A torrent taken up to seed after a restart uploads to a peer that wants it,
// and what it reports counts on from the figures it had before.
func TestATorrentTakenUpToSeedUploadsOnTopOfWhatItHad(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	dir := t.TempDir()
	uri, root := finishedTorrent(t, dir)
	sink := &taskSink{}
	e, err := New(t.TempDir(), sink.apply)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	port := freePort(t)
	if err := e.SetTorrentConfig(TorrentConfig{Port: port}); err != nil {
		t.Fatal(err)
	}
	from := core.TorrentStats{Uploaded: 5000, Ratio: 0.5, SeedSeconds: 3600}
	e.Start(Job{TaskID: "s", URL: uri, Dir: dir, TorrentRoot: root, TorrentName: "Pack", Seed: true, SeedFrom: from})
	defer e.Remove("s", false)
	waitFor(t, "the torrent to seed", 60*time.Second, func() bool { return sink.stats().Seeding })

	raw, err := torrent.DecodeBytes(uri)
	if err != nil {
		t.Fatal(err)
	}
	size := leech(t, raw, port)

	waitFor(t, "the upload to show on top of the earlier one", 60*time.Second, func() bool {
		return sink.stats().Uploaded >= from.Uploaded+size
	})
	s := sink.stats()
	if !s.Seeding || s.Ratio < from.Ratio+0.9 || s.SeedSeconds < from.SeedSeconds {
		t.Errorf("after a peer took the whole torrent the seeding reads %+v, want it seeding on from %+v", s, from)
	}
}

// stats is the swarm reading the sink last applied.
func (s *taskSink) stats() core.TorrentStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return core.TorrentStats{Peers: s.t.Peers, Seeds: s.t.Seeds, Ratio: s.t.Ratio, Uploaded: s.t.Uploaded,
		SeedSeconds: s.t.SeedSeconds, Seeding: s.t.Seeding, AtTarget: s.atTarget}
}

// freePort is a port nothing listens on at the moment, over TCP or UDP, since
// the torrent client binds both. Windows reserves UDP ranges that overlap the
// TCP ports it hands out, so a TCP port alone can fail the client's start.
func freePort(t *testing.T) int {
	t.Helper()
	for range 20 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		u, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
		if err != nil {
			continue
		}
		u.Close()
		return port
	}
	t.Fatal("no port free over both TCP and UDP")
	return 0
}

// leech downloads the torrent raw describes from the one peer listening on
// port, with a client that uploads nothing, and returns its size once it has
// all of it.
func leech(t *testing.T, raw []byte, port int) int64 {
	t.Helper()
	mi, err := metainfo.Load(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	cfg := anacrolix.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.NoUpload = true
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.ListenHost = func(string) string { return "127.0.0.1" }
	cfg.ListenPort = 0
	cl, err := anacrolix.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	tor, err := cl.AddTorrent(mi)
	if err != nil {
		t.Fatal(err)
	}
	tor.AddPeers([]anacrolix.PeerInfo{{Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}}})
	<-tor.GotInfo()
	tor.DownloadAll()
	waitFor(t, "the peer to have the whole torrent", 60*time.Second, func() bool {
		return tor.BytesCompleted() == tor.Length()
	})
	return tor.Length()
}

// A torrent started to seed by hand counts its targets from its mark, so the
// ratio its earlier runs reached does not stop it.
func TestATorrentStartedToSeedByHandCountsItsTargetsFromItsMark(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	dir := t.TempDir()
	uri, root := finishedTorrent(t, dir)
	sink := &taskSink{}
	e, err := New(t.TempDir(), sink.apply)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.SetTorrentConfig(TorrentConfig{SeedRatio: 1}); err != nil {
		t.Fatal(err)
	}
	from := core.TorrentStats{Uploaded: 200 << 10, Ratio: 1.25, SeedSeconds: 600}
	e.Start(Job{TaskID: "s", URL: uri, Dir: dir, TorrentRoot: root, TorrentName: "Pack",
		Seed: true, SeedFrom: from, SeedMark: core.SeedMark{Ratio: from.Ratio, SeedSeconds: from.SeedSeconds}})
	defer e.Remove("s", false)

	waitFor(t, "the torrent to seed", 60*time.Second, func() bool { return sink.stats().Seeding })
	time.Sleep(2 * torrentStatsInterval)
	if s := sink.stats(); !s.Seeding || s.AtTarget {
		t.Errorf("the torrent started by hand reads %+v, want it seeding past the ratio it had met", s)
	}
}

// An upload limit saved while a torrent seeds holds a peer that takes the
// whole torrent to that pace, without the torrent being started again.
func TestAnUploadLimitSlowsATorrentThatIsAlreadySeeding(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race under real upload activity; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	dir := t.TempDir()
	uri, root := finishedTorrentOf(t, dir, 1<<20)
	sink := &taskSink{}
	e, err := New(t.TempDir(), sink.apply)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	port := freePort(t)
	if err := e.SetTorrentConfig(TorrentConfig{Port: port}); err != nil {
		t.Fatal(err)
	}
	e.Start(Job{TaskID: "s", URL: uri, Dir: dir, TorrentRoot: root, TorrentName: "Pack", Seed: true})
	defer e.Remove("s", false)
	waitFor(t, "the torrent to seed", 60*time.Second, func() bool { return sink.stats().Seeding })

	const limit = 256 << 10
	if err := e.SetTorrentConfig(TorrentConfig{Port: port, UploadLimit: limit}); err != nil {
		t.Fatal(err)
	}
	// The limit is the library's for the whole process.
	defer e.SetTorrentConfig(TorrentConfig{Port: port})

	raw, err := torrent.DecodeBytes(uri)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	size := leech(t, raw, port)
	took := time.Since(start)

	// The limiter lets its burst of 1 MiB through at once and paces the rest.
	paced := time.Duration(float64(size-1<<20) / limit * float64(time.Second))
	t.Logf("%d bytes in %s at a limit of %d KiB/s, %.0f KiB/s on average",
		size, took.Round(time.Millisecond), limit>>10, float64(size)/took.Seconds()/1024)
	if took < paced*9/10 {
		t.Errorf("the peer took %d bytes in %s; at %d KiB/s the part after the burst alone takes %s",
			size, took, limit>>10, paced)
	}
	if took > 3*paced {
		t.Errorf("the peer took %s, far longer than the %s the limit asks for", took, paced)
	}
}

// notSeeded waits for the update that ends a seed run with its reason.
func notSeeded(t *testing.T, log *updateLog) core.Update {
	t.Helper()
	var got core.Update
	waitFor(t, "the seed run to end with its reason", 60*time.Second, func() bool {
		for _, u := range log.snapshot() {
			if u.Torrent != nil && u.Torrent.NotSeeded != "" {
				got = u
				return true
			}
		}
		return false
	})
	return got
}

// A start only to seed fetches nothing: a file that is missing or short ends
// it before the library is handed the torrent, with the reason, and leaves the
// files as they were.
func TestASeedStartWithAFileMissingOrShortSaysWhyAndFetchesNothing(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	for _, c := range []struct {
		name  string
		file  string
		spoil func(p string) error
		want  string
	}{
		{"missing", "b.bin", os.Remove, "b.bin is missing"},
		{"short", "a.bin", func(p string) error { return os.Truncate(p, 1000) }, "a.bin has 1000 bytes"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			uri, root := finishedTorrent(t, dir)
			spoilt := filepath.Join(root, c.file)
			if err := c.spoil(spoilt); err != nil {
				t.Fatal(err)
			}
			log := &updateLog{}
			e, err := New(t.TempDir(), log.add)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()

			e.Start(Job{TaskID: "s", URL: uri, Dir: dir, TorrentRoot: root, TorrentName: "Pack",
				Seed: true, SeedFrom: core.TorrentStats{Uploaded: 5000, Ratio: 0.5}})
			defer e.Remove("s", false)

			u := notSeeded(t, log)
			if !strings.Contains(u.Torrent.NotSeeded, c.want) {
				t.Errorf("NotSeeded = %q, want it to say %q", u.Torrent.NotSeeded, c.want)
			}
			if u.Torrent.Seeding || u.Torrent.Uploaded != 5000 {
				t.Errorf("the run ended as %+v, want it over with the upload kept", u.Torrent)
			}
			for _, u := range log.snapshot() {
				if u.Status != "" {
					t.Errorf("a start only to seed reported the status %q", u.Status)
				}
			}
			switch fi, err := os.Stat(spoilt); {
			case c.name == "missing" && err == nil:
				t.Error("the missing file was fetched again")
			case c.name == "short" && (err != nil || fi.Size() != 1000):
				t.Errorf("the short file was touched: %v", err)
			}
		})
	}
}

// A file of the right size that does not match the torrent ends the seed run
// once the library begins to fetch the damaged piece, rather than letting it
// fetch the torrent back to whole and seed that.
func TestASeedRunWithADamagedFileEndsInsteadOfFetchingIt(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race; see TestARealMagnetPutsRealSwarmNumbersOnTheTask")
	}
	files := map[string]int{"a.bin": 64 << 10, "b.bin": 64 << 10}
	_, magnet := testenv.SeedTorrent(t, "Pack", files)
	dir := t.TempDir()
	root := filepath.Join(dir, "Pack")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// The seeder's own content, see testenv.SeedTorrent, with one byte wrong.
	for p, size := range files {
		data := bytes.Repeat([]byte(p), size/len(p)+1)[:size]
		if p == "a.bin" {
			data[100] ^= 0xff
		}
		if err := os.WriteFile(filepath.Join(root, p), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := &updateLog{}
	e, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	e.Start(Job{TaskID: "s", URL: magnet, Dir: dir, TorrentRoot: root, TorrentName: "Pack", Seed: true})
	defer e.Remove("s", false)

	u := notSeeded(t, log)
	if !strings.Contains(u.Torrent.NotSeeded, "a.bin does not match the torrent") {
		t.Errorf("NotSeeded = %q, want it to name the damaged file", u.Torrent.NotSeeded)
	}
	for _, u := range log.snapshot() {
		if u.Torrent != nil && u.Torrent.Seeding {
			t.Error("the torrent seeded after the library had fetched into it")
		}
	}
}
