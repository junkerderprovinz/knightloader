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

	"github.com/junkerderprovinz/knightloader/internal/core"
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
	// formats stands in for a probe's own discovered format list - nil
	// (every existing test's own zero value) is "a source with no formats
	// reported", the same as a real empty formats array.
	formats []ytdlp.FormatEntry
	err     error
	done    chan struct{}
}

func (fakeYtdlpBackend) Download(string, string, map[string]string, int) {}
func (fakeYtdlpBackend) Pause(string)                                    {}
func (fakeYtdlpBackend) Resume(string)                                   {}
func (fakeYtdlpBackend) Remove(string, bool)                             {}

func (f fakeYtdlpBackend) ProbeTitle(_ context.Context, _ string) (ytdlp.ProbeResult, error) {
	defer close(f.done)
	if f.err != nil {
		return ytdlp.ProbeResult{}, f.err
	}
	return ytdlp.ProbeResult{Title: f.title, Formats: f.formats}, nil
}

// blockingYtdlpBackend answers ProbeTitle only once release is closed, so a
// test can let the five-row variant family finish being built and then decide
// for itself whether the title ever arrives at all.
//
// fakeYtdlpBackend above answers instantly, which is the right shape for "the
// probe ran and the name landed" but the wrong one for anything about
// ORDERING: a test that needs the probe to be still outstanding can only get
// there by luck with a fake that has already answered by the time AddLinks
// returns.
type blockingYtdlpBackend struct {
	title   string
	release chan struct{}
}

func (blockingYtdlpBackend) Download(string, string, map[string]string, int) {}
func (blockingYtdlpBackend) Pause(string)                                    {}
func (blockingYtdlpBackend) Resume(string)                                   {}
func (blockingYtdlpBackend) Remove(string, bool)                             {}

func (b blockingYtdlpBackend) ProbeTitle(ctx context.Context, _ string) (ytdlp.ProbeResult, error) {
	select {
	case <-b.release:
		return ytdlp.ProbeResult{Title: b.title}, nil
	case <-ctx.Done():
		return ytdlp.ProbeResult{}, ctx.Err()
	}
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

// TestNamingLatelyRenamesAnAutoDerivedSoloPackage is [35b]'s other half (jdp,
// 2026-08-25: "bei einem Youtubelink heißt der Ordner nur watch. der soll
// den namen anzeigen"): a bare-pasted link with no package of its own gets
// one guessed from its URL's own path at staging time (fileStem/
// derivePackage, app_links.go) - every youtube.com/watch link guesses
// "watch", since the query string carrying the actual video id is not part
// of the URL's path. setTaskName now re-derives that guess once a real name
// arrives, as long as no sibling in the same package already has a real
// name of its own (see noSiblingHasARealNameYet's own doc comment - a
// second link pasted alongside this one, still unresolved, does not block
// the rename, only an already-named one does).
func TestNamingLatelyRenamesAnAutoDerivedSoloPackage(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	done := make(chan struct{})
	wireYtdlp(a, fakeYtdlpBackend{title: "Never Gonna Give You Up", done: done})

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	// Not asserted synchronously here: the fake backend answers with no real
	// network delay, so the probe's own goroutine can rename the package
	// before this function's own next line even runs - genuinely racy to
	// check, and the point of this test is the AFTER state below, not the
	// mid-flight one. derivePackage's own behaviour (a bare YouTube watch
	// link guesses "watch") already has its own coverage elsewhere.
	id := created[0].ID

	waitFor(t, "the probe to rename the auto-derived package", func() bool {
		return snapshot(t, a, id).Package == "Never Gonna Give You Up"
	})
}

// TestNamingSplitsOneLinkOutOfACoincidentallySharedPackage is the case the
// first, narrower version of this fix got wrong: pasting two ordinary
// YouTube links together stages both under "watch" (every bare watch-page
// URL guesses the identical stem - there is nothing "batch-like" about
// this, it is simply the same fallback firing twice), and the earlier
// "only while this task is the package's ONLY member" guard meant NEITHER
// of them could ever rename after that, which is exactly the bug jdp
// reported still happening. The fix renames only the ONE task whose own
// probe just answered, leaving its still-unresolved sibling right where it
// was - each one peels off into its own package as its own name arrives,
// same as if they had never shared one at all.
func TestNamingSplitsOneLinkOutOfACoincidentallySharedPackage(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})

	const urlA = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	const urlB = "https://youtube.com/watch?v=aaaaaaaaaaa"
	taskA := putTask(t, a, core.Task{URL: urlA, Name: urlA, Package: "watch", Status: core.StatusCollected, Enabled: true})
	taskB := putTask(t, a, core.Task{URL: urlB, Name: urlB, Package: "watch", Status: core.StatusCollected, Enabled: true})

	a.setTaskName(taskA.ID, "Never Gonna Give You Up")

	if live := snapshot(t, a, taskA.ID); live.Package != "Never Gonna Give You Up" {
		t.Errorf("resolved task's own package = %q, want it renamed to its own new title", live.Package)
	}
	if live := snapshot(t, a, taskB.ID); live.Package != "watch" {
		t.Errorf("still-unresolved sibling's package = %q, want it left in %q until its own name arrives", live.Package, "watch")
	}
}

