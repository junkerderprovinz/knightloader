package app

// The .torrent fixtures are built with the reference library, as
// internal/resolver/torrent's tests do, since its helpers are unexported.

import (
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

func newTorrentTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

const testPieceLength = 32 << 10

func testPieces(total int64) []byte {
	n := int((total + testPieceLength - 1) / testPieceLength)
	return make([]byte, n*20)
}

// testTorrentURI builds a valid multi-file .torrent and returns it as the data:
// URI staging carries, the shape torrent.ParseUpload returns.
func testTorrentURI(t *testing.T, folder string, files []metainfo.FileInfo) string {
	t.Helper()
	var total int64
	for _, f := range files {
		total += f.Length
	}
	info := metainfo.Info{Name: folder, Files: files, PieceLength: testPieceLength, Pieces: testPieces(total)}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencoding info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib, Announce: "udp://tracker.example.org:6969/announce"}
	b, err := bencode.Marshal(mi)
	if err != nil {
		t.Fatalf("bencoding torrent: %v", err)
	}
	return torrent.EncodeBytes(b)
}

// The selection lands on the task, and Size is the selected subset's total.
func TestAddTorrentStagesTheRequestedSelection(t *testing.T) {
	a := newTorrentTestApp(t)
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{
		{Length: 900, Path: []string{"one.mkv"}},
		{Length: 12, Path: []string{"two.srt"}},
	})
	files := []core.TorrentFile{
		{Path: "one.mkv", Size: 900, Selected: true},
		{Path: "two.srt", Size: 12, Selected: false},
	}

	task, err := a.AddTorrent(uri, files, "TestPack", OriginPaste)
	if err != nil {
		t.Fatalf("AddTorrent: %v", err)
	}
	if task == nil {
		t.Fatal("AddTorrent returned a nil task for a link staged for the first time")
	}
	if task.Status != core.StatusCollected {
		t.Errorf("status = %q, want collected", task.Status)
	}
	if task.Resolver != "torrent" {
		t.Errorf("resolver = %q, want torrent", task.Resolver)
	}
	if task.Name != "Pack" {
		t.Errorf("name = %q, want the torrent's own name", task.Name)
	}
	if task.Size != 900 {
		t.Errorf("size = %d, want 900 (the selected file only, not 912)", task.Size)
	}
	if len(task.TorrentFiles) != 2 {
		t.Fatalf("torrent files = %+v, want 2 entries", task.TorrentFiles)
	}
	if !task.TorrentFiles[0].Selected {
		t.Error("one.mkv came back unselected")
	}
	if task.TorrentFiles[1].Selected {
		t.Error("two.srt came back selected; the caller asked to exclude it")
	}
	// The encoded .torrent must never become the Host value.
	if task.Host == uri || len(task.Host) > 64 {
		t.Errorf("host = %d bytes, want a short bucket rather than the uri itself", len(task.Host))
	}
}

// With every file selected, the size is the whole torrent's.
func TestAddTorrentWithNoSelectionKeepsEveryFileAndTheWholeSize(t *testing.T) {
	a := newTorrentTestApp(t)
	uri := testTorrentURI(t, "Solo", []metainfo.FileInfo{{Length: 4096, Path: []string{"solo.bin"}}})
	files := []core.TorrentFile{{Path: "solo.bin", Size: 4096, Selected: true}}

	task, err := a.AddTorrent(uri, files, "SoloPack", OriginPaste)
	if err != nil {
		t.Fatalf("AddTorrent: %v", err)
	}
	if task == nil || task.Size != 4096 {
		t.Fatalf("task = %+v, want size 4096", task)
	}
}

// An identical URL is never kept, whatever KeepMirrors says.
func TestAddTorrentFoldsAnExactDuplicateAway(t *testing.T) {
	a := newTorrentTestApp(t)
	uri := testTorrentURI(t, "Dup", []metainfo.FileInfo{{Length: 10, Path: []string{"f.bin"}}})
	files := []core.TorrentFile{{Path: "f.bin", Size: 10, Selected: true}}

	first, err := a.AddTorrent(uri, files, "DupPack", OriginPaste)
	if err != nil || first == nil {
		t.Fatalf("first AddTorrent: task=%+v err=%v", first, err)
	}
	second, err := a.AddTorrent(uri, files, "DupPack", OriginPaste)
	if err != nil {
		t.Fatalf("second AddTorrent returned an error: %v", err)
	}
	if second != nil {
		t.Fatalf("second AddTorrent staged %+v, want nil (folded into the first)", second)
	}
}

