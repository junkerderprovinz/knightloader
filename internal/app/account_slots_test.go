package app

// A second account on the same service: rewireBackends registers one slot per
// account, and routing tries a benched key's sibling on the same service before
// moving on to the next service.
//
// The rewire tests swap debridRoutingHosts instead of calling a real debrid
// API. The dispatch tests use fakeResolver and isolateResolvers
// (app_health_test.go), since a real debrid backend would spend a real unlock.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// slotTestHosts replaces rewireBackends' one live call with a fixed host set,
// restored when the test returns.
func slotTestHosts(t *testing.T, hosts ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, h := range hosts {
		set[h] = true
	}
	orig := debridRoutingHosts
	debridRoutingHosts = func(*App, debrid.Service) map[string]bool { return set }
	t.Cleanup(func() { debridRoutingHosts = orig })
}

// seedAccount stores one credential straight through the store, skipping
// SetAccountCredential's own rewire so the test decides when that happens.
func seedAccount(t *testing.T, a *App, service, account, key string) {
	t.Helper()
	if err := a.Accounts.SetCredential(service, account, accounts.Credential{APIKey: key}); err != nil {
		t.Fatal(err)
	}
}

// benchAccount puts one account out of action through the tracker routing
// reads, as a failed download does.
func benchAccount(t *testing.T, a *App, service, account string) {
	t.Helper()
	if _, started := a.acctHealthTracker().ReportFailure(service, account, accounts.HealthTempDisabled, "seed", time.Hour); !started {
		t.Fatalf("setup: expected the bench for %s/%s to start", service, account)
	}
}

func resolverIDOf(res resolver.Resolver) string {
	if res == nil {
		return "<nil>"
	}
	return res.Info().ID
}

func registeredIDs(a *App) map[string]bool {
	out := map[string]bool{}
	for _, id := range a.Registry.IDs() {
		out[id] = true
	}
	return out
}

// newSlotApp is an App with two AllDebrid accounts and one Real-Debrid account
// wired for real.
func newSlotApp(t *testing.T, host string) *App {
	t.Helper()
	// A JD sidecar or a container-supplied key would add unrelated entries to
	// the chain.
	t.Setenv("KL_JD", "")
	t.Setenv("KL_ALLDEBRID", "")
	t.Setenv("KL_REALDEBRID", "")
	a := newAccountsTestApp(t)
	slotTestHosts(t, host)
	seedAccount(t, a, "alldebrid", "", "key-default")
	seedAccount(t, a, "alldebrid", "work", "key-second")
	seedAccount(t, a, "realdebrid", "", "key-realdebrid")
	a.rewireBackends()
	return a
}

// A second stored account needs its own routing entry and backend, or it can
// be displayed but never used.
func TestRewireRegistersOneSlotPerAccount(t *testing.T) {
	a := newSlotApp(t, "slots-wired.example")

	ids := registeredIDs(a)
	for _, want := range []string{"alldebrid", "alldebrid#work", "realdebrid"} {
		if !ids[want] {
			t.Fatalf("resolver %q is not registered; registry holds %v", want, a.Registry.IDs())
		}
	}

	// Two different backends, each with its own credential; a shared one would
	// send both slots out on one key.
	first, second := a.backendFor("alldebrid"), a.backendFor("alldebrid#work")
	if first == a.Engine || second == a.Engine {
		t.Fatalf("backendFor fell through to the engine for a wired debrid slot (default: %T, named: %T)", first, second)
	}
	if first == second {
		t.Error("both AllDebrid slots share one backend; the second account's own key would never be used")
	}
}

// With the first AllDebrid key benched, the link goes to the second AllDebrid
// key before Real-Debrid.
func TestSecondAccountIsTriedBeforeTheNextService(t *testing.T) {
	const host = "slots-fallback.example"
	a := newSlotApp(t, host)
	task := &core.Task{URL: "https://" + host + "/file.bin"}

	// With everything healthy the default account takes the link.
	if got := resolverIDOf(a.resolverForTaskLocked(task)); got != "alldebrid" {
		t.Fatalf("fixture broken: a healthy chain routes to %q, want alldebrid", got)
	}

	benchAccount(t, a, "alldebrid", "")
	if got := resolverIDOf(a.resolverForTaskLocked(task)); got != "alldebrid#work" {
		t.Fatalf("resolverForTaskLocked = %q, want alldebrid#work; the same service's second account comes before the next service", got)
	}

	// Once both AllDebrid accounts are out, the next service takes it.
	benchAccount(t, a, "alldebrid", "work")
	if got := resolverIDOf(a.resolverForTaskLocked(task)); got != "realdebrid" {
		t.Fatalf("resolverForTaskLocked = %q, want realdebrid once every AllDebrid account is benched", got)
	}
}

// A task that came back from the first key is offered the same service's
// second key next.
func TestNextResolverWalksToTheSecondAccountFirst(t *testing.T) {
	const host = "slots-next.example"
	a := newSlotApp(t, host)
	url := "https://" + host + "/file.bin"

	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid"}); next != "alldebrid#work" {
		t.Errorf("nextResolverLocked after alldebrid = %q, want alldebrid#work", next)
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid#work"}); next != "realdebrid" {
		t.Errorf("nextResolverLocked after alldebrid#work = %q, want realdebrid", next)
	}

	// Credential saves, toggles and the host refresh all rewire again, and
	// Register re-appends an existing id, so the order holds only if the
	// accounts are re-registered in routedAccounts' order.
	a.rewireBackends()
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid"}); next != "alldebrid#work" {
		t.Errorf("nextResolverLocked after a second rewire = %q, want alldebrid#work", next)
	}
}

