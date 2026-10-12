package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// startsNothing reports a test failure when the stub backend is handed a
// download within a moment.
func startsNothing(t *testing.T, stub *stubBackend) {
	t.Helper()
	select {
	case id := <-stub.got:
		t.Errorf("download %s was started", id)
	case <-time.After(200 * time.Millisecond):
	}
}

func refusedForTheDataFolder(t *testing.T, a *App, id string) {
	t.Helper()
	got := liveTask(a, id)
	if got.Status != core.StatusError || !wordedAsDataFolder(got) {
		t.Errorf("the download reads %s %q (%s %v), want it refused for the data folder", got.Status, got.Error, got.ErrorCode, got.ErrorParams)
	}
}

// wordedAsDataFolder reports whether a row carries the code the interface
// words a data folder refusal by, with the folder it names.
func wordedAsDataFolder(task core.Task) bool {
	return task.ErrorCode == core.CodeDataFolder && task.ErrorParams["path"] != ""
}

// A folder of its own that leads into the data folder, set before the
// settings refused one or by a rule, is refused when the download would start.
func TestADownloadIsNotStartedIntoTheDataFolder(t *testing.T) {
	a, stub, _ := delegatedFileApp(t)
	queueTask(a, &core.Task{
		ID: "1", URL: "https://elsewhere.example/tokens.json", Name: "tokens.json", Resolver: "elsewhere",
		Status: core.StatusQueued, Enabled: true, Dir: filepath.Join(a.DataDir, "tools"),
	})
	startsNothing(t, stub)
	refusedForTheDataFolder(t, a, "1")
}

// A package named after the data folder puts its files there when the download
// folder holds it.
func TestAPackageFolderThatLeadsIntoTheDataFolderIsRefused(t *testing.T) {
	a, stub, _ := delegatedFileApp(t)
	s := a.Settings.Get()
	s.DownloadDir, s.SubfolderByPackage = filepath.Dir(a.DataDir), true
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	queueTask(a, &core.Task{
		ID: "1", URL: "https://elsewhere.example/settings.json", Name: "settings.json", Resolver: "elsewhere",
		Package: filepath.Base(a.DataDir), Status: core.StatusQueued, Enabled: true,
	})
	startsNothing(t, stub)
	refusedForTheDataFolder(t, a, "1")
}

// A download folder that holds the data folder is refused when the download
// would start, whatever the download is called.
func TestADownloadIsNotStartedInAFolderThatHoldsTheDataFolder(t *testing.T) {
	a, stub, _ := delegatedFileApp(t)
	s := a.Settings.Get()
	s.DownloadDir = filepath.Dir(a.DataDir)
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	queueTask(a, &core.Task{
		ID: "1", URL: "https://elsewhere.example/film.mkv", Name: "film.mkv", Resolver: "elsewhere",
		Status: core.StatusQueued, Enabled: true,
	})
	startsNothing(t, stub)
	got := liveTask(a, "1")
	if got.Status != core.StatusError || got.ErrorCode != core.CodeHoldsDataFolder || got.ErrorParams["path"] == "" {
		t.Errorf("the download reads %s %q (%s %v), want it refused for holding the data folder", got.Status, got.Error, got.ErrorCode, got.ErrorParams)
	}
}

func TestAFolderForOneDownloadIsRefusedInTheDataFolder(t *testing.T) {
	a, _, dir := delegatedFileApp(t)
	putTask(t, a, collectedTask("1", func(c *core.Task) {}))

	inside := filepath.Join(a.DataDir, "tools")
	var p *settings.PathProblem
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Dir: &inside}); !errors.As(err, &p) || p.Code != "dataFolder" {
		t.Errorf("a folder in the data folder was taken for a download: %v", err)
	}
	if got := liveTask(a, "1").Dir; got != "" {
		t.Errorf("the download's folder is %q after the refusal, want it unchanged", got)
	}
	allowed := filepath.Join(a.DataDir, settings.DownloadsDirName, "Films")
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Dir: &allowed}); err != nil {
		t.Errorf("a folder inside the built-in download folder was refused: %v", err)
	}
	if err := a.SetTaskOptions([]string{"1"}, TaskOptions{Dir: &dir}); err != nil {
		t.Errorf("the download folder was refused: %v", err)
	}
}

