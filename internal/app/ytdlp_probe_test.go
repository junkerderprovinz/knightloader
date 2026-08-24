package app

// Coverage for stage()'s yt-dlp counterpart to its own "direct" HEAD probe
// (see availability_test.go's TestAFlakyMinuteDoesNotKillALiveLink and
// friends for that half): a link routed to yt-dlp gets its real title
// probed in the background while it sits in the collector, and the task's
// placeholder name (its own URL - see stage's own comment in app_links.go)
// is updated once the probe answers, or left alone if it never does.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakeYtdlpBackend stands in for a real ytdlp.Backend the same way
// availability_test.go's batchResolver stands in for a real resolver.Checker:
// the point is proving app's own wiring (the type assertion onto
// titleProber, the spawn from stage(), setTaskName's guard), never a real
// yt-dlp process. done is closed exactly once ProbeTitle has been asked, so
// a test can wait for "the probe ran" without a sleep-and-hope loop.
type fakeYtdlpBackend struct {
	title string
	err   error
	done  chan struct{}
}

func (fakeYtdlpBackend) Download(string, string, map[string]string, int) {}
func (fakeYtdlpBackend) Pause(string)                                    {}
func (fakeYtdlpBackend) Resume(string)                                   {}
func (fakeYtdlpBackend) Remove(string, bool)                             {}

func (f fakeYtdlpBackend) ProbeTitle(_ context.Context, _ string) (string, error) {
	defer close(f.done)
	if f.err != nil {
		return "", f.err
	}
	return f.title, nil
}

// wireYtdlp routes ytdlp-shaped links (see routing_test.go's own use of the
// same resolver) to a fake backend, without a real yt-dlp binary anywhere on
// the machine running the test.
func wireYtdlp(a *App, b backend) {
	a.bmu.Lock()
	a.ytdlp = b
	a.bmu.Unlock()
	a.Registry.Register(ytdlp.Resolver{})
}

// TestStagingAYtdlpLinkProbesAndNamesTheTask is the live complaint this
// change exists to fix: a YouTube (etc.) link used to sit in the collector
// showing its own URL - "the names of links and archives are still not
// displayed correctly, e.g. song names for YouTube links" - until a download
// actually started. Staging one now fires the probe, and the task's name is
// the title the probe found once it answers.
func TestStagingAYtdlpLinkProbesAndNamesTheTask(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	done := make(chan struct{})
	wireYtdlp(a, fakeYtdlpBackend{title: "Never Gonna Give You Up", done: done})

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	id := created[0].ID
	if created[0].Resolver != "ytdlp" {
		t.Fatalf("task routed to resolver %q, want ytdlp - the probe never fires without that", created[0].Resolver)
	}

	waitFor(t, "the probe to name the task", func() bool {
		return snapshot(t, a, id).Name == "Never Gonna Give You Up"
	})
}

// TestAFailedYtdlpProbeLeavesThePlaceholderNameAlone is analyze's own
// TestAFlakyMinuteDoesNotKillALiveLink, mirrored for the name column instead
// of Online: a probe that comes back with nothing to say must not turn a
// clean placeholder into an error, or overwrite it with anything at all. The
// task keeps showing its own URL exactly as it did before yt-dlp's own
// progress stream would eventually rename it once a real download starts.
func TestAFailedYtdlpProbeLeavesThePlaceholderNameAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	done := make(chan struct{})
	wireYtdlp(a, fakeYtdlpBackend{err: errors.New("yt-dlp: Video unavailable"), done: done})

	const url = "https://youtube.com/watch?v=gone000000"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	id := created[0].ID

	select {
	case <-done:
		// ProbeTitle has returned its error; probeYtdlpTitle's only remaining
		// work on this path is the "err != nil { return }" check right after
		// the call that closed done, in the same goroutine - there is nothing
		// left for it to do that could still write the task after this point.
	case <-time.After(3 * time.Second):
		t.Fatal("the probe never ran")
	}

	live := snapshot(t, a, id)
	if live.Name != url {
		t.Errorf("Name = %q after a failed probe, want the placeholder URL untouched", live.Name)
	}
}
