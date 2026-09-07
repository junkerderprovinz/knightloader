package app

// The handover to a download backend and everything that comes back from one:
// which backend a task goes to, when it may go, and what happens to a task that
// finishes, fails or asks to be retried.

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// modeForLocked answers "is this going out on an account, or anonymously" for
// the resolver a task has just been routed to.
//
// It is a DISPLAY fact and nothing routes on it (jdp, 2026-09-02: "Wenn man
// links runterladen möchte für die kein premium account hinterlegt ist muss das
// angezeigt werden un der link im free modus heruntergeladen werden. wie in
// JD"). The routing half of that sentence is jd.PriorityFor's, which now puts a
// host JD has a plugin for above a blind anonymous GET; this is the half that
// says so on screen.
//
// Three answers, and the empty one is the common case on purpose. A plain file
// on an ordinary web server is neither free nor premium, and putting either word
// on it would answer a question nobody asked - so only a link a HOSTER is on the
// other end of gets a label at all. Caller holds a.mu.
func (a *App) modeForLocked(t *core.Task, resolverID string) core.DownloadMode {
	if t == nil {
		return core.ModeUnknown
	}
	host := hostOf(t.URL)
	if host == "" {
		return core.ModeUnknown
	}
	// A debrid service IS the account, so anything routed to one is premium by
	// construction - accountForResolverLocked is already this app's oracle for
	// "does this resolver have a tracked account".
	if _, _, ok := a.accountForResolverLocked(resolverID); ok {
		return core.ModePremium
	}
	if resolverID == "jd" {
		// A confirmed-active native login on the sidecar is premium; JD knowing
		// the host without one is exactly what free mode means here.
		if jd.HostActive(host) {
			return core.ModePremium
		}
		if jd.HostKnown(host) {
			return core.ModeFree
		}
	}
	return core.ModeUnknown
}

// dynamicPrio is what res's priority actually is for this specific url -
// ordinarily just Info().Prio, frozen at Registry.Register time and blind to
// the URL entirely, except for JD. A confirmed-active native hoster login
// (internal/hosterauth's reconciler, via jd.SetHostActive) earns that host's
// links a priority above resolver.Direct's fixed 40, and a single
// registration-time number cannot express "above Direct for this host,
// unchanged for every other one" - see jd.PriorityFor's own doc comment,
// which names this exact function as the wiring it was built for and did
// not yet have.
//
// Special-cased on the resolver id rather than an interface because
// jd.Resolver carries the answer as a package-level function, not a method -
// internal/resolver/jd/resolver.go is agent 6D's file this wave, not this
// one's to add a method to. Anything else answers the same number Info()
// always has; if a second resolver ever needs the same per-URL treatment,
// that is the point to grow this into an interface both can implement.
//
// order is settings.Settings.ResolverOrder, the hand-arranged sequence from
// the Prioritätsreihenfolge card (jdp, 2026-09-07: "Die Prioritätsreihenfolge
// soll per drag and drop anordenbar sein"). It is checked FIRST and it wins
// outright: a person who has dragged this list into an order meant it, and a
// hand-made order silently overruled by an automatic boost would be the same
// control-that-does-nothing this whole ranking exists to avoid. Anything the
// order does not name keeps its automatic number, which puts it below every
// named one - orderBase is far above the highest automatic priority
// (jd.activeLoginPrio, 60) for exactly that reason.
//
// This ranks only among resolvers that can take the URL at all: Registry.All
// has already filtered to those. So "put yt-dlp above TorBox" cannot send a
// rapidgator link to yt-dlp; it decides which of the ones that COULD take it
// is asked first.
//
// A hand-arranged entry names a SERVICE and moves every account of it. The
// order comes from the Prioritätsreihenfolge card, which shows one row per
// service (see ResolverPriority), while the chain being ranked here holds one
// entry per configured ACCOUNT (resolver.SlotID). Matching the full slot id
// alone would leave a person's second AllDebrid key on its automatic number
// while their first sat at the top of a hand-made list - the one thing this
// must never do, because a service split in half by the order is a service
// whose second key gets tried after everything else instead of right after
// the first. An order that does name a slot in full is honoured as written:
// that is somebody being deliberately more specific, not a mistake.
func dynamicPrio(res resolver.Resolver, url string, order []string) int {
	id := res.Info().ID
	for i, want := range order {
		if want == id {
			return orderBase - i
		}
	}
	if service, _ := resolver.SplitSlot(id); service != id {
		for i, want := range order {
			if want == service {
				return orderBase - i
			}
		}
	}
	if id == "jd" {
		return jd.PriorityFor(url)
	}
	return res.Info().Prio
}

// orderBase is the priority the first entry of a hand-arranged order gets;
// each following entry gets one less. 1000 rather than something just above
// 60, so that a long hand-made list cannot run down into the automatic band
// and have its tail re-mixed with resolvers it deliberately outranks.
const orderBase = 1000

// rankedChain is chain, stably re-ordered by dynamicPrio rather than trusted
// in the registry's own frozen order. Stable, so that two matches dynamicPrio
// does not distinguish keep exactly the order Registry.All (and so the
// registry's own Prio-at-Register-time sort) already gave them - the re-rank
// only ever moves JD, and only for a host it has just earned a boost for.
func rankedChain(chain []resolver.Resolver, url string, order []string) []resolver.Resolver {
	out := make([]resolver.Resolver, len(chain))
	copy(out, chain)
	sort.SliceStable(out, func(i, j int) bool {
		return dynamicPrio(out[i], url, order) > dynamicPrio(out[j], url, order)
	})
	return out
}

// ResolverPriority is the order services are ACTUALLY asked in, which is what
// the Prioritätsreihenfolge card on the Accounts page shows.
//
// It exists because Registry.AllInfo and Registry.PriorityFor answer the
// registry's own frozen, registration-time order, and dispatch has not walked
// that order since dynamicPrio arrived: a hand-arranged ResolverOrder and JD's
// per-host boost both re-rank it. A card reading straight off the registry
// therefore showed a ladder the downloader does not use - a list that is
// merely plausible, which for a diagnostic display is worse than none.
//
// host empty means the whole registered set, with no URL to match against;
// given, it is narrowed to the chain that host would actually walk.
//
// ONE ROW PER SERVICE, not per account slot. A service with two configured
// accounts has two entries in the chain (resolver.SlotID), and both answer
// this card's question - "when is AllDebrid asked" - identically. A second row
// would say the same thing twice under an id the card has no label for, and
// worse, it would travel straight back into settings.ResolverOrder on the next
// drag, because the ladder saves the ids it was handed. Which of a service's
// accounts is tried first is not arranged here at all; it is the order they
// are wired in (routedAccounts, app_accounts.go).
func (a *App) ResolverPriority(host string) []resolver.Info {
	host = strings.TrimSpace(host)
	url := ""
	chain := a.Registry.List()
	if host != "" {
		url = "https://" + host + "/"
		chain = a.Registry.All(url)
	}
	ranked := rankedChain(chain, url, a.Settings.Get().ResolverOrder)
	out := make([]resolver.Info, 0, len(ranked))
	seen := map[string]bool{}
	for _, res := range ranked {
		info := res.Info()
		service, _ := resolver.SplitSlot(info.ID)
		if seen[service] {
			continue
		}
		seen[service] = true
		// The service's own id, so a service configured with nothing but a
		// named account still reads as itself here and a drag writes an order
		// dynamicPrio matches.
		info.ID = service
		out = append(out, info)
	}
	return out
}

// setWaitingLocked writes each queued task's reason for not running and clears
// it everywhere else in the given set.
//
// `only` is the blanket answer (the halted case, where every queued task has the
// same one); `per` is the per-task map the dispatch loop fills in. Passing both
// is how one function serves the two callers without either of them having to
// build a full map.
//
// It broadcasts only what CHANGED. A dispatch pass runs on nearly every event in
// the app, and a queue of two hundred waiting downloads would otherwise put two
// hundred identical task messages on every open browser several times a second.
//
// Caller holds a.mu.
func (a *App) setWaitingLocked(ids []string, only core.Waiting, per ...map[string]core.Waiting) {
	var changed []core.Task
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		want := only
		for _, m := range per {
			if w, ok := m[id]; ok {
				want = w
			}
		}
		if t.Waiting == want {
			continue
		}
		t.Waiting = want
		changed = append(changed, *t)
	}
	if len(changed) > 0 {
		// Off this goroutine for the same reason settled is - see there.
		a.spawn(func() { a.publishTasks(changed) })
	}
}

