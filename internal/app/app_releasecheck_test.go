package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/update"
)

// fakeRelease stands in for GitHub: every check answers with info, and the
// checks are counted.
type fakeRelease struct {
	info   update.Info
	checks atomic.Int32
}

func (f *fakeRelease) wire(a *App) {
	a.releaseVersion = "v2.7.5"
	a.checkRelease = func(context.Context) update.Info {
		f.checks.Add(1)
		return f.info
	}
}

var newerRelease = update.Info{
	Checked:   true,
	Available: true,
	Current:   "v2.7.5",
	Latest:    "v2.8.0",
	URL:       "https://github.com/junkerderprovinz/knightloader/releases/tag/v2.8.0",
}

// releaseCheckApp builds an App on dir with the release check switched on or off.
func releaseCheckApp(t *testing.T, dir string, on bool) *App {
	t.Helper()
	t.Setenv("KL_JD", "")
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	s := a.Settings.Get()
	s.AutoUpdateCheck = on
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTheReleaseCheckRemembersWhatItFoundAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	first := releaseCheckApp(t, dir, true)
	f := &fakeRelease{info: newerRelease}
	f.wire(first)
	first.releaseNotices.Store(true)
	first.CheckRelease(context.Background())
	first.Close()

	again := releaseCheckApp(t, dir, true)
	(&fakeRelease{}).wire(again)
	again.releaseNotices.Store(true)
	got := again.ReleaseNotice()
	if got == nil || got.Version != "v2.8.0" || got.URL != newerRelease.URL {
		t.Fatalf("after a restart the notice is %+v, want v2.8.0 and its page", got)
	}
	if wait := again.releaseDue(24 * time.Hour); wait < 23*time.Hour {
		t.Errorf("after a restart the next check is due in %s, want about a day after the last one", wait)
	}
}

func TestTheReleaseCheckIsDueOnceADayHasPassed(t *testing.T) {
	dir := t.TempDir()
	stale, err := json.Marshal(releaseRecord{CheckedAt: time.Now().Add(-25 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, releaseCheckFile), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	a := releaseCheckApp(t, dir, true)
	if wait := a.releaseDue(24 * time.Hour); wait > 0 {
		t.Errorf("a check from 25 hours ago leaves %s to wait, want it due", wait)
	}
}

func TestTheReleaseCheckAsksOnceADayAndOnlyWhileItsSwitchIsOn(t *testing.T) {
	a := releaseCheckApp(t, t.TempDir(), false)
	f := &fakeRelease{info: newerRelease}
	f.wire(a)

	a.spawn(func() { a.releaseCheckLoop(time.Millisecond, time.Hour, 5*time.Millisecond) })
	time.Sleep(50 * time.Millisecond)
	if n := f.checks.Load(); n != 0 {
		t.Fatalf("asked GitHub %d times with the switch off", n)
	}

	s := a.Settings.Get()
	s.AutoUpdateCheck = true
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a check once the switch is on", func() bool { return f.checks.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if n := f.checks.Load(); n != 1 {
		t.Errorf("asked GitHub %d times within the day, want once", n)
	}
}

func TestAReleaseCheckThatCouldNotAskIsTriedAgainSoon(t *testing.T) {
	a := releaseCheckApp(t, t.TempDir(), true)
	f := &fakeRelease{info: update.Info{Current: "v2.7.5"}}
	f.wire(a)

	a.spawn(func() { a.releaseCheckLoop(time.Millisecond, time.Hour, 5*time.Millisecond) })
	waitFor(t, "a second try after a failed check", func() bool { return f.checks.Load() >= 2 })
	if a.ReleaseNotice() != nil {
		t.Error("a check that could not ask GitHub left a notice")
	}
}

func TestTheReleaseNoticeNamesOnlyANewerReleaseWhileTheSwitchIsOn(t *testing.T) {
	a := releaseCheckApp(t, t.TempDir(), true)
	f := &fakeRelease{info: newerRelease}
	f.wire(a)
	a.CheckRelease(context.Background())

	if a.ReleaseNotice() != nil {
		t.Error("a copy that runs no release check carries a notice")
	}
	a.releaseNotices.Store(true)
	if got := a.ReleaseNotice(); got == nil || got.Version != "v2.8.0" {
		t.Fatalf("notice = %+v, want v2.8.0", got)
	}
	if got := a.HealthReport().Release; got == nil || got.Version != "v2.8.0" {
		t.Errorf("the health report carries %+v, want v2.8.0", got)
	}

	a.releaseVersion = "v2.8.0"
	if got := a.ReleaseNotice(); got != nil {
		t.Errorf("once that release runs the notice is %+v, want none", got)
	}

	a.releaseVersion = "v2.7.5"
	s := a.Settings.Get()
	s.AutoUpdateCheck = false
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	if got := a.ReleaseNotice(); got != nil {
		t.Errorf("with the switch off the notice is %+v, want none", got)
	}
}
