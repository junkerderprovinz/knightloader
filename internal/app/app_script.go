package app

// The seam between internal/script and the app: constructing the Host (see
// New), adapting *App to script.Actions, converting core.Task/QueueCounters
// into script's own TaskView/QueueView, and building every script.Firing
// this app publishes. Wiring internal/script's package doc comment
// explicitly leaves to whoever lands it - see its "wiring this in is
// deliberately not this package's job" section.
//
// This file is the ONLY one in internal/app that builds a Firing. Every
// other site that has something to report (app_dispatch.go's onUpdate,
// app_tasks.go's put and verifyTask, app_extract.go's settleExtraction,
// app_captcha.go's poller, app_accounts.go's health sweep) calls one
// named helper here and does not otherwise learn internal/script's
// vocabulary. That is not tidiness: those files are the app's hottest and
// most contended, and a Firing literal written inline in six of them is six
// places to forget the queue snapshot, six places to publish while still
// holding a.mu, and six places somebody has to find the day a payload gains
// a field.
//
// Two of the eleven triggers have no single site to hook and are polled from
// their own loop instead: queue.idle (watchQueueIdleForScripts) and
// package.done (watchPackagesForScripts). The rest fire from the one place
// the app already knew the fact.

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// scriptTaskView copies a core.Task into script's own read-only shape - see
// script.TaskView's own doc comment for why this is a field-by-field copy
// at the call site rather than a conversion internal/script performs
// itself: that package does not import internal/core at all.
func scriptTaskView(t core.Task) script.TaskView {
	return script.TaskView{
		ID:        t.ID,
		Name:      t.Name,
		URL:       t.URL,
		Host:      t.Host,
		Package:   t.Package,
		Status:    string(t.Status),
		Size:      t.Size,
		Loaded:    t.Loaded,
		Speed:     t.Speed,
		Error:     t.Error,
		Reason:    string(t.Reason),
		Retries:   t.Retries,
		Priority:  t.Priority,
		Comment:   t.Comment,
		NextTry:   t.NextTry,
		CreatedAt: t.CreatedAt,
	}
}

// ScriptTask returns one task's script.TaskView by id, false when no such
// task exists - the lookup internal/api's script run route needs to build
// the optional task argument to Scripts.RunNow. Snapshots under a.mu the
// same way SafeTaskFile (app_files.go) already does for its own single-task
// lookup, rather than handing out the live *core.Task a script could
// otherwise see change under it mid-run.
func (a *App) ScriptTask(id string) (script.TaskView, bool) {
	a.mu.Lock()
	t := a.tasks[id]
	var snap core.Task
	if t != nil {
		snap = *t
	}
	a.mu.Unlock()
	if t == nil {
		return script.TaskView{}, false
	}
	return scriptTaskView(snap), true
}

// ScriptQueue is the QueueView every Fire/RunNow call, in this package and
// in internal/api's script run route, builds from - one conversion of
// Counters' three shared fields (script.QueueView mirrors QueueCounters'
// own Files/Disabled/Running - see view.go's own doc comment) rather than a
// copy of it at every call site.
func (a *App) ScriptQueue() script.QueueView {
	c := a.Counters()
	return script.QueueView{Files: c.Files, Disabled: c.Disabled, Running: c.Running}
}

// publishEvent stamps the queue counters onto a Firing and puts it on the
// bus. Every event this app raises goes through here, which is what keeps
// "the queue as it was at that moment" a promise rather than a field six
// call sites remember to fill in.
//
// NEVER CALL IT WHILE HOLDING a.mu. Two independent reasons, either one
// fatal on its own: ScriptQueue reaches Counters, which takes a.mu itself
// and would deadlock outright; and script.Bus.Publish delivers on this
// goroutine, so a subscriber would inherit the app's own lock and could
// reach Hub.Broadcast - every connected browser - with it held. Every caller
// in this package publishes after its critical section has closed, and the
// ones that need a snapshot taken under the lock take the snapshot there and
// publish afterwards (see fireExtractDone's callers).
func (a *App) publishEvent(f script.Firing) {
	f.Queue = a.ScriptQueue()
	a.Events.Publish(f)
}