// hostCapFor is the per-host connection ceiling the resolver about to carry
// this task has an opinion about, or 0 for one that does not - see
// resolver.HostCapper. It is read here, once, at the exact point connsFor's
// own ceilings list is built, so a multihoster's per-host fact joins that
// list precisely as documented on connsFor: one more ceiling, never a second
// competing limit.
func hostCapFor(res resolver.Resolver, host string) int {
	hc, ok := res.(resolver.HostCapper)
	if !ok {
		return 0
	}
	return hc.HostCap(host)
}

// maxPerHostFor is how many transfers this ONE host may have in flight: its
// own entry in settings.HostRules when it has one, and the global MaxPerHost
// when it does not.
//
// Read at the single point cfg.MaxPerHost was read before, which is what keeps
// this an override rather than a second limit sitting beside the first. Hosters
// do not agree with each other - one tolerates eight connections, the next
// refuses from two - and with one number for all of them the only safe value is
// the strictest host's, which throttles every other download on the box for the
// sake of one hoster.
//
// A host with no entry answers exactly what it answered before this table
// existed, and the entry can be larger as well as smaller than the global: the
// per-host figure is what the PERSON says this host tolerates, and it is still
// under the global concurrency limit, which is checked a few lines above this
// one and can only ever be reached first.
func maxPerHostFor(cfg settings.Settings, host string) int {
	if r := cfg.HostRuleFor(host); r.MaxPerHost > 0 {
		return r.MaxPerHost
	}
	return cfg.MaxPerHost
}

// resolverForTaskLocked picks the resolver a task should be dispatched
// through: the one recorded on it if that still exists AND its account is
// currently usable, else the best current match by rankedChain whose account
// is also usable - see accountRoutableLocked (app_health.go). A resolver
// whose account has been benched is passed over here rather than at the
// registry: it stays fully registered (docs/build-plan.md package 14, row 2),
// so a task recorded on a now-benched resolver falls through to the next one
// in the chain instead of being stranded on a backend this function refuses
// to return.
//
// A SECOND ACCOUNT ON THE SAME SERVICE IS JUST THE NEXT LINK IN THAT CHAIN,
// and it needs no branch of its own here. rewireBackends registers one entry
// per configured account at the same priority (resolver.SlotID,
// routedAccounts), so a person's two AllDebrid keys sit next to each other in
// rankedChain, ahead of whatever service comes next: the loop below skips the
// benched one and lands on the other before it ever reaches Real-Debrid. The
// same holds for nextResolverLocked, which walks the identical order.
//
// The fallback loop below is written out inline rather than delegated to a
// helper in app_health.go: the one that used to make this identical decision
// (against the registry's frozen order) had no seam to hand it rankedChain's
// re-ranked order instead, and app_health.go is another agent's file this
// wave. accountRoutableLocked itself is exported for exactly this: called
// from here, never edited here.
//
// Caller holds a.mu.
func (a *App) resolverForTaskLocked(t *core.Task) resolver.Resolver {
	// A pin is checked first and it is the whole answer: neither the recorded
	// backend below nor the ranked chain under it gets a say, because both of
	// those are ways for the app to pick a backend and a pin is somebody
	// having already picked one. It can still come back nil - see
	// pinnedResolverLocked - and the caller has to say so out loud rather than
	// fall through to the chain.
	if t.ResolverPin != "" {
		return a.pinnedResolverLocked(t)
	}
	if t.Resolver != "" && a.accountRoutableLocked(t.Resolver) {
		for _, res := range a.Registry.All(t.URL) {
			if res.Info().ID == t.Resolver {
				return res
			}
		}
	}
	for _, res := range rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder) {
		if a.accountRoutableLocked(res.Info().ID) {
			return res
		}
	}
	return nil
}

// pinnedResolverLocked picks the backend a pinned task must go through, or nil.
//
// It walks the same ranked chain everything else does and takes the first
// entry the pin names AND whose account is currently usable. Two things fall
// out of that shape, and both are the point:
//
//   - A pin naming a SERVICE with two configured accounts is satisfied by
//     either of them, in the order the chain already has them. That is what
//     "pin this to AllDebrid" means to the person who typed it; making them
//     name a slot to get their second key tried would be a pin that breaks the
//     moment a key is added.
//   - ACCOUNT HEALTH IS NOT BYPASSED. A benched, invalid or expired account is
//     skipped here exactly as it is for an unpinned task, and if that leaves
//     nothing the answer is nil rather than a quiet hop to another service.
//     A pin that could be overruled by the app's own ranking whenever the
//     named backend was inconvenient would not be a pin.
//
// Caller holds a.mu.
func (a *App) pinnedResolverLocked(t *core.Task) resolver.Resolver {
	for _, res := range rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder) {
		id := res.Info().ID
		if !pinMatches(t.ResolverPin, id) {
			continue
		}
		if a.accountRoutableLocked(id) {
			return res
		}
	}
	return nil
}

// pinFailureLocked is the sentence and the typed cause for a pinned task that
// has nowhere to go, and it tells the two causes apart because the two fixes
// are nothing alike.
//
// A pin naming a backend that does not claim this link at all is a mistake
// somebody made and can undo: the pin is wrong, or the link is not the kind
// that backend fetches. That is ReasonUnsupported and it will not mend itself.
//
// A pin naming a backend that DOES claim the link, whose account is not usable
// right now, is the case this whole design turns on. It is reported as a
// failure - visible, on the row, with the backend named - rather than being
// held quietly the way an unpinned task with the same problem is
// (WaitingAccount, see dispatchLocked). The unpinned task is held because the
// app can still choose; a pinned one has been told not to choose, so silence
// would leave somebody watching a row that says "waiting" for a bench that
// might last six hours, with nothing on screen connecting it to the backend
// they nailed it to. ReasonAuth rather than a bespoke value: the credential is
// what is wrong, which is exactly what that reason already means, and the
// ordinary retry backoff then applies - so an account that comes back within
// the retry budget picks the task up again on its own.
//
// Caller holds a.mu.
func (a *App) pinFailureLocked(t *core.Task) (string, core.Reason) {
	for _, res := range a.Registry.All(t.URL) {
		if pinMatches(t.ResolverPin, res.Info().ID) {
			return "pinned to " + t.ResolverPin + ", and that backend's account is not usable right now", core.ReasonAuth
		}
	}
	return "pinned to " + t.ResolverPin + ", which does not handle this link", core.ReasonUnsupported
}

// pinMatches reports whether a registered resolver id satisfies a pin.
//
// The pin may name the full slot ("alldebrid#work") or just the service
// ("alldebrid"), which is deliberately the same rule settings.ResolverOrder is
// matched by (see dynamicPrio). One vocabulary for "which backend do you mean"
// across both features is worth more than either of them being individually
// stricter, and a person who writes a service name into either one means all
// of its accounts in both.
func pinMatches(pin, resolverID string) bool {
	if pin == resolverID {
		return true
	}
	service, _ := resolver.SplitSlot(resolverID)
	return pin == service
}

// ResolverPinnable reports whether resolverID could be pinned to a task at
// all, so a request naming a backend this instance does not have is refused
// where somebody can read the refusal rather than turning into a row that
// fails on the next dispatch pass for reasons nothing on screen explains.
//
// An empty id is valid: that is how a pin is taken off again.
//
// Checked against the REGISTRY, which holds only the backends actually wired
// up right now - a debrid service appears there once a key for it is stored
// (rewireBackends), so this refuses "alldebrid" on an instance with no
// AllDebrid key, which is the honest answer.
func (a *App) ResolverPinnable(resolverID string) error {
	id := strings.TrimSpace(resolverID)
	if id == "" {
		return nil
	}
	for _, registered := range a.Registry.IDs() {
		if pinMatches(id, registered) {
			return nil
		}
	}
	return fmt.Errorf("%q is not a download backend this instance has", id)
}

