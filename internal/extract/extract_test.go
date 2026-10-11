package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"github.com/junkerderprovinz/knightloader/internal/extract/extracttest"
)

func TestSupported(t *testing.T) {
	yes := []string{
		"a.zip", "a.rar", "a.part1.rar", "a.part01.rar", "b.7z", "b.7z.001",
		"c.tar.gz", "c.tgz", "UP.ZIP",
		"d.tar", "d.tar.bz2", "d.tbz2", "d.tbz", "d.tar.xz", "d.txz",
		"d.tar.zst", "d.tzst", "e.gz", "e.bz2", "e.xz", "e.zst", "REPORT.TAR.XZ",
	}
	no := []string{
		"a.part2.rar", "a.part02.rar", "a.r00", "a.r01", "b.7z.002",
		"movie.mkv", "x.txt", "a.z01", "b.7z.003", "c.tar.gz.002",
	}
	for _, n := range yes {
		if !Supported(n) {
			t.Errorf("Supported(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if Supported(n) {
			t.Errorf("Supported(%q) = true, want false", n)
		}
	}
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "bundle.zip")
	f, _ := os.Create(arc)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("docs/readme.txt")
	_, _ = w.Write([]byte("hello"))
	w2, _ := zw.Create("top.bin")
	_, _ = w2.Write([]byte{1, 2, 3})
	_ = zw.Close()
	_ = f.Close()

	res, err := Extract(arc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 {
		t.Fatalf("Files = %d, want 2", res.Files)
	}
	b, err := os.ReadFile(filepath.Join(dir, "bundle", "docs", "readme.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

func TestExtractZipSlipRejected(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "evil.zip")
	f, _ := os.Create(arc)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../escape.txt")
	_, _ = w.Write([]byte("pwn"))
	_ = zw.Close()
	_ = f.Close()

	if _, err := Extract(arc); err == nil {
		t.Fatal("zip-slip entry extracted without error")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("zip-slip file escaped the destination dir")
	}
}

func TestExtractTarGz(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "data.tar.gz")
	f, _ := os.Create(arc)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("tar-content")
	_ = tw.WriteHeader(&tar.Header{Name: "nested/file.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	res, err := Extract(arc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 {
		t.Fatalf("Files = %d, want 1", res.Files)
	}
	b, err := os.ReadFile(filepath.Join(dir, "data", "nested", "file.txt"))
	if err != nil || string(b) != "tar-content" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

// tarStream builds a tar holding a single regular file. Keeping it in memory
// lets the same bytes be fed to a plain .tar and to every compressor below.
func tarStream(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzipWriter(_ *testing.T, dst io.Writer) io.WriteCloser { return gzip.NewWriter(dst) }

func bzip2Writer(t *testing.T, dst io.Writer) io.WriteCloser {
	t.Helper()
	// The stdlib only decompresses bzip2, so the test needs a third-party
	// encoder to produce something extract.go can be pointed at.
	w, err := bzip2.NewWriter(dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func xzWriter(t *testing.T, dst io.Writer) io.WriteCloser {
	t.Helper()
	w, err := xz.NewWriter(dst)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func zstdWriter(t *testing.T, dst io.Writer) io.WriteCloser {
	t.Helper()
	w, err := zstd.NewWriter(dst)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// codecs is the writer side of every single-stream compression the package can
// open, so one table drives both the "stream is a tar" and the "stream is one
// plain file" case for each of them.
var codecs = []struct {
	name      string
	tarSuffix string // spelling used when the stream holds a tar
	rawSuffix string // spelling used when the stream holds one plain file
	compress  func(t *testing.T, dst io.Writer) io.WriteCloser
}{
	{"gzip", ".tar.gz", ".gz", gzipWriter},
	{"bzip2", ".tar.bz2", ".bz2", bzip2Writer},
	{"xz", ".tar.xz", ".xz", xzWriter},
	{"zstd", ".tar.zst", ".zst", zstdWriter},
}

// writeCompressed writes payload through compress into a new file at path.
func writeCompressed(t *testing.T, path string, compress func(*testing.T, io.Writer) io.WriteCloser, payload []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := compress(t, f)
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// Each codec's tar spelling is decompressed and then walked as a tar.
func TestExtractCompressedTar(t *testing.T) {
	for _, c := range codecs {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, "data"+c.tarSuffix)
			writeCompressed(t, arc, c.compress, tarStream(t, "nested/file.txt", []byte("tar-content")))

			res, err := Extract(arc)
			if err != nil {
				t.Fatal(err)
			}
			if res.Files != 1 {
				t.Fatalf("Files = %d, want 1", res.Files)
			}
			if len(res.Volumes) != 1 || res.Volumes[0] != arc {
				t.Fatalf("Volumes = %v, want [%s]", res.Volumes, arc)
			}
			b, err := os.ReadFile(filepath.Join(dir, "data", "nested", "file.txt"))
			if err != nil || string(b) != "tar-content" {
				t.Fatalf("extracted content = %q, %v", b, err)
			}
		})
	}
}

// A compressed payload that is not a tar lands as one file named after the
// archive minus its compression suffix. The payload is larger than a tar header
// block, so the probe rejects it on the header checksum rather than on its
// length.
func TestExtractCompressedSingleFile(t *testing.T) {
	payload := bytes.Repeat([]byte("plain payload "), 200)
	for _, c := range codecs {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, "notes.txt"+c.rawSuffix)
			writeCompressed(t, arc, c.compress, payload)

			res, err := Extract(arc)
			if err != nil {
				t.Fatal(err)
			}
			if res.Files != 1 {
				t.Fatalf("Files = %d, want 1", res.Files)
			}
			if len(res.Volumes) != 1 || res.Volumes[0] != arc {
				t.Fatalf("Volumes = %v, want [%s]", res.Volumes, arc)
			}
			// Beside the archive rather than inside a folder named after the
			// file it produces, which would collide with a sibling of that name.
			b, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b, payload) {
				t.Fatalf("extracted %d bytes, want %d", len(b), len(payload))
			}
		})
	}
}

// One half of the content-beats-name rule: a ".tgz" holding a single gzipped
// file and no tar still extracts, rather than answering with a tar parse error.
func TestExtractTgzWithoutTarFallsBackToSingleFile(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "blob.tgz")
	writeCompressed(t, arc, gzipWriter, []byte("not a tar at all"))

	res, err := Extract(arc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 {
		t.Fatalf("Files = %d, want 1", res.Files)
	}
	b, err := os.ReadFile(filepath.Join(dir, "blob"))
	if err != nil || string(b) != "not a tar at all" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

// The other half: a plain ".gz" that does hold a tar gets walked, rather than
// landing as one opaque file named after the archive.
func TestExtractGzWithTarIsUnpacked(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "release.gz")
	writeCompressed(t, arc, gzipWriter, tarStream(t, "bin/tool", []byte("payload")))

	res, err := Extract(arc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 {
		t.Fatalf("Files = %d, want 1", res.Files)
	}
	b, err := os.ReadFile(filepath.Join(dir, "release", "bin", "tool"))
	if err != nil || string(b) != "payload" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

// A bare .tar, the one shape with no compression layer in front of the walker.
func TestExtractTar(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "plain.tar")
	if err := os.WriteFile(arc, tarStream(t, "docs/readme.txt", []byte("bare-tar")), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Extract(arc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 {
		t.Fatalf("Files = %d, want 1", res.Files)
	}
	if len(res.Volumes) != 1 || res.Volumes[0] != arc {
		t.Fatalf("Volumes = %v, want [%s]", res.Volumes, arc)
	}
	b, err := os.ReadFile(filepath.Join(dir, "plain", "docs", "readme.txt"))
	if err != nil || string(b) != "bare-tar" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

// Tar stores entry names verbatim, so "../escape.txt" reaches safePath
// untouched and a downloaded tar could otherwise write outside its own folder.
func TestExtractTarSlipRejected(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "evil.tar")
	if err := os.WriteFile(arc, tarStream(t, "../escape.txt", []byte("pwn")), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Extract(arc); err == nil {
		t.Fatal("tar-slip entry extracted without error")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("tar-slip file escaped the destination dir")
	}
}

// The mapping from archive name to extraction directory, where getting it wrong
// unpacks "data.tar.gz" into a folder called "data.tar".
func TestDestDirStripsArchiveSuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"data.tar.gz", "data"},
		{"data.tgz", "data"},
		{"data.tar.bz2", "data"},
		{"data.tbz2", "data"},
		{"data.tar.xz", "data"},
		{"data.txz", "data"},
		{"data.tar.zst", "data"},
		{"data.tzst", "data"},
		{"data.tar", "data"},
		{"notes.txt.gz", "notes.txt"},
		{"notes.txt.zst", "notes.txt"},
		{"bundle.zip", "bundle"},
		{"film.part01.rar", "film"},
		{"set.7z.001", "set"},
	}
	for _, c := range cases {
		if got := filepath.Base(destDir(filepath.Join("dl", c.in))); got != c.want {
			t.Errorf("destDir(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A "dump.sql.gz" downloaded next to an existing "dump.sql": creating a
// directory of that name would fail and report a routine download as a broken
// archive.
func TestSingleStreamDoesNotCollideWithASibling(t *testing.T) {
	dir := t.TempDir()
	sibling := filepath.Join(dir, "dump.sql")
	if err := os.WriteFile(sibling, []byte("older dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	arc := filepath.Join(dir, "dump.sql.gz")
	writeCompressed(t, arc, gzipWriter, []byte("newer dump"))

	res, err := Extract(arc)
	if err != nil {
		t.Fatalf("extracting beside an existing file of the target name failed: %v", err)
	}
	if res.Files != 1 {
		t.Fatalf("Files = %d, want 1", res.Files)
	}
	b, err := os.ReadFile(sibling)
	if err != nil || string(b) != "newer dump" {
		t.Fatalf("sibling holds %q, %v", b, err)
	}
	// No stray folder may be left behind.
	if fi, err := os.Stat(sibling); err == nil && fi.IsDir() {
		t.Error("the target became a directory")
	}
}

// An archive named after a folder it must not reach, unpacked beside itself,
// is refused before anything is written, and so is one sent there by Dest.
func TestABarredDestinationIsRefusedBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "data.zip")
	f, _ := os.Create(arc)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("auth.json")
	_, _ = w.Write([]byte(`{}`))
	_ = zw.Close()
	_ = f.Close()
	barred := filepath.Join(dir, "data")
	errBarred := errors.New("barred")
	isBarred := func(d string) error {
		if filepath.Clean(d) == barred {
			return errBarred
		}
		return nil
	}

	for name, o := range map[string]Options{
		"beside the archive": {Barred: isBarred},
		"through Dest":       {Dest: dir, Barred: isBarred},
	} {
		var b *BarredError
		if _, err := o.Extract(arc); !errors.As(err, &b) || b.Dir != barred || !errors.Is(err, errBarred) {
			t.Errorf("unpacking %s into a barred folder gave %v, want a BarredError naming %s", name, err, barred)
		}
		if _, err := os.Stat(barred); !os.IsNotExist(err) {
			t.Fatalf("unpacking %s wrote into the barred folder: %v", name, err)
		}
	}
}

// An archive unpacked into a folder above a barred one reaches it through the
// folders its entries name. Each file is checked where it would land, before
// anything of it is written, whatever format carries it.
func TestAnEntryThatReachesABarredFolderIsRefused(t *testing.T) {
	const entry = "Roaming/KnightLoader/settings.json"
	body := []byte(`{"planted":true}`)
	writers := map[string]func(t *testing.T, dir string) string{
		"zip": func(t *testing.T, dir string) string {
			arc := filepath.Join(dir, "AppData.zip")
			f, err := os.Create(arc)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			w, _ := zw.Create(entry)
			_, _ = w.Write(body)
			_ = zw.Close()
			_ = f.Close()
			return arc
		},
		"tar": func(t *testing.T, dir string) string {
			arc := filepath.Join(dir, "AppData.tar")
			if err := os.WriteFile(arc, tarStream(t, entry, body), 0o644); err != nil {
				t.Fatal(err)
			}
			return arc
		},
		"tar.gz": func(t *testing.T, dir string) string {
			arc := filepath.Join(dir, "AppData.tar.gz")
			writeCompressed(t, arc, gzipWriter, tarStream(t, entry, body))
			return arc
		},
		"rar": func(t *testing.T, dir string) string {
			vols := extracttest.RarSet(4096, extracttest.File{Name: entry, Body: body})
			return extracttest.WriteRarSet(t, dir, "AppData", vols)[0]
		},
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			barred := filepath.Join(parent, "AppData", "Roaming", "KnightLoader")
			errBarred := errors.New("barred")
			o := Options{
				Dest: parent,
				BarredFile: func(p string) error {
					if rel, err := filepath.Rel(barred, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
						return errBarred
					}
					return nil
				},
			}
			_, err := o.Extract(write(t, t.TempDir()))
			var b *BarredError
			if !errors.As(err, &b) || !errors.Is(err, errBarred) || b.Dir != filepath.Join(barred, "settings.json") {
				t.Errorf("unpacking gave %v, want a BarredError naming %s", err, filepath.Join(barred, "settings.json"))
			}
			if _, err := os.Stat(filepath.Join(barred, "settings.json")); !os.IsNotExist(err) {
				t.Errorf("the entry was written into the barred folder: %v", err)
			}
		})
	}
}

// A single compressed file is written beside its archive, and its name is
// checked there like any entry's.
func TestASingleCompressedFileIsCheckedWhereItLands(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "settings.json.gz")
	writeCompressed(t, arc, gzipWriter, []byte(`{"planted":true}`))
	target := filepath.Join(dir, "settings.json")
	errBarred := errors.New("barred")
	o := Options{BarredFile: func(p string) error {
		if p == target {
			return errBarred
		}
		return nil
	}}
	if _, err := o.Extract(arc); !errors.Is(err, errBarred) {
		t.Errorf("unpacking gave %v, want the file refused", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("the file was written: %v", err)
	}
}
