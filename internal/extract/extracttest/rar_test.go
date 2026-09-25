package extracttest

import (
	"bytes"
	"io"
	"testing"

	"github.com/nwaples/rardecode/v2"
)

// A set read back through the same reader the extractor uses: every file whole,
// every checksum passing, every volume visited.
func TestARarSetReadsBackWhole(t *testing.T) {
	files := []File{
		{"movie.bin", bytes.Repeat([]byte("0123456789abcdef"), 700)},
		{"docs/small.txt", []byte("hello\n")},
	}
	paths := WriteRarSet(t, t.TempDir(), "set", RarSet(4096, files...))
	if len(paths) != 3 {
		t.Fatalf("wrote %d volumes, want 3", len(paths))
	}

	rc, err := rardecode.OpenReader(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	for _, want := range files {
		h, err := rc.Next()
		if err != nil {
			t.Fatalf("before %s: %v", want.Name, err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("%s: %v", h.Name, err)
		}
		if h.Name != want.Name || !bytes.Equal(got, want.Body) {
			t.Errorf("read %s with %d bytes, want %s with %d", h.Name, len(got), want.Name, len(want.Body))
		}
	}
	if _, err := rc.Next(); err != io.EOF {
		t.Errorf("after the last file: %v, want the end of the set", err)
	}
	if n := len(rc.Volumes()); n != len(paths) {
		t.Errorf("the reader went through %d volumes, want %d", n, len(paths))
	}
}

// Everything that fits in one volume is a plain archive, not a set of one.
func TestWhatFitsInOneVolumeIsAPlainArchive(t *testing.T) {
	vols := RarSet(1<<20, File{"small.txt", []byte("hello\n")})
	if len(vols) != 1 {
		t.Fatalf("wrote %d volumes, want 1", len(vols))
	}
	r, err := rardecode.NewReader(bytes.NewReader(vols[0]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(r); err != nil || string(got) != "hello\n" {
		t.Fatalf("small.txt = %q, %v", got, err)
	}
}
