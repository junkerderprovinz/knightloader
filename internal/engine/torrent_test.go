package engine

import (
	"errors"
	"testing"

	"github.com/GopeedLab/gopeed/pkg/base"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// TestLandingPathsMirrorWhereTheLibraryWrites: the containment check is only
// worth something if it checks the paths the library really writes.
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

// TestAHostileResolvedTorrentIsRefusedBeforeAnythingIsCreated tests the check
// as the engine composes it. A magnet's file list never passes the resolver,
// so this is all that stands between a hostile swarm and a write outside the
// download folder.
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

// TestTheSizeShownIsTheSelectionsAndNotTheWholeTorrents: a bt resolve reports
// the whole torrent's size however few files are selected.
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
	// The selection comes from the client, so out-of-range indices are ignored.
	if _, size := torrentMeta(res, []int{0, 99, -1}); size != 700 {
		t.Fatalf("size = %d, want 700 with the impossible indices ignored", size)
	}
	if name, _ := torrentMeta(&base.Resource{Files: []*base.FileInfo{{Name: "movie.mkv"}}}, nil); name != "movie.mkv" {
		t.Fatal("a single-file torrent lost its name")
	}
}

// TestStartRecognisesTheTorrentShapesFromTheURLAlone: a magnet sent down the
// HTTP path would still download, but without the containment check.
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