// fireLinkAdded reports one link that actually entered the list - see
// script.TriggerLinkAdded for why a link the filter is holding is not one.
// t is already a detached copy (put's own `c := *t`), so nothing here reads
// a task another goroutine is writing to.
func (a *App) fireLinkAdded(t core.Task) {
	if t.Skipped {
		return
	}
	tv := scriptTaskView(t)
	a.publishEvent(script.Firing{Trigger: script.TriggerLinkAdded, Task: &tv})
}

// fireChecksumFailed reports a finished file whose hash did not match. Only
// the mismatch: see script.TriggerChecksumFailed for why "could not check"
// is not the same event.
func (a *App) fireChecksumFailed(t core.Task) {
	tv := scriptTaskView(t)
	a.publishEvent(script.Firing{Trigger: script.TriggerChecksumFailed, Task: &tv})
}

// fireExtractDone reports one finished unpacking. The download the archive
// belongs to is looked up and attached, so a script bound to extract.done
// still gets the five task verbs - but its absence is not an error: the task
// may well have been removed while the archive was being unpacked, which is
// an ordinary thing to do to a queue and not a reason to withhold the event.
func (a *App) fireExtractDone(j ExtractJob, failed bool) {
	v := script.ExtractView{
		JobID:    j.ID,
		TaskID:   j.TaskID,
		Name:     j.Name,
		Dir:      j.Dir,
		Package:  j.Package,
		OK:       !failed,
		Error:    j.Error,
		Password: j.Password,
		Files:    j.Files,
		Bytes:    j.Bytes,
		Nested:   j.Nested,
	}
	f := script.Firing{Trigger: script.TriggerExtractDone, Extract: &v}
	if tv, ok := a.ScriptTask(j.TaskID); ok {
		f.Task = &tv
	}
	a.publishEvent(f)
}

// fireReconnectDone reports one finished reconnect run, from the single
// place a run can end (Reconnect). err carries reconnect.ErrUnchanged when
// the router did as it was told and the address stayed put, which is why OK
// and Changed are two separate answers rather than one - see
// script.ReconnectView.
func (a *App) fireReconnectDone(res reconnect.Result, err error) {
	v := script.ReconnectView{
		OK:      err == nil,
		Changed: err == nil && res.NewIP.IsValid() && res.NewIP != res.OldIP,
		Checks:  res.Checks,
	}
	if res.OldIP.IsValid() {
		v.From = res.OldIP.String()
	}
	if res.NewIP.IsValid() {
		v.To = res.NewIP.String()
	}
	if err != nil {
		v.Error = err.Error()
	}
	a.publishEvent(script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &v})
}

// fireAccountExpiry reports an account whose expiry has just been read as
// past. It is given BOTH readings and decides for itself, because the sweep
// that calls it runs every fifteen minutes for the life of the process: a
// lapsed account is lapsed on every one of those passes, and firing on the
// state rather than on the crossing would be a notification script sending
// the same message four times an hour, for ever.
//
// The crossing is (was not past) -> (is past), plus one deliberate extra
// case: an expiry that MOVED and is still in the past fires again. That is a
// renewal the provider did not actually apply, which is a different fact
// from the original lapse and worth hearing about a second time.
func (a *App) fireAccountExpiry(svc, account string, prev AccountHealth, had bool, next AccountHealth) {
	if !accountLapsed(next) {
		return
	}
	if had && accountLapsed(prev) && prev.Expiry == next.Expiry {
		return
	}
	a.publishEvent(script.Firing{Trigger: script.TriggerAccountExpired, Account: &script.AccountView{
		Service: svc,
		Account: account,
		Label:   a.accountLabel(svc, account),
		Tier:    next.Tier,
		Expiry:  next.Expiry,
	}})
}

// accountLapsed reads one health row's expiry as "already past". An empty
// Expiry is never lapsed and that is the whole subtlety: formatExpiry writes
// "" both for an account with nothing to expire (a free tier) and for one
// nothing has read yet, and treating either as expired would fire this event
// for every free account on the box on the first sweep after boot.
func accountLapsed(h AccountHealth) bool {
	if h.Expiry == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, h.Expiry)
	if err != nil {
		// Not a timestamp this build wrote. Silently treating an
		// unparseable string as expired would turn a format change into a
		// wave of false alarms; leaving it alone costs one missed event on
		// a value nothing in this tree produces.
		return false
	}
	return at.Before(time.Now())
}

