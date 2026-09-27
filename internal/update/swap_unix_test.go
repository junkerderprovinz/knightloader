//go:build !windows

package update

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFileKeepsTheModeAndLeavesNoOld(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "prog")
	if err := os.WriteFile(prog, []byte("old"), 0o750); err != nil {
		t.Fatal(err)
	}
	download := filepath.Join(dir, downloadPrefix(prog)+"1")
	if err := os.WriteFile(download, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A running program on Linux keeps its inode, so holding the file open
	// stands in for it.
	running, err := os.Open(prog)
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()

	if err := replaceFile(prog, download); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(prog)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Errorf("mode %v, want 0750", info.Mode().Perm())
	}
	if got := readFile(t, prog); got != "new" {
		t.Errorf("program holds %q", got)
	}
	buf := make([]byte, 3)
	if _, err := running.ReadAt(buf, 0); err != nil || string(buf) != "old" {
		t.Errorf("the running copy reads %q, %v", buf, err)
	}
	if got := dirEntries(t, dir); len(got) != 1 {
		t.Errorf("folder holds %v", got)
	}
}

func TestFetchRefusesAFolderItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	srv := serve(t, release(t, "v1.4.0").withSums(keep))
	u := newUpdater(t, srv, "v1.3.0")
	dir := filepath.Dir(u.Path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	rel, err := u.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Fetch(t.Context(), rel); err == nil {
		t.Fatal("fetched into a read-only folder")
	}
}

func TestUnzipKeepsSymlinksInsideTheBundle(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	writeZip(t, archive, []zipEntry{
		{"Prog.app/Contents/Frameworks/A/lib", "lib", 0o644},
		{"Prog.app/Contents/Frameworks/Current", "A", fs.ModeSymlink | 0o777},
	})
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzip(archive, dest); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dest, "Prog.app", "Contents", "Frameworks", "Current")
	if target, err := os.Readlink(link); err != nil || target != "A" {
		t.Fatalf("link reads %q, %v", target, err)
	}
	if got := readFile(t, filepath.Join(link, "lib")); got != "lib" {
		t.Errorf("through the link: %q", got)
	}
}

func TestUnzipRefusesToWriteThroughALinkOutOfTheFolder(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	writeZip(t, archive, []zipEntry{
		{"Prog.app/out", "../..", fs.ModeSymlink | 0o777},
		{"Prog.app/out/evil", "x", 0o644},
	})
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzip(archive, dest); err == nil {
		t.Fatal("wrote through a link that leaves the folder")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("the file landed outside the folder")
	}
}
