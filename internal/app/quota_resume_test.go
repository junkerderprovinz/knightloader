package app

// A spent download volume comes back by itself, daily for Debrid-Link's
// per-hoster traffic. A link that met it waits for it, however often it meets
// it again, the way JDownloader waits out its own accounts' traffic.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const rapidgatorSpent = "debridlink: debrid-link /downloader/add: the daily traffic limit for this hoster is used up"

// rapidgatorApp routes rapidgator.net through Debrid-Link first and
// JDownloader's free mode after it, with the retry settings left at their
// defaults.
func rapidgatorApp(t *testing.T) (*App, chan string) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 4, 4
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.DiskReserve, s.DiskLowSpace, s.DiskCriticalSpace = 0, 0, 0
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 32)
	a.bmu.Lock()
	a.debrid["debridlink"] = &routeSpy{name: "debridlink", events: events}
	a.jd = &routeSpy{name: "jd", events: events}
	a.bmu.Unlock()
	isolateResolvers(a,
		sitesResolver{id: "debridlink", prio: 50, sites: []string{"rapidgator.net"}},
		sitesResolver{id: "jd", prio: 10, sites: []string{"rapidgator.net"}},
	)
	return a, events
}

// awaitEvents waits until every wanted backend call has come, in any order.
func awaitEvents(t *testing.T, events chan string, within time.Duration, want ...string) {
	t.Helper()
	missing := map[string]bool{}
	for _, w := range want {
		missing[w] = true
	}
	deadline := time.After(within)
	for len(missing) > 0 {
		select {
		case e := <-events:
			delete(missing, e)
		case <-deadline:
			t.Fatalf("backend calls %v never came", missing)
		}
	}
}

// runAgain puts a settled task back on its backend, as its retry would.
func runAgain(a *App, id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tasks[id].Status = core.StatusRunning
	a.active[id] = true
	a.started[id] = true
}

func TestALinkThatMetASpentHosterVolumeGoesBackToTheServiceOnceItIsBack(t *testing.T) {
	a, events := rapidgatorApp(t)
	old := volumeRecheck
	volumeRecheck = 300 * time.Millisecond
	t.Cleanup(func() { volumeRecheck = old })

	queueForSite(a, "r1", "rapidgator.net", "")
	dispatchNow(a)
	awaitEvents(t, events, 2*time.Second, "debridlink download r1")

	// More rounds than the default retry count, since the volume can still be
	// spent when it is looked at again.
	for round := 1; round <= 5; round++ {
		a.onUpdate("r1", core.Update{Status: core.StatusError, Err: rapidgatorSpent})
		awaitEvents(t, events, 2*time.Second, "debridlink remove r1", "jd download r1")

		a.onUpdate("r1", core.Update{Status: core.StatusError, Err: jdCaptchaSkipped, Note: "Skipped - Captcha is required"})
		got := liveTask(a, "r1")
		if got.ErrorCode != core.CodeQuotaUsed || got.GaveUp || got.NextTry.IsZero() {
			t.Fatalf("round %d: code %q, gave up %v, next try %v; want a wait for the volume",
				round, got.ErrorCode, got.GaveUp, got.NextTry)
		}
		if wait := time.Until(got.NextTry); wait > volumeRecheck+time.Second {
			t.Fatalf("round %d: the retry waits %s, want it once the volume is looked at again", round, wait)
		}

		awaitEvents(t, events, 3*time.Second, "jd remove r1", "debridlink download r1")
	}
}

func TestATaskPinnedToASpentHosterVolumeKeepsWaitingForIt(t *testing.T) {
	a, events := rapidgatorApp(t)
	queueForSite(a, "p1", "rapidgator.net", "debridlink")
	dispatchNow(a)
	awaitEvents(t, events, 2*time.Second, "debridlink download p1")

	for round := 1; round <= 5; round++ {
		a.onUpdate("p1", core.Update{Status: core.StatusError, Err: rapidgatorSpent})
		got := liveTask(a, "p1")
		if got.ErrorCode != core.CodeQuotaUsed || got.GaveUp || got.NextTry.IsZero() {
			t.Fatalf("round %d: code %q, gave up %v, next try %v; want a wait for the volume",
				round, got.ErrorCode, got.GaveUp, got.NextTry)
		}
		runAgain(a, "p1")
	}
}
