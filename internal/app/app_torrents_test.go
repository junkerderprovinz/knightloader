package app

// AddTorrent's own tests build their .torrent fixtures the same way
// internal/resolver/torrent's own tests do (hand-rolled with the reference
// library, read back with the reference library) rather than importing that
// package's unexported helpers, which a different package cannot reach.

import (
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// newTorrentTestApp mirrors every other file in this package's own test setup
// (queue_test.go's newQueueApp, accounts_test.go's newAccountsTestApp, and so
// on) - there is no single shared helper across this package's test files,
// each one builds its own App the same three lines.
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

// testTorrentURI builds a valid multi-file .torrent and returns it already
// re-expressed as the data: URI staging carries a torrent as - the same shape
// torrent.ParseUpload hands a caller.
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

// TestAddTorrentStagesTheRequestedSelection is the core promise: what the
// caller marked Selected is what lands on the task, and Size is the selected
// subset's own total, not the whole torrent's - see torrentSize's own comment
// for why that second part matters as much as the first.
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
	// The base64 of the whole .torrent must never end up in a bucket that gets
	// read back out as a column value - see torrentHost's own comment.
	if task.Host == uri || len(task.Host) > 64 {
		t.Errorf("host = %d bytes, want a short bucket rather than the uri itself", len(task.Host))
	}
}

// TestAddTorrentWithNoSelectionKeepsEveryFileAndTheWholeSize is the "never
// shown a tree" case (a single-file torrent, or a caller that passes through
// whatever Parse defaulted): every file stays selected, so the size is the
// whole torrent's own total.
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

// TestAddTorrentFoldsAnExactDuplicateAway is the same rule every other intake
// path already gives an identical URL: never kept, whatever KeepMirrors says
// (default false here, so the second call must not even fall into the sibling
// branch) - see keepsAsSibling's own comment, "a duplicate is never kept".
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

// TestAddTorrentNeverLeavesAPackageBlank mirrors what a plain pasted link
// already gets from addLinksFrom (nameBucket, then the catch-all): a torrent
// staged with no package name must not sit ownerless forever, the same reason
// unpackagedIDs exists at all.
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

// TestTorrentSizeUsesTheSelectionNotTheWholeTorrent is torrentSize on its own,
// isolating the exact lesson internal/engine/torrent.go's torrentMeta already
// learned live: a resolve that reports the whole torrent's bytes regardless of
// selection produces a number nobody believes once the real one shows up.
func TestTorrentSizeUsesTheSelectionNotTheWholeTorrent(t *testing.T) {
	md := torrent.Metadata{TotalSize: 129 << 20} // the whole-torrent figure, deliberately wrong for this case
	files := []core.TorrentFile{
		{Path: "movie.mkv", Size: 128 << 20, Selected: false},
		{Path: "subs.srt", Size: 1500, Selected: true},
	}
	if got := torrentSize(md, files); got != 1500 {
		t.Errorf("torrentSize = %d, want 1500 (the one selected file)", got)
	}
	// No files known yet (a freshly pasted magnet) falls back to the whole
	// figure Describe reported, honestly "not yet known" rather than 0.
	if got := torrentSize(md, nil); got != md.TotalSize {
		t.Errorf("torrentSize with no file list = %d, want %d", got, md.TotalSize)
	}
}

// TestTorrentHostIsNeverTheEncodedFile is torrentHost on its own: an uploaded
// .torrent's uri is the base64 of the whole file, and hostOf's ordinary
// fallback (the raw string, whenever url.Parse finds no hostname) would put
// that multi-megabyte string in Task.Host and from there into every JSON
// response the task ever appears in - and a magnet's own query string is the
// same wrong answer at a smaller size, not a different problem: it is still
// what a naming template's <jd:hoster> resolves to and what the frontend's
// Host column shows, neither of which wants a magnet URI in it. Both kinds
// get their own short, fixed bucket rather than hostOf's raw-string answer.
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

// TestPastedMagnetGetsTheShortHostBucket is TestTorrentHostIsNeverTheEncodedFile
// proven through the path a real user paste actually takes: app_links.go's
// stage, via AddLinksFrom, not a direct call to torrentHost itself. A correct
// torrentHost wired to the wrong call site would leave this failing while the
// unit test above passed - which is exactly what was true here before this
// wave (stage built its Host from the bare hostOf(u), never torrentHost at
// all).
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

// TestStartTasksThreadsTheSelectionIntoTheEngineJob is the dispatch-side half
// of the file-tree promise: app_dispatch.go's own Job literal has to carry
// core.SelectedTorrentIndices(t.TorrentFiles), or a selection made in the
// collector never reaches the engine at all and every torrent downloads in
// full regardless of what was unticked - the exact outcome decision 6 of
// docs/torrent-support.md exists to prevent.
//
// This cannot assert the exact indices the download library received without
// reaching into the engine's own internals, which are private by design (see
// internal/engine's own tests for that level of proof, against a real
// magnet). What it CAN and does assert is that a torrent task staged through
// AddTorrent - the only place TorrentFiles is populated today - actually
// reaches the engine's own torrent branch when started, rather than being
// silently mishandled: a short metadata timeout makes that branch fail fast
// and by name (resolveTorrent's own sentence), which is the observable proof
// that StartTasks -> the dispatch loop -> Engine.Start -> startTorrent ->
// resolveTorrent is the intact chain this test exercises end to end.
//
// Gated on -short because reaching the engine's torrent branch is the same
// thing as booting gopeed's shared torrent.Client for this process (see
// Engine.SetTorrentConfig's own comment on initClient), and that client binds
// a wildcard peer listener - verified here, 0.0.0.0 and [::] on the same port,
// alongside the app's ordinary loopback proxy. Correct for the real app,
// pointless for a unit test, and on Windows it is a firewall prompt every
// single run: go test builds a fresh binary at a fresh temp path each time, so
// no allow-rule can ever stick to it. Nothing about what this test asserts
// changes; a plain go test ./... still runs it in full.
func TestStartTasksThreadsTheSelectionIntoTheEngineJob(t *testing.T) {
	if testing.Short() {
		t.Skip("this starts a torrent client, which opens a network-facing listener")
	}
	a := newTorrentTestApp(t)
	a.Engine.SetMetadataTimeout(200 * time.Millisecond)

	// A magnet, not an upload: a magnet's Resolve returns instantly (no bytes
	// to parse) and its swarm wait is exactly what SetMetadataTimeout bounds,
	// which is what keeps this test fast and deterministic rather than
	// depending on how quickly an embedded info dict resolves.
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	created := a.AddLinksFrom([]string{magnet}, "DispatchTest", OriginPaste)
	if len(created) != 1 {
		t.Fatalf("AddLinksFrom staged %d tasks, want 1", len(created))
	}
	id := created[0].ID

	// TorrentFiles set directly on the live task, under the same lock the
	// production writer uses (app_links.go's own convention, binding on tests
	// too) - AddLinksFrom's own magnet path never populates it (the swarm has
	// not answered yet), so this stands in for the moment it eventually would.
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
		t.Fatalf("status = %q after 5s, want error (the short metadata timeout should have settled this) - task: %+v", last.Status, last)
	}
	// resolveTorrent's own sentence, not a generic dispatch failure - proof
	// this specific task reached the engine's torrent branch rather than, say,
	// silently being treated as an ordinary HTTP job (which a magnet URL would
	// fail differently and much faster).
	if !strings.Contains(last.Error, "torrent") {
		t.Errorf("error = %q, want the resolveTorrent metadata-timeout sentence naming the torrent", last.Error)
	}
}