// PinResolver nails each of these tasks to one backend, or takes the pin off
// again when resolverID is empty.
//
// It dispatches straight afterwards, and that is the half that makes the
// control feel like one: a queued task pinned to a working backend starts on
// this very pass, and one pinned to a backend that cannot take it fails on it,
// with the reason on the row. Waiting for the next unrelated event to reveal
// which of the two happened would make a pin something you set and then watch.
//
// A RUNNING transfer is not moved. The bytes are already coming from the old
// backend and nothing here can retarget them mid-flight; the pin decides where
// the NEXT attempt goes, which is what a restart is for. Said here because the
// alternative - quietly stopping a transfer because somebody changed a
// dropdown - is a bigger act than the control looks like it is making.
func (a *App) PinResolver(ids []string, resolverID string) error {
	id := strings.TrimSpace(resolverID)
	// Checked before a single task is touched, the same contract
	// SetTaskOptions states: refusing halfway through leaves a selection with
	// the first eight rows edited and an error that says nothing about which.
	if err := a.ResolverPinnable(id); err != nil {
		return err
	}
	a.mu.Lock()
	var touched []string
	for _, taskID := range ids {
		t := a.tasks[taskID]
		if t == nil || t.ResolverPin == id {
			continue
		}
		t.ResolverPin = id
		touched = append(touched, taskID)
	}
	if len(touched) > 0 {
		a.dispatchLocked()
	}
	// The copies are taken AFTER the dispatch, never before it: the pass can
	// settle one of these very tasks as failed, and a copy snapshotted a line
	// earlier would be saved over that verdict a moment later with a row that
	// still said "queued".
	changed := make([]core.Task, 0, len(touched))
	for _, taskID := range touched {
		if t := a.tasks[taskID]; t != nil {
			changed = append(changed, *t)
		}
	}
	a.mu.Unlock()
	for i := range changed {
		_ = a.Store.Save(&changed[i])
		a.Hub.Broadcast("task", &changed[i])
	}
	return nil
}

// nextResolverLocked returns the resolver that should try after the one the
// task just used, or "" when the chain is exhausted. Walks rankedChain rather
// than the registry's raw order for the same reason resolverForTaskLocked
// does: a task that started on a dynamically-promoted JD must fall back to
// whatever actually came next in THAT order, not the frozen one it never used.
// Caller holds a.mu.
func (a *App) nextResolverLocked(t *core.Task) string {
	// A pinned task has no next: the chain is what the pin replaces. Answered
	// here, at the one function every fallback path in this file asks, rather
	// than at each of those paths - a pin honoured by the dispatcher and
	// forgotten by the fallback would move the task to another backend the
	// first time the pinned one said "not mine", which is the silent diversion
	// the whole feature exists to make impossible.
	if t.ResolverPin != "" {
		return ""
	}
	chain := rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder)
	for i, res := range chain {
		if res.Info().ID == t.Resolver {
			if i+1 < len(chain) {
				return chain[i+1].Info().ID
			}
			return "" // the chain is exhausted
		}
	}
	// The recorded backend is not registered any more â€” a credential was
	// removed, or a binary went missing. Restart the chain from the top rather
	// than leaving the task stranded on a backend that no longer exists.
	for _, res := range chain {
		if res.Info().ID != t.Resolver {
			return res.Info().ID
		}
	}
	return ""
}

// dispatchLocked starts queued tasks while slots are free. FIFO with per-host
// skip-ahead: a host at its limit doesn't block other hosts behind it.
// routeForLocked picks the outbound connection this task should leave by, and
// reports which one that was.
//
// The task's own choice wins when it made one; otherwise the picker walks the
// configured list for this host, honouring filters, per-connection limits and
// the ban list. A picker that has not been built yet, or a list with nothing
// usable in it, both mean the same thing to the caller: leave by the machine's
// own address, which is what the zero Route says.
//
// A connection whose Route cannot be built is treated as no connection rather
// than as a failed download. Sanitize has already refused the malformed ones, so
// reaching this is a bug rather than a user error, and taking the whole download
// down for it would turn a bad proxy row into a queue that stops.
//
// Caller holds a.mu.
func (a *App) routeForLocked(t *core.Task, host string) (proxycfg.Route, string) {
	p := a.picker
	if p == nil {
		return proxycfg.Route{}, ""
	}
	// What each connection is already carrying, so a per-connection limit means
	// something. Counted over the running set, which is the only set that has a
	// connection assigned.
	inUse := map[string]int{}
	for id := range a.active {
		if other := a.tasks[id]; other != nil && other.Connection != "" {
			inUse[other.Connection]++
		}
	}
	e, ok := p.PickFor(t.Connection, host, inUse)
	if !ok {
		return proxycfg.Route{}, ""
	}
	r, err := e.Route()
	if err != nil {
		log.Printf("connection %s is unusable, going out directly: %v", e.ID, err)
		return proxycfg.Route{}, ""
	}
	return r, e.ID
}

// maxForcedDownloads bounds the pool forced tasks run in, on top of the ordinary
// concurrency limit rather than inside it.
//
// It exists because "start now" on a selection is one keystroke: without a bound,
// forcing two hundred links opens two hundred transfers, every one of them slower
// than the four that would have finished by now. JDownloader carries the same
// idea as GeneralSettings.MaxForcedDownloads.
const maxForcedDownloads = 3

// countStartLocked books a task into the slot accounting it belongs to.
//
// One function rather than two lines at each of the two start sites, because the
// two sites drifting apart is exactly how a forced task ends up counted as an
// ordinary one and quietly evicts somebody else's slot.
//
// Caller holds a.mu.
func (a *App) countStartLocked(t *core.Task, host string, perHost map[string]int, forced, normal *int) {
	if t.Forced {
		*forced++
		return
	}
	*normal++
	perHost[host]++
}

