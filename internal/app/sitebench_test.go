package app

// A service that switches off one site is still the best way to every other
// site it carries. Benching its account for that would send every link it
// could fetch to JDownloader's free mode for as long as the bench lasts.

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// siteDownErr is TorBox's answer for a site it has switched off, as the task
// row shows it.
const siteDownErr = "torbox: torbox /api/webdl/createwebdownload: TEMPORARILY_DISABLED " +
	"The site you are trying to download from is temporarily disabled."

// sitesResolver claims every link on the listed sites.
type sitesResolver struct {
	id    string
	prio  int
	sites []string
}

func (r sitesResolver) Info() resolver.Info { return resolver.Info{ID: r.id, Prio: r.prio} }
func (r sitesResolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && slices.Contains(r.sites, u.Hostname())
}
func (sitesResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// siteBenchApp has TorBox carry three sites and Debrid-Link one of them, below
// TorBox.
func siteBenchApp(t *testing.T) (*App, chan string) {
	t.Helper()
	a := newQueueApp(t)
	s := settings.Defaults()
	s.MaxConcurrent, s.MaxPerHost = 8, 8
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.DiskReserve, s.DiskLowSpace, s.DiskCriticalSpace = 0, 0, 0
	s.MaxRetries = 0
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 16)
	a.bmu.Lock()
	a.debrid["torbox"] = &routeSpy{name: "torbox", events: events}
	a.debrid["debridlink"] = &routeSpy{name: "debridlink", events: events}
	a.bmu.Unlock()
	isolateResolvers(a,
		sitesResolver{id: "torbox", prio: 50, sites: []string{"down.example", "up.example", "only.example"}},
		sitesResolver{id: "debridlink", prio: 47, sites: []string{"down.example"}},
	)
	return a, events
}

// queueForSite files one queued task for a site.
func queueForSite(a *App, id, site, pin string) *core.Task {
	t := &core.Task{
		ID: id, URL: "https://" + site + "/file/" + id, Status: core.StatusQueued,
		Enabled: true, ResolverPin: pin,
	}
	a.mu.Lock()
	a.tasks[id] = t
	a.queue = append(a.queue, id)
	a.mu.Unlock()
	return t
}

func TestASiteTorBoxSwitchedOffLeavesItForTheOtherSites(t *testing.T) {
	t.Parallel()
	a, events := siteBenchApp(t)
	queueForSite(a, "d1", "down.example", "")
	dispatchNow(a)
	wantEvents(t, events, "torbox download d1")

	a.onUpdate("d1", core.Update{Status: core.StatusError, Err: siteDownErr, HostDown: true})

	got := map[string]bool{nextEvent(t, events): true, nextEvent(t, events): true}
	if !got["torbox remove d1"] || !got["debridlink download d1"] {
		t.Fatalf("backend calls %v, want the link handed from TorBox to Debrid-Link", got)
	}
	wantEvents(t, events)
	if !a.acctHealthTracker().Usable("torbox", "") {
		t.Fatal("TorBox's account was benched over one site it switched off")
	}

	queueForSite(a, "u1", "up.example", "")
	dispatchNow(a)
	wantEvents(t, events, "torbox download u1")

	queueForSite(a, "d2", "down.example", "")
	dispatchNow(a)
	wantEvents(t, events, "debridlink download d2")
}

// TorBox switches a site off for the whole service, so a second TorBox account
// is not asked about it either.
func TestASwitchedOffSiteIsPassedOverOnEveryAccountOfTheService(t *testing.T) {
	t.Parallel()
	a, events := siteBenchApp(t)
	a.bmu.Lock()
	a.debrid["torbox#b"] = &routeSpy{name: "torbox#b", events: events}
	a.bmu.Unlock()
	isolateResolvers(a,
		sitesResolver{id: "torbox", prio: 50, sites: []string{"down.example"}},
		sitesResolver{id: "torbox#b", prio: 49, sites: []string{"down.example"}},
		sitesResolver{id: "debridlink", prio: 47, sites: []string{"down.example"}},
	)
	queueForSite(a, "d1", "down.example", "")
	dispatchNow(a)
	wantEvents(t, events, "torbox download d1")

	a.onUpdate("d1", core.Update{Status: core.StatusError, Err: siteDownErr, HostDown: true})

	got := settle(events)
	if got["torbox#b download d1"] {
		t.Fatalf("backend calls %v: the second TorBox account was asked about a site TorBox has switched off", got)
	}
	if !got["debridlink download d1"] {
		t.Fatalf("backend calls %v, want Debrid-Link to take the link", got)
	}
}

// Where nothing else carries the site, the link waits for it rather than
// failing, and goes back to TorBox by itself once the bench is over.
func TestALinkOnlyTheRefusingServiceCarriesWaitsForTheSite(t *testing.T) {
	a, events := siteBenchApp(t)
	old := siteBenchLength
	siteBenchLength = 300 * time.Millisecond
	t.Cleanup(func() { siteBenchLength = old })

	task := queueForSite(a, "o1", "only.example", "")
	dispatchNow(a)
	wantEvents(t, events, "torbox download o1")

	a.onUpdate("o1", core.Update{Status: core.StatusError, Err: siteDownErr, HostDown: true})
	if got := nextEvent(t, events); got != "torbox remove o1" {
		t.Fatalf("backend call %q, want TorBox to drop the task", got)
	}
	a.mu.Lock()
	status, waiting := task.Status, task.Waiting
	a.mu.Unlock()
	if status != core.StatusQueued || waiting != core.WaitingAccount {
		t.Fatalf("status %q, waiting %q; want the task queued and waiting for the service", status, waiting)
	}

	if got := nextEvent(t, events); got != "torbox download o1" {
		t.Fatalf("backend call %q after the bench, want TorBox to take the link again", got)
	}
}

// A pinned task stays with its service, and the service's own sentence says
// why it failed.
func TestAPinnedTaskOnASwitchedOffSiteFailsWithTheServicesWords(t *testing.T) {
	t.Parallel()
	a, events := siteBenchApp(t)
	task := queueForSite(a, "p1", "down.example", "torbox")
	dispatchNow(a)
	wantEvents(t, events, "torbox download p1")

	a.onUpdate("p1", core.Update{Status: core.StatusError, Err: siteDownErr, HostDown: true})

	wantEvents(t, events)
	a.mu.Lock()
	status, msg := task.Status, task.Error
	a.mu.Unlock()
	if status != core.StatusError {
		t.Errorf("status = %q, want %q", status, core.StatusError)
	}
	if !strings.Contains(msg, "TEMPORARILY_DISABLED") {
		t.Errorf("error = %q, want TorBox's own answer", msg)
	}
}
