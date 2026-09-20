package app

// stage's yt-dlp counterpart to the "direct" HEAD probe in
// availability_test.go: a link routed to yt-dlp has its title probed in the
// background while it sits in the collector, and the task's placeholder name,
// its own URL, is replaced once the probe answers or left alone if it does not.

import (
	"context"
	"errors"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakeYtdlpBackend stands in for a real ytdlp.Backend, as
// availability_test.go's batchResolver does for a resolver.Checker: what is
// tested is this package's wiring, the type assertion onto titleProber, the
// spawn from stage and setTaskName's guard. done is closed once ProbeTitle has
// been asked, so a test can wait for the probe without polling.
type fakeYtdlpBackend struct {
	title string
	// formats stands in for a probe's discovered format list. Nil means a
	// source that reported no formats, as an empty array would.
	formats []ytdlp.FormatEntry
	err     error
	done    chan struct{}
	// closeOnce keeps a second probe from closing done twice, which would panic
	// and abort every remaining test in the package instead of failing one. The
	// second call is logged, so whatever probes twice is still a finding.
	//
	// A pointer, because the fake is passed by value and a copied sync.Once
	// guards nothing.
	closeOnce *sync.Once
	probes    *probeLog
}

// probeLog is every URL a fake was asked about, in order. Shared by pointer for
// the same reason closeOnce is.
type probeLog struct {
	mu   sync.Mutex
	urls []string
}

func (p *probeLog) add(url string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.urls = append(p.urls, url)
	return len(p.urls)
}

// newFakeYtdlp builds the fake and its done channel together, so no call site
// can construct one whose guards are nil. Every field a test cares about is set
// on the returned value afterwards.
func newFakeYtdlp() (fakeYtdlpBackend, chan struct{}) {
	done := make(chan struct{})
	return fakeYtdlpBackend{done: done, closeOnce: &sync.Once{}, probes: &probeLog{}}, done
}

func (fakeYtdlpBackend) Download(string, string, map[string]string, int) {}
func (fakeYtdlpBackend) Pause(string)                                    {}
func (fakeYtdlpBackend) Resume(string)                                   {}
func (fakeYtdlpBackend) Remove(string, bool)                             {}

func (f fakeYtdlpBackend) ProbeTitle(_ context.Context, url string) (ytdlp.ProbeResult, error) {
	if n := f.probes.add(url); n > 1 {
		// Logged rather than failed here: t.Fatal from a background goroutine
		// is illegal, and whatever the second probe writes fails its own
		// assertion if it matters.
		log.Printf("fakeYtdlpBackend: probe %d for %q; this fake is one-shot", n, url)
	}
	f.closeOnce.Do(func() { close(f.done) })
	if f.err != nil {
		return ytdlp.ProbeResult{}, f.err
	}
	return ytdlp.ProbeResult{Title: f.title, Formats: f.formats}, nil
}

// blockingYtdlpBackend answers ProbeTitle only once release is closed, so a
// test can let the variant family finish being built and then choose when the
// title arrives. fakeYtdlpBackend answers instantly, which has already closed
// the window an ordering test needs by the time AddLinks returns.
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

// wireYtdlp routes ytdlp-shaped links to a fake backend, so no real yt-dlp
// binary is needed on the machine running the test.
func wireYtdlp(a *App, b backend) {
	a.bmu.Lock()
	a.ytdlp = b
	a.bmu.Unlock()
	a.Registry.Register(ytdlp.Resolver{})
}

// Staging a media link fires the probe, so the row carries the video's title
// rather than its URL while it waits in the collector.
func TestStagingAYtdlpLinkProbesAndNamesTheTask(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Never Gonna Give You Up"
	wireYtdlp(a, fake)

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	id := created[0].ID
	if created[0].Resolver != "ytdlp" {
		t.Fatalf("task routed to resolver %q, want ytdlp; the probe never fires otherwise", created[0].Resolver)
	}

	waitFor(t, "the probe to name the task", func() bool {
		return snapshot(t, a, id).Name == "Never Gonna Give You Up"
	})
}

// A bare-pasted link with no package of its own is filed under a guess made
// from its URL path, and every youtube.com/watch link guesses "watch" because
// the video id sits in the query string. setTaskName re-derives that guess once
// a real name arrives, unless a sibling in the package already has one; see
// noSiblingHasARealNameYet.
func TestNamingLatelyRenamesAnAutoDerivedSoloPackage(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, _ := newFakeYtdlp()
	fake.title = "Never Gonna Give You Up"
	wireYtdlp(a, fake)

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	// The mid-flight state is not asserted: the fake answers with no network
	// delay, so the probe's goroutine can rename the package before the next
	// line runs. What this test is about is the settled state below.
	id := created[0].ID

	waitFor(t, "the probe to rename the auto-derived package", func() bool {
		return snapshot(t, a, id).Package == "Never Gonna Give You Up"
	})
}

// Pasting two ordinary YouTube links together stages both under "watch",
// because the same fallback fires twice rather than because they belong
// together. Only the task whose probe just answered is renamed, so each peels
// off into its own package as its name arrives.
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

// A package is left alone once any member carries a real name, which marks an
// already-resolved batch: a crawl names every member at once and never leaves
// one at the URL placeholder for a later probe.
func TestNamingNeverRenamesAPackageASiblingAlreadyNamedForReal(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})

	// Both URLs guess "watch", so this reaches the guard the test is about
	// rather than bailing out on a package and guess mismatch.
	const urlA = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	const urlB = "https://youtube.com/watch?v=aaaaaaaaaaa"
	taskA := putTask(t, a, core.Task{URL: urlA, Name: urlA, Package: "watch", Status: core.StatusCollected, Enabled: true})
	// taskB already carries a real name, as a crawled batch would from the
	// moment it was staged, while taskA stands in for an unresolved link.
	putTask(t, a, core.Task{URL: urlB, Name: "Some Other Video", Package: "watch", Status: core.StatusCollected, Enabled: true})

	a.setTaskName(taskA.ID, "Never Gonna Give You Up")

	if live := snapshot(t, a, taskA.ID); live.Package != "watch" {
		t.Errorf("package = %q after resolving, want %q left alone since a sibling already had a real name", live.Package, "watch")
	}
}

