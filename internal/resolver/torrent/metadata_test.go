package torrent

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func TestAnOrdinaryTorrentParses(t *testing.T) {
	b := multiFile(t, "Show.S01", []fileInfo{
		file(700<<20, "Show.S01E01.mkv"),
		file(700<<20, "Show.S01E02.mkv"),
		file(2<<10, "subs", "en.srt"),
	}, false)
	md, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if md.Name != "Show.S01" {
		t.Fatalf("Name = %q", md.Name)
	}
	if md.Private {
		t.Fatal("Private is set on a torrent that never claimed to be")
	}
	if md.TotalSize != 700<<20+700<<20+2<<10 {
		t.Fatalf("TotalSize = %d", md.TotalSize)
	}
	want := []string{"Show.S01E01.mkv", "Show.S01E02.mkv", "subs/en.srt"}
	if len(md.Files) != len(want) {
		t.Fatalf("got %d files, want %d", len(md.Files), len(want))
	}
	for i, w := range want {
		if md.Files[i].Path != w {
			t.Fatalf("file %d path = %q, want %q", i, md.Files[i].Path, w)
		}
	}
	if md.InfoHash == "" || len(md.InfoHash) != 40 {
		t.Fatalf("InfoHash = %q, want a 40-character hex hash", md.InfoHash)
	}
	if len(md.Trackers) != 1 || md.Trackers[0] != "http://tracker.example.org/announce" {
		t.Fatalf("Trackers = %v", md.Trackers)
	}
}

// A single-file torrent has no path list at all - the name IS the file - and
// the same tree has to come out of it, or the selection UI has a shape it
// cannot draw.
func TestASingleFileTorrentIsAOneEntryTree(t *testing.T) {
	md, err := Parse(singleFile(t, "movie.mkv", 4<<20))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(md.Files) != 1 || md.Files[0].Path != "movie.mkv" || md.Files[0].Size != 4<<20 {
		t.Fatalf("Files = %+v", md.Files)
	}
}

// BEP 27's private flag is the entire input to the DHT/PEX decision, and this
// is the only place in the app that can see it: gopeed reads the same flag in
// its own bt fetcher and never exposes it, so a build that trusted the download
// library to hand it over would find nothing there.
func TestThePrivateFlagIsReadAndReported(t *testing.T) {
	b := multiFile(t, "Private.Release", []fileInfo{file(1<<20, "a.bin")}, true)
	md, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !md.Private {
		t.Fatal("info.private was set in the file and did not survive the parse")
	}
	pub, err := Parse(multiFile(t, "Public.Release", []fileInfo{file(1<<20, "a.bin")}, false))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pub.Private {
		t.Fatal("a public torrent came back private, which would switch DHT and PEX off for everyone")
	}
}

