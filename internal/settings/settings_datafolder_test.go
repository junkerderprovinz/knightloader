package settings

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// linkTo makes link lead to dir: a symlink, or on Windows without the
// privilege for one a junction, which Go follows the same way.
func linkTo(t *testing.T, dir, link string) {
	t.Helper()
	err := os.Symlink(dir, link)
	if err == nil {
		return
	}
	if runtime.GOOS == "windows" && exec.Command("cmd", "/c", "mklink", "/J", link, dir).Run() == nil {
		return
	}
	t.Skipf("this system cannot make a link: %v", err)
}

// dataFolder is a data folder with its built-in download folder, as a running
// install has it.
func dataFolder(t *testing.T) string {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(data, DownloadsDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(data, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	return data
}

// The data folder holds the password hash, the tokens, the settings and the
// tools KnightLoader runs. No folder a download writes into may lie there,
// however it is spelled, except the built-in download folder.
func TestAFolderInTheDataFolderIsRefused(t *testing.T) {
	data := dataFolder(t)
	outside := t.TempDir()
	link := filepath.Join(outside, "into-data")
	linkTo(t, data, link)
	toolsLink := filepath.Join(outside, "into-tools")
	linkTo(t, filepath.Join(data, "tools"), toolsLink)

	for name, dir := range map[string]string{
		"the data folder":               data,
		"a folder of its own":           filepath.Join(data, "tools"),
		"a folder not made yet":         filepath.Join(data, "new", "deeper"),
		"a way back out of downloads":   filepath.Join(data, DownloadsDirName, "..", "tools"),
		"a link to it":                  link,
		"a folder behind a link":        filepath.Join(link, "tools"),
		"a template inside it":          filepath.Join(data, "x", "<jd:packagename>"),
		"a link to a folder inside it":  toolsLink,
		"a folder behind such a link":   filepath.Join(toolsLink, "new"),
		"a template behind such a link": filepath.Join(toolsLink, "<jd:packagename>"),
	} {
		if !InsideData(data, dir) {
			t.Errorf("%s (%s) is not seen as inside the data folder", name, dir)
		}
		var p *PathProblem
		if err := Validate(data, "the download folder", dir); !errors.As(err, &p) || p.Code != "dataFolder" {
			t.Errorf("%s (%s) was not refused as the data folder: %v", name, dir, err)
		}
	}
	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(filepath.Join(data, "tools"))
		if !InsideData(data, upper) {
			t.Errorf("%s, the data folder spelled in capitals, is not seen as inside it", upper)
		}
	}
}

func TestTheBuiltInDownloadFolderAndFoldersOutsideAreAllowed(t *testing.T) {
	data := dataFolder(t)
	for name, dir := range map[string]string{
		"the built-in download folder": filepath.Join(data, DownloadsDirName),
		"a folder inside it":           filepath.Join(data, DownloadsDirName, "Films"),
		"a folder beside the data":     filepath.Join(filepath.Dir(data), "downloads"),
		"the folder above the data":    filepath.Dir(data),
	} {
		if InsideData(data, dir) {
			t.Errorf("%s (%s) is seen as inside the data folder", name, dir)
		}
	}
}

// A folder above the data folder is refused like one inside it: an archive
// whose entries name the folders in between would write into the data folder.
func TestAFolderThatHoldsTheDataFolderIsRefused(t *testing.T) {
	data := dataFolder(t)
	parent := filepath.Dir(data)
	link := filepath.Join(t.TempDir(), "into-parent")
	linkTo(t, parent, link)
	for name, dir := range map[string]string{
		"the folder above it":      parent,
		"the folder above that":    filepath.Dir(parent),
		"the root of its disk":     filepath.VolumeName(data) + string(filepath.Separator),
		"a link to the folder":     link,
		"a template on the folder": filepath.Join(parent, "<jd:packagename>"),
	} {
		if !OverlapsData(data, dir) {
			t.Errorf("%s (%s) is not seen as holding the data folder", name, dir)
		}
		var p *PathProblem
		if err := Validate(data, "the download folder", dir); !errors.As(err, &p) || p.Code != "holdsDataFolder" {
			t.Errorf("%s (%s) was not refused for holding the data folder: %v", name, dir, err)
		}
	}
	for _, f := range folderFields {
		var s Settings
		switch f.key {
		case "downloadDir":
			s.DownloadDir = parent
		case "workDir":
			s.WorkDir = parent
		case "watchDir":
			s.WatchDir = parent
		case "extractTo":
			s.ExtractTo = parent
		case "extractMoveTo":
			s.ExtractMoveTo = parent
		default:
			t.Fatalf("no case for %s", f.key)
		}
		var p *PathProblem
		if err := CheckFolders(data, s, nil); !errors.As(err, &p) || p.Code != "holdsDataFolder" || p.Field != f.key {
			t.Errorf("%s above the data folder: %v, want a holdsDataFolder refusal for it", f.key, err)
		}
	}
	beside := filepath.Join(parent, "downloads")
	if OverlapsData(data, beside) {
		t.Errorf("%s, beside the data folder, is seen as overlapping it", beside)
	}
}

// A download folder that leads back to the data folder is no way around the
// rule.
func TestADownloadFolderLinkedToTheDataFolderIsNoException(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(data, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkTo(t, data, filepath.Join(data, DownloadsDirName))
	if !InsideData(data, filepath.Join(data, "tools")) {
		t.Error("a download folder that is a link to the data folder made the data folder allowed")
	}
}

// Every top-level folder a save can name is held to the rule, the ones only
// checked for being absolute as well.
func TestCheckFoldersRefusesEveryFolderInTheDataFolder(t *testing.T) {
	data := dataFolder(t)
	inside := filepath.Join(data, "tools")
	for _, f := range folderFields {
		var s Settings
		switch f.key {
		case "downloadDir":
			s.DownloadDir = inside
		case "workDir":
			s.WorkDir = inside
		case "watchDir":
			s.WatchDir = inside
		case "extractTo":
			s.ExtractTo = inside
		case "extractMoveTo":
			s.ExtractMoveTo = inside
		default:
			t.Fatalf("no case for %s", f.key)
		}
		var p *PathProblem
		if err := CheckFolders(data, s, nil); !errors.As(err, &p) || p.Code != "dataFolder" || p.Field != f.key {
			t.Errorf("%s in the data folder: %v, want a dataFolder refusal for it", f.key, err)
		}
	}
}

// TestTheGuardAnswersAsInsideDataDoes asks a DataGuard and InsideData about
// the same paths, a folder that becomes a link into the data folder after the
// guard has looked at it among them.
func TestTheGuardAnswersAsInsideDataDoes(t *testing.T) {
	data := dataFolder(t)
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "season"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := NewDataGuard(data)
	paths := []string{
		filepath.Join(out, "season", "e01.mkv"),
		filepath.Join(out, "new", "e01.mkv"),
		filepath.Join(data, "tools", "x.exe"),
		filepath.Join(data, "settings.json"),
		filepath.Join(data, DownloadsDirName, "film.mkv"),
		filepath.Join(data, DownloadsDirName),
		data,
	}
	for _, p := range paths {
		if got, want := g.Inside(p), InsideData(data, p); got != want {
			t.Errorf("guard says %v for %s, InsideData %v", got, p, want)
		}
	}
	swapped := filepath.Join(out, "season")
	if err := os.Remove(swapped); err != nil {
		t.Fatal(err)
	}
	linkTo(t, data, swapped)
	if !g.Inside(filepath.Join(swapped, "settings.json")) {
		t.Error("the guard kept its answer for a folder that became a link into the data folder")
	}
}

// TestTheGuardNoticesAFolderAboveTurnedIntoALink: the guard has looked at a
// folder, and the folder above it is then replaced by a link into the data
// folder, which holds a folder of the same name. A file in it counts as inside.
func TestTheGuardNoticesAFolderAboveTurnedIntoALink(t *testing.T) {
	data := dataFolder(t)
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "show", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := NewDataGuard(data)
	if g.Inside(filepath.Join(out, "show", "tools", "e01.mkv")) {
		t.Fatal("a folder outside the data folder counts as inside")
	}
	if err := os.RemoveAll(filepath.Join(out, "show")); err != nil {
		t.Fatal(err)
	}
	linkTo(t, data, filepath.Join(out, "show"))
	if !g.Inside(filepath.Join(out, "show", "tools", "e02.mkv")) {
		t.Error("the guard kept its answer for a folder whose parent became a link into the data folder")
	}
}

// TestTheGuardLooksAtEachFolderOnce: an archive of thousands of entries in a
// few folders is checked file by file, and the walk up the tree runs once per
// folder, not once per file.
func TestTheGuardLooksAtEachFolderOnce(t *testing.T) {
	data := dataFolder(t)
	out := t.TempDir()
	const folders, files = 20, 2000
	for i := range folders {
		if err := os.MkdirAll(filepath.Join(out, "d"+string(rune('a'+i)), "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	g := NewDataGuard(data)
	walked := map[string]int{}
	g.walk = func(dataDir, path string) bool {
		walked[path]++
		return InsideData(dataDir, path)
	}
	for i := range files {
		path := filepath.Join(out, "d"+string(rune('a'+i%folders)), "sub", "f"+strings.Repeat("x", i%7)+string(rune('0'+i%10))+".txt")
		if g.Inside(path) {
			t.Fatalf("%s counts as inside the data folder", path)
		}
	}
	if len(walked) != folders {
		t.Errorf("the guard walked %d paths for %d files in %d folders, want each folder once", len(walked), files, folders)
	}
	for path, n := range walked {
		if n != 1 {
			t.Errorf("the guard walked %s %d times, want once", path, n)
		}
	}
}
