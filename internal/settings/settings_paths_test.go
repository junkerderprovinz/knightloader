package settings

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The platform trap that makes the same template legal on Linux and illegal on
// Windows. Answering with the bare separator whenever nothing fixed stands in
// front of the first placeholder means "<jd:packagename>/unpacked" is accepted
// by the container build and refused by the desktop one, because
// filepath.IsAbs("/") is true and filepath.IsAbs(`\`) is false.
func TestFixedPrefixDoesNotInventARoot(t *testing.T) {
	sep := string(filepath.Separator)

	// Nothing fixed at all: the empty string, on every platform. Asserted on
	// the value rather than through filepath.IsAbs, which is the function whose
	// answer differs between the two, so a test written through it can only
	// fail on one of them.
	if got := fixedPrefix("<jd:packagename>" + sep + "unpacked"); got != "" {
		t.Errorf("fixedPrefix = %q for a template with no fixed part, want the empty string; returning a bare separator here is absolute on Linux and relative on Windows", got)
	}
	// A real root followed only by placeholders keeps its root.
	if got := fixedPrefix(sep + "<jd:packagename>"); got != sep {
		t.Errorf("fixedPrefix = %q for a rooted template, want the root %q", got, sep)
	}
	// A fixed head is returned unchanged.
	head := filepath.Join(sep+"serien", "neu")
	if got := fixedPrefix(head + sep + "<jd:packagename>"); got != head {
		t.Errorf("fixedPrefix = %q, want the fixed head %q", got, head)
	}
}

// The settings page saves while a path is still being typed, so every prefix of
// "D:\Downloads" reaches Validate on its way there. None of them may be left
// behind as a folder.
func TestValidateCreatesNoFolder(t *testing.T) {
	base := t.TempDir()
	for _, dir := range []string{
		filepath.Join(base, "Down"),
		filepath.Join(base, "a", "b", "c"),
		filepath.Join(base, "series", "<jd:packagename>"),
	} {
		if err := Validate("the download folder", dir); err != nil {
			t.Errorf("Validate(%q) = %v, want it accepted", dir, err)
		}
	}
	if left, err := os.ReadDir(base); err != nil || len(left) != 0 {
		t.Errorf("checking three missing folders left %v behind (%v)", left, err)
	}
}

func TestValidateStillProbesAFolderThatExists(t *testing.T) {
	dir := t.TempDir()
	if err := Validate("the download folder", dir); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("the write probe was left behind: %v", left)
	}
}

func TestValidateRefusesAFileInTheWay(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{file, filepath.Join(file, "sub")} {
		var p *PathProblem
		if err := Validate("the download folder", dir); !errors.As(err, &p) || p.Code != "cannotCreate" {
			t.Errorf("Validate(%q) = %v, want cannotCreate", dir, err)
		}
	}
}

func TestValidateRefusesAFolderThatCouldNotBeCreated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only mode does not stop creating folders on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root creates folders whatever the mode says")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	var p *PathProblem
	if err := Validate("the download folder", filepath.Join(locked, "new", "deeper")); !errors.As(err, &p) || p.Code != "cannotCreate" {
		t.Errorf("a folder below a read-only one answered %v, want cannotCreate", err)
	}
	if err := Validate("the download folder", locked); !errors.As(err, &p) || p.Code != "cannotWrite" {
		t.Errorf("a read-only folder answered %v, want cannotWrite", err)
	}
	if left, _ := os.ReadDir(locked); len(left) != 0 {
		t.Errorf("the refused checks left %v behind", left)
	}
}
