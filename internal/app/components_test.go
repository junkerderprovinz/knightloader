package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/jre"
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

// askedAgo backdates the last time the automatic update asked after id.
func askedAgo(a *App, id string, ago time.Duration) {
	a.componentChecks().mark(id, time.Now().Add(-ago))
}

// cadenceCase is one component with an App whose update of it only counts.
type cadenceCase struct {
	id    string
	every time.Duration
	app   func(t *testing.T) (*App, func() int32)
	run   func(a *App)
}

func cadenceCases() []cadenceCase {
	return []cadenceCase{
		{
			id: "yt-dlp", every: 24 * time.Hour,
			app: func(t *testing.T) (*App, func() int32) {
				a := autoUpdateApp(t, true)
				f := &fakeYtdlpRelease{tag: "2026.01.01", install: installsFine}
				f.wire(a)
				return a, f.asked.Load
			},
			run: func(a *App) { a.autoUpdateYtdlp() },
		},
		{
			id: "ffmpeg", every: 7 * 24 * time.Hour,
			app: func(t *testing.T) (*App, func() int32) {
				a, fetched := ffmpegApp(t)
				fetchedFFmpeg(t, a, time.Now())
				return a, fetched.Load
			},
			run: func(a *App) { a.autoUpdateFFmpeg() },
		},
		{
			id: "deno", every: 7 * 24 * time.Hour,
			app: func(t *testing.T) (*App, func() int32) {
				a, fetched := denoApp(t)
				fetchedDeno(t, a, time.Now())
				return a, fetched.Load
			},
			run: func(a *App) { a.autoUpdateDeno() },
		},
		{
			id: "java", every: 7 * 24 * time.Hour,
			app: func(t *testing.T) (*App, func() int32) {
				a := javaUpdateApp(t, true)
				f := &fakeAdoptium{}
				f.wire(t, a, jre.Version)
				return a, f.asked.Load
			},
			run: func(a *App) { a.autoUpdateJava() },
		},
		{
			id: "jdownloader", every: 7 * 24 * time.Hour,
			app: func(t *testing.T) (*App, func() int32) {
				a := jdAutoUpdateApp(t, true)
				f := &fakeJD{}
				f.wire(a)
				return a, f.checks.Load
			},
			run: func(a *App) { a.autoUpdateJD() },
		},
	}
}

func TestYtdlpIsAskedAfterDailyAndEveryOtherComponentWeekly(t *testing.T) {
	for _, c := range cadenceCases() {
		t.Run(c.id, func(t *testing.T) {
			a, asked := c.app(t)
			c.run(a)
			c.run(a)
			if asked() != 1 {
				t.Fatalf("asked %d times in a row, want once", asked())
			}
			askedAgo(a, c.id, c.every-time.Hour)
			c.run(a)
			if asked() != 1 {
				t.Fatalf("asked again an hour before it was due")
			}
			askedAgo(a, c.id, c.every+time.Hour)
			c.run(a)
			if asked() != 2 {
				t.Fatalf("asked %d times once it was due, want twice", asked())
			}
		})
	}
}

func TestTheLastAskOutlivesARestart(t *testing.T) {
	a, fetched := denoApp(t)
	fetchedDeno(t, a, time.Now())
	a.autoUpdateDeno()

	again, err := newApp(a.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { again.Close() })
	again.InstallMissingTools = true
	again.updateDeno = a.updateDeno
	again.autoUpdateDeno()
	if fetched.Load() != 1 {
		t.Fatalf("asked %d times across a restart within the week, want once", fetched.Load())
	}
}

func TestAMissingToolIsFetchedBeforeItIsDue(t *testing.T) {
	a, fetched := ffmpegApp(t)
	askedAgo(a, "ffmpeg", time.Hour)
	a.autoUpdateFFmpeg()
	if fetched.Load() != 1 {
		t.Fatalf("a missing ffmpeg was fetched %d times, want once", fetched.Load())
	}
}

func TestAnUpdateThatWaitsForADownloadIsTriedAgainAtTheNextLook(t *testing.T) {
	a := jdAutoUpdateApp(t, true)
	f := &fakeJD{available: true}
	f.wire(a)
	putTask(t, a, core.Task{ID: "jd", Resolver: "jd", Status: core.StatusRunning, Enabled: true})

	a.autoUpdateJD()
	a.autoUpdateJD()
	if f.checks.Load() != 2 {
		t.Fatalf("JD was asked %d times while its update waited, want it asked again", f.checks.Load())
	}
}

func TestUpdateNowRunsEveryComponentWhetherTheSwitchIsOnOrNot(t *testing.T) {
	a := autoUpdateApp(t, false)
	a.InstallMissingTools = true
	t.Setenv("PATH", t.TempDir())
	t.Setenv("KL_DENO", "")
	putRuntime(t, jre.Dir(a.DataDir), jre.Version, "old java")
	yt := &fakeYtdlpRelease{tag: "2026.09.01", install: installsFine}
	yt.wire(a)
	var ffmpeg, deno atomic.Int32
	a.updateFFmpeg = func(context.Context, string) (mediatools.FFmpegRecord, bool, error) {
		ffmpeg.Add(1)
		return mediatools.FFmpegRecord{Version: "N-2"}, false, nil
	}
	a.updateDeno = func(context.Context, string) (mediatools.DenoRecord, bool, error) {
		deno.Add(1)
		return mediatools.DenoRecord{Version: "2.9.8"}, true, nil
	}
	java := &fakeAdoptium{}
	java.wire(t, a, jre.Version)
	jd := &fakeJD{}
	jd.wire(a)

	a.StartComponentUpdate()
	waitFor(t, "Update now to finish", func() bool { return !a.ComponentUpdateState().Running })

	if yt.installs.Load() != 1 || jd.checks.Load() != 1 || java.asked.Load() != 1 {
		t.Errorf("fetched yt-dlp %d times, asked JD %d and Adoptium %d times, want each once", yt.installs.Load(), jd.checks.Load(), java.asked.Load())
	}
	want := map[string]string{
		"yt-dlp":      ComponentUpdated,
		"jdownloader": ComponentCurrent,
		"java":        ComponentCurrent,
	}
	if mediatools.CanInstallFFmpeg(a.DataDir) == nil {
		want["ffmpeg"] = ComponentUpdated
	}
	if mediatools.CanInstallDeno(a.DataDir) == nil {
		want["deno"] = ComponentCurrent
	}
	got := a.ComponentUpdateState().Results
	for id, state := range want {
		if got[id].State != state {
			t.Errorf("%s came to %q, want %q", id, got[id].State, state)
		}
	}
}

func TestUpdateNowLeavesTheDesktopsJavaToADesktop(t *testing.T) {
	a := autoUpdateApp(t, true)
	a.SwitchJava = nil
	(&fakeYtdlpRelease{tag: "2026.01.01", install: installsFine}).wire(a)
	(&fakeJD{}).wire(a)

	a.StartComponentUpdate()
	waitFor(t, "Update now to finish", func() bool { return !a.ComponentUpdateState().Running })

	if r, ok := a.ComponentUpdateState().Results["java"]; ok {
		t.Errorf("a build without a Java of its own reports Java as %+v", r)
	}
}