// Caller holds a.mu.
func (a *App) dispatchLocked() {
	// Started here, once (app_captcha.go's own sync.Once), rather than from
	// cmd/knightloader/main.go the way StartHosterAuth is: main.go is not
	// this wave's file to add a line to (build-plan.md section 3's Wave 7
	// table), and dispatchLocked is the closest thing this file owns to
	// "runs once at start-up and on nearly everything after" - the schedule
	// runner's first Apply calls this directly, before a browser could have
	// loaded the page. Ahead of the halted check on purpose: a captcha wait
	// has nothing to do with whether the queue is paused, and a poller that
	// only started once somebody resumed the queue would miss every
	// challenge raised while it was halted.
	a.ensureCaptchaPoller()
	// Started from here for exactly the reasons the captcha poller above is,
	// and with the same sync.Once shape - see ensureStallWatcher. Ahead of the
	// halted check for the same reason as well: a transfer that was already
	// running when somebody stopped the queue is precisely the one that stands
	// still overnight, and a watcher that only woke up once the queue was
	// moving again would not be looking at it.
	a.ensureStallWatcher()
	// And the disk guard's own loop, with the same sync.Once shape and for the
	// same reasons - see ensureDiskWatcher. Ahead of the halted check too: a
	// volume filling up is a fact about the machine rather than about the
	// queue, and the watcher has to be running before somebody resumes a queue
	// onto a disk that ran out while it was stopped.
	a.ensureDiskWatcher()
	if a.halted {
		// Every queued row says why nothing is moving, not only the head card.
		// A stopped queue and a full one look identical on a list of rows that
		// all say "waiting", and those are the two situations somebody opening
		// this page is trying to tell apart.
		a.setWaitingLocked(a.queue, core.WaitingHalted)
		return
	}
	cfg := a.Settings.Get()
	a.sortQueueLocked()
	// settled collects what the dispatcher turns down. A task refused in here is
	// refused under the lock, long after every caller took its copy, so the reason
	// has to leave with a copy of its own: without it the store and every open
	// browser keep the task the caller saved â€” "queued", no error â€” and the user
	// is left with a download that never starts and says nothing about why. That
	// is the silent disappearance the staging record exists to prevent, moved one
	// button along.
	var settled []core.Task
	// Why each task that does NOT start is not starting, filled in as the loop
	// turns them down and applied in one pass at the end - see setWaitingLocked
	// for why it is applied rather than written as it goes.
	waiting := map[string]core.Waiting{}
	perHost := map[string]int{}
	// Forced tasks are counted apart because they are about to be let past both
	// limits, so counting them inside the ordinary total would have one forced
	// download push an ordinary one out of a slot it already holds.
	forcedActive := 0
	normalActive := 0
	for id := range a.active {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		if t.Forced {
			forcedActive++
			continue
		}
		normalActive++
		perHost[hostOf(t.URL)]++
	}
	var rest []string
	// One reading of the destination volumes for the whole pass - see
	// spaceCheck for why it is per pass and why it is shared across the tasks
	// in it.
	space := newSpaceCheck(cfg)
	// The queue as it stood when this pass began. Used at the end to give a
	// reason to what is still waiting AND to clear it from what just got a slot:
	// a dispatched task leaves `rest`, so applying the reasons to `rest` alone
	// left last pass's answer sitting on a row that is now running. Found by the
	// test, not by reading.
	before := append([]string(nil), a.queue...)
	for _, id := range a.queue {
		t := a.tasks[id]
		if t == nil {
			continue // removed while queued
		}
		// "We will not try this again" is untrue of anything sitting in the wait
		// queue, whatever put it back there. Cleared HERE rather than at each of
		// the places that requeue a task, because this loop is the one point all
		// of them meet: the two fallback branches in onUpdate below, Resume, the
		// boot requeue, and RestartTasks in app_queue.go, which appends to the
		// queue and calls this function before it takes the copies it broadcasts.
		// One clear that cannot be forgotten, on the same principle Waiting is
		// recomputed from scratch on every pass rather than erased by whoever
		// fixed the cause.
		t.GaveUp = false
		// The flags that mean "not this one", checked here because this is the
		// only place bytes are ever set moving: StartTasks with no ids is "start
		// everything", and without this a link the user switched off downloads
		// anyway the moment anything touches the queue. Kept in the queue rather
		// than dropped, so it holds its place and goes when it is switched back on.
		//
		// captchaWaitingLocked joins Enabled/Hold for the identical reason build-
		// plan.md section 8's Wave 7 note asks for: a task blocked on a captcha
		// must not be re-dispatched while the human has not answered yet. Ordinarily
		// such a task is a.active, never in this loop at all - a captcha only ever
		// blocks a link already handed to JD - so this mainly guards a narrower race
		// (RestartTasks/Resume/boot requeue briefly putting a still-tracked id back
		// in a.queue). Re-dispatching it would resolve it through a resolver a
		// second time and hand JD the same link again while the first attempt is
		// still mid-captcha, which is exactly the kind of duplicate submission
		// Hold's own check exists to prevent for a link the user parked on purpose.
		if !t.Enabled || t.Hold || a.captchaWaitingLocked(id) {
			switch {
			case !t.Enabled:
				waiting[id] = core.WaitingDisabled
			case t.Hold:
				waiting[id] = core.WaitingHold
			default:
				waiting[id] = core.WaitingCaptcha
			}
			rest = append(rest, id)
			continue
		}
		h := hostOf(t.URL)
		// Where this task's bytes would land, worked out once for the two
		// checks below that need it - dirFor expands a path template, so
		// calling it twice per task per pass is work for nothing.
		dir := a.dirFor(t)
		// The room on that volume, asked BEFORE a slot is spent on this task
		// and before the resolver is called - see app_diskguard.go.
		//
		// AHEAD OF THE SLOT AND HOST LIMITS ON PURPOSE, even though those are
		// cheaper to evaluate. When a disk is full, "all slots busy" is a true
		// sentence about the wrong problem: it sends the reader to raise
		// MaxConcurrent, which changes nothing, and the one answer that would
		// have told them to free some space is shown on none of the rows. The
		// reading itself is taken once per destination folder per pass, so the
		// extra cost of asking first is a map lookup.
		if w := space.admit(dir, t); w != core.WaitingNone {
			waiting[id] = w
			rest = append(rest, id)
			continue
		}
		// "Start now" is the whole point of the flag, and until this landed the
		// dispatcher never read it: a forced task moved to the head of the queue
		// and then waited for a slot exactly like every other task, so the menu
		// entry did something almost invisible and the field's own comment
		// ("past the concurrency and per-host limits") was a promise nothing kept.
		//
		// Past the limits, not past all of them. Forced downloads get a small pool
		// of their own on top of the ordinary one, which is what JD does with
		// MaxForcedDownloads: without a bound, "force" on a selection of two
		// hundred links is the app opening two hundred transfers and the user
		// wondering why everything stalled. A constant rather than a setting for
		// now - it is a safety rail, and one more spinner on a settings page is
		// not what makes this understandable.
		if t.Forced {
			if forcedActive >= maxForcedDownloads {
				waiting[id] = core.WaitingForced
				rest = append(rest, id)
				continue
			}
		} else {
			if normalActive >= cfg.MaxConcurrent {
				waiting[id] = core.WaitingSlot
				rest = append(rest, id)
				continue
			}
			if perHost[h] >= maxPerHostFor(cfg, h) {
				// Said apart from WaitingSlot because the fix is different:
				// this one frees up when THIS host's transfers finish, and
				// raising MaxConcurrent does nothing for it.
				waiting[id] = core.WaitingHost
				rest = append(rest, id)
				continue
			}
		}
		if a.started[id] {
			a.active[id] = true
			a.countStartLocked(t, h, perHost, &forcedActive, &normalActive)
			space.commit(dir, t)
			go a.backendFor(t.Resolver).Resume(id)
			continue
		}
		// The filter is asked again here because this is the last moment before
		// bytes move: a rule written after a link was staged has never seen it,
		// and "start everything" reaches every collected task. Asking once, at
		// paste time, would make the filter something a single button undoes.
		//
		// Except for a link the user has already restored out of the holding
		// area. They read this exact refusal and overruled it, and putting the
		// same rule back in its way would make Restore a button that undoes
		// itself.
		if v := a.filter(candidateOf(t)); v.Rejected && !filterWaived(t) {
			t.Status = core.StatusError
			t.Online = core.AvailOffline
			t.Error = rejection(v)
			// Written even though it is the empty value: this box refused the link
			// on purpose and none of the transfer failures describes that, but the
			// task may still carry the reason from the attempt that failed before
			// it, and a rule rejection labelled "disk full" is exactly the confident
			// wrong answer the taxonomy exists to prevent.
			t.Reason = core.ReasonUnknown
			settled = append(settled, *t)
			continue
		}
		// Honour the resolver already recorded on the task: after a fallback it
		// is deliberately not the highest-priority match any more.
		res := a.resolverForTaskLocked(t)
		if res == nil {
			if t.ResolverPin != "" {
				// A pinned task is settled here rather than held. The holding
				// branch below exists because the app still has other backends
				// to try once an account recovers; this task has been told not
				// to use them, so "waiting" would be a row that never moves for
				// a reason nothing on it names. See pinFailureLocked for the
				// two sentences and for why one of them is ReasonAuth, which
				// leaves the ordinary retry armed.
				t.Status = core.StatusError
				t.Error, t.Reason = a.pinFailureLocked(t)
				settled = append(settled, *t)
				continue
			}
			if a.hasUnroutableMatchLocked(t.URL) {
				waiting[id] = core.WaitingAccount
				// Something DOES claim this link - it is just benched right
				// now (app_health.go). Hold it exactly where it is instead
				// of settling it as unsupported, which would be a lie about
				// a link every one of these backends can normally fetch -
				// see docs/build-plan.md package 14, row 2.
				rest = append(rest, id)
				continue
			}
			t.Status = core.StatusError
			t.Error = "no resolver matches"
			t.Reason = core.ReasonUnsupported
			settled = append(settled, *t)
			continue
		}
		t.Resolver = res.Info().ID
		t.Mode = a.modeForLocked(t, t.Resolver)
		// The shutdown context, because this call is made with mu held: a resolver
		// that hangs would otherwise keep the lock â€” and with it the whole app â€”
		// until its own timeout, and Close would wait behind a hoster.
		result, err := res.Resolve(a.ctx, resolver.Request{URL: t.URL})
		if err != nil {
			t.Status = core.StatusError
			t.Error = err.Error()
			// The error value itself, not the sentence built from it: this is one of
			// the few places the app still holds the real error, and a wrapped
			// syscall or a cancelled shutdown context survives here and nowhere later.
			t.Reason = classify(failure{err: err})
			settled = append(settled, *t)
			continue
		}
		be := a.backendFor(t.Resolver)
		policy := collide.ParsePolicy(cfg.CollisionPolicy)
		// A destination that is already taken is settled here instead of being
		// downloaded over.
		//
		// SKIP IS THE ONLY POLICY THIS PLACE CAN DECIDE, and that is not a
		// shortcoming of the check - it is what makes skip different from the other
		// two. Skip is a refusal to start, which asks nothing of whoever would have
		// fetched the bytes, so it holds for a delegated backend exactly as it does
		// for the engine. Rename and overwrite have to name the file that gets
		// written, and only the engine can be told a name, so those are applied on
		// the way into it - where the resolved name is finally known.
		//
		// The check is skipped entirely while the name is still unknown, because a
		// collision decided on a URL-shaped name is a decision about nothing. The
		// engine covers that case for its own tasks once it has resolved one.
		if policy == collide.Skip && filename(t) != "" {
			// Sanitized, or the sentence below names a file that never existed: what
			// lands on disk is the name after the writer's own rewrite.
			target := filepath.Join(dir, collide.SafeName(t.Name))
			if taken, err := collide.Check(target); err == nil && taken {
				// The availability is left alone: nothing was learned about the
				// link here, only about the folder it was going to land in. The
				// reason is cleared for the same purpose as at the filter above -
				// a name that is already taken is not one of the transfer failures,
				// and the previous attempt's label must not stand in for it.
				t.Status = core.StatusError
				t.Error = "not downloaded: " + target + " already exists"
				t.Reason = core.ReasonUnknown
				settled = append(settled, *t)
				continue
			}
		}
		a.active[id] = true
		a.started[id] = true
		a.countStartLocked(t, h, perHost, &forcedActive, &normalActive)
		space.commit(dir, t)
		conns := connsFor(t, cfg, result.Connections, hostCapFor(res, h))
		// Which outbound connection carries this one. Until this call existed,
		// proxycfg.NewPicker had no caller anywhere in the tree: connections could
		// be added, filtered, ordered and switched on, and every download still
		// left by the machine's own address. The column meant to show which one
		// carried it was blank for the same reason, because nothing ever wrote
		// Task.Connection.
		route, chosen := a.routeForLocked(t, h)
		if chosen != t.Connection {
			t.Connection = chosen
		}
		if be == a.Engine {
			// The collision policy travels down this branch only, because this is
			// the one backend that can be told the name it must write. See the skip
			// check above and engine.Job.Collision for the other half of that.
			go a.Engine.Start(engine.Job{
				TaskID: id, URL: result.DirectURL, Headers: result.Headers,
				Conns: conns, Dir: dir, Route: route,
				Collision: policy, MaxCollisionAttempts: cfg.CollisionMaxAttempts,
				// nil for every non-torrent task (core.SelectedTorrentIndices(nil) is
				// nil), and read by the engine only inside its own torrent.IsURI(j.URL)
				// branch - see engine.Job.TorrentSelect's own comment - so this is safe
				// to set unconditionally rather than gated on t.Resolver == "torrent".
				// WITHOUT THIS LINE the file-tree step (11.5D) has no effect at all: a
				// selection a person unticked in the collector would still be handed to
				// Engine.Start as an empty TorrentSelect, which the library reads as
				// "fetch everything" - the exact outcome decision 6 of
				// docs/torrent-support.md exists to prevent.
				TorrentSelect: core.SelectedTorrentIndices(t.TorrentFiles),
			})
		} else {
			// Only the embedded engine takes a route today. A delegated backend
			// runs in its own process or on another machine and reaches the
			// internet its own way, so pretending otherwise would put a
			// connection name on a task that never used it.
			go be.Download(id, result.DirectURL, result.Headers, conns)
		}
	}
	a.queue = rest
	// Applied in one pass, at the end, over everything that was queued when the
	// pass began. Anything the loop did not name is cleared, which covers both
	// the task that just got a slot and the one whose limit was raised - and it
	// is what makes the value self-maintaining: nobody has to remember to erase
	// a reason that has stopped applying.
	a.setWaitingLocked(before, core.WaitingNone, waiting)
	if len(settled) > 0 {
		// Off this goroutine, because the caller still holds mu and the store write
		// must not happen under it. A caller that snapshots after dispatching
		// publishes the same state again, which is harmless: both copies say what
		// the task ended up as, so whichever lands last says the same thing.
		a.spawn(func() { a.publishTasks(settled) })
	}
}

