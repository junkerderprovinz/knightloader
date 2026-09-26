package app

// A host rule's preferred and excluded services. pinApp's two services both
// claim every link on pinHost, alldebrid ranked above torbox, so a task that
// lands on torbox got there because of the rule.

import (
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// ruleForPinHost gives pinHost a host rule on top of pinApp's settings.
func ruleForPinHost(t *testing.T, a *App, rule settings.HostRule) {
	t.Helper()
	s := a.Settings.Get()
	s.HostRules = map[string]settings.HostRule{pinHost: rule}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
}

func dispatchOnce(a *App) {
	a.mu.Lock()
	a.dispatchLocked()
	a.mu.Unlock()
}

func TestAHostsPreferredServiceIsAskedFirst(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	ruleForPinHost(t, a, settings.HostRule{Prefer: "torbox"})
	queuePinned(a, "h1", "")

	dispatchOnce(a)

	wantHandled(t, bes["torbox"], "h1")
	wantNothingHandled(t, bes["alldebrid"], "the ranking took a link its host prefers another service for")
}

// A rule written after the link was collected still counts: the collector's
// pick is only a pick. The preferred service is JD, since a debrid service
// ranked above the pick would take the link over anyway.
func TestAPreferredServiceOverrulesThePickFromTheCollector(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	jd := &pinBackend{got: make(chan string, 4)}
	a.bmu.Lock()
	a.jd = jd
	a.bmu.Unlock()
	a.Registry.Register(pinResolver{id: "jd", host: pinHost, prio: 10})
	ruleForPinHost(t, a, settings.HostRule{Prefer: "jd"})
	queuePinned(a, "h1", "")
	a.mu.Lock()
	a.tasks["h1"].Resolver = "alldebrid"
	a.mu.Unlock()

	dispatchOnce(a)

	wantHandled(t, jd, "h1")
	wantNothingHandled(t, bes["alldebrid"], "the collector's pick outranked the host's preferred service")
}

func TestAPreferredServiceWithABenchedAccountFallsBackToTheOrder(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	ruleForPinHost(t, a, settings.HostRule{Prefer: "torbox"})
	a.acctHealthTracker().ReportFailure("torbox", "", accounts.HealthInvalid, "test", 0)
	queuePinned(a, "h1", "")

	dispatchOnce(a)

	wantHandled(t, bes["alldebrid"], "h1")
	wantNothingHandled(t, bes["torbox"], "a link went to a preferred service whose account is benched")
}

// Declining the link hands it on down the ordinary order, which puts the
// service ranked first back in line.
func TestALinkThePreferredServiceDeclinesGoesDownTheOrder(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	ruleForPinHost(t, a, settings.HostRule{Prefer: "torbox"})
	queuePinned(a, "h1", "")
	dispatchOnce(a)
	wantHandled(t, bes["torbox"], "h1")

	a.onUpdate("h1", core.Update{Status: core.StatusError, Err: "not my business", Unsupported: true})

	wantHandled(t, bes["alldebrid"], "h1")
}

func TestAnExcludedServiceIsNeverUsedForThatHost(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	ruleForPinHost(t, a, settings.HostRule{Exclude: []string{"alldebrid"}})
	// Collected on alldebrid before the rule existed, which must not be a way
	// back to it.
	queuePinned(a, "h1", "")
	a.mu.Lock()
	a.tasks["h1"].Resolver = "alldebrid"
	a.mu.Unlock()

	dispatchOnce(a)

	wantHandled(t, bes["torbox"], "h1")
	wantNothingHandled(t, bes["alldebrid"], "an excluded service was handed the link")
	wantNothingHandled(t, bes["alldebrid#work"], "a second account of an excluded service was handed the link")

	a.mu.Lock()
	next := a.nextResolverLocked(a.tasks["h1"])
	a.mu.Unlock()
	if service, _ := resolver.SplitSlot(next); service == "alldebrid" {
		t.Errorf("the fallback after torbox is %q, an excluded service", next)
	}
}

// A rule saved while a download is paused applies when it is resumed: the
// excluded service does not get it back, as with a pin to another one.
func TestAPausedDownloadMovesOffAServiceExcludedMeanwhile(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	queuePinned(a, "h1", "")
	dispatchOnce(a)
	wantHandled(t, bes["alldebrid"], "h1")
	a.Pause("h1")

	ruleForPinHost(t, a, settings.HostRule{Exclude: []string{"alldebrid"}})
	a.Resume("h1")

	wantHandled(t, bes["torbox"], "h1")
	if got := taskState(a, "h1").Resolver; got != "torbox" {
		t.Errorf("resolver = %q, want the download moved to torbox", got)
	}
}

// excludeEveryBackend gives pinHost a rule that switches off every backend
// that claims url.
func excludeEveryBackend(t *testing.T, a *App, url string) {
	t.Helper()
	var every []string
	for _, res := range a.Registry.All(url) {
		every = append(every, res.Info().ID)
	}
	ruleForPinHost(t, a, settings.HostRule{Exclude: every})
}

// wantHostExcluded checks that a failure names the host rule as its cause, in
// the sentence and in the code the interface words it by.
func wantHostExcluded(t *testing.T, got core.Task) {
	t.Helper()
	if !strings.Contains(got.Error, "excluded for "+pinHost) {
		t.Errorf("error %q; want the exclusion named", got.Error)
	}
	if got.ErrorCode != core.CodeHostExcluded || got.ErrorParams["host"] != pinHost {
		t.Errorf("reads as %q %v, want %q for %s", got.ErrorCode, got.ErrorParams, core.CodeHostExcluded, pinHost)
	}
}

// A rule that leaves nothing able to take a link has to be named as the cause,
// or the failure reads like a link nobody supports.
func TestALinkEveryBackendIsExcludedForSaysSo(t *testing.T) {
	t.Parallel()
	a, _ := pinApp(t)
	queuePinned(a, "h1", "")
	a.mu.Lock()
	url := a.tasks["h1"].URL
	a.mu.Unlock()
	excludeEveryBackend(t, a, url)

	dispatchOnce(a)

	got := taskState(a, "h1")
	if got.Status != core.StatusError {
		t.Errorf("status %q, want it failed", got.Status)
	}
	wantHostExcluded(t, got)
}

func TestALinkStagedUnderARuleThatExcludesEveryBackendSaysSo(t *testing.T) {
	t.Parallel()
	a, _ := pinApp(t)
	url := "https://" + pinHost + "/staged.bin"
	excludeEveryBackend(t, a, url)

	created := a.AddLinks([]string{url}, "")

	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want 1", len(created))
	}
	wantHostExcluded(t, *created[0])
}

