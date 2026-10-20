package backup

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// at is a fixed instant in UTC, offset by whole days.
func at(days int) time.Time {
	return time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC).AddDate(0, 0, days)
}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func names(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

func writeArchive(w io.Writer) error {
	_, err := w.Write([]byte("archive"))
	return err
}

func TestAutoNamesSortByTimeAndSayTheyAreAutomatic(t *testing.T) {
	early := AutoName(at(0), false)
	late := AutoName(at(1), true)
	if early != "knightloader-auto-20261001-030000.zip" {
		t.Errorf("AutoName = %q", early)
	}
	if late != "knightloader-auto-20261002-030000.zip.sealed" {
		t.Errorf("sealed AutoName = %q", late)
	}
	if !(early < late) {
		t.Errorf("%q does not sort before %q", early, late)
	}
	// A name made in another zone still reads as the same instant.
	local := at(0).In(time.FixedZone("CEST", 2*3600))
	if got := AutoName(local, false); got != early {
		t.Errorf("AutoName in another zone = %q, want %q", got, early)
	}
}

func TestParseAutoNameTakesOnlyWhatAutoNameMakes(t *testing.T) {
	for _, name := range []string{
		AutoName(at(0), false),
		AutoName(at(0), true),
	} {
		got, ok := ParseAutoName(name)
		if !ok || !got.Equal(at(0)) {
			t.Errorf("ParseAutoName(%q) = %v, %v", name, got, ok)
		}
	}
	for _, name := range []string{
		"knightloader-backup-20261001-030000.zip",
		"knightloader-auto-20261001-030000.zip.tmp",
		".knightloader-auto-20261001-030000.zip.tmp",
		"knightloader-auto-20261001-030000.tar",
		"knightloader-auto-2026101-030000.zip",
		"knightloader-auto-20261001-030000-copy.zip",
		"knightloader-auto-../../etc/passwd.zip",
		"notes.txt",
	} {
		if _, ok := ParseAutoName(name); ok {
			t.Errorf("ParseAutoName(%q) accepted a name AutoName never makes", name)
		}
	}
}

func TestListAutoReturnsTheAutomaticBackupsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, AutoName(at(0), false))
	touch(t, dir, AutoName(at(2), true))
	touch(t, dir, AutoName(at(1), false))
	touch(t, dir, "knightloader-backup-20261005-030000.zip")
	touch(t, dir, "readme.txt")
	if err := os.Mkdir(filepath.Join(dir, AutoName(at(9), false)), 0o700); err != nil {
		t.Fatal(err)
	}

	files, err := ListAuto(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Name)
	}
	want := []string{AutoName(at(2), true), AutoName(at(1), false), AutoName(at(0), false)}
	if !slices.Equal(got, want) {
		t.Errorf("ListAuto = %v, want %v", got, want)
	}
	if files[0].Bytes != 1 || !files[0].CreatedAt.Equal(at(2)) {
		t.Errorf("newest file = %+v", files[0])
	}
}

func TestListAutoOfAMissingFolderIsEmpty(t *testing.T) {
	files, err := ListAuto(filepath.Join(t.TempDir(), "nothing-here"))
	if err != nil || len(files) != 0 {
		t.Errorf("ListAuto of a missing folder = %v, %v", files, err)
	}
}

