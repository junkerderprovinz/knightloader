package torrent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// A real magnet link, the one cmd/spike-torrent proved the whole stats path
// against. Kept whole rather than trimmed: the parts a test would be tempted to
// cut (the tracker list, the display name) are the parts Resolve reads.
const sintelMagnet = "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Ftracker.openbittorrent.com%3A6969%2Fannounce"

var res Resolver

// TestMatchTakesTheTwoIntakeShapesAndNothingElse. The two false cases matter
// more than the two true ones: an https link to a .torrent file belongs to the
// direct resolver, and claiming it here would turn a plain GET into a swarm
// join against a file nobody has read.
func TestMatchTakesTheTwoIntakeShapesAndNothingElse(t *testing.T) {
	torrentBytes := singleFile(t, "movie.mkv", 4<<20)
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"a real magnet", sintelMagnet, true},
		{"a bare magnet scheme with a hash", "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10", true},
		{"MAGNET in capitals, as some sites emit", strings.ToUpper("magnet:") + "?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10", true},
		{"an uploaded .torrent", EncodeBytes(torrentBytes), true},
		{"an https link that ends in .torrent", "https://example.org/files/ubuntu.torrent", false},
		{"an ordinary https link", "https://example.org/movie.mkv", false},
		{"the magnet scheme alone", "magnet:", false},
		{"a data URI of the right type holding something else", EncodeBytes([]byte("this is not bencode at all")), false},
		{"a data URI of a different type", "data:text/plain;base64,aGVsbG8=", false},
		{"an empty string", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := res.Match(c.in); got != c.want {
				t.Fatalf("Match = %v, want %v", got, c.want)
			}
		})
	}
}

// Match runs on every pasted line, so it has to survive whatever is in one
// without taking the process down with it. A panic here is a paste box that
// kills the app.
func TestMatchDoesNotPanicOnRubbish(t *testing.T) {
	for _, in := range []string{
		"data:application/x-bittorrent;base64,",
		"data:application/x-bittorrent;base64,!!!!not base64!!!!",
		"data:application/x-bittorrent;base64," + strings.Repeat("A", 1<<20),
		"magnet:?xt=urn:btih:" + strings.Repeat("z", 5000),
		"d",
		"\x00\x01\x02",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Match(%.40q) panicked: %v", in, r)
				}
			}()
			res.Match(in)
		}()
	}
}

// A magnet with no info hash PARSES CLEANLY in the library gopeed hands it to
// (anacrolix's ParseMagnetV2Uri returns no error for it), and the torrent
// client then waits forever for metadata about nothing. This is the exact
// "accepted now, confusing failure later" shape the wave brief named, so the
// refusal has to happen here and has to say why.
func TestAMagnetWithNoInfoHashIsRefusedWithAReason(t *testing.T) {
	_, err := res.Resolve(context.Background(), resolver.Request{URL: "magnet:?dn=Something+Nice&tr=udp%3A%2F%2Ftracker.example.org%3A6969"})
	if !errors.Is(err, ErrBadMagnet) {
		t.Fatalf("error = %v, want ErrBadMagnet", err)
	}
	if !strings.Contains(err.Error(), "info hash") {
		t.Fatalf("the refusal %q does not say what is missing", err)
	}
}

// THE TWO MAGNET LINKS THAT KILL THE PROCESS, refused here so they never
// reach the code that dies on them.
//
// Found by running the malformed cases rather than by reading: the torrent
// client this app ends up inside asserts on a non-zero v1 info hash with a bare
// panic (anacrolix/torrent Client.AddTorrentOpt, panicif.Zero), on a goroutine
// gopeed owns, so nothing above it can recover and the whole instance goes
// down. Both of these are a line of text somebody can paste into the add box.
//
// If this test ever starts failing because the refusals were relaxed, run the
// engine's own live suite before believing the library was fixed - the failure
// mode is a panic, not an error, and it will not show up as a red test in this
// package.
func TestTheTwoMagnetsThatCrashTheTorrentClientAreRefused(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		says string
	}{
		{
			"an all-zeroes info hash, which parses perfectly and then asserts",
			"magnet:?xt=urn:btih:0000000000000000000000000000000000000000",
			"zeroes",
		},
		{
			"a v2-only magnet, whose empty v1 field trips the same assertion",
			"magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e&dn=bittorrent-v2-test",
			"v2-only",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !res.Match(c.uri) {
				t.Fatal("Match said no, so nothing would refuse it either - it would fall through to another backend")
			}
			_, err := res.Resolve(context.Background(), resolver.Request{URL: c.uri})
			if !errors.Is(err, ErrBadMagnet) {
				t.Fatalf("error = %v, want ErrBadMagnet", err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the refusal %q does not say %q, so nobody reading it learns anything", err, c.says)
			}
		})
	}
}

