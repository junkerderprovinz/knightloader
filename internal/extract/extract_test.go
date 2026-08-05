package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
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

// TestExtractCompressedTar pins that each codec's tar spelling is decompressed
// and then walked as a tar. If it failed, a whole archive family would have
// silently stopped unpacking, or would have landed as one unusable blob.
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

// TestExtractCompressedSingleFile pins that a compressed payload which is not a
// tar lands as one file named after the archive minus its compression suffix.
// The payload is deliberately larger than a tar header block, so the probe has
// to reject it on the header checksum rather than on "too short to be a tar".
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
			b, err := os.ReadFile(filepath.Join(dir, "notes.txt", "notes.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b, payload) {
				t.Fatalf("extracted %d bytes, want %d", len(b), len(payload))
			}
		})
	}
}

// TestExtractTgzWithoutTarFallsBackToSingleFile is one half of the
// content-beats-name rule: a ".tgz" that holds a single gzipped file and no tar
// still has to extract. If it failed we would be trusting the extension and
// handing the user a tar parse error for a perfectly good download.
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
	b, err := os.ReadFile(filepath.Join(dir, "blob", "blob"))
	if err != nil || string(b) != "not a tar at all" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
}

// TestExtractGzWithTarIsUnpacked is the other half: a plain ".gz" that does
// hold a tar gets walked. If it failed the user would get one opaque file named
// after the archive instead of the release they downloaded.
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

// TestExtractTar covers a bare .tar, the one shape with no compression layer in
// front of the tar walker.
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

// TestExtractTarSlipRejected pins zip-slip safety on the tar path. Tar stores
// entry names verbatim, so "../escape.txt" reaches safePath untouched; if this
// stopped erroring, any downloaded tar could overwrite files outside its own
// extraction directory.
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

// TestDestDirStripsArchiveSuffix pins the mapping from archive name to
// extraction directory. Getting it wrong is quiet and ugly: "data.tar.gz" would
// unpack into a folder called "data.tar".
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