// defaultConns is what one download opens when nobody has an opinion: not the
// task, not a rule, not the settings. It is written down here and nowhere else,
// which is what the global setting's zero buys - a second copy of this number in
// settings.Defaults would be a second one to forget when this one moves.
const defaultConns = 4

// connsFor is how many connections one download opens, and it is the only place
// that number is decided. It reaches the backend from here for a fresh dispatch
// and for a restart alike.
//
// ONE precedence, and this is it:
//
//	value = first of (per-task, matching rule, host table, global setting, defaultConns)
//	conns = min(value, every ceiling that applies, rules.MaxChunks)
//
// The first two terms both arrive on t.Chunks, and that is not the two being
// conflated: the Packagizer writes it as the link is staged and a hand edit is
// made afterwards, over the top of it. "By hand outranks the rule" is therefore
// settled by the order the two happen in, and needs no second field that only
// this function would ever read.
//
// ZERO MEANS "USE THE ONE BELOW IT", never "no connections" - on the task and in
// the settings alike. A download opening no sockets would simply never start,
// and an untouched spinner is exactly how somebody would ask for it.
//
// A CEILING CAN ONLY LOWER THE COUNT, which is the whole reframing. What a
// resolver puts in Connections is a statement about what the HOST tolerates, not
// about what the user wants, so it arrives here as a ceiling and can never raise
// the number. That is what lets somebody set 1 chunk for a hoster that bans
// multi-connection and actually get 1: read as an override, their 1 would be
// quietly replaced by whatever the resolver last said. The per-host and
// account-tier caps are the same kind of fact and arrive the same way, as one
// more ceiling rather than one more branch. A ceiling of zero is a caller with
// nothing to say about the host, not a host that permits nothing.
//
// The last ceiling is the engine's own: rules.MaxChunks is 16 because gopeed's
// HTTP fetcher will not honour more, so a count that got past it - an older
// build, a value written straight into the store - is cut here rather than
// handed on as a promise nothing downstream keeps.
//
// THE HOST TABLE SITS BETWEEN THE TASK AND THE GLOBAL SETTING, and it is a
// value in that chain rather than one more ceiling. Everything on the ceilings
// list is a REPORT about the host, from a resolver that has been told what the
// host permits, and a report may only ever lower the count. A HostRules entry
// is a person writing down what they want for this hoster, so it has to be
// able to say eight on an instance whose global is four - as a ceiling it
// could only ever say "at most", which cannot express the case the table was
// asked for ("ein Hoster vertraegt acht Verbindungen, der naechste sperrt ab
// zwei"). It is still cut by every ceiling below, so a resolver that knows the
// host permits two beats a hopeful eight, and by rules.MaxChunks like anything
// else.
func connsFor(t *core.Task, cfg settings.Settings, ceilings ...int) int {
	conns := defaultConns
	// hostOf(t.URL) and not t.Host, so this is keyed on exactly what the
	// dispatcher counts transfers per host by a few lines up. The two agree
	// today; keying one of them off the other field is how they stop agreeing.
	hostChunks := cfg.HostRuleFor(hostOf(t.URL)).Chunks
	switch {
	case t.Chunks > 0:
		conns = t.Chunks
	case hostChunks > 0:
		conns = hostChunks
	case cfg.Chunks > 0:
		conns = cfg.Chunks
	}
	for _, c := range ceilings {
		if c > 0 && c < conns {
			conns = c
		}
	}
	if conns > rules.MaxChunks {
		conns = rules.MaxChunks
	}
	return conns
}

// Pause takes one task out of the running set AND out of the wait queue: it is
// a per-task instruction, and the task stays out until somebody resumes it.
func (a *App) Pause(id string) { a.stop(id, false) }

// StopBack is the hard stop's per-task half: the transfer is stopped, but the
// task goes BACK INTO the wait queue rather than out of it.
//
// The difference decides whether the play button works at all. StopAll pauses
// everything in flight and halts the queue behind it - two effects from one
// press. Releasing the halt undid only the second, so the tasks the stop had
// paused were sitting outside the queue with nothing left that would ever hand
// them to a backend again. Measured on the live instance: play answers
// `halted: false`, and four seconds later it is still 19 paused, 0 running,
// 0 B/s. That is jdp's "Die Start und Stopp buttons funktionieren einfach
// nirgends! Es lädt auch nirgends was runter", and no amount of pressing play
// could have fixed it.
//
// Keeping them queued needs no record of "what the stop stopped" and no second
// state to keep in step: the halt is the thing that is off, and lifting it is
// the whole of turning it back on. A task somebody paused BY HAND stays paused
// through all of this, which is the distinction worth preserving - that is a
// per-task decision, and the master switch has no business undoing it.
func (a *App) StopBack(id string) { a.stop(id, true) }

