package extract

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/extract/extracttest"
)

// Each failure below is one the interface words differently, with a different
// thing to do about it, so each has to come back as its own problem and with
// the file it happened in.
func TestAVolumeCutShortIsADamagedPartNamedAfterItself(t *testing.T) {
	dir := t.TempDir()
	movie := bytes.Repeat([]byte("0123456789abcdef"), 700)
	vols := extracttest.RarSet(4096, extracttest.File{Name: "movie.bin", Body: movie})
	vols[1] = leftover(vols[1], 1024)
	paths := extracttest.WriteRarSet(t, dir, "set", vols)

	_, err := Run(context.Background(), Request{Path: paths[0]})
	if !errors.Is(err, ErrDamaged) {
		t.Fatalf("err = %v, want ErrDamaged", err)
	}
	if got := PartOf(err); got != "set.part2.rar" {
		t.Errorf("PartOf = %q, want set.part2.rar", got)
	}
}

func TestAFirstPartThatIsNoRarAtAllIsDamaged(t *testing.T) {
	dir := t.TempDir()
	arc := write(t, filepath.Join(dir, "set.part1.rar"), []byte("<html><body>File not found</body></html>"))

	_, err := Run(context.Background(), Request{Path: arc})
	if !errors.Is(err, ErrDamaged) || PartOf(err) != "set.part1.rar" {
		t.Fatalf("err = %v (part %q), want ErrDamaged in set.part1.rar", err, PartOf(err))
	}
}

// The part to name is the one that is not there, not the one the reader had
// open when it looked for the next.
func TestAMissingVolumeIsNamed(t *testing.T) {
	dir := t.TempDir()
	movie := bytes.Repeat([]byte("0123456789abcdef"), 700)
	paths := extracttest.WriteRarSet(t, dir, "set", extracttest.RarSet(4096, extracttest.File{Name: "movie.bin", Body: movie}))
	if len(paths) < 3 {
		t.Fatalf("the set has %d volumes, want at least three", len(paths))
	}
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), Request{Path: paths[0]})
	if !errors.Is(err, ErrPartMissing) {
		t.Fatalf("err = %v, want ErrPartMissing", err)
	}
	if got := PartOf(err); got != "set.part2.rar" {
		t.Errorf("PartOf = %q, want set.part2.rar", got)
	}
}

func TestASplitSetWithoutItsSecondPartNamesThatPart(t *testing.T) {
	dir := t.TempDir()
	first := write(t, filepath.Join(dir, "film.mkv.001"), []byte("first"))

	_, err := Run(context.Background(), Request{Path: first})
	if !errors.Is(err, ErrPartMissing) || PartOf(err) != "film.mkv.002" {
		t.Fatalf("err = %v (part %q), want ErrPartMissing for film.mkv.002", err, PartOf(err))
	}
}

func TestAZipFailingItsChecksumIsDamaged(t *testing.T) {
	dir := t.TempDir()
	arc := write(t, filepath.Join(dir, "release.zip"), zipWithBadEntry(t,
		entry{"one.txt", []byte("fine")},
		entry{"two.txt", []byte("damaged")},
	))

	_, err := Run(context.Background(), Request{Path: arc})
	if !errors.Is(err, ErrDamaged) || PartOf(err) != "release.zip" {
		t.Fatalf("err = %v (part %q), want ErrDamaged in release.zip", err, PartOf(err))
	}
}

func TestAFormatThisBuildDoesNotReadIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	unknown := write(t, filepath.Join(dir, "notes.bin"), bytes.Repeat([]byte("x"), 64))
	retired := write(t, filepath.Join(dir, "dos.arj"), bytes.Repeat([]byte("x"), 64))

	for _, arc := range []string{unknown, retired} {
		_, err := Run(context.Background(), Request{Path: arc})
		if !errors.Is(err, ErrUnsupported) || PartOf(err) != filepath.Base(arc) {
			t.Errorf("err = %v (part %q), want ErrUnsupported in %s", err, PartOf(err), filepath.Base(arc))
		}
	}
}

// A write the disk turns down is about the disk, and the archive is fine: it
// must not read as a damaged part, or somebody downloads forty volumes again
// for a full drive.
func TestAFullDiskIsNotADamagedArchive(t *testing.T) {
	err := inPart(&os.PathError{Op: "write", Path: "/out/movie.bin", Err: syscall.ENOSPC}, "set.part1.rar")
	if errors.Is(err, ErrDamaged) || errors.Is(err, ErrPartMissing) || PartOf(err) != "" {
		t.Errorf("err = %v (part %q), want the write error as it was", err, PartOf(err))
	}
	if !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("err = %v, want the disk's own error kept", err)
	}
}

func TestAPasswordIsNotTiedToAPart(t *testing.T) {
	if err := inPart(ErrPasswordRequired, "set.part1.rar"); err != ErrPasswordRequired {
		t.Errorf("err = %v, want ErrPasswordRequired as it was", err)
	}
}

func TestThePartErrorSaysExtractOnce(t *testing.T) {
	err := partError(ErrUnsupported, "notes.bin")
	if got, want := err.Error(), "extract: notes.bin: unsupported archive"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