// A torrent staged without a package gets one from nameBucket or the catch-all,
// like a pasted link.
func TestAddTorrentNeverLeavesAPackageBlank(t *testing.T) {
	a := newTorrentTestApp(t)
	uri := testTorrentURI(t, "Named", []metainfo.FileInfo{{Length: 10, Path: []string{"f.bin"}}})
	files := []core.TorrentFile{{Path: "f.bin", Size: 10, Selected: true}}

	task, err := a.AddTorrent(uri, files, "", OriginPaste)
	if err != nil || task == nil {
		t.Fatalf("AddTorrent: task=%+v err=%v", task, err)
	}
	if task.Package == "" && !task.ManualPackage {
		t.Error("package is still blank; neither nameBucket nor the catch-all claimed this task")
	}
}

func TestTorrentSizeUsesTheSelectionNotTheWholeTorrent(t *testing.T) {
	md := torrent.Metadata{TotalSize: 129 << 20} // the whole-torrent figure
	files := []core.TorrentFile{
		{Path: "movie.mkv", Size: 128 << 20, Selected: false},
		{Path: "subs.srt", Size: 1500, Selected: true},
	}
	if got := torrentSize(md, files); got != 1500 {
		t.Errorf("torrentSize = %d, want 1500 (the one selected file)", got)
	}
	// With no file list yet (a fresh magnet) it falls back to Describe's
	// figure.
	if got := torrentSize(md, nil); got != md.TotalSize {
		t.Errorf("torrentSize with no file list = %d, want %d", got, md.TotalSize)
	}
}

// hostOf's raw-string fallback would put the encoded file or the magnet query
// into Task.Host, the Host column and the <jd:hoster> path variable.
func TestTorrentHostIsNeverTheEncodedFile(t *testing.T) {
	uri := testTorrentURI(t, "H", []metainfo.FileInfo{{Length: 10, Path: []string{"f.bin"}}})
	if got := torrentHost(uri); got != "torrent-upload" {
		t.Errorf("torrentHost(uploaded) = %q, want the short fixed bucket", got)
	}
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	if got := torrentHost(magnet); got != "torrent-magnet" {
		t.Errorf("torrentHost(magnet) = %q, want the short fixed bucket", got)
	}
	if got := torrentHost("https://example.com/f.bin"); got != hostOf("https://example.com/f.bin") {
		t.Errorf("torrentHost(non-torrent) = %q, want hostOf's own answer %q", got, hostOf("https://example.com/f.bin"))
	}
}

// A pasted magnet goes through stage, which must use torrentHost too.
func TestPastedMagnetGetsTheShortHostBucket(t *testing.T) {
	a := newTorrentTestApp(t)
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	created := a.AddLinksFrom([]string{magnet}, "HostBucketTest", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("AddLinksFrom staged %d tasks, want 1", len(created))
	}
	if got := created[0].Host; got != "torrent-magnet" {
		t.Errorf("host = %q, want the short fixed bucket \"torrent-magnet\"", got)
	}
}

// A started torrent task reaches the engine's torrent branch, which is where
// the selection is passed on. The indices
// themselves are private to the engine; its own tests cover them. A short
// metadata timeout makes the branch fail fast with resolveTorrent's sentence.
//
// The torrent client binds a wildcard peer listener, which on Windows raises a
// firewall prompt for every fresh test binary, so this runs behind
// testenv.RequireWideListener.
func TestStartTasksThreadsTheSelectionIntoTheEngineJob(t *testing.T) {
	testenv.RequireWideListener(t)
	if testing.Short() {
		t.Skip("this starts a torrent client, which opens a network-facing listener")
	}
	a := newTorrentTestApp(t)
	a.Engine.SetMetadataTimeout(200 * time.Millisecond)

	// A magnet resolves instantly and its swarm wait is what
	// SetMetadataTimeout bounds.
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	created := a.AddLinksFrom([]string{magnet}, "DispatchTest", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("AddLinksFrom staged %d tasks, want 1", len(created))
	}
	id := created[0].ID

	// The magnet path never fills TorrentFiles before the swarm answers, so it
	// is set here under the lock the production writer uses.
	a.mu.Lock()
	a.tasks[id].TorrentFiles = []core.TorrentFile{
		{Path: "one.mkv", Size: 900, Selected: true},
		{Path: "two.srt", Size: 12, Selected: false},
	}
	a.mu.Unlock()

	a.StartTasks([]string{id})

	deadline := time.Now().Add(5 * time.Second)
	var last *core.Task
	for time.Now().Before(deadline) {
		for _, tsk := range a.Tasks() {
			if tsk.ID == id {
				last = tsk
			}
		}
		if last != nil && last.Status == core.StatusError {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last == nil {
		t.Fatal("task vanished from the list after StartTasks")
	}
	if last.Status != core.StatusError {
		t.Fatalf("status = %q after 5s, want error (the short metadata timeout should have settled this); task: %+v", last.Status, last)
	}
	// resolveTorrent's sentence rather than an HTTP failure.
	if !strings.Contains(last.Error, "torrent") {
		t.Errorf("error = %q, want the resolveTorrent metadata-timeout sentence naming the torrent", last.Error)
	}
}
