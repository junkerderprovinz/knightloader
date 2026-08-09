package extract

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
)

func touch(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func gone(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

// A settings file written by an older build spells the disposal as a boolean,
// and every value that is not one of the three words has to land on "keep":
// the one answer that cannot destroy an archive somebody still wants.
func TestParseDisposalFallsBackToKeeping(t *testing.T) {
	for _, in := range []string{"", "true", "false", "recycle", "DELETE ", " Trash"} {
		got := ParseDisposal(in)
		want := DefaultDisposal
		switch in {
		case "DELETE ":
			want = DisposalDelete
		case " Trash":
			want = DisposalTrash
		}
		if got != want {
			t.Errorf("ParseDisposal(%q) = %q, want %q", in, got, want)
		}
	}
}

// The collision menu has to be collide's list minus what an extraction cannot
// honour, and never a list of its own: a word offered here that ParseCollision
// folds away is a setting that saves cleanly and then does something else.
func TestCollisionsOffersOnlyWhatItKeeps(t *testing.T) {
	got := Collisions()
	if len(got) == 0 {
		t.Fatal("Collisions() is empty, so the chooser would offer nothing at all")
	}
	for _, p := range got {
		if ParseCollision(string(p)) != p {
			t.Errorf("Collisions() offers %q, which ParseCollision turns into %q", p, ParseCollision(string(p)))
		}
	}
	if slices.Contains(got, collide.Ask) {
		t.Error("Collisions() offers ask, which would park an extraction nobody can answer")
	}
	for _, p := range collide.Policies() {
		if ParseCollision(string(p)) == p && !slices.Contains(got, p) {
			t.Errorf("collide offers %q and this package keeps it, but the menu drops it", p)
		}
	}
}

// The capability line has to name what the readers really take. Every entry is
// put back through the gate the app itself uses, and every format the suffix
// table knows has to appear exactly once.
func TestFormatsAreAllStartableAndOnePerFormat(t *testing.T) {
	got := Formats()
	if len(got) == 0 {
		t.Fatal("Formats() is empty, so the page would claim this build opens nothing")
	}
	seen := map[archiveFormat]string{}
	for _, suffix := range got {
		if !Supported("archive" + suffix) {
			t.Errorf("Formats() names %q, which Supported refuses", suffix)
		}
		f := formatFromName(suffix)
		if prev, dup := seen[f]; dup {
			t.Errorf("Formats() names both %q and %q for one format", prev, suffix)
		}
		seen[f] = suffix
	}
	for _, s := range archiveSuffixes {
		if f := formatFromName(s); f != formatUnknown && Supported("archive"+s) {
			if _, ok := seen[f]; !ok {
				t.Errorf("the extractor opens %q and the capability line never mentions its format", s)
			}
		}
	}
	// The shortest spelling per format, not the longest: ".tar.gz" is a name,
	// ".gz" is the format.
	if !slices.Contains(got, ".gz") || slices.Contains(got, ".tar.gz") {
		t.Errorf("Formats() = %v, want the bare .gz rather than .tar.gz", got)
	}
}

// Keep must leave the file alone even when it is handed one, because the sweep
// and the disposal share a switch and "keep everything" has to mean it.
func TestDisposeKeepsWhenAskedTo(t *testing.T) {
	dir := t.TempDir()
	path := touch(t, filepath.Join(dir, "film.rar"))
	if err := (Options{Disposal: DisposalKeep}).Dispose([]string{path}); err != nil {
		t.Fatal(err)
	}
	if gone(t, path) {
		t.Error("the archive was removed under the keep disposal")
	}
}

// A file that somebody removed by hand between the extraction and the disposal
// is not a failure worth reporting: the outcome asked for is the outcome.
func TestDisposeToleratesAFileThatIsAlreadyGone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "never-there.rar")
	if err := (Options{Disposal: DisposalDelete}).Dispose([]string{path}); err != nil {
		t.Errorf("Dispose on a missing file = %v, want no error", err)
	}
}

func TestTrashMovesTheArchiveAndStampsIt(t *testing.T) {
	root := t.TempDir()
	path := touch(t, filepath.Join(root, "film.rar"))
	before := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, before, before); err != nil {
		t.Fatal(err)
	}

	o := Options{Disposal: DisposalTrash, TrashRoot: root}
	if err := o.Dispose([]string{path}); err != nil {
		t.Fatal(err)
	}
	if !gone(t, path) {
		t.Error("the archive is still where it was after being trashed")
	}
	moved := filepath.Join(root, TrashName, "film.rar")
	info, err := os.Stat(moved)
	if err != nil {
		t.Fatalf("the archive is not in the trash: %v", err)
	}
	// The stamp is what the sweep ages from. Without it an archive that was
	// downloaded a year ago is a year old the moment it is trashed, and the
	// first sweep takes it.
	if info.ModTime().Before(time.Now().Add(-time.Minute)) {
		t.Errorf("trashed at %v, which is the file's old timestamp rather than now", info.ModTime())
	}
}