// fireCaptchaPending reports one challenge nobody has answered yet. Called
// only for challenges the store had not seen before - see
// script.TriggerCaptchaPending for why "still pending" must not fire on
// every two-second poll.
func (a *App) fireCaptchaPending(c captcha.Challenge) {
	v := script.CaptchaView{
		ID:     c.ID,
		TaskID: c.TaskID,
		Host:   c.Host,
		Kind:   string(c.Kind),
		Prompt: c.Prompt,
	}
	if !c.ExpiresAt.IsZero() {
		v.ExpiresAt = c.ExpiresAt.UTC().Format(time.RFC3339)
	}
	f := script.Firing{Trigger: script.TriggerCaptchaPending, Captcha: &v}
	// A challenge with no TaskID is "a real, expected answer, not a bug" in
	// internal/captcha's own words, so the lookup failing is not one either.
	if c.TaskID != "" {
		if tv, ok := a.ScriptTask(c.TaskID); ok {
			f.Task = &tv
		}
	}
	a.publishEvent(f)
}

// scriptActions adapts *App to script.Actions. It exists because the
// interface's exact method set - Retry(string) error, SetPriority(string,
// int), SetComment(string, string) - does not match any of App's own
// existing methods closely enough for *App to satisfy it structurally on
// its own (SetPriority(ids []string, priority int) takes a slice, and
// nothing on App is named Retry or SetComment at all). See script.Actions'
// own doc comment for the contract every method here has to uphold:
// non-blocking, and Retry must treat an empty taskID as retrying NOTHING.
type scriptActions struct{ a *App }

func (s scriptActions) Pause(taskID string)  { s.a.Pause(taskID) }
func (s scriptActions) Resume(taskID string) { s.a.Resume(taskID) }

// Retry rejects an empty taskID rather than forwarding it to RestartTasks,
// which treats an EMPTY slice as "every errored task" (app_queue.go's own
// doc comment) - the exact hazard script.Actions' doc comment names
// RestartTasks as the reference case for. internal/script's own bindings
// never construct a closure over an empty taskID (sandbox.go's taskGlobal),
// so this branch is a second, independent line of defence rather than the
// one this build relies on.
func (s scriptActions) Retry(taskID string) error {
	if taskID == "" {
		return errors.New("script: retry needs a task id")
	}
	s.a.RestartTasks([]string{taskID})
	return nil
}

func (s scriptActions) SetPriority(taskID string, priority int) {
	s.a.SetPriority([]string{taskID}, priority)
}

// SetComment routes through SetTaskOptions, whose only validation
// (folder/filename/name/chunks bounds - see its own doc comment) never
// triggers for a Comment-only patch, so the error it could return here is
// not one a script's fire-and-forget setComment(text) binding has any use
// acting on - the same reasoning notify()'s own rate-limit failure is
// swallowed for (sandbox.go's notifyFn).
func (s scriptActions) SetComment(taskID string, text string) {
	_ = s.a.SetTaskOptions([]string{taskID}, TaskOptions{Comment: &text})
}

// scriptIdlePoll matches idleaction.Controller's own defaultPoll: short
// enough that a script reacting to the queue emptying is not a visibly late
// notification, without turning a.Counters() into a busy loop.
const scriptIdlePoll = 2 * time.Second