func (a *App) stop(id string, requeue bool) {
	a.mu.Lock()
	// The SLOT is freed first, before anything can decide there is nothing to do.
	//
	// The nil check below used to come first and return, which leaks a
	// concurrency slot for ever if the task disappears between a caller reading
	// a.active and reaching here - and StopAll does exactly that: it copies the
	// ids under the lock, lets go, and stops them one at a time, because Pause
	// takes the lock itself. Anything in that window (housekeeping removing an
	// old row, a delete arriving from a browser) leaves an id in a.active with
	// no task behind it, holding a place nothing will ever give back.
	//
	// Caught by CI under -race, which is the only place it has ever appeared:
	// "1 downloads are still active after the hard stop". Whatever else is true
	// of a task being stopped, it is not running.
	wasActive := a.active[id]
	delete(a.active, id)
	t := a.tasks[id]
	if t == nil {
		// Nothing left to write a status onto, and nothing to requeue - but the
		// slot is gone, so the dispatcher can use it.
		a.mu.Unlock()
		return
	}
	if requeue {
		// Put back IN, not merely "left alone".
		//
		// The first cut of this only skipped the dequeue, on the reading that a
		// task being stopped is in the queue and should stay there. It is not:
		// dispatchLocked keeps the ones it could NOT hand out and writes that
		// back as the whole queue (`a.queue = rest`), so a task it dispatched
		// left the queue at that moment. Skipping the dequeue therefore did
		// nothing at all - the row said "queued", the dispatcher never saw it
		// again, and the play button was as dead as before. Caught on the live
		// instance and not by the test, because the test had put the id in the
		// queue by hand and running tasks are never there.
		if !slices.Contains(a.queue, id) {
			a.queue = append(a.queue, id)
		}
	} else {
		a.dequeueLocked(id)
	}
	// The status is written HERE, for a waiting task and a running one alike,
	// and that symmetry is the fix rather than a tidy-up.
	//
	// The running branch used to write nothing and call the backend, on the
	// assumption that the backend would report the new state. Some do. The
	// engine's own pause does not emit one, so a task the engine was driving
	// stayed StatusRunning for ever: the transfer really had stopped, the bar
	// really had frozen, and the row still said "running" (jdp, 2026-08-31:
	// "auch in der container instanz funktioniert der stopp button nicht. der
	// status zeigt weiterhin läuft an"). It reached TorBox because a TorBox task
	// hands its direct URL to the engine and then delegates Pause to it, so it
	// inherits exactly that gap - but the defect was never TorBox's, it was in
	// every path that ends at the engine.
	//
	// **A state the app COMMANDED is the app's to record.** Waiting for a
	// backend to volunteer it makes correctness depend on every backend
	// remembering, and a backend that forgets fails silently and looks like a
	// dead button. A later event from the backend still wins, which is what
	// makes writing it here safe: this is the optimistic value, not a claim
	// about the network.
	// "Waiting" for the hard stop, "paused" for a per-task pause. Both are
	// honest about the transfer having stopped, which is what was wrong before
	// either existed; they differ in what it takes to start again, and the row
	// should say which.
	if requeue {
		t.Status = core.StatusQueued
	} else {
		t.Status = core.StatusPaused
	}
	t.Speed = 0
	// And the stall mark with it, for the same reason the speed is zeroed: it
	// is a reading of a transfer that was running, and this one is not any
	// more. The watcher would take it back on its own within a tick (see
	// markStallsLocked, which walks what it has marked precisely so a task that
	// left the running set by a path it does not own still gets cleaned); doing
	// it here means the row the user is looking at is right in the answer to
	// the button they just pressed.
	t.StalledSince = time.Time{}
	c := *t
	if !wasActive {
		a.mu.Unlock()
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
		return
	}
	a.dispatchLocked()
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
	a.backendFor(t.Resolver).Pause(id)
}

func (a *App) Resume(id string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil || a.active[id] {
		a.mu.Unlock()
		return
	}
	t.Status = core.StatusQueued
	t.Speed = 0
	c := *t
	a.dequeueLocked(id)
	a.queue = append(a.queue, id)
	a.dispatchLocked()
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
}

// HonoursCollisionPolicy reports whether the collision policy in settings
// reaches the file a task on this resolver actually writes.
//
// It is false for every delegated backend, and that is not a gap waiting to be
// filled: headless JD, TorBox and yt-dlp fetch in their own process and name the
// file themselves, so nothing this app can say about the destination gets there.
// Only skip still applies to them, because skip is decided before the handover.
//
// It is exported so the interface can ask. A rename control offered on a row
// that will silently ignore it is worse than no control at all - the user sets
// it, watches a file get overwritten anyway, and stops trusting the setting on
// the rows where it does work.
func (a *App) HonoursCollisionPolicy(resolverID string) bool {
	return a.backendFor(resolverID) == a.Engine
}

func (a *App) backendFor(resolverID string) backend {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	if b, ok := a.debrid[resolverID]; ok && b != nil {
		return b
	}
	switch {
	case resolverID == remotefs.ResolverID && a.remotefs != nil:
		// Not the engine, even though a WebDAV task's resolved URL is an
		// ordinary https one the engine could take directly: the backend is
		// what hands that link on (see remotefs.Backend.Download), and routing
		// around it here would leave the ftp/sftp half with no backend and the
		// WebDAV half pausing through a different object than it started on.
		return a.remotefs
	case resolverID == "jd" && a.jd != nil:
		return a.jd
	case resolverID == "torbox" && a.torbox != nil:
		return a.torbox
	case resolverID == "ytdlp" && a.ytdlp != nil:
		return a.ytdlp
	default:
		return a.Engine
	}
}

// ReconnectState is what the interface shows beside the reconnect button.
type ReconnectState struct {
	Busy bool `json:"busy"`
	// Configured is whether a reconnect could run at all, which is a different
	// question from whether it would succeed.
	Configured bool `json:"configured"`
}

// ReconnectState reports whether a reconnect is configured and whether one is
// running, so the interface can show it without starting one to find out.
func (a *App) ReconnectState() ReconnectState {
	return ReconnectState{Busy: a.Reconnector.Busy(), Configured: a.reconnectConfigured()}
}

// reconnectConfigured reports whether the user has finished setting reconnect
// up. It is asked before every automatic attempt, because an unconfigured
// reconnect fired on every retry is a goroutine and a log line per failure that
// tell nobody anything.
func (a *App) reconnectConfigured() bool {
	return a.Settings.Get().Reconnect.Validate() == nil
}

// Reconnect runs one reconnect now, on the caller's behalf. It is the one
// place a run can end - reconnectThenRetry goes through here rather than
// calling Reconnector.Do itself, so there is a single site that publishes
// reconnect.done and no second one to forget.
func (a *App) Reconnect(ctx context.Context) (reconnect.Result, error) {
	res, err := a.Reconnector.Do(ctx)
	a.fireReconnectDone(res, err)
	return res, err
}

// reconnectThenRetry asks the router for a new address and, if the address
// really moved, brings the waiting retry forward.
//
// Every error leaves the ordinary backoff to run, ErrUnchanged included: that
// one means the address did not move, and retrying then is exactly the hammering
// the reconnect exists to stop.
func (a *App) reconnectThenRetry(id string) {
	// Through Reconnect, not Reconnector.Do: that wrapper is what publishes
	// reconnect.done, and an automatic run is exactly the one a script most
	// wants to hear about - the user is not watching, nobody pressed
	// anything, and the address either moved or it did not.
	if _, err := a.Reconnect(a.ctx); err != nil {
		log.Printf("reconnect after task %s hit a limit: %v", id, err)
		return
	}
	a.retryAfter(id, 0)
}