func TestAGoodMagnetResolvesToItselfWithItsDisplayName(t *testing.T) {
	got, err := res.Resolve(context.Background(), resolver.Request{URL: sintelMagnet})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.DirectURL != sintelMagnet {
		t.Fatalf("DirectURL = %q, want the magnet unchanged", got.DirectURL)
	}
	if got.Name != "Sintel" {
		t.Fatalf("Name = %q, want the display name", got.Name)
	}
	// A resolver that states a connection count has that number read as a
	// per-host chunk ceiling, and it means nothing to a swarm. See Direct.
	if got.Connections != 0 {
		t.Fatalf("Connections = %d; a torrent has no opinion about chunks", got.Connections)
	}
}

// With no dn there is still something true to show. "download" would be a
// guess; the info hash is the torrent.
func TestAMagnetWithNoNameFallsBackToItsInfoHash(t *testing.T) {
	const hash = "08ada5a7a6183aae1e09d831df6748d566095a10"
	got, err := res.Resolve(context.Background(), resolver.Request{URL: "magnet:?xt=urn:btih:" + hash})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != hash {
		t.Fatalf("Name = %q, want the info hash", got.Name)
	}
}

// Resolve re-encodes the bytes it parsed rather than passing the caller's URI
// through, so the engine can only ever be handed something that went through
// Parse. Proven by handing in a URI whose base64 has been padded with
// whitespace: the same bytes, a different string, and the answer has to be the
// canonical one.
func TestResolveHandsOnTheBytesItActuallyChecked(t *testing.T) {
	raw := singleFile(t, "movie.mkv", 4<<20)
	canonical := EncodeBytes(raw)
	got, err := res.Resolve(context.Background(), resolver.Request{URL: canonical})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.DirectURL != canonical {
		t.Fatalf("DirectURL is not the re-encoding of the parsed bytes")
	}
	if got.Name != "movie.mkv" {
		t.Fatalf("Name = %q, want the torrent's own name", got.Name)
	}
	if got.Size != 4<<20 {
		t.Fatalf("Size = %d, want the torrent's total", got.Size)
	}
}

// A hostile .torrent must be refused HERE, by the resolver, and not left to
// fail somewhere further in. This is the one the wave brief singled out.
func TestResolveRefusesATraversingTorrent(t *testing.T) {
	b := multiFile(t, "Show.S01", []fileInfo{
		file(1024, "..", "..", "..", "etc", "passwd"),
	}, false)
	_, err := res.Resolve(context.Background(), resolver.Request{URL: EncodeBytes(b)})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
}

// Describe is what the collector's file tree is drawn from. A magnet has no
// tree yet and must say so by having none, not by failing.
func TestDescribeAnswersAnEmptyTreeForAMagnetAndARealOneForAFile(t *testing.T) {
	md, err := res.Describe(sintelMagnet)
	if err != nil {
		t.Fatalf("Describe(magnet): %v", err)
	}
	if len(md.Files) != 0 {
		t.Fatalf("a magnet described %d files; nobody knows them yet", len(md.Files))
	}
	if md.InfoHash == "" {
		t.Fatal("a magnet described no info hash")
	}

	b := multiFile(t, "Show.S01", []fileInfo{file(10, "ep01.mkv"), file(20, "subs", "ep01.srt")}, false)
	md, err = res.Describe(EncodeBytes(b))
	if err != nil {
		t.Fatalf("Describe(torrent): %v", err)
	}
	if len(md.Files) != 2 {
		t.Fatalf("described %d files, want 2", len(md.Files))
	}
	if md.Files[1].Path != "subs/ep01.srt" {
		t.Fatalf("path = %q, want the forward-slashed in-torrent path", md.Files[1].Path)
	}
	for _, f := range md.Files {
		if !f.Selected {
			t.Fatalf("%q arrived unticked; unticking is the deliberate act", f.Path)
		}
	}
}

// ParseUpload is the intake gate: there must be no way to get a stageable URI
// out of it without the parse having passed.
func TestParseUploadWillNotHandBackAURIForBytesItRefused(t *testing.T) {
	b := multiFile(t, "..", []fileInfo{file(10, "ep01.mkv")}, false)
	_, uri, err := ParseUpload(b)
	if err == nil {
		t.Fatal("a torrent named \"..\" was accepted")
	}
	if uri != "" {
		t.Fatalf("a refused upload still produced a stageable URI %q", uri)
	}
}