// Two AllDebrid accounts are two entries in the dispatch chain but one row on
// the priority ladder, under the service id a hand-arranged order names.
func TestResolverPriorityShowsOneRowPerService(t *testing.T) {
	a := newSlotApp(t, "slots-ladder.example")

	seen := map[string]int{}
	for _, info := range a.ResolverPriority("") {
		seen[info.ID]++
	}
	if seen["alldebrid"] != 1 {
		t.Fatalf("ResolverPriority lists alldebrid %d times, want exactly 1: %+v", seen["alldebrid"], a.ResolverPriority(""))
	}
	if seen["alldebrid#work"] != 0 {
		t.Error("ResolverPriority exposes a raw account slot id; the ladder has no label for one and would save it straight into ResolverOrder on the next drag")
	}
}

// A hand-arranged order names services, so putting Real-Debrid above AllDebrid
// moves both AllDebrid accounts down together.
func TestHandOrderMovesEveryAccountOfTheService(t *testing.T) {
	const host = "slots-handorder.example"
	a := newSlotApp(t, host)
	url := "https://" + host + "/file.bin"

	cfg := a.Settings.Get()
	cfg.ResolverOrder = []string{"realdebrid", "alldebrid"}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}

	if got := resolverIDOf(a.resolverForTaskLocked(&core.Task{URL: url})); got != "realdebrid" {
		t.Fatalf("resolverForTaskLocked = %q, want realdebrid; the hand-arranged order wins", got)
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "realdebrid"}); next != "alldebrid" {
		t.Errorf("nextResolverLocked after realdebrid = %q, want alldebrid", next)
	}
	// The named account followed its service down instead of keeping its
	// automatic number, which would have put it first.
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid"}); next != "alldebrid#work" {
		t.Errorf("nextResolverLocked after alldebrid = %q, want alldebrid#work", next)
	}
}

// A download failing on the first key comes back queued on the second key of
// the same service, and only the first key is benched.
func TestInFlightFailureMovesToTheSiblingAccount(t *testing.T) {
	a := newAccountsTestApp(t)
	const testURL = "https://slots-inflight.example/file.bin"
	isolateResolvers(a,
		fakeResolver{id: "alldebrid", prio: 34, host: "slots-inflight.example"},
		fakeResolver{id: "alldebrid#work", prio: 34, host: "slots-inflight.example"},
		fakeResolver{id: "realdebrid", prio: 33, host: "slots-inflight.example"},
	)

	task := &core.Task{ID: "t1", URL: testURL, Resolver: "alldebrid", Status: core.StatusRunning}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "alldebrid: an unexpected failure"})

	a.mu.Lock()
	status, res := task.Status, task.Resolver
	a.mu.Unlock()

	if status == core.StatusError {
		t.Fatal("the task settled as StatusError; a failure on one of two accounts is not the link's fault")
	}
	if res != "alldebrid#work" {
		t.Errorf("task.Resolver = %q, want alldebrid#work", res)
	}
	if a.acctHealthTracker().Usable("alldebrid", "") {
		t.Error("the account that failed still reads usable")
	}
	if !a.acctHealthTracker().Usable("alldebrid", "work") {
		t.Error("the second account was benched by the first one's failure")
	}
}

// Per-account benching depends on reading the account out of the slot id.
func TestAccountForResolverReadsTheAccountOutOfTheSlotID(t *testing.T) {
	a := newAccountsTestApp(t)

	cases := []struct{ id, service, account string }{
		{"alldebrid", "alldebrid", ""},
		{"alldebrid#work", "alldebrid", "work"},
		{"torbox#second key", "torbox", "second key"},
		// Only the first "#" separates; later ones belong to the account id.
		{"realdebrid#a#b", "realdebrid", "a#b"},
	}
	for _, c := range cases {
		service, account, ok := a.accountForResolverLocked(c.id)
		if !ok || service != c.service || account != c.account {
			t.Errorf("accountForResolverLocked(%q) = (%q, %q, %v), want (%q, %q, true)", c.id, service, account, ok, c.service, c.account)
		}
	}

	// Resolvers without a stored debrid credential have no tracked account; a
	// captcha solver has a credential but routes nothing.
	for _, id := range []string{"jd", "ytdlp", "direct", "http", "torrent", "2captcha", ""} {
		if _, _, ok := a.accountForResolverLocked(id); ok {
			t.Errorf("accountForResolverLocked(%q) ok = true, want false", id)
		}
		if !a.accountRoutableLocked(id) {
			t.Errorf("accountRoutableLocked(%q) = false, want true; health must not block a resolver with no tracked account", id)
		}
	}

	// The bench applies to one account only.
	benchAccount(t, a, "alldebrid", "")
	if a.accountRoutableLocked("alldebrid") {
		t.Error("the benched account still reads routable")
	}
	if !a.accountRoutableLocked("alldebrid#work") {
		t.Error("benching one account took the service's other account down with it")
	}
}

// An account deleted or switched off loses its slot, or links would keep
// routing to a credential the store no longer has.
func TestRewireDropsAGoneAccountsSlot(t *testing.T) {
	a := newSlotApp(t, "slots-removed.example")
	if !registeredIDs(a)["alldebrid#work"] {
		t.Fatal("fixture broken: the second account was never registered")
	}

	a.SetAccountEnabled("alldebrid", "work", false)
	ids := registeredIDs(a)
	if ids["alldebrid#work"] {
		t.Error("a switched-off account kept its slot")
	}
	if !ids["alldebrid"] {
		t.Error("switching the second account off unregistered the first one too")
	}

	seedAccount(t, a, "alldebrid", "", "")
	a.rewireBackends()
	if registeredIDs(a)["alldebrid"] {
		t.Error("the default account kept its slot after its credential was cleared")
	}
}
