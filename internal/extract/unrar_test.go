package extract

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/extract/extracttest"
)

// The package's tests run on rardecode unless they ask for UnRAR, so a machine
// with UnRAR installed reads them the same way as one without.
func TestMain(m *testing.M) {
	UnrarPath = func() string { return "" }
	os.Exit(m.Run())
}

// withUnrar makes bin the UnRAR this test starts.
func withUnrar(t *testing.T, bin string) {
	t.Helper()
	prev := UnrarPath
	UnrarPath = func() string { return bin }
	t.Cleanup(func() { UnrarPath = prev })
}

// realUnrar is an UnRAR on this machine, from KL_UNRAR or PATH, or a skip.
func realUnrar(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("KL_UNRAR"); p != "" {
		return p
	}
	p, err := exec.LookPath(unrarName)
	if err != nil {
		t.Skip("no UnRAR on this machine")
	}
	return p
}

func TestUnrarIsAskedForNoPasswordWithoutOne(t *testing.T) {
	got := unrarArgs("/dl/set.part01.rar", "/dl/set/.unrar-1", "")
	if !slices.Contains(got, "-p-") || slices.ContainsFunc(got, func(a string) bool { return strings.HasPrefix(a, "-p") && a != "-p-" }) {
		t.Errorf("args %q, want -p- and no other password", got)
	}
	end := got[len(got)-3:]
	if end[0] != "--" || end[1] != "/dl/set.part01.rar" || !strings.HasSuffix(end[2], string(filepath.Separator)) {
		t.Errorf("args end in %q, want --, the archive and the folder with a trailing separator", end)
	}
}

func TestUnrarGetsThePasswordItTries(t *testing.T) {
	got := unrarArgs("a.rar", "out", "s3cret")
	if !slices.Contains(got, "-ps3cret") || slices.Contains(got, "-p-") {
		t.Errorf("args %q, want -ps3cret and not -p-", got)
	}
}

func TestUnrarRunsInAUTF8Locale(t *testing.T) {
	var all, lang string
	for _, kv := range unrarEnv() {
		if v, ok := strings.CutPrefix(kv, "LC_ALL="); ok {
			all = v
		}
		if v, ok := strings.CutPrefix(kv, "LANG="); ok {
			lang = v
		}
	}
	if !strings.HasSuffix(all, "UTF-8") || all != lang {
		t.Errorf("LC_ALL=%q LANG=%q, want one UTF-8 locale for both", all, lang)
	}
}

func TestUnrarFailuresReadLikeTheBuiltInReaders(t *testing.T) {
	const set = "/dl/Show.part01.rar"
	for _, c := range []struct {
		name   string
		code   int
		stderr string
		is     error
		part   string
	}{
		{"checksum", unrarCRC, "Show.mkv : checksum error", ErrDamaged, "Show.part01.rar"},
		{"bad archive", unrarBadArchive, "The archive is corrupt", ErrDamaged, "Show.part01.rar"},
		{"wrong password", unrarBadPassword, "The specified password is incorrect.", ErrPasswordRequired, ""},
		{"checksum from a wrong password", unrarCRC, "Checksum error in the encrypted file Show.mkv. Corrupt file or wrong password.", ErrPasswordRequired, ""},
		{"missing volume", unrarCRC, "Cannot find volume /dl/Show.part07.rar", ErrPartMissing, "Show.part07.rar"},
		{"disk full", unrarWrite, "Write error in the file Show.mkv\nNo space left on device", syscall.ENOSPC, ""},
		{"nothing inside", unrarNoFiles, "No files to extract", ErrUnsupported, "Show.part01.rar"},
	} {
		err := unrarError(c.code, c.stderr, set)
		if !errors.Is(err, c.is) {
			t.Errorf("%s: %v, want %v", c.name, err, c.is)
		}
		if got := PartOf(err); got != c.part {
			t.Errorf("%s: part %q, want %q", c.name, got, c.part)
		}
	}
}

