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

	for name, dir := range map[string]string{
		"the data folder":             data,
		"a folder of its own":         filepath.Join(data, "tools"),
		"a folder not made yet":       filepath.Join(data, "new", "deeper"),
		"a way back out of downloads": filepath.Join(data, DownloadsDirName, "..", "tools"),
		"a link to it":                link,
		"a folder behind a link":      filepath.Join(link, "tools"),
		"a template inside it":        filepath.Join(data, "x", "<jd:packagename>"),
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
