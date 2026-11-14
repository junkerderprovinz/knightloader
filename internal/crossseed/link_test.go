package crossseed

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func seedFiles(t *testing.T) (string, []FileEntry) {
	t.Helper()
	dir := t.TempDir()
	files := []FileEntry{{"Show.S01/f0.mkv", 3}, {"Show.S01/sub/f1.srt", 2}}
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, f.Size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, files
}

func TestHardlinksPutTheSameFilesInTheNewFolder(t *testing.T) {
	src, files := seedFiles(t)
	dst := filepath.Join(filepath.Dir(src), "linked-"+filepath.Base(src))
	t.Cleanup(func() { os.RemoveAll(dst) })
	if err := Link(LinkHard, src, dst, files); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		a, _ := os.Stat(filepath.Join(src, filepath.FromSlash(f.Path)))
		b, err := os.Stat(filepath.Join(dst, filepath.FromSlash(f.Path)))
		if err != nil || !os.SameFile(a, b) {
			t.Fatalf("%s is not the same file in both folders: %v", f.Path, err)
		}
	}
}

func TestLinkingAgainLeavesWhatIsThere(t *testing.T) {
	src, files := seedFiles(t)
	dst := filepath.Join(filepath.Dir(src), "again-"+filepath.Base(src))
	t.Cleanup(func() { os.RemoveAll(dst) })
	if err := Link(LinkHard, src, dst, files); err != nil {
		t.Fatal(err)
	}
	if err := Link(LinkHard, src, dst, files); err != nil {
		t.Fatalf("a second run failed: %v", err)
	}
}

func TestAFileOfAnotherSizeInTheWayIsNotReplaced(t *testing.T) {
	src, files := seedFiles(t)
	dst := t.TempDir()
	p := filepath.Join(dst, "Show.S01", "f0.mkv")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("somebody else's"), 0o644)
	if err := Link(LinkHard, src, dst, files); err == nil {
		t.Fatal("a file in the way was replaced")
	}
	if b, _ := os.ReadFile(p); string(b) != "somebody else's" {
		t.Fatal("the file in the way was changed")
	}
}

func TestAPathThatClimbsOutIsRefused(t *testing.T) {
	src, _ := seedFiles(t)
	err := Link(LinkHard, src, t.TempDir(), []FileEntry{{"../escape", 1}})
	if err == nil {
		t.Fatal("a path out of the folder was linked")
	}
}

func TestHardlinksAcrossFilesystemsAreRefusedWithTheReason(t *testing.T) {
	defer func(f func(a, b string) (bool, error)) { sameFilesystem = f }(sameFilesystem)
	sameFilesystem = func(a, b string) (bool, error) { return false, nil }
	src, files := seedFiles(t)
	err := Link(LinkHard, src, t.TempDir(), files)
	if !errors.Is(err, ErrOtherFilesystem) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheSamePathLinksNothing(t *testing.T) {
	src, files := seedFiles(t)
	if err := Link(LinkNone, src, filepath.Join(src, "never"), files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "never")); !os.IsNotExist(err) {
		t.Fatal("something was made")
	}
}

func TestCheckFilesFindsAFileMissingOrCutShort(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "12345")
	if err := CheckFiles(dir, []FileEntry{{"a", 5}}); err != nil {
		t.Fatal(err)
	}
	for _, files := range [][]FileEntry{{{"a", 6}}, {{"b", 1}}, {{"../a", 5}}} {
		if err := CheckFiles(dir, files); err == nil {
			t.Errorf("%+v passed the check", files)
		}
	}
}