// watchQueueIdleForScripts fires TriggerQueueIdle once per idle stretch,
// independent of whether idleaction.Controller's own end-of-queue action is
// even configured - that controller only evaluates its Fire callback when
// cfg.Action != ActionNone (internal/idleaction/controller.go's tick), so a
// script bound to queue.idle would otherwise never fire for the (ordinary)
// person who wants the notification without also turning on auto-pause.
//
// Started from New, after the boot-time task list is whole (the same
// ordering note a.sched.Start/a.idleAction.Start already carry: a.tasks is
// fully populated well before this runs, so there is no boot-time race to
// read a half-assembled queue). Stopped by a.ctx like every other App-owned
// goroutine - see a.spawn.
//
// Level-triggered like idleaction.Controller.tick, for the same reason: a
// script enabled while the queue already sits idle must still get its
// firing rather than waiting for a transition that already happened.
// Unlike that controller there is no everBusy gate - firing once for a
// queue that is genuinely idle at boot is this trigger doing exactly what
// its own name says, not the heavier "silently pause a queue nobody has
// touched yet" mistake everBusy exists to prevent for the pause action
// specifically (idleaction/controller.go's own doc comment on that field).
func (a *App) watchQueueIdleForScripts() {
	ticker := time.NewTicker(scriptIdlePoll)
	defer ticker.Stop()
	settled := false
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if !a.queueIdleForAction() {
				settled = false
				continue
			}
			if settled {
				continue
			}
			settled = true
			a.publishEvent(script.Firing{Trigger: script.TriggerQueueIdle})
		}
	}
}

// scriptPackagePoll is how often the package sweep looks. The same interval
// scriptIdlePoll uses, chosen the same way: a script reacting to a package
// finishing must not feel late, and one pass is the same single walk of
// a.tasks that Counters already makes twice a second on a busy queue.
const scriptPackagePoll = 2 * time.Second

// packageTaskPending answers the question script.TriggerPackageDone is
// built on: is this file something its package is still WAITING for?
//
// The four answers that are not obvious, each of which decides whether the
// event ever fires at all:
//
//   - A link the filter is holding (Skipped) is not pending. It is not work
//     in progress, it is a decision waiting for a person who may never make
//     it, and counting it would mean a package with one filtered link never
//     reports finished. It is still counted in PackageView.Skipped, so a
//     script can say so.
//   - A file the user switched off is not pending, exactly as
//     queueIdleForAction subtracts disabled files from the idle test: "not
//     right now" from the person who owns the queue is not owed work.
//   - A file that failed WITH a retry armed (NextTry set) IS pending. This
//     is the same "final word only" reading ClassifyTaskUpdate applies to
//     task.failed - a file between two backoff attempts has not finished
//     failing, and calling the package done in that window would fire the
//     event and then download another file.
//   - Everything else - collected, queued, running, paused, extracting - is
//     pending. Collected is the one worth stating: a link sitting in the
//     collector has not been offered to the queue yet, and a package that
//     reported itself finished with two of its five links never started
//     would be reporting on a download nobody made. Counters() takes the
//     opposite view for its own purpose (a collected file is not queue work,
//     so it does not hold the IDLE action off), and the two are answering
//     different questions on purpose.
func packageTaskPending(t *core.Task) bool {
	if t.Skipped || !t.Enabled {
		return false
	}
	switch t.Status {
	case core.StatusDone:
		return false
	case core.StatusError:
		return !t.NextTry.IsZero()
	default:
		return true
	}
}

// pkgTally is one package as the sweep sees it: the view a script gets, plus
// the two numbers that decide whether it fires. They are kept out of
// PackageView because they are this file's arithmetic, not a fact about the
// package worth handing to a script.
type pkgTally struct {
	view    script.PackageView
	pending int
	settled int
}

// complete is the definition of "this package is done", and the third clause
// is the one that is easy to leave out.
//
// Pending == 0 alone would call a package finished the instant it is created
// - a paste of five links whose tasks are all still being staged has nothing
// pending yet either - and it would call a package of nothing but filtered
// or switched-off links finished, which is a package that never downloaded a
// byte and never will. Requiring at least one file to have actually SETTLED
// (finished or finally failed) is what makes this event mean "the last part
// arrived" rather than "nobody is doing anything".
func (p pkgTally) complete() bool {
	return p.view.Files > 0 && p.pending == 0 && p.settled > 0
}