// An UnRAR that cannot be started leaves the set to rardecode.
func TestARarSetUnpacksWhenUnrarDoesNotStart(t *testing.T) {
	withUnrar(t, filepath.Join(t.TempDir(), "no-such-unrar"))
	dir := t.TempDir()
	body := bytes.Repeat([]byte("0123456789abcdef"), 700)
	paths := extracttest.WriteRarSet(t, dir, "set", extracttest.RarSet(4096, extracttest.File{Name: "movie.bin", Body: body}))

	out, err := Run(context.Background(), Request{Path: paths[0]})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "set", "movie.bin")); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("movie.bin came out as %d bytes, %v", len(got), err)
	}
	if len(out.Volumes) != len(paths) {
		t.Errorf("Volumes = %v, want the %d parts", out.Volumes, len(paths))
	}
}

func TestUnrarUnpacksAMultiVolumeSet(t *testing.T) {
	withUnrar(t, realUnrar(t))
	dir := t.TempDir()
	movie := bytes.Repeat([]byte("0123456789abcdef"), 700)
	paths := extracttest.WriteRarSet(t, dir, "Höllenfahrt", extracttest.RarSet(4096,
		extracttest.File{Name: "Höllenfahrt/Höllenfahrt.mkv", Body: movie},
		extracttest.File{Name: "Höllenfahrt/Sample/sample.mkv", Body: []byte("sample\n")},
	))

	out, err := Run(context.Background(), Request{Path: paths[0]})
	if err != nil {
		t.Fatal(err)
	}
	// The folder named after the set is taken off, as the built-in reader does.
	if got, err := os.ReadFile(filepath.Join(dir, "Höllenfahrt", "Höllenfahrt.mkv")); err != nil || !bytes.Equal(got, movie) {
		t.Fatalf("Höllenfahrt.mkv came out as %d bytes, %v", len(got), err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "Höllenfahrt", "Sample", "sample.mkv")); err != nil || string(got) != "sample\n" {
		t.Fatalf("Sample/sample.mkv = %q, %v", got, err)
	}
	if out.Files != 2 || len(out.Volumes) != len(paths) {
		t.Errorf("files %d and volumes %v, want 2 and the %d parts", out.Files, out.Volumes, len(paths))
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "Höllenfahrt", ".unrar-*")); len(left) != 0 {
		t.Errorf("UnRAR's own folder was left behind: %v", left)
	}
}

func TestUnrarOpensAnEncryptedArchiveWithThePasswordThatFits(t *testing.T) {
	withUnrar(t, realUnrar(t))
	dir, path := encryptedRar(t, "locked.rar", rarContentsEncrypted)

	if _, err := (Options{Passwords: []string{"wrong", rarPassword}}).Extract(path); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "locked", "small.txt")); err != nil || string(got) != rarBody {
		t.Fatalf("small.txt = %q, %v", got, err)
	}
}

func TestUnrarSaysAPasswordIsNeeded(t *testing.T) {
	withUnrar(t, realUnrar(t))
	dir, path := encryptedRar(t, "locked.rar", rarHeadersEncrypted)

	if _, err := Run(context.Background(), Request{Path: path}); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("err = %v, want ErrPasswordRequired", err)
	}
	if exists(filepath.Join(dir, "locked")) {
		t.Error("the attempt without the password left its folder behind")
	}
}

func TestUnrarNamesTheBrokenVolume(t *testing.T) {
	withUnrar(t, realUnrar(t))
	dir := t.TempDir()
	movie := bytes.Repeat([]byte("0123456789abcdef"), 700)
	vols := extracttest.RarSet(4096, extracttest.File{Name: "movie.bin", Body: movie})
	paths := extracttest.WriteRarSet(t, dir, "set", vols)
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), Request{Path: paths[0]})
	if !errors.Is(err, ErrPartMissing) && !errors.Is(err, ErrDamaged) {
		t.Fatalf("err = %v, want a missing or damaged part", err)
	}
	if exists(filepath.Join(dir, "set")) {
		t.Error("the failed extraction left its folder behind")
	}
}
