package engine

import (
	"errors"
	"testing"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// landingPaths has to mirror where the download library really writes, because
// the containment check is only worth anything if it checks the paths that get
// used. The two shapes are the whole rule: a resource with a Name is a folder
// torrent and everything nests under it, a resource without one is a single
// file whose name is the entire path.
func TestLandingPathsMirrorWhereTheLibraryWrites(t *testing.T) {
	folder := &base.Resource{Name: "Show.S01", Files: []*base.FileInfo{
		{Name: "ep01.mkv", Path: ""},
		{Name: "en.srt", Path: "subs"},
	}}
	want := []string{"Show.S01/ep01.mkv", "Show.S01/subs/en.srt"}
	got := landingPaths(folder)
	if len(got) != len(want) {
		t.Fatalf("landingPaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("landingPaths = %v, want %v", got, want)
		}
	}

	single := &base.Resource{Files: []*base.FileInfo{{Name: "movie.mkv"}}}
	if got := landingPaths(single); len(got) != 1 || got[0] != "movie.mkv" {
		t.Fatalf("landingPaths(single) = %v", got)
	}
	if got := landingPaths(nil); got != nil {
		t.Fatalf("landingPaths(nil) = %v", got)
	}
}

// THE GATE THE MAGNET PATH DEPENDS ON. A magnet's file list is never seen by
// the resolver - it arrives from the swarm after the download library already
// has the link - so this composition is the only thing standing between a
// hostile swarm and a write outside the download folder. Tested as the engine
// actually composes it, resource in and refusal out, rather than by testing the
// two halves separately and assuming they meet.
func TestAHostileResolvedTorrentIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	dir := t.TempDir()
	hostile := []*base.Resource{
		{Name: "Show.S01", Files: []*base.FileInfo{{Name: "passwd", Path: "../../../etc"}}},
		{Name: "..", Files: []*base.FileInfo{{Name: "ep01.mkv"}}},
		{Files: []*base.FileInfo{{Name: "x.mkv", Path: ".."}}},
		{Name: "ok", Files: []*base.FileInfo{{Name: "fine.mkv"}, {Name: "passwd", Path: "../../.."}}},
	}
	for _, res := range hostile {
		if err := torrent.Contained(dir, landingPaths(res)); !errors.Is(err, torrent.ErrUnsafePath) {
			t.Fatalf("resource %+v was accepted (err = %v)", res, err)
		}
	}
	fine := &base.Resource{Name: "Show.S01", Files: []*base.FileInfo{
		{Name: "ep01.mkv"}, {Name: "en.srt", Path: "subs"},
	}}
	if err := torrent.Contained(dir, landingPaths(fine)); err != nil {
		t.Fatalf("an ordinary torrent was refused: %v", err)
	}
}

// The size shown for a partial selection has to be the selection's size, and
// the library will not work it out on this path: base.Resource.CalcSize is
// called with nil during a bt resolve, so res.Size is the whole torrent however
// few files were asked for. Verified live - a resolve limited to one 1.5 KB
// subtitle still reported 129 MB - which is why this is computed here.
func TestTheSizeShownIsTheSelectionsAndNotTheWholeTorrents(t *testing.T) {
	res := &base.Resource{Name: "Show.S01", Size: 1000, Files: []*base.FileInfo{
		{Name: "a.mkv", Size: 700},
		{Name: "b.mkv", Size: 250},
		{Name: "c.srt", Size: 50},
	}}
	if name, size := torrentMeta(res, nil); name != "Show.S01" || size != 1000 {
		t.Fatalf("no selection gave %q/%d, want the whole torrent", name, size)
	}
	if _, size := torrentMeta(res, []int{1, 2}); size != 300 {
		t.Fatalf("size = %d, want 300 for the two selected files", size)
	}
	// An index the torrent does not have contributes nothing rather than
	// panicking: the selection is client-supplied and arrives here as integers.
	if _, size := torrentMeta(res, []int{0, 99, -1}); size != 700 {
		t.Fatalf("size = %d, want 700 with the impossible indices ignored", size)
	}
	if name, _ := torrentMeta(&base.Resource{Files: []*base.FileInfo{{Name: "movie.mkv"}}}, nil); name != "movie.mkv" {
		t.Fatal("a single-file torrent lost its name")
	}
}

// Start decides for itself which pipeline a job belongs in, from the URL. If
// this ever stops being true, a magnet dispatched through the ordinary path
// would be built with an HTTP request extra and skip the containment check
// entirely - and it would still download, which is why it needs a test rather
// than a reading.
func TestStartRecognisesTheTorrentShapesFromTheURLAlone(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10", true},
		{"data:application/x-bittorrent;base64,ZA==", true},
		{"https://example.org/movie.mkv", false},
		{"https://example.org/thing.torrent", false},
		{"", false},
	}
	for _, c := range cases {
		if got := torrent.IsURI(c.url); got != c.want {
			t.Fatalf("IsURI(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}
