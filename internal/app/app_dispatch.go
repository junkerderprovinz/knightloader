package app

// The handover to a download backend and everything that comes back from one:
// which backend a task goes to, when it may go, and what happens to a task that
// finishes, fails or asks to be retried.

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// resolverForTaskLocked picks the resolver a task should be dispatched through:
// the one recorded on it if that still exists, else the best current match.
// Caller holds a.mu.
func (a *App) resolverForTaskLocked(t *core.Task) resolver.Resolver {
	if t.Resolver != "" {
		for _, res := range a.Registry.All(t.URL) {
			if res.Info().ID == t.Resolver {
				return res
			}
		}
	}
	return a.Registry.For(t.URL)
}

// nextResolverLocked returns the resolver that should try after the one the
// task just used, or "" when the chain is exhausted. Caller holds a.mu.
func (a *App) nextResolverLocked(t *core.Task) string {
	chain := a.Registry.All(t.URL)
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
		// The two flags that mean "not this one", checked here because this is the
		// only place bytes are ever set moving: StartTasks with no ids is "start
		// everything", and without this a link the user switched off downloads
		// anyway the moment anything touches the queue. Kept in the queue rather
		// than dropped, so it holds its place and goes when it is switched back on.
		if !t.Enabled || t.Hold {
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
		// A destination that is already taken is settled here instead of being
		// downloaded over. Only "skip" can be honoured today: every other policy
		// has to name the file it writes, and no backend accepts a destination
		// file name â€” the engine is handed a directory and names the file itself.
		// The check is skipped entirely while the name is still unknown, because a
		// collision decided on a URL-shaped name is a decision about nothing.
		if collide.ParsePolicy(cfg.CollisionPolicy) == collide.Skip && filename(t) != "" {
			target := filepath.Join(dir, t.Name)
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
		conns := connsFor(t, cfg, result.Connections)
		if be := a.backendFor(t.Resolver); be == a.Engine {
			go a.Engine.DownloadTo(id, result.DirectURL, result.Headers, conns, dir)
		} else {
			go be.Download(id, result.DirectURL, result.Headers, conns)
		}
	}
	a.queue = rest
	if len(settled) > 0 {
		// Off this goroutine, because the caller still holds mu and the store write
		// must not happen under it. A caller that snapshots after dispatching
		// publishes the same state again, which is harmless: both copies say what
		// the task ended up as, so whichever lands last says the same thing.
		go a.publishTasks(settled)
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
	if !wasActive {
		// Still waiting in the queue: just mark it paused.
		t.Status = core.StatusPaused
		t.Speed = 0
		c := *t
		a.mu.Unlock()
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
		return
	}
	a.dispatchLocked()
	a.mu.Unlock()
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
	if u.Name != "" {
		t.Name = u.Name
	}
	if u.Size > 0 {
		t.Size = u.Size
	}
	if u.Status != "" {
		t.Status = u.Status
	}
	if u.Loaded > 0 {
		t.Loaded = u.Loaded
	}
	t.Speed = u.Speed
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
			go a.verifyTask(id, filepath.Join(a.dirFor(t), t.Name))
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
	reconnectFor := ""
	if retryIn > 0 && u.Retry > 0 && !a.halted && addressMayHelp(t.Reason) && a.reconnectConfigured() {
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
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
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
		go a.reconnectThenRetry(reconnectFor)
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
