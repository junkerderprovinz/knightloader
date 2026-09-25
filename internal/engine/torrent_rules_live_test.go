package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	anacrolix "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// requireTorrentClient skips a test that starts a real torrent client on the
// same terms as the other torrent tests in this package.
func requireTorrentClient(t *testing.T) {
	t.Helper()
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client")
	}
	if raceEnabled {
		// A started torrent seeds, and gopeed v1.9.3's bt.Fetcher races
		// inside itself while it does; see TestARealMagnetPutsRealSwarmNumbersOnTheTask.
		t.Skip("gopeed v1.9.3's own bt.Fetcher has an internal data race once a torrent runs")
	}
}

// seedFile is one file of a torrent a test seeds, by its path inside it.
type seedFile struct {
	path string
	size int
}

// seedTorrent writes files into a folder named name, builds the .torrent for
// it and seeds it from a client of its own that listens on loopback only. It
// returns the .torrent's bytes and a magnet that names the seeder as a peer, so
// a download needs neither a tracker nor the DHT.
func seedTorrent(t *testing.T, name string, files []seedFile) ([]byte, string) {
	t.Helper()
	root, err := os.MkdirTemp("", "kl-bt-seeder-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	for i, f := range files {
		p := filepath.Join(root, name, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.Repeat([]byte{byte('a' + i)}, f.size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib}
	raw, err := bencode.Marshal(mi)
	if err != nil {
		t.Fatal(err)
	}

	cfg := anacrolix.NewDefaultClientConfig()
	cfg.DataDir = root
	cfg.Seed = true
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.ListenHost = func(string) string { return "127.0.0.1" }
	cl, err := anacrolix.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	tor, err := cl.AddTorrent(&mi)
	if err != nil {
		t.Fatal(err)
	}
	if err := tor.VerifyData(); err != nil {
		t.Fatal(err)
	}

	m := mi.Magnet(nil, &info)
	m.Params.Set("x.pe", fmt.Sprintf("127.0.0.1:%d", cl.LocalPort()))
	return raw, m.String()
}

// startWithRules starts uri with rules on a real engine and waits until the
// engine has handed it to the library. It returns the engine, its updates, the
// download folder and the library's id for the task.
func startWithRules(t *testing.T, uri string, rules torrent.FileRules) (*Engine, *taskSink, string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "kl-bt-rules-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sink := &taskSink{}
	e, err := New(dir, sink.apply)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.SetMetadataTimeout(30 * time.Second)

	const id = "rules-1"
	e.Start(Job{TaskID: id, URL: uri, Dir: dir, FileRules: rules})
	t.Cleanup(func() { e.Remove(id, true) })

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		gid := e.toGopeed[id]
		e.mu.Unlock()
		if gid != "" {
			return e, sink, dir, gid
		}
		if got := sink.snapshot(); got.Status == core.StatusError {
			t.Fatalf("the torrent failed: %s", got.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the engine never handed the torrent to the library")
	return nil, nil, "", ""
}

// librarySelection waits for the library's task to carry its size, which it
// works out from the selection when the task starts.
func librarySelection(t *testing.T, e *Engine, gid string, size int64) []int {
	t.Helper()
	var got []int
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		task := e.d.GetTask(gid)
		if task == nil || task.Meta.Res == nil || task.Meta.Opts == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		got = slices.Clone(task.Meta.Opts.SelectFiles)
		if task.Meta.Res.Size == size {
			return got
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the library's task never came to the %d bytes the rules chose; it fetches files %v", size, got)
	return nil
}

var ruledFiles = []seedFile{
	{"Movie.mkv", 96 << 10},
	{"Movie.nfo", 3 << 10},
	{"Sample/Movie.Sample.mkv", 20 << 10},
}

var skipSampleAndNfo = torrent.FileRules{Exclude: []string{`\.nfo$`, `^Sample/`}}

func TestAnUploadsFileRulesReachTheLibrary(t *testing.T) {
	requireTorrentClient(t)
	raw, _ := seedTorrent(t, "Movie", ruledFiles)
	e, _, _, gid := startWithRules(t, torrent.EncodeBytes(raw), skipSampleAndNfo)
	if got := librarySelection(t, e, gid, 96<<10); !slices.Equal(got, []int{0}) {
		t.Fatalf("the library fetches files %v, want only the film", got)
	}
}

// A magnet's file list arrives from the swarm, so its choice is made after the
// library has resolved it; this is the test that notices a library that stops
// keeping the options from Resolve to Create.
func TestAMagnetsFileRulesReachTheLibraryAndTheDisk(t *testing.T) {
	requireTorrentClient(t)
	_, magnet := seedTorrent(t, "Movie", ruledFiles)
	e, sink, dir, gid := startWithRules(t, magnet, skipSampleAndNfo)
	if got := librarySelection(t, e, gid, 96<<10); !slices.Equal(got, []int{0}) {
		t.Fatalf("the library fetches files %v, want only the film", got)
	}

	deadline := time.Now().Add(45 * time.Second)
	for sink.snapshot().Status != core.StatusDone {
		if time.Now().After(deadline) {
			t.Fatalf("status = %q after 45s, want done", sink.snapshot().Status)
		}
		time.Sleep(200 * time.Millisecond)
	}
	onDisk := func(p string) (os.FileInfo, error) {
		return os.Stat(filepath.Join(dir, "Movie", filepath.FromSlash(p)))
	}
	if fi, err := onDisk("Movie.mkv"); err != nil || fi.Size() != 96<<10 {
		t.Fatalf("the film is not on disk whole: %v", err)
	}
	for _, skipped := range []string{"Movie.nfo", "Sample/Movie.Sample.mkv"} {
		if _, err := onDisk(skipped); err == nil {
			t.Errorf("%s was left on disk although the rules skip it", skipped)
		}
	}
}
