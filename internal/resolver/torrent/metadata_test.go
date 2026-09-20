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

// A single-file torrent has no path list; its name is the file.
func TestASingleFileTorrentIsAOneEntryTree(t *testing.T) {
	md, err := Parse(singleFile(t, "movie.mkv", 4<<20))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(md.Files) != 1 || md.Files[0].Path != "movie.mkv" || md.Files[0].Size != 4<<20 {
		t.Fatalf("Files = %+v", md.Files)
	}
}

// BEP 27's private flag decides DHT and PEX, and gopeed does not expose it.
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

// Each case breaks one property of a valid torrent. The typed error matters
// because the intake route picks its status code from it.
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
			if strings.TrimSpace(err.Error()) == "" {
				t.Fatal("the refusal carries no sentence")
			}
		})
	}
}

// A huge file list is a tree the browser draws and a selection the store
// holds, whatever the piece geometry says.
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

// Real torrents carry dead tracker schemes, so those are dropped and counted
// rather than refused.
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

// The escaping cases are real multi-segment paths; each also checks that a
// naive join would land outside, so the case keeps testing something.
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
			// The naive join must really land outside, or the case tests
			// nothing.
			full := filepath.Join(dir, filepath.FromSlash(rel))
			if strings.HasPrefix(full, filepath.Clean(dir)+string(filepath.Separator)) {
				t.Fatalf("%q resolves to %q, which is inside the folder - this case no longer tests anything", rel, full)
			}
		})
	}

	// Rooted paths are refused on every platform, although Join would treat
	// them as relative.
	for _, abs := range []string{"/etc/passwd", `\Windows\System32\x`, `C:\Windows\x`, "c:/windows/x"} {
		if err := Contained(dir, []string{abs}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Contained(%q) = %v, want ErrUnsafePath", abs, err)
		}
	}

	// A colon stays inside dir textually but writes an NTFS alternate data
	// stream, so it is refused on its own.
	for _, rel := range []string{"readme.txt:payload.exe", "sub:stream/a.mkv"} {
		if err := Contained(dir, []string{rel}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Contained(%q) = %v, want ErrUnsafePath", rel, err)
		}
	}
	// A backslash escapes only on Windows, so it cannot join the escaping list
	// with its platform-dependent self-check; it is refused everywhere.
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
