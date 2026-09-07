package app

// A SECOND ACCOUNT ON THE SAME SERVICE, at the two points that actually decide
// it: rewireBackends turning stored credentials into one registered slot per
// account, and resolverForTaskLocked/nextResolverLocked/onUpdate walking those
// slots so a benched first key falls through to the second key of the SAME
// service before the next service is asked at all.
//
// Until 2026-09-07 the routing table had exactly one entry per service id and
// rewireBackends read only each service's default account: a person with two
// TorBox keys could add, name and switch on the second one, watch it appear on
// the accounts page with a tier and a traffic figure, and never have a single
// link go through it. These tests fail on that build.
//
// The rewire-level tests drive the real wiring with debridRoutingHosts swapped
// (see its own doc comment) rather than a real debrid API - the same rule
// accounts_test.go's package comment states: a unit test that depends on a
// third party being reachable is not a unit test. The dispatch-level tests use
// the fakeResolver/isolateResolvers pair app_health_test.go already defines,
// because a real debrid.Backend reached by a real dispatch would spend a real
// unlock call.

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

// slotTestHosts points the one live call rewireBackends would make at a fixed
// host set, so a test can seed credentials and rewire without a third party
// being reachable. Restored when the test returns.
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

// benchAccount puts one account out of action the way a real failed download
// does - through the tracker routing itself reads (accountRoutableLocked).
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

// newSlotApp is an App with two AllDebrid accounts and one Real-Debrid
// account wired for real - the fixture both halves of this file's central
// question need: does a service's second key exist in the routing table, and
// is it reached before the next service.
func newSlotApp(t *testing.T, host string) *App {
	t.Helper()
	// A JD sidecar or a container-supplied key would add entries to the chain
	// that have nothing to do with what is being measured here.
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

// TestRewireRegistersOneSlotPerAccount is the finding itself: a second stored
// account on a service has to become its own entry in the routing table with
// its own backend, or it is a credential the app can display and never use.
func TestRewireRegistersOneSlotPerAccount(t *testing.T) {
	a := newSlotApp(t, "slots-wired.example")

	ids := registeredIDs(a)
	for _, want := range []string{"alldebrid", "alldebrid#work", "realdebrid"} {
		if !ids[want] {
			t.Fatalf("resolver %q is not registered; registry holds %v - a configured account with no slot is one that can never be asked", want, a.Registry.IDs())
		}
	}

	// Two DIFFERENT backends, each built from its own credential. One shared
	// backend would send both slots' downloads out on the same key, which is
	// the same defect wearing a second entry in the registry.
	first, second := a.backendFor("alldebrid"), a.backendFor("alldebrid#work")
	if first == a.Engine || second == a.Engine {
		t.Fatalf("backendFor fell through to the engine for a wired debrid slot (default: %T, named: %T)", first, second)
	}
	if first == second {
		t.Error("both AllDebrid slots share one backend; the second account's own key would never be used")
	}
}

// TestSecondAccountIsTriedBeforeTheNextService is THE row this whole change
// exists for. With the first AllDebrid key benched, the link must go to the
// SECOND AllDebrid key - not down to Real-Debrid, which is what a routing
// table with one entry per service could only ever do.
func TestSecondAccountIsTriedBeforeTheNextService(t *testing.T) {
	const host = "slots-fallback.example"
	a := newSlotApp(t, host)
	task := &core.Task{URL: "https://" + host + "/file.bin"}

	// The state the next assertion is a change FROM, checked rather than
	// assumed: with everything healthy the default account takes the link.
	if got := resolverIDOf(a.resolverForTaskLocked(task)); got != "alldebrid" {
		t.Fatalf("fixture broken: a healthy chain routes to %q, want alldebrid", got)
	}

	benchAccount(t, a, "alldebrid", "")
	if got := resolverIDOf(a.resolverForTaskLocked(task)); got != "alldebrid#work" {
		t.Fatalf("resolverForTaskLocked = %q, want alldebrid#work - the same service's second account has to be tried before the next service is asked at all", got)
	}

	// And the service order itself is untouched: once BOTH AllDebrid accounts
	// are out, the next service takes it, exactly as it always did.
	benchAccount(t, a, "alldebrid", "work")
	if got := resolverIDOf(a.resolverForTaskLocked(task)); got != "realdebrid" {
		t.Fatalf("resolverForTaskLocked = %q, want realdebrid once every AllDebrid account is benched - per-account choice must not disturb the order between services", got)
	}
}

// TestNextResolverWalksToTheSecondAccountFirst is the fallback half of the
// same order: a task that has already been handed to the first key and come
// back must be offered the second key of that service next, not the next
// service.
func TestNextResolverWalksToTheSecondAccountFirst(t *testing.T) {
	const host = "slots-next.example"
	a := newSlotApp(t, host)
	url := "https://" + host + "/file.bin"

	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid"}); next != "alldebrid#work" {
		t.Errorf("nextResolverLocked after alldebrid = %q, want alldebrid#work", next)
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid#work"}); next != "realdebrid" {
		t.Errorf("nextResolverLocked after alldebrid#work = %q, want realdebrid - the chain leaves the service only once its accounts are used up", next)
	}

	// And the order survives a re-wire, which is not a hypothetical: every
	// credential save, every enable toggle and the six-hourly host refresh all
	// call rewireBackends again. Registering an id that is already there
	// removes and re-appends it (resolver.Registry.Register), so the accounts
	// of one service keep their arrangement only because they are all
	// re-registered in the same order routedAccounts hands them out in.
	a.rewireBackends()
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid"}); next != "alldebrid#work" {
		t.Errorf("nextResolverLocked after a second rewire = %q, want alldebrid#work - the accounts of one service shuffled", next)
	}
}