// Two releases can carry an archive of the same name. The second one must not
// replace the first one's copy in the folder that exists to be able to give it
// back.
func TestTrashKeepsBothArchivesOfOneName(t *testing.T) {
	root := t.TempDir()
	o := Options{Disposal: DisposalTrash, TrashRoot: root}
	for _, sub := range []string{"a", "b"} {
		path := touch(t, filepath.Join(root, sub, "film.rar"))
		if err := o.Dispose([]string{path}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, TrashName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the trash holds %v, want both copies kept", names)
	}
}

func TestSweepTrashTakesOnlyWhatIsOldEnough(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, TrashName)
	old := touch(t, filepath.Join(dir, "old.rar"))
	fresh := touch(t, filepath.Join(dir, "fresh.rar"))
	stamp := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(old, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	n, err := SweepTrash(root, 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("swept %d, want 1", n)
	}
	if !gone(t, old) {
		t.Error("the old archive survived the sweep")
	}
	if gone(t, fresh) {
		t.Error("the sweep took an archive that was trashed today")
	}
}

// Zero retention is "keep it until I say so", which is a real answer and must
// not be read as "keep it for no time at all".
func TestSweepTrashDoesNothingWithoutARetention(t *testing.T) {
	root := t.TempDir()
	path := touch(t, filepath.Join(root, TrashName, "old.rar"))
	stamp := time.Now().Add(-9000 * time.Hour)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if n, err := SweepTrash(root, 0); err != nil || n != 0 {
		t.Fatalf("SweepTrash with no retention = %d, %v; want 0, nil", n, err)
	}
	if gone(t, path) {
		t.Error("the sweep ran with the retention switched off")
	}
}

// An install that has never trashed anything has no trash folder, and every
// extraction sweeps. That must not be an error on every single one of them.
func TestSweepTrashIsQuietWithoutAFolder(t *testing.T) {
	if n, err := SweepTrash(t.TempDir(), time.Hour); err != nil || n != 0 {
		t.Fatalf("SweepTrash with no trash folder = %d, %v; want 0, nil", n, err)
	}
}

func TestInfoFilesInPicksOnlyInfoFiles(t *testing.T) {
	files := []string{
		"/dl/Film/film.mkv", "/dl/Film/film.NFO", "/dl/Film/film.sfv",
		"/dl/Film/read.diz", "/dl/Film/site.url", "/dl/Film/film.rar",
	}
	o := Options{InfoFiles: true, Disposal: DisposalDelete}
	got := o.InfoFilesIn(files)
	want := []string{"/dl/Film/film.NFO", "/dl/Film/film.sfv", "/dl/Film/read.diz", "/dl/Film/site.url"}
	if !slices.Equal(got, want) {
		t.Errorf("InfoFilesIn = %v, want %v", got, want)
	}
}

// The sweep has no disposal of its own, so "keep everything" cannot mean "keep
// the archive and destroy the notes beside it".
func TestInfoFilesAreKeptWhenTheArchiveIs(t *testing.T) {
	o := Options{InfoFiles: true, Disposal: DisposalKeep}
	if got := o.InfoFilesIn([]string{"/dl/Film/film.nfo"}); got != nil {
		t.Errorf("InfoFilesIn under the keep disposal = %v, want nothing", got)
	}
}

func TestSkipRefusesAFolderThatIsAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "film.zip")
	if err := os.MkdirAll(filepath.Join(dir, "film"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Options{Collision: collide.Skip}.destination(path)
	if err == nil {
		t.Fatal("destination under skip accepted a folder that was already there")
	}
	if !errors.Is(err, ErrDestinationTaken) {
		t.Errorf("destination under skip = %v, want ErrDestinationTaken", err)
	}
}

func TestRenameStepsAsideAndOverwriteDoesNot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "film.zip")
	taken := filepath.Join(dir, "film")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}

	renamed, err := Options{Collision: collide.Rename}.destination(path)
	if err != nil {
		t.Fatal(err)
	}
	if renamed == taken {
		t.Errorf("rename chose %q, which is the folder it was supposed to step around", renamed)
	}
	// The claim is given up again so the extractor's own MkdirAll can have it.
	if _, err := os.Lstat(renamed); !os.IsNotExist(err) {
		t.Errorf("rename left %q on disk; the extractor has to be the one that creates it", renamed)
	}

	same, err := Options{Collision: collide.Overwrite}.destination(path)
	if err != nil {
		t.Fatal(err)
	}
	if same != taken {
		t.Errorf("overwrite chose %q, want the existing folder %q", same, taken)
	}
}

// Subfolder is meaningless without a collect folder: beside the archive the
// download folder is already the package folder, and a second one inside it
// would give "Films/Film/Film".
func TestSubfolderOnlyAppliesUnderACollectFolder(t *testing.T) {
	beside := Options{Subfolder: true, Package: "Films"}.baseDest("/dl/film.zip")
	if beside != filepath.Clean("/dl/film") {
		t.Errorf("baseDest without a destination = %q, want the folder beside the archive", beside)
	}
	collected := Options{Dest: filepath.FromSlash("/unpacked"), Subfolder: true, Package: "Films"}.baseDest("/dl/film.zip")
	if want := filepath.Join("/unpacked", "Films", "film"); collected != want {
		t.Errorf("baseDest = %q, want %q", collected, want)
	}
}