// THE ADVERSARIAL SET. Each case takes an otherwise valid torrent and breaks
// exactly one thing, so a refusal is about that thing and nothing else. A
// refusal is not enough on its own either: the test insists on the typed error,
// because the intake route branches on it to pick a status code and a sentence.
func TestParseRefusesHostileAndMalformedTorrents(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T) []byte
		want error
	}{
		{
			"a file path that climbs out of the folder",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "..", "..", "..", "etc", "passwd")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a single dot-dot segment, which filepath.Join really does resolve",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "..", "elsewhere.mkv")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a separator smuggled inside one path segment",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "a/../../b.mkv")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a backslash segment, which is a separator on the platform this runs on",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, `..\..\b.mkv`)}, false)
			},
			ErrUnsafePath,
		},
		{
			"an absolute path",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "/etc/passwd")}, false)
			},
			ErrUnsafePath,
		},
		{
			"an empty path segment",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "sub", "", "a.mkv")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a NUL byte in a file name",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "a\x00b.mkv")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a torrent whose own name is the traversal",
			func(t *testing.T) []byte {
				return multiFile(t, "..", []fileInfo{file(1024, "a.mkv")}, false)
			},
			ErrUnsafePath,
		},
		{
			"the same path listed twice, so a tick box and a file are not the same thing",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(10, "a.mkv"), file(20, "a.mkv")}, false)
			},
			ErrDuplicatePath,
		},
		{
			"the same path in two cases, on a filesystem that has one",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(10, "A.mkv"), file(20, "a.MKV")}, false)
			},
			ErrDuplicatePath,
		},
		{
			"the same path differing only by a trailing space, which Windows strips silently",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(10, "a.mkv"), file(20, "a.mkv ")}, false)
			},
			ErrDuplicatePath,
		},
		{
			"a colon inside a file name, an NTFS alternate-data-stream separator",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "readme.txt:payload.exe")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a colon inside a folder segment, not only the final one",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "sub:stream", "a.mkv")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a Windows reserved device name",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "CON")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a Windows reserved device name with an extension, which Windows still treats as the device",
			func(t *testing.T) []byte {
				return multiFile(t, "Show.S01", []fileInfo{file(1024, "con.txt")}, false)
			},
			ErrUnsafePath,
		},
		{
			"a zero piece length",
			func(t *testing.T) []byte {
				return build(t, metainfo.Info{Name: "x.bin", Length: 1 << 20, PieceLength: 0, Pieces: make([]byte, 20)}, "")
			},
			ErrPieceGeometry,
		},
		{
			"a piece length of one byte, which is a hash list larger than the data",
			func(t *testing.T) []byte {
				return build(t, metainfo.Info{Name: "x.bin", Length: 4096, PieceLength: 1, Pieces: make([]byte, 4096*20)}, "")
			},
			ErrPieceGeometry,
		},
		{
			"a hash list that is not a whole number of hashes",
			func(t *testing.T) []byte {
				return build(t, metainfo.Info{Name: "x.bin", Length: 1 << 20, PieceLength: testPieceLength, Pieces: make([]byte, 641)}, "")
			},
			ErrPieceGeometry,
		},
		{
			"far fewer hashes than the data needs",
			func(t *testing.T) []byte {
				return build(t, metainfo.Info{Name: "x.bin", Length: 100 << 30, PieceLength: testPieceLength, Pieces: make([]byte, 20)}, "")
			},
			ErrPieceGeometry,
		},
		{
			"a torrent that describes no bytes at all",
			func(t *testing.T) []byte {
				return build(t, metainfo.Info{Name: "x.bin", Length: 0, PieceLength: testPieceLength, Pieces: nil}, "")
			},
			ErrPieceGeometry,
		},
		{
			"a control character in an announce URL",
			func(t *testing.T) []byte {
				info := metainfo.Info{Name: "x.bin", Length: 1 << 20, PieceLength: testPieceLength, Pieces: pieces(1 << 20)}
				ib, err := bencode.Marshal(info)
				if err != nil {
					t.Fatal(err)
				}
				mi := metainfo.MetaInfo{InfoBytes: ib, Announce: "http://tracker.example.org/ann\nounce"}
				b, err := bencode.Marshal(mi)
				if err != nil {
					t.Fatal(err)
				}
				return b
			},
			ErrBadTracker,
		},
		{
			"bytes that are not bencode",
			func(t *testing.T) []byte { return []byte("<html>404 not found</html>") },
			ErrNotTorrent,
		},
		{
			"an empty upload",
			func(t *testing.T) []byte { return nil },
			ErrNotTorrent,
		},
		{
			"a bencoded dict with no info in it",
			func(t *testing.T) []byte { return []byte("d8:announce3:abce") },
			ErrNoInfo,
		},
		{
			"a blob over the size limit",
			func(t *testing.T) []byte { return make([]byte, MaxTorrentBytes+1) },
			ErrTooLarge,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.make(t))
			if !errors.Is(err, c.want) {
				t.Fatalf("error = %v, want %v", err, c.want)
			}
			// A refusal nobody can read is a refusal nobody can act on.
			if strings.TrimSpace(err.Error()) == "" {
				t.Fatal("the refusal carries no sentence")
			}
		})
	}
}

