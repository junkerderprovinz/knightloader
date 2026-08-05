package extract

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestSupported(t *testing.T) {
	yes := []string{"a.zip", "a.rar", "a.part1.rar", "a.part01.rar", "b.7z", "b.7z.001", "c.tar.gz", "c.tgz", "UP.ZIP"}
	no := []string{"a.part2.rar", "a.part02.rar", "a.r00", "a.r01", "b.7z.002", "movie.mkv", "x.txt", "a.z01"}
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