func (a *App) onUpdate(id string, u core.Update) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	// A status the app did not ask for cannot undo one it did.
	//
	// Pause writes "paused" under the lock and then tells the backend, and the
	// comment there calls a later backend event "the thing that makes writing it
	// here safe". For a POLLING backend that is exactly backwards. JD's poller
	// ticks every 750 ms and reports whatever JD's own list says; the link is
	// still in that list a moment after the pause, so the very next tick wrote
	// "running" straight over the pause. Measured on the live instance: stop
	// answers `running: 0, halted: true`, and the two rows are back to "running"
	// before the answer is on screen. That is the stop button jdp reported four
	// times, and no amount of writing the status in Pause could ever have fixed
	// it, because the overwrite happens afterwards.
	//
	// Terminal states are exempt, and that exemption is the whole rule rather
	// than a loophole: done and error are facts about the file, true whatever
	// anybody intended, and one arriving a moment after a pause is still true.
	// Running and queued are claims about INTENT, and on intent the app is the
	// authority - it is the only party that heard the user.
	//
	// Anchored on `a.active` alone, and NOT on the task's current status. The
	// first cut also required the task to be "paused", which was right for as
	// long as the hard stop left tasks paused - and stopped being right the
	// moment it started returning them to the wait queue instead (see StopBack).
	// A stopped-but-queued task would have been dragged back to "running" by the
	// very next poll, which is the original defect wearing a different status.
	//
	// The rule underneath is simpler than either version: **the dispatcher
	// decides what is running.** A task it is not driving is not in a.active,
	// and a backend that says otherwise is describing something the app has
	// already ended.
	stale := u.Status != core.StatusDone && u.Status != core.StatusError && !a.active[id]
	if u.Name != "" {
		t.Name = u.Name
	}
	if u.Size > 0 {
		t.Size = u.Size
	}
	if u.Status != "" && !stale {
		t.Status = u.Status
	}
	if u.Loaded > 0 {
		t.Loaded = u.Loaded
	}
	// Zeroed rather than carried for a stopped task: the bytes already written
	// are a fact worth keeping, the speed they were arriving at is not, and a
	// paused row showing 4 MB/s is the same lie in a smaller font.
	if stale {
		t.Speed = 0
	} else {
		t.Speed = u.Speed
	}
	// The live detail follows the same staleness rule the status does: a poll
	// that arrives for a task nobody is running any more must not put a note on
	// it, and a task that has stopped has nothing to be doing.
	if stale {
		t.Note = ""
	} else {
		t.Note = u.Note
	}
	if u.Torrent != nil {
		u.Torrent.ApplyTo(t)
	}
	if u.Err != "" {
		t.Error = u.Err
		// Classified here rather than in each backend, because the update channel
		// carries a sentence and not an error: the engine, JD, yt-dlp and every
		// debrid service report through the same field, and one classifier they all
		// pass through is the only way the same failure gets the same label whoever
		// hit it. A backend that already knows better says so with u.Unsupported,
		// which is handled below.
		t.Reason = classify(failure{text: u.Err})
	}
	// Told to account health before anything below re-dispatches the slot this
	// terminal status frees, so a queued task sharing this one's account sees
	// the fresh verdict on the very next pass rather than the one after. A
	// resolver with no tracked account (jd, ytdlp, the engine's own
	// direct/http-fallback) answers ok=false and nothing happens here - see
	// app_health.go's accountForResolverLocked.
	var accountUnroutable bool
	if svc, acct, ok := a.accountForResolverLocked(t.Resolver); ok {
		switch u.Status {
		case core.StatusError:
			accountUnroutable = a.reportAccountFailure(svc, acct, t.Reason, u.Err)
		case core.StatusDone:
			a.reportAccountSuccess(svc, acct)
		}
	}
	// Terminal states free the scheduling slot for the next queued task.
	if u.Status == core.StatusDone || u.Status == core.StatusError {
		// The stall mark is a reading of a transfer that is still going, so a
		// settled row must not keep one - "standing still for 40 minutes" on a
		// finished download is a sentence about something that is over. Cleared
		// here rather than left to the watcher's next tick, which would leave it
		// on screen for up to one interval and put it in the broadcast this very
		// update sends.
		t.StalledSince = time.Time{}
		delete(a.active, id)
		a.dispatchLocked()
	}
	var hitStopMark bool
	if u.Status == core.StatusDone {
		t.Online = core.AvailOnline
		t.Retries = 0
		t.NextTry = time.Time{}
		// Reset with Retries above and for the same reason: both count what it
		// took to get here, and a finished download that is started again later
		// must not begin one restart short of its own ceiling.
		t.StallRestarts = 0
		// Renamed here, ahead of everything below that turns t.Name into a path:
		// the checksum sweep, the extraction candidate, the copy the store and
		// every browser get. Done afterwards, each of those would be about a file
		// that is no longer at that name.
		a.renameFinishedLocked(t)
		if a.stopMark == id {
			// Recorded as the manual halt too, because that is what it is: the user
			// said "finish this, then stop", and the mark is only the delay on it.
			// Left out of the manual flag it would be invisible to scheduleBase, and
			// the first boundary that changed anything at all â€” a nightly limit
			// ending â€” would hand the runner a state saying "not paused" and start
			// the queue again for a reason nothing on screen could explain.
			a.manualHalt = true
			a.halted = true
			a.stopMark = ""
			hitStopMark = true
		}
		if a.Settings.Get().VerifyChecksums {
			path := filepath.Join(a.dirFor(t), t.Name)
			a.spawn(func() { a.verifyTask(id, path) })
		}
	}
	// A backend that says the link is not its business hands the task to the
	// next one in the chain instead of failing it. This is deliberately not a
	// guess about the error text: only an explicit signal advances the chain,
	// so a hoster link that genuinely failed is never re-downloaded as a plain
	// web page. The chain only moves downwards, so it terminates.
	var fallbackTo backend
	if u.Status == core.StatusError && u.Unsupported {
		if next := a.nextResolverLocked(t); next != "" {
			fallbackTo = a.backendFor(t.Resolver)
			log.Printf("task %s: %s could not fetch the link, trying %s", id, t.Resolver, next)
			t.Resolver = next
			t.Mode = a.modeForLocked(t, next)
			t.Status = core.StatusQueued
			t.Error = ""
			// Always cleared with the sentence it belongs to. A task back in the
			// queue carrying the last backend's reason has the interface advising
			// about a failure that has since been taken back.
			t.Reason = core.ReasonUnknown
			t.Loaded = 0
			t.Speed = 0
			delete(a.started, id)
			a.queue = append(a.queue, id)
			a.dispatchLocked()
		} else {
			// The chain is exhausted, which is the strongest form of "no backend
			// handles this": every backend that matched the link has now had its
			// turn and said the link is not its business.
			t.Reason = core.ReasonUnsupported
		}
	} else if u.Status == core.StatusError && accountUnroutable && t.ResolverPin != "" {
		// Pinned, so the requeue below - which is a hop to the next backend
		// in all but name - is exactly what must not happen. The failure the
		// backend just reported stands, relabelled to name the pin, and the
		// ordinary retry backoff further down does the rest: an account that
		// recovers inside the retry budget picks the task up again, and one
		// that does not leaves a row saying which backend it was nailed to and
		// what was wrong with it.
		t.Error, t.Reason = a.pinFailureLocked(t)
	} else if u.Status == core.StatusError && accountUnroutable {
		// The account this task was using is unavailable - benched, invalid,
		// expired or in error (accountForResolverLocked/reportAccountFailure,
		// app_health.go) - not the link's own fault. Row 2 of account health:
		// this must not turn into a hard error for the task, only into the
		// same requeue-and-try-the-next-backend shape u.Unsupported already
		// gets above, so it is held for the fallback chain exactly like the
		// queued tasks resolverForTaskLocked already skips this account for.
		fallbackTo = a.backendFor(t.Resolver)
		next := a.nextResolverLocked(t)
		if next != "" {
			log.Printf("task %s: the account behind %s is unavailable, trying %s", id, t.Resolver, next)
		} else {
			// Nothing else claims this link either. Clearing the resolver
			// sends the next dispatch pass back to resolverForTaskLocked's
			// own search instead of pinning the task to a backend that just
			// proved unusable; hasUnroutableMatchLocked is what holds it
			// quietly there if that search also comes up empty.
			log.Printf("task %s: the account behind %s is unavailable, holding for it to recover", id, t.Resolver)
		}
		t.Resolver = next
		t.Mode = a.modeForLocked(t, next)
		t.Status = core.StatusQueued
		t.Error = ""
		t.Reason = core.ReasonUnknown
		t.Loaded = 0
		t.Speed = 0
		delete(a.started, id)
		a.queue = append(a.queue, id)
		a.dispatchLocked()
	}

	// The mirror set follows the task, and it is read from the task's own status
	// rather than from the update: a link handed on to the next backend a moment
	// ago is queued again, not settled, and unfiling it there would let a second
	// copy of it be staged while the first is still running.
	switch {
	case t.Status == core.StatusDone || t.Status == core.StatusError:
		// A settled download stops blocking its own re-add: pasting a finished or
		// failed link again is a deliberate second attempt, not a duplicate.
		a.forgetLinkLocked(t)
	case u.Name != "" || u.Size > 0:
		// The name and the byte count usually arrive from the backend, long after
		// the link was filed with neither. Re-filing it is what lets a mirror
		// pasted from a second hoster be recognised at all under a policy that
		// compares those; Add replaces the record rather than filing it twice.
		a.dupes.Add(linkEntry(t))
	}

	// A failure is not automatically the end: hosters throttle, connections
	// drop. Retry a bounded number of times with a growing delay before the
	// task is left for the user to deal with.
	//
	// A full disk is the exception, and it is why the reason is worth having at
	// all. Nothing about the next ten minutes frees a byte, so the retries only
	// spend the queue's slots and, worse, bury the one failure the user could
	// have fixed under five more attempts that end in the same sentence. It is
	// left settled where the error is on screen and the disk can be emptied.
	var retryIn time.Duration
	if u.Status == core.StatusError && fallbackTo == nil {
		cfg := a.Settings.Get()
		// What this particular failure, on this particular host, is worth
		// waiting for - see settings.RetryFor. With both tables empty it
		// answers the fifteen-seconds-to-ten-minutes backoff and MaxRetries,
		// which is what every branch below was hard-coded to before the tables
		// existed, so an install that has configured nothing behaves as it did.
		plan := cfg.RetryFor(string(t.Reason), hostOf(t.URL))
		switch {
		case plan.Never:
			// Somebody wrote "never" against this host or this reason, and
			// that is a decision, not an exhausted counter. It settles into
			// the end state that says so - see core.Task.GaveUp - rather than
			// looking identical to a download that ran out of attempts, which
			// would send the next person to raise MaxRetries and wonder why
			// nothing changed.
			t.GaveUp = true
			t.NextTry = time.Time{}
		case t.Reason == core.ReasonCaptcha:
			// Nothing about the next ten minutes answers a captcha. Retrying
			// only spends the queue's slots and buries the one line that told
			// somebody what to do, exactly as with a full disk below - and on a
			// headless JD with no MyJDownloader session the challenge is never
			// offered over the API at all (see internal/captcha/jdsource.go's
			// own note), so the retry cannot succeed even in principle.
			//
			// Marked as given up rather than merely left unarmed: this app has
			// decided, and raising the retry count will not change its mind.
			t.GaveUp = true
			t.NextTry = time.Time{}
		case t.Reason == core.ReasonDiskFull:
			// Cleared as well as not armed: the list reads a pending retry off this
			// field, and a task that will never be tried again must not show the
			// "retrying automatically" mark that stops people acting on it. Same
			// end state as the captcha above, and for the same reason: this is
			// policy, not a counter running out.
			t.GaveUp = true
			t.NextTry = time.Time{}
		case t.Retries < plan.Tries:
			t.Retries++
			retryIn = u.Retry
			if retryIn <= 0 {
				retryIn = retryDelay(t.Retries, plan.Delay, plan.Max)
			}
			t.NextTry = time.Now().Add(retryIn)
		default:
			// Out of attempts, which is NOT the same end state as the three
			// above: this one is mended by allowing more of them, so it stays
			// a plain failure and GaveUp stays false.
			t.NextTry = time.Time{}
		}
	}
	// The spare copy takes over from here, when the user asked for that and the
	// source has nothing left to try. It is placed after the switch above rather
	// than inside it because it reads that switch's answer: retryIn is how the
	// backoff says whether this failure was the last word. See
	// handOverToMirrorLocked (app_mirror.go) for the whole of the policy - it can
	// also take back a retry the switch just armed, which is why retryIn comes
	// back out of it and why this sits above the reconnect that reads retryIn.
	var mirrorCopy *core.Task
	if u.Status == core.StatusError && fallbackTo == nil {
		mirrorCopy, retryIn = a.handOverToMirrorLocked(t, retryIn)
	}
	// A hoster limit keyed to this box's address is the one failure a new address
	// actually fixes, and a backend asking for another attempt after a delay
	// (u.Retry) is how it says it hit one. Skipped while the queue is halted: a
	// reconnect reboots the router to help downloads that are not running, and
	// drops the ones that are.
	//
	// The reason is the second opinion, and it can only veto: a backend that asks
	// for a delayed retry on a failure a new address plainly cannot mend - a dead
	// link, credentials that were refused - takes the whole house off the
	// internet for nothing.
	// u.Retry is one signal and the typed reason is the other, because on its own
	// u.Retry was none: no backend in the tree ever sets it, so the whole
	// automatic reconnect - the point of the feature - was unreachable, and only
	// the button on the settings page could ever fire one. A classifier that has
	// just decided this failure IS a limit is the signal that actually arrives.
	//
	// The honest caveat, written here because it will be tempting to narrow this
	// later: ReasonLimit covers both an address-keyed limit and an account
	// allowance being used up, and a new address only mends the first. Telling
	// them apart needs a distinction the taxonomy does not carry yet. Firing on
	// both is the cheaper mistake - the run ends in ErrUnchanged or in an address
	// that changes nothing, the ordinary backoff is already armed either way, and
	// at most one reconnect runs at a time.
	limitHit := u.Retry > 0 || t.Reason == core.ReasonLimit
	reconnectFor := ""
	if retryIn > 0 && limitHit && !a.halted && addressMayHelp(t.Reason) && a.reconnectConfigured() {
		reconnectFor = id
	}
	// A finished download that completes an archive continues as an extraction.
	// For a multi-volume set this only fires once the last part has arrived, and
	// the unpacking switch is read off the volume that will be opened rather than
	// off this one — see extractionDueLocked.
	var extractCopy *core.Task
	if u.Status == core.StatusDone {
		if target := a.extractNowLocked(t, a.Settings.Get()); target != nil && target != t {
			c := *target
			extractCopy = &c
		}
	}
	c := *t
	a.mu.Unlock()
	if fallbackTo != nil {
		// Clear the old backend's state so a later restart does not resume a
		// download that belongs to a backend the task no longer uses.
		fallbackTo.Remove(id, true)
	}
	// u.Status == "" only ever happens for a torrent's periodic seeding-stats
	// poll: engine/torrent.go's pollOne sends one deliberately, so a done
	// torrent seeding for hours does not re-run rename/checksum/dispatch every
	// three seconds - see that function's own doc comment. Saving here would
	// persist nothing worth persisting (Peers/Seeds/Ratio/Uploaded/Seeding are
	// documented on core.Task as deliberately NOT persisted), and the script
	// trigger below would fire task.done again on every one of those polls for
	// as long as the torrent seeds if it ran unconditionally - reproduced
	// live: roughly 2400 firings over a default 2h seed window, the Wave 11 x
	// Wave 11.5 collision neither wave's own tests could see alone. The
	// broadcast stays unconditional: it is what makes the live peer/seed/ratio
	// numbers this wave adds actually update while seeding.
	if u.Status != "" {
		_ = a.Store.Save(&c)
	}
	a.Hub.Broadcast("task", &c)
	if u.Status != "" {
		// The one broadcast per settled state script.ClassifyTaskUpdate's own
		// doc comment is grounded in: NextTry is already set (above, before
		// this point) when a failure still has an automatic retry pending, so a
		// script bound to task.failed fires on the final word only, never once
		// per backoff attempt.
		tv := scriptTaskView(c)
		if trig, ok := script.ClassifyTaskUpdate(tv); ok {
			a.publishEvent(script.Firing{Trigger: trig, Task: &tv})
		}
	}
	if extractCopy != nil {
		_ = a.Store.Save(extractCopy)
		a.Hub.Broadcast("task", extractCopy)
	}
	if mirrorCopy != nil {
		_ = a.Store.Save(mirrorCopy)
		a.Hub.Broadcast("task", mirrorCopy)
	}
	if retryIn > 0 {
		a.retryAfter(id, retryIn)
	}
	if reconnectFor != "" {
		// Off the lock and off this goroutine: Do blocks for up to the whole
		// configured timeout, and holding mu for two minutes would stop the app.
		// The ordinary backoff above is already armed, and re-entering a task that
		// has since been restarted is a no-op, so the two cannot fight.
		a.spawn(func() { a.reconnectThenRetry(reconnectFor) })
	}
	if hitStopMark {
		log.Printf("stop mark reached at %s; the queue is halted", c.Name)
		a.Hub.Broadcast("queue", a.Queue())
	}
}