// The priority card narrowed to one host shows the order that host's links are
// asked in, so the rule has to show there too.
func TestTheChainShownForAHostFollowsItsRule(t *testing.T) {
	t.Parallel()
	a, _ := pinApp(t)
	ruleForPinHost(t, a, settings.HostRule{Prefer: "torbox", Exclude: []string{"alldebrid"}})

	var ids []string
	for _, r := range a.ResolverPriority(pinHost) {
		ids = append(ids, r.ID)
	}
	if len(ids) == 0 || ids[0] != "torbox" {
		t.Errorf("chain for %s = %v, want the preferred torbox first", pinHost, ids)
	}
	if slices.Contains(ids, "alldebrid") {
		t.Errorf("chain for %s = %v, want the excluded alldebrid left out", pinHost, ids)
	}
	if got := a.stagingResolverFor("https://" + pinHost + "/c.bin"); got == nil || got.Info().ID != "torbox" {
		t.Errorf("a link is collected with %v, want the preferred torbox", got)
	}
}

// The pin is somebody's decision about one download, the rule a default for
// the host, so the pin wins over both halves of it.
func TestAPinOutranksTheHostRule(t *testing.T) {
	t.Parallel()
	a, bes := pinApp(t)
	ruleForPinHost(t, a, settings.HostRule{Prefer: "alldebrid", Exclude: []string{"torbox"}})
	queuePinned(a, "h1", "torbox")

	dispatchOnce(a)

	wantHandled(t, bes["torbox"], "h1")
	wantNothingHandled(t, bes["alldebrid"], "the host rule overruled a pin")
}