// TestResolverPriorityShowsOneRowPerService pins the card's own contract
// against the chain it describes: two AllDebrid accounts are two entries in
// dispatch's chain and exactly ONE row on the Prioritätsreihenfolge ladder,
// under the service's own id - the id a hand-arranged order has to name for
// dynamicPrio to move both accounts together.
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

// TestHandOrderMovesEveryAccountOfTheService is the half dynamicPrio's
// service-level match exists for: a hand-arranged order names services, so
// dragging Real-Debrid above AllDebrid must move BOTH AllDebrid accounts down
// with it. A slot left on its automatic number would be tried after
// everything the order names, splitting a service in half.
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
		t.Fatalf("resolverForTaskLocked = %q, want realdebrid - the hand-arranged order has to win", got)
	}
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "realdebrid"}); next != "alldebrid" {
		t.Errorf("nextResolverLocked after realdebrid = %q, want alldebrid", next)
	}
	// The named account followed its service down rather than being left
	// behind at its automatic 49, which would have put it FIRST.
	if next := a.nextResolverLocked(&core.Task{URL: url, Resolver: "alldebrid"}); next != "alldebrid#work" {
		t.Errorf("nextResolverLocked after alldebrid = %q, want alldebrid#work - a hand order names a service and moves every account of it", next)
	}
}

// TestInFlightFailureMovesToTheSiblingAccount is the live half, through
// onUpdate: a download that fails on the first key must come back queued on
// the SECOND key of the same service, and the second key must still read as
// usable - benching a service wholesale on one key's failure would throw away
// the account that was going to rescue the download.
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
		t.Errorf("task.Resolver = %q, want alldebrid#work - the same service's other account gets the next turn", res)
	}
	if a.acctHealthTracker().Usable("alldebrid", "") {
		t.Error("the account that failed still reads usable")
	}
	if !a.acctHealthTracker().Usable("alldebrid", "work") {
		t.Error("the SECOND account was benched by the first one's failure; the two accounts have separate health, or the fallback lands on an account this app has already written off")
	}
}

// TestAccountForResolverReadsTheAccountOutOfTheSlotID pins the mapping every
// health decision above rests on. It is what makes a per-account bench
// possible at all: an id that answered ("alldebrid", "") for both slots would
// bench the wrong key on every failure.
func TestAccountForResolverReadsTheAccountOutOfTheSlotID(t *testing.T) {
	a := newAccountsTestApp(t)

	cases := []struct{ id, service, account string }{
		{"alldebrid", "alldebrid", ""},
		{"alldebrid#work", "alldebrid", "work"},
		{"torbox#second key", "torbox", "second key"},
		// A "#" a person typed into their own account id belongs to the
		// account, not to a third field - the first separator is the only one.
		{"realdebrid#a#b", "realdebrid", "a#b"},
	}
	for _, c := range cases {
		service, account, ok := a.accountForResolverLocked(c.id)
		if !ok || service != c.service || account != c.account {
			t.Errorf("accountForResolverLocked(%q) = (%q, %q, %v), want (%q, %q, true)", c.id, service, account, ok, c.service, c.account)
		}
	}

	// Everything that owns a resolver id for a reason other than a stored
	// debrid credential stays out - a captcha solver has a catalogue entry and
	// a credential, and still routes nothing.
	for _, id := range []string{"jd", "ytdlp", "direct", "http", "torrent", "2captcha", ""} {
		if _, _, ok := a.accountForResolverLocked(id); ok {
			t.Errorf("accountForResolverLocked(%q) ok = true, want false - this resolver has no tracked account", id)
		}
		if !a.accountRoutableLocked(id) {
			t.Errorf("accountRoutableLocked(%q) = false, want true - a resolver with no tracked account must never be blocked by health", id)
		}
	}

	// And the bench really is per account, which is the whole point of
	// carrying one in the id.
	benchAccount(t, a, "alldebrid", "")
	if a.accountRoutableLocked("alldebrid") {
		t.Error("the benched account still reads routable")
	}
	if !a.accountRoutableLocked("alldebrid#work") {
		t.Error("benching one account took the service's other account down with it")
	}
}

// TestRewireDropsAGoneAccountsSlot is the other end of the same wiring: a
// second account that is deleted or switched off must lose its slot, or its
// links keep routing to a backend holding a credential the store no longer
// has. The old sweep could not express this at all - it only ever asked
// whether a SERVICE was still configured.
func TestRewireDropsAGoneAccountsSlot(t *testing.T) {
	a := newSlotApp(t, "slots-removed.example")
	if !registeredIDs(a)["alldebrid#work"] {
		t.Fatal("fixture broken: the second account was never registered")
	}

	a.SetAccountEnabled("alldebrid", "work", false)
	ids := registeredIDs(a)
	if ids["alldebrid#work"] {
		t.Error("a switched-off account kept its slot; it would keep taking links on a key the user has turned off")
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
