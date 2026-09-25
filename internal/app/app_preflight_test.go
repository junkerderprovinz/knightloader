package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/startupcheck"
)

// probeGlob matches a probe file the check should not have left. It has to stay
// in step with startupcheck's unexported probePrefix.
const probeGlob = ".knightloader-startup-*"

// leftovers is every probe file lying in a directory.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, probeGlob))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// newPreflightApp is an app whose download folder is an empty directory apart
// from the data directory, so a leftover probe file is unambiguous.
func newPreflightApp(t *testing.T) (*App, string) {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	downloads := filepath.Join(t.TempDir(), "downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := a.Settings.Get()
	cfg.DownloadDir = downloads
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if got := a.Settings.Get().DownloadDir; got != downloads {
		t.Fatalf("the settings store refused the download folder: %q", got)
	}
	return a, downloads
}

// waitForReport polls until the boot pass has finished. a.Close would cancel
// a.ctx first and turn every row into a timeout.
func waitForReport(t *testing.T, a *App) *startupcheck.Report {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if rep := a.StartupReport(); rep != nil && rep.State == startupcheck.StateDone {
			return rep
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the start report never finished: %+v", a.StartupReport())
	return nil
}

// Nil means nothing looked, which differs from everything passing, and app.New
// must not start a check since hundreds of tests call it.
func TestStartupReportIsNilUntilSomethingStartsOne(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if rep := a.StartupReport(); rep != nil {
		t.Errorf("app.New produced a start report on its own: %+v", rep)
	}
}

// An empty check list alone cannot tell "switched off" from "found nothing".
func TestMarkStartupCheckOffIsNotAnEmptyPass(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	a.MarkStartupCheckOff()
	rep := a.StartupReport()
	if rep == nil {
		t.Fatal("no report at all after the check was marked off")
	}
	if rep.State != startupcheck.StateOff {
		t.Errorf("state = %q, want %q", rep.State, startupcheck.StateOff)
	}
	if rep.Checks == nil {
		t.Error("checks is nil, which encodes as JSON null and throws whatever walks it")
	}
}

// Every configured folder gets a row at boot, and none of them gets a file.
func TestBootPassLooksAtEverythingAndWritesNothing(t *testing.T) {
	t.Parallel()
	a, downloads := newPreflightApp(t)

	a.StartStartupCheck()
	rep := waitForReport(t, a)

	if rep.Probed {
		t.Error("the boot report says it wrote its test file; the boot pass only looks")
	}
	if got := leftovers(t, downloads); len(got) > 0 {
		t.Errorf("the boot pass left %v in the download folder", got)
	}
	if got := leftovers(t, a.DataDir); len(got) > 0 {
		t.Errorf("the boot pass left %v in the data directory", got)
	}

	byID := map[string]int{}
	for _, c := range rep.Checks {
		byID[c.ID]++
		if c.Verdict == "" {
			t.Errorf("row %q came back with no verdict at all", c.ID)
		}
		if c.Probed {
			t.Errorf("row %q says it was written to during a boot pass", c.Subject)
		}
	}
	for _, want := range []string{startupcheck.IDData, startupcheck.IDJava, startupcheck.IDYtdlp, startupcheck.IDFfmpeg, startupcheck.IDFfprobe, startupcheck.IDClock} {
		if byID[want] != 1 {
			t.Errorf("%d rows for %q, want exactly one: %v", byID[want], want, byID)
		}
	}
	if byID[startupcheck.IDFolder] < 1 {
		t.Errorf("no folder row at all, with a download folder configured: %v", byID)
	}
}

// The button runs the write test and keeps the stored boot reading, which is
// what a support thread needs.
func TestPressingCheckAgainWritesAndKeepsTheBootReading(t *testing.T) {
	t.Parallel()
	a, downloads := newPreflightApp(t)

	a.StartStartupCheck()
	boot := waitForReport(t, a)

	now := a.RunStartupCheckNow()
	if !now.Probed {
		t.Error("the pressed pass says it wrote nothing")
	}
	var probedFolders int
	for _, c := range now.Checks {
		if c.ID == startupcheck.IDFolder && c.Probed {
			probedFolders++
		}
	}
	if probedFolders < 1 {
		t.Errorf("no folder was actually written to: %+v", now.Checks)
	}
	if got := leftovers(t, downloads); len(got) > 0 {
		t.Errorf("the write test left %v behind", got)
	}

	after := a.StartupReport()
	if after == nil {
		t.Fatal("the boot report is gone")
	}
	if after.Probed {
		t.Error("the stored boot report now says it wrote a file; the press overwrote it")
	}
	if !after.StartedAt.Equal(boot.StartedAt) {
		t.Errorf("the stored report started at %s, want the boot reading's own %s", after.StartedAt, boot.StartedAt)
	}
}

// A category folder like "/mnt/user/media/<jd:packagename>" never exists as
// written, so it is checked at its fixed prefix instead of reporting a missing
// folder on every boot.
func TestStartupFoldersCutsTemplatesBackToARealPath(t *testing.T) {
	a, downloads := newPreflightApp(t)

	base := t.TempDir()
	serien := filepath.Join(base, "serien")
	cfg := a.Settings.Get()
	cfg.WorkDir = filepath.Join(base, "work")
	cfg.Categories = []settings.Category{
		{ID: "serien", Name: "Serien", Dir: filepath.Join(serien, "<jd:packagename>")},
		// The same folder as WorkDir: one row, and the first role that named it
		// wins.
		{ID: "arbeit", Name: "Arbeit", Dir: filepath.Join(base, "work")},
	}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	var dirs []string
	roles := map[string]string{}
	for _, f := range a.startupFolders() {
		dirs = append(dirs, f.Dir)
		if _, seen := roles[f.Dir]; !seen {
			roles[f.Dir] = f.Role
		}
	}

	want := []string{downloads, filepath.Join(base, "work"), serien}
	if len(dirs) != len(want) {
		t.Fatalf("folders = %v, want exactly %v (deduped, templates cut back)", dirs, want)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Errorf("folder %d = %q, want %q", i, dirs[i], want[i])
		}
	}
	if roles[filepath.Join(base, "work")] != startupcheck.RoleWork {
		t.Errorf("the shared folder took the role %q, want the first one that named it (%q)", roles[filepath.Join(base, "work")], startupcheck.RoleWork)
	}
	if roles[downloads] != startupcheck.RoleDownloads {
		t.Errorf("the download folder took the role %q", roles[downloads])
	}
}

// The diagnostics bundle goes into public bug reports, and a desktop data
// directory contains the user's name. The default download folder sits inside
// it. api's TestDiagnosticsShipsNoPaths pins the same rule from the other end.
func TestMaskReportKeepsTheDataDirectoryOutOfTheBundle(t *testing.T) {
	data := filepath.Join("C:", "Users", "somebody", "AppData", "KnightLoader")
	rep := startupcheck.Report{Checks: []startupcheck.Check{
		{
			ID:       startupcheck.IDFolder,
			Subject:  filepath.Join(data, "downloads", "films"),
			Measured: data,
			Err:      "open " + filepath.Join(data, "downloads") + ": permission denied",
		},
		{ID: startupcheck.IDClock, Detail: "CEST +02:00"},
	}}

	maskReport(&rep, data)

	for _, c := range rep.Checks {
		for field, v := range map[string]string{"subject": c.Subject, "measured": c.Measured, "detail": c.Detail, "err": c.Err} {
			if strings.Contains(v, data) {
				t.Errorf("%s still carries the data directory: %q", field, v)
			}
		}
	}
	if !strings.Contains(rep.Checks[0].Subject, dataDirMask) {
		t.Errorf("subject = %q, want the folder still readable with the data directory masked", rep.Checks[0].Subject)
	}
	if !strings.HasSuffix(rep.Checks[0].Subject, filepath.Join("downloads", "films")) {
		t.Errorf("subject = %q, want everything below the data directory kept", rep.Checks[0].Subject)
	}
	if rep.Checks[1].Detail != "CEST +02:00" {
		t.Errorf("an unrelated field was rewritten: %q", rep.Checks[1].Detail)
	}
}

// Go's error strings use the platform separator, while anything passed through
// filepath.ToSlash uses the other one.
func TestMaskReportCatchesBothSpellings(t *testing.T) {
	data := filepath.Join("C:", "kl", "data")
	slashed := filepath.ToSlash(data)
	if slashed == data {
		t.Skip("this platform spells a path only one way")
	}
	rep := startupcheck.Report{Checks: []startupcheck.Check{{Err: "stat " + slashed + "/downloads: no such file"}}}

	maskReport(&rep, data)
	if strings.Contains(rep.Checks[0].Err, slashed) {
		t.Errorf("err = %q, want the forward-slash spelling masked too", rep.Checks[0].Err)
	}
}

// A box that points at a JD sidecar needs no Java, so a missing one is not a
// failure there.
func TestJavaIsSkippedNotFailedWhereItIsNotNeeded(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		skipped bool
	}{
		{name: "nothing set, so this instance starts its own", env: map[string]string{"KL_JD": "", "KL_PROVISION_JD": ""}},
		{name: "KL_JD points somewhere else", env: map[string]string{"KL_JD": "http://jd.lan:3128", "KL_PROVISION_JD": ""}, skipped: true},
		{name: "provisioning switched off", env: map[string]string{"KL_JD": "", "KL_PROVISION_JD": "0"}, skipped: true},
		// main.go reads this with envInt: "1" is on and anything unparsable is
		// off, and the report has to read it the same way.
		{name: "provisioning explicitly on", env: map[string]string{"KL_JD": "", "KL_PROVISION_JD": "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got := javaNotNeeded() != ""
			if got != tc.skipped {
				t.Errorf("javaNotNeeded() skipped = %v, want %v", got, tc.skipped)
			}
		})
	}
}

