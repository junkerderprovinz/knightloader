package app

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

// fakeYtdlpRelease stands in for GitHub: latest answers with tag, and install
// counts its calls and returns what install returns.
type fakeYtdlpRelease struct {
	tag      string
	asked    atomic.Int32
	installs atomic.Int32
	install  func(ctx context.Context) (mediatools.ManagedRecord, error)
}

func (f *fakeYtdlpRelease) wire(a *App) {
	a.latestYtdlp = func(_ context.Context, installed string) mediatools.Latest {
		f.asked.Add(1)
		return mediatools.Latest{Checked: true, Tag: f.tag, Compare: mediatools.CompareVersions(f.tag, installed)}
	}
	a.installYtdlp = func(ctx context.Context, _ string) (mediatools.ManagedRecord, error) {
		f.installs.Add(1)
		return f.install(ctx)
	}
}

// autoUpdateApp builds an App whose yt-dlp is the stub printing 2026.01.01, with
// the daily update switched on or off.
func autoUpdateApp(t *testing.T, on bool) *App {
	t.Helper()
	t.Setenv("KL_YTDLP", ytdlpStub(t))
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	s := a.Settings.Get()
	s.YtdlpAutoUpdate = on
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	return a
}

func installsFine(context.Context) (mediatools.ManagedRecord, error) {
	return mediatools.ManagedRecord{Tag: "2026.09.01", Version: "2026.09.01", Asset: "yt-dlp"}, nil
}

func TestTheDailyUpdateFetchesANewerYtdlpWhenSwitchedOn(t *testing.T) {
	a := autoUpdateApp(t, true)
	f := &fakeYtdlpRelease{tag: "2026.09.01", install: installsFine}
	f.wire(a)

	a.autoUpdateYtdlp()

	if f.installs.Load() != 1 {
		t.Fatalf("a newer release was fetched %d times, want once", f.installs.Load())
	}
}

func TestTheDailyUpdateDoesNothingWhenSwitchedOff(t *testing.T) {
	a := autoUpdateApp(t, false)
	f := &fakeYtdlpRelease{tag: "2026.09.01", install: installsFine}
	f.wire(a)

	a.autoUpdateYtdlp()

	if f.asked.Load() != 0 || f.installs.Load() != 0 {
		t.Fatalf("switched off, the update asked GitHub %d times and fetched %d times", f.asked.Load(), f.installs.Load())
	}
}

func TestTheDailyUpdateDownloadsNothingWhenYtdlpIsCurrent(t *testing.T) {
	a := autoUpdateApp(t, true)
	f := &fakeYtdlpRelease{tag: "2026.01.01", install: installsFine}
	f.wire(a)

	a.autoUpdateYtdlp()

	if f.asked.Load() != 1 {
		t.Fatalf("GitHub was asked %d times, want once", f.asked.Load())
	}
	if f.installs.Load() != 0 {
		t.Fatal("the newest release was downloaded although it is the one in use")
	}
}

// mediatools.Install leaves the working copy alone when the new one fails its
// checksum or smoke test (TestAFailedSmokeTestReplacesNothing). This is the
// other half: the App keeps running what it ran and tries again next time.
func TestAFailedDailyUpdateKeepsTheYtdlpInUse(t *testing.T) {
	a := autoUpdateApp(t, true)
	before := a.MediaTools().Ytdlp
	f := &fakeYtdlpRelease{tag: "2026.09.01", install: func(context.Context) (mediatools.ManagedRecord, error) {
		return mediatools.ManagedRecord{}, errors.New("nothing was replaced; the copy you had is still running")
	}}
	f.wire(a)

	a.autoUpdateYtdlp()

	after := a.MediaTools().Ytdlp
	if after.Path != before.Path || after.Version != before.Version || after.Source != before.Source {
		t.Fatalf("after a failed update yt-dlp is %+v, want it unchanged from %+v", after, before)
	}
	if rec, _ := mediatools.LoadRecord(a.DataDir); rec != nil {
		t.Fatalf("a failed update left a record behind: %+v", rec)
	}
	if _, err := os.Stat(mediatools.BinaryPath(a.DataDir)); err == nil {
		t.Fatal("a failed update left a fetched copy behind")
	}

	a.autoUpdateYtdlp()
	if f.installs.Load() != 2 {
		t.Fatalf("the next run fetched %d times in all, want the failed release tried again", f.installs.Load())
	}
}

func TestTheDailyUpdateLoopRunsAndStopsOnClose(t *testing.T) {
	a := autoUpdateApp(t, true)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	f := &fakeYtdlpRelease{tag: "2026.09.01", install: func(ctx context.Context) (mediatools.ManagedRecord, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return mediatools.ManagedRecord{}, ctx.Err()
	}}
	f.wire(a)

	a.spawn(func() { a.ytdlpAutoLoop(time.Millisecond, time.Hour) })
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the loop never ran the update")
	}

	closed := make(chan struct{})
	go func() {
		a.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return while a daily update was downloading")
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("Close returned before the download in flight was cancelled")
	}
}