// A finished file that sits in the data folder, from before the folder was
// refused, is not renamed there: a rename writes a new name into that folder.
func TestAFileInTheDataFolderIsNotRenamedThere(t *testing.T) {
	a, _, _ := delegatedFileApp(t)
	file := fileBytes(t, filepath.Join(a.DataDir, "film.mkv"), 16)
	a.mu.Lock()
	a.tasks["1"] = &core.Task{
		ID: "1", URL: "https://elsewhere.example/film.mkv", Name: "film.mkv", Resolver: "elsewhere",
		Status: core.StatusDone, Enabled: true, Size: 16, File: file,
	}
	a.mu.Unlock()

	name := "renamed.mkv"
	_ = a.SetTaskOptions([]string{"1"}, TaskOptions{Name: &name})

	if fileExists(filepath.Join(a.DataDir, name)) || !fileExists(file) {
		t.Error("the file was renamed inside the data folder")
	}
	if got := liveTask(a, "1"); got.Name != "film.mkv" || !wordedAsDataFolder(got) {
		t.Errorf("the download reads %q with %q (%s), want the old name and the data folder refusal", got.Name, got.Error, got.ErrorCode)
	}
}

// Renaming a package whose folder is named after it moves that folder, and not
// to a name that puts it in the data folder.
func TestAPackageIsNotRenamedIntoTheDataFolder(t *testing.T) {
	a, _, _ := delegatedFileApp(t)
	parent := filepath.Dir(a.DataDir)
	s := a.Settings.Get()
	s.DownloadDir, s.SubfolderByPackage = parent, true
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	file := fileBytes(t, filepath.Join(parent, "Films", "film.mkv"), 16)
	a.mu.Lock()
	a.tasks["1"] = &core.Task{
		ID: "1", URL: "https://elsewhere.example/film.mkv", Name: "film.mkv", Resolver: "elsewhere",
		Package: "Films", Status: core.StatusDone, Enabled: true, Size: 16, File: file,
	}
	a.mu.Unlock()

	_, err := a.RenamePackage([]string{"1"}, filepath.Base(a.DataDir))
	var r *RenameRefusal
	if !errors.As(err, &r) || r.Code != "dataFolder" {
		t.Errorf("renaming the package after the data folder gave %v, want a dataFolder refusal", err)
	}
	if !fileExists(file) {
		t.Error("the package's file left its folder")
	}
}

// The extractor is told the data folder is barred; the extract package refuses
// a barred destination before writing.
func TestNothingIsUnpackedIntoTheDataFolder(t *testing.T) {
	a, _, _ := delegatedFileApp(t)
	opts := a.extractOptionsFor(nil, a.Settings.Get())
	if opts.Barred == nil || opts.Barred(filepath.Join(a.DataDir, "film")) == nil {
		t.Error("the extraction options do not bar the data folder")
	}
	if opts.Barred(filepath.Join(a.DataDir, settings.DownloadsDirName, "film")) != nil {
		t.Error("the extraction options bar the built-in download folder")
	}
	if opts.Barred(filepath.Dir(a.DataDir)) == nil {
		t.Error("the extraction options do not bar a folder that holds the data folder")
	}
	planted := filepath.Join(a.DataDir, "settings.json")
	if opts.BarredFile == nil || opts.BarredFile(planted) == nil {
		t.Error("the extraction options let a file be written into the data folder")
	}
	if move := a.moveOptions(nil, a.Settings.Get()); move.Barred == nil || move.Barred(planted) == nil {
		t.Error("the move options let a file be moved into the data folder")
	}
}