// scriptPackageTallies groups a task map by package name. Pure over the map
// so the definition above can be tested with plain struct literals instead
// of a whole App and a real download.
//
// Done + Failed + Skipped + Disabled deliberately does not partition Files -
// see script.PackageView's own doc comment. Disabled is counted alongside
// whatever else the file is, because "the user switched it off" is true of a
// finished download as much as of a queued one, and a script asking "did
// anything get turned off in here" wants both.
//
// Caller holds a.mu, or owns the map outright.
func scriptPackageTallies(tasks map[string]*core.Task) map[string]pkgTally {
	out := make(map[string]pkgTally, len(tasks))
	for _, t := range tasks {
		name := strings.TrimSpace(t.Package)
		if name == "" {
			// No package is not a package. Folding every unpackaged download
			// into one bucket under the empty name is the same mistake
			// packageFilesLocked (app_extract.go) refuses to make for the
			// info-file sweep, and here it would fire one package.done for
			// the whole shared download folder.
			continue
		}
		p := out[name]
		p.view.Name = name
		p.view.Files++
		p.view.Bytes += t.Loaded
		if t.Skipped {
			p.view.Skipped++
		}
		if !t.Enabled {
			p.view.Disabled++
		}
		switch {
		case packageTaskPending(t):
			p.pending++
		case t.Skipped:
			// Held by the filter: neither waited on nor settled. It never
			// ran, so counting it as an outcome would put a file in Done or
			// Failed that was never tried.
		case t.Status == core.StatusDone:
			p.view.Done++
			p.settled++
		case t.Status == core.StatusError:
			p.view.Failed++
			p.settled++
		}
		out[name] = p
	}
	return out
}

// watchPackagesForScripts fires script.TriggerPackageDone: the event this
// whole bus exists for, because task.done fires PER FILE and "do something
// once the last part of this set has arrived" is the commonest thing anyone
// wants from an automation hook.
//
// POLLED, NOT HOOKED, and that is a deliberate trade of two seconds of
// latency for correctness. A package can reach "nothing left to wait for"
// through more doors than anybody can hold in their head at once: the last
// file finishing (app_dispatch.go's onUpdate), an archive settling half an
// hour later (app_extract.go's settleExtraction, which never goes through
// onUpdate at all), the last stuck file being deleted, switched off, or
// restored from the holding area. Hooking the two obvious ones would have
// left an archive package - the exact case somebody wants this for - silently
// never firing, and the bug would have been invisible in any test that did
// not unpack a real .rar.
//
// Edge-triggered against the previous pass, per package. A package that
// gains a link after it fired goes back to incomplete and fires AGAIN when
// that link settles: it finished, it stopped being finished, and it finished
// again, and refusing the second one would mean the second half of a release
// somebody added five minutes later never triggers the unpack script. A
// script that must act only once has to say so itself - this package
// deliberately offers no cross-run store (see internal/script's own package
// doc comment).
//
// The first pass fires nothing, whatever it finds. Every package finished in
// a previous run of the process is complete the moment this loop starts, and
// announcing all of them at boot would be a notification script sending one
// message per package the user ever downloaded. Two seconds of "we have not
// looked yet" is the price, and it costs at most one missed firing for a
// package that happens to finish inside the first tick after a restart.
func (a *App) watchPackagesForScripts() {
	ticker := time.NewTicker(scriptPackagePoll)
	defer ticker.Stop()
	var complete map[string]bool
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			tallies := scriptPackageTallies(a.tasks)
			a.mu.Unlock()

			// Rebuilt rather than updated in place, so a package whose last
			// task was removed drops out instead of sitting in this map for
			// the life of the process - and so a package name that comes
			// back later is treated as the new package it is.
			next := make(map[string]bool, len(tallies))
			var done []script.PackageView
			for name, p := range tallies {
				next[name] = p.complete()
				if p.complete() && complete != nil && !complete[name] {
					done = append(done, p.view)
				}
			}
			// nil means "first pass" and is what makes the seeding above
			// silent; every later pass has a real map, empty or not.
			complete = next

			// Sorted because map iteration is random and two packages
			// finishing in the same pass would otherwise reach a script in a
			// different order every time - the kind of nondeterminism that
			// makes an intermittent report impossible to reproduce.
			sort.Slice(done, func(i, j int) bool { return done[i].Name < done[j].Name })
			for i := range done {
				v := done[i]
				a.publishEvent(script.Firing{Trigger: script.TriggerPackageDone, Package: &v})
			}
		}
	}
}