// TestAFlakyMinuteDoesNotKillALiveLink for the name column instead of Online: a
// probe with nothing to say leaves the placeholder alone, and yt-dlp's progress
// stream renames the task once a download starts.
func TestAFailedYtdlpProbeLeavesThePlaceholderNameAlone(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, done := newFakeYtdlp()
	fake.err = errors.New("yt-dlp: Video unavailable")
	wireYtdlp(a, fake)

	const url = "https://youtube.com/watch?v=gone000000"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	id := created[0].ID

	select {
	case <-done:
		// ProbeTitle has returned its error, and probeYtdlpTitle returns on the
		// next line in the same goroutine, so nothing can still write the task.
	case <-time.After(3 * time.Second):
		t.Fatal("the probe never ran")
	}

	live := snapshot(t, a, id)
	if live.Name != url {
		t.Errorf("Name = %q after a failed probe, want the placeholder URL untouched", live.Name)
	}
	// applyProbeFormats is not reached on a failed probe, so Online stays
	// unset. A failure is not read as "offline": a timeout, an age gate or a
	// transient site hiccup is not the host saying the file is gone.
	if live.Online != "" {
		t.Errorf("Online = %q after a failed probe, want it left unset", live.Online)
	}
}

// nameBucket decides a package from a snapshot and writes it afterwards. A
// probe answering in that gap sets the real name while the package is still
// unset, so setTaskName's re-guess finds nothing to replace and the write then
// files a correctly-titled link under "watch". The fake answers instantly,
// which is that order, and the assertion is on the settled state.
func TestAQuickProbeStillFixesTheGuessedPackage(t *testing.T) {
	for i := 0; i < 25; i++ {
		a, _ := newRuleApp(t, func(*settings.Settings, string) {})
		fake, _ := newFakeYtdlp()
		fake.title = "Never Gonna Give You Up"
		wireYtdlp(a, fake)

		created := a.AddLinks([]string{"https://youtube.com/watch?v=dQw4w9WgXcQ"}, "")
		if len(created) != 1 {
			t.Fatalf("round %d: AddLinks created %d tasks, want 1", i, len(created))
		}
		id := created[0].ID

		waitFor(t, "the guessed package to be replaced by the probed title", func() bool {
			return snapshot(t, a, id).Package == "Never Gonna Give You Up"
		})
		if got := snapshot(t, a, id).Package; got == "watch" {
			t.Fatalf("round %d: package stayed %q; the URL-path guess won", i, got)
		}
	}
}

// yt-dlp's -j probe reads every format before it answers, which can take a
// dozen seconds, and for that whole stretch the URL-path guess would show the
// folder as "watch". No guess is made while the probe is outstanding. The
// blocking backend is what holds that window open long enough to assert.
func TestAPendingMediaLinkIsNotFiledUnderItsURLPath(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	release := make(chan struct{})
	wireYtdlp(a, blockingYtdlpBackend{title: "Never Gonna Give You Up", release: release})

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}

	// Every row of the family, not just the primary: the variant siblings are
	// created inside stage and appear in no id list of their own, so they are
	// easy to leave behind in the old folder.
	for _, row := range a.Tasks() {
		if row.URL != url {
			continue
		}
		if row.Package != "" {
			t.Errorf("a link waiting on its title probe was filed under %q, want no package yet (variant %q)", row.Package, row.Variant)
		}
	}

	close(release)
	waitFor(t, "the probe to file the family under the video's own title", func() bool {
		return snapshot(t, a, created[0].ID).Package == "Never Gonna Give You Up"
	})
	// And the siblings came with it: one folder for every row of one video.
	for _, row := range a.Tasks() {
		if row.URL == url && row.Package != "Never Gonna Give You Up" {
			t.Errorf("variant %q stayed in %q, want the whole family in one folder", row.Variant, row.Package)
		}
	}
}

// The other end of that rule: the naming passes skip a link while its probe
// runs, so a probe that never answers must still fall back to the URL-path
// guess rather than leaving the link ungrouped for good.
func TestAFailedProbeStillFilesTheLink(t *testing.T) {
	a, _ := newRuleApp(t, func(*settings.Settings, string) {})
	fake, done := newFakeYtdlp()
	fake.err = errors.New("yt-dlp: unsupported url")
	wireYtdlp(a, fake)

	const url = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	created := a.AddLinks([]string{url}, "")
	if len(created) != 1 {
		t.Fatalf("AddLinks created %d tasks, want 1", len(created))
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the probe never ran")
	}
	waitFor(t, "the failed probe to fall back to the URL-path guess", func() bool {
		return snapshot(t, a, created[0].ID).Package == "watch"
	})
}