// An extraction folder or a move folder that leads into the data folder, as a
// hand-edited settings file or a rule can set, unpacks nothing there.
func TestAnArchiveIsNotUnpackedOrMovedIntoTheDataFolder(t *testing.T) {
	for _, setting := range []string{"extractTo", "extractMoveTo"} {
		t.Run(setting, func(t *testing.T) {
			a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
				s.Extract, s.VerifyChecksums = false, false
			})
			inside := filepath.Join(a.DataDir, "tools")
			s := a.Settings.Get()
			if setting == "extractTo" {
				s.ExtractTo = inside
			} else {
				s.ExtractMoveTo = inside
			}
			if _, err := a.ApplySettings(s); err != nil {
				t.Fatal(err)
			}
			writeZip(t, filepath.Join(base, "release.zip"), "yt-dlp", "not the real one")
			task := stageDone(t, a, "1", "release.zip")

			if err := a.StartExtraction([]string{task.ID}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the job settling", func() bool {
				j, ok := jobFor(a, task.ID)
				return ok && j.Status != ExtractQueued && j.Status != ExtractRunning
			})
			j, _ := jobFor(a, task.ID)
			if j.ErrorCode != core.CodeDataFolder {
				t.Errorf("the job ended %s with %q (%s), want the data folder refusal", j.Status, j.Error, j.ErrorCode)
			}
			if got := liveTask(a, task.ID); !wordedAsDataFolder(got) {
				t.Errorf("the row reads %q (%s %v), want the data folder refusal", got.Error, got.ErrorCode, got.ErrorParams)
			}
			for _, p := range []string{filepath.Join(inside, "yt-dlp"), filepath.Join(inside, "release", "yt-dlp")} {
				if fileExists(p) {
					t.Errorf("%s was written into the data folder", p)
				}
			}
		})
	}
}

// unpackAndSettle unpacks the archive of a finished download and waits for the
// job to end.
func unpackAndSettle(t *testing.T, a *App, task *core.Task) ExtractJob {
	t.Helper()
	if err := a.StartExtraction([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job settling", func() bool {
		j, ok := jobFor(a, task.ID)
		return ok && j.Status != ExtractQueued && j.Status != ExtractRunning
	})
	j, _ := jobFor(a, task.ID)
	return j
}

// notPlanted fails the test when the settings file in the data folder is the
// one an archive brought.
func notPlanted(t *testing.T, a *App) {
	t.Helper()
	if b, err := os.ReadFile(filepath.Join(a.DataDir, "settings.json")); err == nil && strings.Contains(string(b), "planted") {
		t.Errorf("the archive replaced the settings file: %s", b)
	}
}

// An extraction folder above the data folder, as a hand-edited settings file
// can set, would let AppData.zip unpack over the settings, the login and the
// tools. Nothing of it is written.
func TestAnArchiveAboveTheDataFolderDoesNotReachIt(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "AppData", "Roaming", "KnightLoader")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	a, base := newRuleAppIn(t, data, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		s.ExtractTo = home
	})
	writeZip(t, filepath.Join(base, "AppData.zip"), "Roaming/KnightLoader/settings.json", `{"planted":true}`)
	task := stageDone(t, a, "1", "AppData.zip")

	j := unpackAndSettle(t, a, task)
	if j.ErrorCode != core.CodeHoldsDataFolder {
		t.Errorf("the job ended %s with %q (%s), want the refusal for a folder holding the data folder", j.Status, j.Error, j.ErrorCode)
	}
	notPlanted(t, a)
}

// A link already in the folder an archive unpacks into, leading into the data
// folder, is followed when each file is checked.
func TestAnArchiveEntryIsNotWrittenThroughALinkIntoTheDataFolder(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
	})
	release := filepath.Join(base, "release")
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(release, "x")
	if err := os.Symlink(a.DataDir, link); err != nil {
		if out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, a.DataDir).CombinedOutput(); runtime.GOOS != "windows" || jerr != nil {
			t.Skipf("this system cannot make a link: %v %s", err, out)
		}
	}
	writeZip(t, filepath.Join(base, "release.zip"), "x/settings.json", `{"planted":true}`)
	task := stageDone(t, a, "1", "release.zip")

	j := unpackAndSettle(t, a, task)
	if j.ErrorCode != core.CodeDataFolder {
		t.Errorf("the job ended %s with %q (%s), want the data folder refusal", j.Status, j.Error, j.ErrorCode)
	}
	notPlanted(t, a)
}