// java takes two dashes and ffmpeg one; the wrong spelling reports an unknown
// version or a usage error for a healthy binary.
func TestStartupToolsAsksEachBinaryTheWayItAnswers(t *testing.T) {
	t.Setenv("KL_YTDLP", "")
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	want := map[string]string{
		startupcheck.IDJava:    "--version",
		startupcheck.IDYtdlp:   "--version",
		startupcheck.IDFfmpeg:  "-version",
		startupcheck.IDFfprobe: "-version",
	}
	seen := map[string]bool{}
	for _, tool := range a.startupTools() {
		seen[tool.ID] = true
		if len(tool.Args) != 1 || tool.Args[0] != want[tool.ID] {
			t.Errorf("%s is asked with %v, want [%s]", tool.ID, tool.Args, want[tool.ID])
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("%s is not looked for at all", id)
		}
	}
}

// The container image sets KL_YTDLP to an absolute path, so the check must look
// at that binary rather than yt-dlp on PATH.
func TestStartupToolsHonoursKLYTDLP(t *testing.T) {
	t.Setenv("KL_YTDLP", filepath.Join("opt", "bin", "yt-dlp-custom"))
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	for _, tool := range a.startupTools() {
		if tool.ID != startupcheck.IDYtdlp {
			continue
		}
		if tool.Bin != filepath.Join("opt", "bin", "yt-dlp-custom") {
			t.Errorf("yt-dlp is looked for as %q, want the KL_YTDLP value", tool.Bin)
		}
		return
	}
	t.Error("no yt-dlp row at all")
}