// TestNamingNeverRenamesAPackageASiblingAlreadyNamedForReal is the guard
// that actually matters: a package is left alone once ANY of its members
// already carries a real (non-placeholder) name - the signal that this was
// a deliberate, already-resolved batch (a real crawl hands every member a
// real name immediately, never leaving one at the URL placeholder for a
// later probe) rather than a coincidental collision of not-yet-named links.
func TestNamingNeverRenamesAPackageASiblingAlreadyNamedForReal(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})

	// Both URLs guess "watch" - same as every test above - so this reaches
	// the guard this test is actually about (t.Package == the URL guess)
	// rather than bailing out earlier on a package/guess mismatch.
	const urlA = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	const urlB = "https://youtube.com/watch?v=aaaaaaaaaaa"
	taskA := putTask(t, a, core.Task{URL: urlA, Name: urlA, Package: "watch", Status: core.StatusCollected, Enabled: true})
	// taskB already carries a real name, as a genuine crawled batch would
	// from the moment it was staged - unlike taskA here, standing in for a
	// link that has not resolved yet.
	putTask(t, a, core.Task{URL: urlB, Name: "Some Other Video", Package: "watch", Status: core.StatusCollected, Enabled: true})

	a.setTaskName(taskA.ID, "Never Gonna Give You Up")

	if live := snapshot(t, a, taskA.ID); live.Package != "watch" {
		t.Errorf("package = %q after resolving, want the shared batch package %q left alone (a sibling already had a real name)", live.Package, "watch")
	}
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
	// applyProbeFormats (app_ytdlp_variants.go) is never even reached on a
	// failed probe - probeYtdlpTitle returns before calling it - so Online
	// must stay exactly as unset as it started. A failure is deliberately
	// NOT read as "offline": too many failure causes (a timeout, an age
	// gate, a transient site hiccup) are not the host actually saying the
	// file is gone.
	if live.Online != "" {
		t.Errorf("Online = %q after a failed probe, want it left unset (a failure is not read as offline)", live.Online)
	}
}

// TestAQuickProbeStillFixesTheGuessedPackage pins the ordering that made the
// rename fail in practice, rather than leaving it to chance.
//
// nameBucket decides a package from a SNAPSHOT and writes it afterwards. A
// probe answering in that gap sets the real name while the package is still
// unset, so setTaskName's own re-guess finds nothing to replace - and then the
// write lands, filing a correctly-titled YouTube link under "watch". That is
// the complaint the feature was built for, reappearing whenever the probe was
// quick.
//
// Driven deterministically here: the fake backend answers instantly, which is
// exactly the losing order, and the assertion is on the settled state rather
// than on who won.
func TestAQuickProbeStillFixesTheGuessedPackage(t *testing.T) {
	for i := 0; i < 25; i++ {
		a, _ := newRuleApp(t, func(*settings.Settings, string) {})
		done := make(chan struct{})
		wireYtdlp(a, fakeYtdlpBackend{title: "Never Gonna Give You Up", done: done})

		created := a.AddLinks([]string{"https://youtube.com/watch?v=dQw4w9WgXcQ"}, "")
		if len(created) != 1 {
			t.Fatalf("round %d: AddLinks created %d tasks, want 1", i, len(created))
		}
		id := created[0].ID

		waitFor(t, "the guessed package to be replaced by the probed title", func() bool {
			return snapshot(t, a, id).Package == "Never Gonna Give You Up"
		})
		if got := snapshot(t, a, id).Package; got == "watch" {
			t.Fatalf("round %d: package stayed %q - the URL-path guess won", i, got)
		}
	}
}