// retryDelay grows with each attempt so a hoster cool-down has time to pass,
// without making the last attempt feel abandoned.
//
// base and cap arrive from settings.Settings.RetryFor rather than being the two
// numbers written in here, and that is the whole of what made this policy
// configurable: the SHAPE of the backoff is still this one function, and the
// values it doubles between are whatever this failure's host and reason say
// they are. An install that has configured neither is handed
// settings.DefaultRetryDelay and settings.DefaultRetryMax, which are the 15s
// and 10min this function used to hold, so the curve is bit for bit the one it
// always produced.
//
// The clamp on attempt is what benchDelay in app_health.go has for the same
// reason: the shift is what overflows, and a configured attempt count arriving
// from settings must not be able to turn a delay negative.
func retryDelay(attempt int, base, ceiling time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 32 {
		attempt = 32
	}
	if base <= 0 {
		base = settings.DefaultRetryDelay
	}
	if ceiling <= 0 {
		ceiling = settings.DefaultRetryMax
	}
	d := base * time.Duration(uint64(1)<<uint(attempt-1))
	if d <= 0 || d > ceiling {
		d = ceiling
	}
	return d
}

// retryAfter re-runs a failed task once the delay has passed, unless the user
// touched it in the meantime.
func (a *App) retryAfter(id string, d time.Duration) {
	time.AfterFunc(d, func() {
		a.mu.Lock()
		t := a.tasks[id]
		due := t != nil && t.Status == core.StatusError && !t.NextTry.IsZero()
		a.mu.Unlock()
		if due {
			a.RestartTasks([]string{id})
		}
	})
}