func TestTheNextBackupIsAnIntervalAfterTheNewestOnDisk(t *testing.T) {
	dir := t.TempDir()
	if next := NextAuto(nil, 7); !next.IsZero() {
		t.Errorf("with no backup on disk the next one is due at once, got %v", next)
	}
	touch(t, dir, AutoName(at(0), false))
	touch(t, dir, AutoName(at(3), false))
	// A backup made by hand on a later day does not move the schedule.
	touch(t, dir, "knightloader-backup-20261010-030000.zip")

	files, err := ListAuto(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := NextAuto(files, 7); !got.Equal(at(10)) {
		t.Errorf("NextAuto = %v, want %v", got, at(10))
	}
}

// A container that restarts every night starts with nothing in memory, and
// finds the schedule where the files on disk say it is.
func TestARestartBeforeTheIntervalMakesNoExtraBackup(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteAuto(dir, at(0), false, writeArchive); err != nil {
		t.Fatal(err)
	}
	for day := range 7 {
		files, err := ListAuto(dir)
		if err != nil {
			t.Fatal(err)
		}
		if boot := at(day).Add(time.Hour); !boot.Before(NextAuto(files, 7)) {
			t.Errorf("a start on day %d found a backup due", day)
		}
	}
	files, err := ListAuto(dir)
	if err != nil {
		t.Fatal(err)
	}
	if at(7).Before(NextAuto(files, 7)) {
		t.Error("a week after the last backup none is due")
	}
}

func TestWriteAutoLeavesOnlyTheFinishedArchive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), AutoDirName)
	file, err := WriteAuto(dir, at(0), true, writeArchive)
	if err != nil {
		t.Fatal(err)
	}
	if file.Name != AutoName(at(0), true) || file.Bytes != int64(len("archive")) {
		t.Errorf("WriteAuto = %+v", file)
	}
	if got := names(dir); !slices.Equal(got, []string{file.Name}) {
		t.Errorf("the folder holds %v, want only %s", got, file.Name)
	}
	b, err := os.ReadFile(filepath.Join(dir, file.Name))
	if err != nil || string(b) != "archive" {
		t.Errorf("the archive reads %q, %v", b, err)
	}
}

func TestAFailedWriteLeavesNoArchiveBehind(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, AutoName(at(-1), false))
	boom := errors.New("disk full")
	_, err := WriteAuto(dir, at(0), false, func(w io.Writer) error {
		if _, err := w.Write([]byte("half an arch")); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WriteAuto = %v, want the writer's error", err)
	}
	if got := names(dir); !slices.Equal(got, []string{AutoName(at(-1), false)}) {
		t.Errorf("after a failed write the folder holds %v", got)
	}
}

func TestWriteAutoClearsWhatAnInterruptedRunLeft(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "."+AutoName(at(-1), false)+".tmp")
	touch(t, dir, ".knightloader-auto-20260930-030000.db.tmp")
	touch(t, dir, ".hidden-of-someone-else.tmp")

	file, err := WriteAuto(dir, at(0), false, writeArchive)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".hidden-of-someone-else.tmp", file.Name}
	if got := names(dir); !slices.Equal(got, want) {
		t.Errorf("the folder holds %v, want %v", got, want)
	}
}

func TestPruneAutoKeepsTheNewestAndNothingElseIsTouched(t *testing.T) {
	dir := t.TempDir()
	for day := range 5 {
		touch(t, dir, AutoName(at(day), day%2 == 0))
	}
	manual := "knightloader-backup-20260901-030000.zip"
	foreign := []string{manual, "my-own-backup.zip", "knightloader-auto-old-copy.zip", "notes.txt"}
	for _, name := range foreign {
		touch(t, dir, name)
	}

	removed, err := PruneAuto(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	wantRemoved := []string{AutoName(at(2), true), AutoName(at(1), false), AutoName(at(0), true)}
	if !slices.Equal(removed, wantRemoved) {
		t.Errorf("PruneAuto removed %v, want %v", removed, wantRemoved)
	}
	want := append([]string{AutoName(at(3), false), AutoName(at(4), true)}, foreign...)
	slices.Sort(want)
	if got := names(dir); !slices.Equal(got, want) {
		t.Errorf("after pruning the folder holds %v, want %v", got, want)
	}
}

func TestPruneAutoWithFewerFilesThanKeptRemovesNothing(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, AutoName(at(0), false))
	removed, err := PruneAuto(dir, 7)
	if err != nil || len(removed) != 0 {
		t.Errorf("PruneAuto = %v, %v", removed, err)
	}
	if got := names(dir); len(got) != 1 || !strings.HasPrefix(got[0], "knightloader-auto-") {
		t.Errorf("the folder holds %v", got)
	}
}

func TestAnAutomaticBackupIsReadableByItsOwnerAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no unix modes")
	}
	dir := filepath.Join(t.TempDir(), AutoDirName)
	file, err := WriteAuto(dir, at(0), false, writeArchive)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, file.Name): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got&^want != 0 {
			t.Errorf("%s has mode %o, want no more than %o", path, got, want)
		}
	}
}
