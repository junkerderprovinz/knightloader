package app

// A desktop app installed a minute ago has no ffmpeg yet and fetches one. A
// video sent in that minute must not fail for want of it.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const mergeWithoutFFmpeg = "yt-dlp: ERROR: You have requested merging of multiple formats but ffmpeg is not installed. Aborting"

// ffmpegFetch stands in for the download of ffmpeg: started is told when it
// begins, and it ends with whatever is sent on result.
type ffmpegFetch struct {
	started chan struct{}
	result  chan error
}

// ffmpegWaitApp is a desktop-like queue with no ffmpeg on PATH and the daily
// update on or off. The ffmpeg fetch and the yt-dlp checks before it hang
// until the test lets them go.
func ffmpegWaitApp(t *testing.T, autoUpdate bool) (*App, *ffmpegFetch) {
	t.Helper()
	if err := mediatools.CanInstallFFmpeg(t.TempDir()); err != nil {
		t.Skip(err)
	}
	a := retryApp(t, func(s *settings.Settings) { s.YtdlpAutoUpdate = autoUpdate })
	a.InstallMissingTools = true
	t.Setenv("PATH", t.TempDir())
	f := &ffmpegFetch{started: make(chan struct{}, 1), result: make(chan error, 1)}
	a.installFFmpeg = func(ctx context.Context, _ string) (mediatools.FFmpegRecord, error) {
		f.started <- struct{}{}
		select {
		case err := <-f.result:
			return mediatools.FFmpegRecord{Version: "N-1", Asset: "ffmpeg.zip"}, err
		case <-ctx.Done():
			return mediatools.FFmpegRecord{}, ctx.Err()
		}
	}
	a.latestYtdlp = func(ctx context.Context, _ string) mediatools.Latest {
		<-ctx.Done()
		return mediatools.Latest{}
	}
	a.installYtdlp = func(ctx context.Context, _ string) (mediatools.ManagedRecord, error) {
		<-ctx.Done()
		return mediatools.ManagedRecord{}, ctx.Err()
	}
	return a, f
}

// fetching runs the ffmpeg half of the daily update and returns once its
// download has begun. The channel closes when the update is over.
func (f *ffmpegFetch) fetching(t *testing.T, a *App) <-chan struct{} {
	t.Helper()
	over := make(chan struct{})
	go func() {
		defer close(over)
		a.autoUpdateFFmpeg()
	}()
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the ffmpeg fetch never began")
	}
	return over
}

func (f *ffmpegFetch) end(t *testing.T, over <-chan struct{}, err error) {
	t.Helper()
	f.result <- err
	select {
	case <-over:
	case <-time.After(5 * time.Second):
		t.Fatal("the ffmpeg fetch never finished")
	}
}

func failsForWantOfFFmpeg(a *App, id string) {
	runningOn(a, id, plainHost, simpleResolverID)
	a.onUpdate(id, core.Update{Status: core.StatusError, Err: mergeWithoutFFmpeg, Reason: core.ReasonNoFFmpeg})
}

func TestAVideoNeedingFFmpegWaitsWhileTheAppFetchesIt(t *testing.T) {
	a, f := ffmpegWaitApp(t, true)
	over := f.fetching(t, a)

	failsForWantOfFFmpeg(a, "v1")

	got := liveTask(a, "v1")
	if got.Status != core.StatusQueued || got.Waiting != core.WaitingFFmpeg {
		t.Fatalf("status %q waiting %q, want the row queued and waiting for ffmpeg", got.Status, got.Waiting)
	}
	if got.Error != "" || got.Reason != core.ReasonUnknown {
		t.Errorf("a waiting row shows the failure %q (%q)", got.Error, got.Reason)
	}
	f.end(t, over, nil)
}

func TestTheWaitingVideoStartsOnceFFmpegIsInPlace(t *testing.T) {
	a, f := ffmpegWaitApp(t, true)
	over := f.fetching(t, a)
	failsForWantOfFFmpeg(a, "v1")

	f.end(t, over, nil)

	a.mu.Lock()
	active := a.active["v1"]
	a.mu.Unlock()
	got := liveTask(a, "v1")
	if !active || got.Waiting != core.WaitingNone {
		t.Fatalf("active %v waiting %q, want the row started once ffmpeg arrived", active, got.Waiting)
	}
}

func TestTheWaitingVideoFailsWhenTheFetchFails(t *testing.T) {
	a, f := ffmpegWaitApp(t, true)
	over := f.fetching(t, a)
	failsForWantOfFFmpeg(a, "v1")

	f.end(t, over, errors.New("github: 503"))

	got := liveTask(a, "v1")
	if got.Status != core.StatusError || got.Reason != core.ReasonNoFFmpeg || got.ErrorCode != core.CodeNoFFmpeg {
		t.Fatalf("status %q reason %q code %q, want the noFFmpeg failure", got.Status, got.Reason, got.ErrorCode)
	}
	if got.Error != mergeWithoutFFmpeg {
		t.Errorf("error %q, want yt-dlp's own sentence %q", got.Error, mergeWithoutFFmpeg)
	}
	if !got.GaveUp || got.Waiting != core.WaitingNone {
		t.Errorf("gave up %v waiting %q, want a settled failure that waits for nothing", got.GaveUp, got.Waiting)
	}
	a.mu.Lock()
	queued := len(a.queue)
	a.mu.Unlock()
	if queued != 0 {
		t.Errorf("%d rows still queued", queued)
	}
}

// At start the yt-dlp check goes first, and a row restored from the last
// session can fail before the ffmpeg download has begun.
func TestAVideoFailingBeforeTheFetchBeginsWaitsToo(t *testing.T) {
	a, _ := ffmpegWaitApp(t, true)
	a.StartYtdlpAutoUpdate()

	failsForWantOfFFmpeg(a, "v1")

	if got := liveTask(a, "v1"); got.Waiting != core.WaitingFFmpeg {
		t.Fatalf("status %q waiting %q, want the row waiting for the fetch about to begin", got.Status, got.Waiting)
	}
}

func TestWithTheDailyUpdateOffAVideoWithoutFFmpegFailsAtOnce(t *testing.T) {
	a, _ := ffmpegWaitApp(t, false)
	a.StartYtdlpAutoUpdate()

	failsForWantOfFFmpeg(a, "v1")

	if got := liveTask(a, "v1"); got.Status != core.StatusError || got.Reason != core.ReasonNoFFmpeg {
		t.Fatalf("status %q reason %q, want the noFFmpeg failure with nothing being fetched", got.Status, got.Reason)
	}
}