// The file list length is its own limit because the file list is its own
// resource: a hundred thousand entries is a tree the browser has to draw and a
// selection the store has to hold, whatever the piece geometry says.
func TestParseRefusesAnAbsurdlyLongFileList(t *testing.T) {
	files := make([]fileInfo, MaxFiles+1)
	for i := range files {
		files[i] = file(1, "f", strings.Repeat("a", 1)+itoa(i))
	}
	_, err := Parse(multiFile(t, "Many", files, false))
	if !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("error = %v, want ErrTooManyFiles", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

// A tracker this app cannot speak to is dropped and counted, not fatal. Real
// torrents carry dead schemes all the time and refusing the file over one would
// refuse half the world; saying nothing at all would leave somebody chasing a
// stalled torrent with no idea that most of its trackers were ignored.
func TestUnusableTrackersAreDroppedAndCounted(t *testing.T) {
	info := metainfo.Info{Name: "x.bin", Length: 1 << 20, PieceLength: testPieceLength, Pieces: pieces(1 << 20)}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{
		InfoBytes: ib,
		Announce:  "udp://tracker.example.org:6969/announce",
		AnnounceList: [][]string{
			{"http://ok.example.org/announce", "dht://not-a-tracker"},
			{"file:///etc/passwd", "wss://ok.example.net", "", "not a url at all"},
			{"udp://tracker.example.org:6969/announce"},
		},
	}
	b, err := bencode.Marshal(mi)
	if err != nil {
		t.Fatal(err)
	}
	md, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"udp://tracker.example.org:6969/announce", "http://ok.example.org/announce", "wss://ok.example.net"}
	if len(md.Trackers) != len(want) {
		t.Fatalf("Trackers = %v, want %v", md.Trackers, want)
	}
	for i := range want {
		if md.Trackers[i] != want[i] {
			t.Fatalf("Trackers = %v, want %v", md.Trackers, want)
		}
	}
	if md.DroppedTrackers != 4 {
		t.Fatalf("DroppedTrackers = %d, want 4", md.DroppedTrackers)
	}
}

// THE CONTAINMENT TEST, and it is written the way Wave 10's was not.
//
// That wave shipped a check that joined a single-segment name onto a directory
// and confirmed the result was inside it - which it always was, by
// construction, whatever the directory was. The test that let it through only
// ever passed it well-formed names, so it could not have failed. This one feeds
// Contained the paths a real .torrent can carry, and the escaping cases are the
// point: if they pass, the check is doing nothing.
func TestContainedRefusesEveryPathThatLeavesTheFolder(t *testing.T) {
	dir := t.TempDir()
	escaping := []string{
		"../elsewhere.mkv",
		"../../etc/passwd",
		"Show/../../../etc/passwd",
		"..",
		"a/b/../../../c",
	}
	for _, rel := range escaping {
		t.Run("refuses "+rel, func(t *testing.T) {
			err := Contained(dir, []string{rel})
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("Contained(%q) = %v, want ErrUnsafePath", rel, err)
			}
			// And the check really had work to do: the join genuinely lands
			// outside. If this assertion ever fails the case has stopped being
			// adversarial and the test above it has become the tautology.
			full := filepath.Join(dir, filepath.FromSlash(rel))
			if strings.HasPrefix(full, filepath.Clean(dir)+string(filepath.Separator)) {
				t.Fatalf("%q resolves to %q, which is inside the folder - this case no longer tests anything", rel, full)
			}
		})
	}

	// Rooted paths, refused on every platform and not only on the one where
	// filepath.IsAbs happens to agree. None of these escape through Join - Join
	// treats them as relative - so they are here to pin the refusal, which is
	// about a torrent stating a root at all.
	for _, abs := range []string{"/etc/passwd", `\Windows\System32\x`, `C:\Windows\x`, "c:/windows/x"} {
		if err := Contained(dir, []string{abs}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Contained(%q) = %v, want ErrUnsafePath", abs, err)
		}
	}

	// A colon does not escape the folder through Join the way "../" does - the
	// resulting path is still, textually, inside dir - which is exactly why it
	// needs its own check rather than being folded into the escaping list
	// above: it is refused for creating an NTFS alternate-data-stream on an
	// existing file, not for resolving outside the folder. This is the second
	// gate a magnet's file list actually reaches (safeComponent's own test
	// covers the .torrent-upload path, which never calls Contained at all).
	for _, rel := range []string{"readme.txt:payload.exe", "sub:stream/a.mkv"} {
		if err := Contained(dir, []string{rel}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Contained(%q) = %v, want ErrUnsafePath", rel, err)
		}
	}
	// A backslash is the identical shape of case, and cannot live in the
	// escaping list above for the identical reason the colon cannot: its own
	// naive-join self-check ("the join genuinely lands outside") is ITSELF
	// platform-dependent - filepath.Join only walks ".." past a backslash on
	// Windows, so on Linux "..\\elsewhere.mkv" textually stays inside dir,
	// which used to be exactly the gap between "passes on the machine this
	// was built on" and "passes where this app actually ships" (caught live
	// by Linux CI, not by this test suite on this Windows machine). Checked
	// here instead, the same way the colon is: refused unconditionally by
	// Contained regardless of what filepath.Join would or would not do with
	// it on whichever platform happens to be running.
	for _, rel := range []string{`..\elsewhere.mkv`, `Show\..\..\etc\passwd`} {
		if err := Contained(dir, []string{rel}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Contained(%q) = %v, want ErrUnsafePath", rel, err)
		}
	}
	if err := Contained(dir, []string{""}); !errors.Is(err, ErrUnsafePath) {
		t.Fatal("an empty path was accepted")
	}
	if err := Contained("", []string{"a.mkv"}); !errors.Is(err, ErrUnsafePath) {
		t.Fatal("a check against no folder at all was accepted")
	}
}

func TestContainedAcceptsOrdinaryTorrentPaths(t *testing.T) {
	dir := t.TempDir()
	ok := []string{
		"movie.mkv",
		"Show.S01/ep01.mkv",
		"Show.S01/subs/en.srt",
		"a.b.c/d..e/f.mkv",
		"...weird but legal",
	}
	if err := Contained(dir, ok); err != nil {
		t.Fatalf("Contained refused an ordinary torrent: %v", err)
	}
}
