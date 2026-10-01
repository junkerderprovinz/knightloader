package update

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

type zipEntry struct {
	name string
	body string
	mode fs.FileMode
}

func writeZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.SetMode(e.mode)
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSwapTradesTheWholeApp(t *testing.T) {
	dir := t.TempDir()
	// Renamed by its user, which the update keeps.
	app := filepath.Join(dir, "My Prog.app")
	oldExe := filepath.Join(app, "Contents", "MacOS", "prog")
	if err := os.MkdirAll(filepath.Dir(oldExe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldExe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "stale"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(dir, downloadPrefix(app)+"zip")
	writeZip(t, archive, []zipEntry{
		{"Prog.app/", "", fs.ModeDir | 0o755},
		{"Prog.app/Contents/Info.plist", "plist", 0o644},
		{"Prog.app/Contents/MacOS/prog", "new", 0o755},
	})

	u := &Updater{Path: app, Logf: t.Logf}
	if err := u.Swap(archive, &Release{Version: "1.4.0", asset: "prog.zip"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, oldExe); got != "new" {
		t.Errorf("program holds %q", got)
	}
	if got := readFile(t, filepath.Join(app, "Contents", "Info.plist")); got != "plist" {
		t.Errorf("Info.plist holds %q", got)
	}
	if _, err := os.Stat(filepath.Join(app, "Contents", "stale")); err == nil {
		t.Error("a file of the old bundle survived in the new one")
	}
	if got := readFile(t, filepath.Join(app+".old", "Contents", "MacOS", "prog")); got != "old" {
		t.Errorf("the old bundle holds %q", got)
	}

	os.Remove(archive)
	u.Cleanup()
	if got := dirEntries(t, dir); len(got) != 1 || got[0] != "My Prog.app" {
		t.Errorf("after cleanup the folder holds %v", got)
	}
}

// A second release can arrive before the restart that would pick up the first.
// The bundle this run started from is the .old one by then, and it has to stay
// until the next start.
func TestASecondSwapInOneRunKeepsTheRunningBundle(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Prog.app")
	exe := filepath.Join(app, "Contents", "MacOS", "prog")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("running"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := &Updater{Path: app, Logf: t.Logf}
	for _, version := range []string{"1.4.0", "1.5.0"} {
		archive := filepath.Join(dir, downloadPrefix(app)+"zip")
		writeZip(t, archive, []zipEntry{
			{"Prog.app/", "", fs.ModeDir | 0o755},
			{"Prog.app/Contents/MacOS/prog", version, 0o755},
		})
		if err := u.Swap(archive, &Release{Version: version, asset: "prog.zip"}); err != nil {
			t.Fatal(err)
		}
		os.Remove(archive)
	}

	if got := readFile(t, exe); got != "1.5.0" {
		t.Errorf("program holds %q, want the newest release", got)
	}
	if got := readFile(t, filepath.Join(app+".old", "Contents", "MacOS", "prog")); got != "running" {
		t.Errorf("the old bundle holds %q, want the one this run started from", got)
	}
	if got := dirEntries(t, dir); len(got) != 2 {
		t.Errorf("folder holds %v, want the app and the old bundle", got)
	}
}

func TestSwapKeepsTheAppWhenTheArchiveHasNone(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Prog.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "a.zip")
	writeZip(t, archive, []zipEntry{{"readme.txt", "x", 0o644}})
	u := &Updater{Path: app, Logf: t.Logf}
	if err := u.Swap(archive, &Release{Version: "1.4.0", asset: "a.zip"}); err == nil {
		t.Fatal("swapped in an archive without an app")
	}
	if got := dirEntries(t, dir); len(got) != 2 {
		t.Errorf("folder holds %v, want the app and the archive", got)
	}
}

func TestUnzipRefusesNamesOutsideTheFolder(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	writeZip(t, archive, []zipEntry{{"../evil", "x", 0o644}})
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzip(archive, dest); err == nil {
		t.Fatal("extracted a file outside the folder")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("the file landed outside the folder")
	}
}
