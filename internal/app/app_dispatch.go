package app

// The handover to a download backend and everything that comes back from one:
// which backend a task goes to, when it may go, and what happens to a task that
// finishes, fails or asks to be retried.

import (
	"context"
	"log"
	"path/filepath"
	"sort"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

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
func dynamicPrio(res resolver.Resolver, url string) int {
	if res.Info().ID == "jd" {
		return jd.PriorityFor(url)
	}
	return res.Info().Prio
}

// rankedChain is chain, stably re-ordered by dynamicPrio rather than trusted
// in the registry's own frozen order. Stable, so that two matches dynamicPrio
// does not distinguish keep exactly the order Registry.All (and so the
// registry's own Prio-at-Register-time sort) already gave them - the re-rank
// only ever moves JD, and only for a host it has just earned a boost for.
func rankedChain(chain []resolver.Resolver, url string) []resolver.Resolver {
	out := make([]resolver.Resolver, len(chain))
	copy(out, chain)
	sort.SliceStable(out, func(i, j int) bool {
		return dynamicPrio(out[i], url) > dynamicPrio(out[j], url)
	})
	return out
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
// The fallback loop below is written out inline rather than delegated to a
// helper in app_health.go: the one that used to make this identical decision
// (against the registry's frozen order) had no seam to hand it rankedChain's
// re-ranked order instead, and app_health.go is another agent's file this
// wave. accountRoutableLocked itself is exported for exactly this: called
// from here, never edited here.
//
// Caller holds a.mu.
func (a *App) resolverForTaskLocked(t *core.Task) resolver.Resolver {
	if t.Resolver != "" && a.accountRoutableLocked(t.Resolver) {
		for _, res := range a.Registry.All(t.URL) {
			if res.Info().ID == t.Resolver {
				return res
			}
		}
	}
	for _, res := range rankedChain(a.Registry.All(t.URL), t.URL) {
		if a.accountRoutableLocked(res.Info().ID) {
			return res
		}
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
	chain := rankedChain(a.Registry.All(t.URL), t.URL)
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
	if a.halted {
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
	for _, id := range a.queue {
		t := a.tasks[id]
		if t == nil {
			continue // removed while queued
		}
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
			rest = append(rest, id)
			continue
		}
		h := hostOf(t.URL)
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
				rest = append(rest, id)
				continue
			}
		} else {
			if normalActive >= cfg.MaxConcurrent {
				rest = append(rest, id)
				continue
			}
			if perHost[h] >= cfg.MaxPerHost {
				rest = append(rest, id)
				continue
			}
		}
		if a.started[id] {
			a.active[id] = true
			a.countStartLocked(t, h, perHost, &forcedActive, &normalActive)
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
			if a.hasUnroutableMatchLocked(t.URL) {
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
		dir := a.dirFor(t)
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
//	value = first of (per-task, matching rule, global setting, defaultConns)
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
func connsFor(t *core.Task, cfg settings.Settings, ceilings ...int) int {
	conns := defaultConns
	switch {
	case t.Chunks > 0:
		conns = t.Chunks
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

func (a *App) Pause(id string) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	wasActive := a.active[id]
	delete(a.active, id)
	a.dequeueLocked(id)
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
	t.Status = core.StatusPaused
	t.Speed = 0
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

// Reconnect runs one reconnect now, on the caller's behalf.
func (a *App) Reconnect(ctx context.Context) (reconnect.Result, error) {
	return a.Reconnector.Do(ctx)
}

// reconnectThenRetry asks the router for a new address and, if the address
// really moved, brings the waiting retry forward.
//
// Every error leaves the ordinary backoff to run, ErrUnchanged included: that
// one means the address did not move, and retrying then is exactly the hammering
// the reconnect exists to stop.
func (a *App) reconnectThenRetry(id string) {
	if _, err := a.Reconnector.Do(a.ctx); err != nil {
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
	// Anchored on `!a.active[id]` and not on the status alone: a task the
	// dispatcher is genuinely driving is in a.active, so a real restart is never
	// mistaken for a stale echo.
	stale := u.Status != core.StatusDone && u.Status != core.StatusError &&
		t.Status == core.StatusPaused && !a.active[id]
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
		delete(a.active, id)
		a.dispatchLocked()
	}
	var hitStopMark bool
	if u.Status == core.StatusDone {
		t.Online = core.AvailOnline
		t.Retries = 0
		t.NextTry = time.Time{}
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
		switch {
		case t.Reason == core.ReasonDiskFull:
			// Cleared as well as not armed: the list reads a pending retry off this
			// field, and a task that will never be tried again must not show the
			// "retrying automatically" mark that stops people acting on it.
			t.NextTry = time.Time{}
		case t.Retries < cfg.MaxRetries:
			t.Retries++
			retryIn = u.Retry
			if retryIn <= 0 {
				retryIn = retryDelay(t.Retries)
			}
			t.NextTry = time.Now().Add(retryIn)
		default:
			t.NextTry = time.Time{}
		}
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
			a.Scripts.Fire(trig, &tv, a.ScriptQueue())
		}
	}
	if extractCopy != nil {
		_ = a.Store.Save(extractCopy)
		a.Hub.Broadcast("task", extractCopy)
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
func retryDelay(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt-1)) * 15 * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
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
