package app

// The seam between internal/script and the app: the Host (see New), the
// script.Actions adapter, the conversions into script's own views, and every
// script.Firing this app publishes.
//
// This is the only file in internal/app that builds a Firing. The other sites
// call one named helper here, so the queue snapshot and the rule about not
// publishing under a.mu live in one place instead of in the hottest files of
// the package.
//
// queue.idle and package.done have no single site to hook and are polled from
// their own loops (watchQueueIdleForScripts, watchPackagesForScripts).

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

// scriptTaskView copies a core.Task into script's read-only view; internal/script
// does not import internal/core.
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

// ScriptTask returns one task's view by id, or false when there is no such
// task. It snapshots under a.mu so a script never sees the live task change
// mid-run.
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

// ScriptQueue is the QueueView every Fire and RunNow call builds from.
func (a *App) ScriptQueue() script.QueueView {
	c := a.Counters()
	return script.QueueView{Files: c.Files, Disabled: c.Disabled, Running: c.Running}
}

// publishEvent stamps the queue counters onto a Firing and puts it on the bus.
//
// Callers must not hold a.mu: Counters takes it and would deadlock, and
// Publish delivers on this goroutine, so a subscriber would run under the
// app's lock.
func (a *App) publishEvent(f script.Firing) {
	f.Queue = a.ScriptQueue()
	a.Events.Publish(f)
}

// fireLinkAdded reports one link that entered the list. A link the filter holds
// is not one (see script.TriggerLinkAdded). t is already a detached copy.
func (a *App) fireLinkAdded(t core.Task) {
	if t.Skipped {
		return
	}
	tv := scriptTaskView(t)
	a.publishEvent(script.Firing{Trigger: script.TriggerLinkAdded, Task: &tv})
}

// fireChecksumFailed reports a finished file whose hash did not match; "could
// not check" is a different event (see script.TriggerChecksumFailed).
func (a *App) fireChecksumFailed(t core.Task) {
	tv := scriptTaskView(t)
	a.publishEvent(script.Firing{Trigger: script.TriggerChecksumFailed, Task: &tv})
}

// fireExtractDone reports one finished unpacking with its download attached
// when that still exists. The task may have been removed during unpacking,
// which is no reason to withhold the event.
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

// fireReconnectDone reports one finished reconnect run. err is
// reconnect.ErrUnchanged when the router obeyed but the address stayed, which
// is why OK and Changed are separate (see script.ReconnectView).
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

// fireAccountExpiry reports an account whose expiry has just passed. The health
// sweep runs every fifteen minutes, so it fires on the crossing rather than the
// state. An expiry that moved and is still past fires again: a renewal the
// provider did not apply is worth hearing about.
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

// accountLapsed reports whether a health row's expiry is in the past. An empty
// Expiry means a free tier or not yet read, and is never lapsed.
func accountLapsed(h AccountHealth) bool {
	if h.Expiry == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, h.Expiry)
	if err != nil {
		// Not a timestamp this build wrote; a format change must not raise a
		// wave of false alarms.
		return false
	}
	return at.Before(time.Now())
}

// fireCaptchaPending reports a challenge the store had not seen before, so the
// two-second poll does not fire it again (see script.TriggerCaptchaPending).
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
	// A challenge without a TaskID is valid in internal/captcha.
	if c.TaskID != "" {
		if tv, ok := a.ScriptTask(c.TaskID); ok {
			f.Task = &tv
		}
	}
	a.publishEvent(f)
}

// scriptActions adapts *App to script.Actions, whose method set App's own
// methods do not match. Every method must be non-blocking (see script.Actions).
type scriptActions struct{ a *App }

func (s scriptActions) Pause(taskID string)  { s.a.Pause(taskID) }
func (s scriptActions) Resume(taskID string) { s.a.Resume(taskID) }

// Retry rejects an empty taskID, since RestartTasks reads an empty slice as
// every errored task.
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

// SetComment ignores the error because SetTaskOptions validates nothing a
// comment-only patch can trip.
func (s scriptActions) SetComment(taskID string, text string) {
	_ = s.a.SetTaskOptions([]string{taskID}, TaskOptions{Comment: &text})
}

// scriptIdlePoll matches idleaction.Controller's defaultPoll.
const scriptIdlePoll = 2 * time.Second

// watchQueueIdleForScripts fires TriggerQueueIdle once per idle stretch. The
// idle controller only ticks when an end-of-queue action is configured, and a
// script should fire without one.
//
// It is level-triggered like the controller, so a script enabled while the
// queue is already idle still fires. There is no everBusy gate: firing for a
// queue idle at boot is what the trigger means, unlike pausing one.
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

// scriptPackagePoll is how often the package sweep looks.
const scriptPackagePoll = 2 * time.Second

// packageTaskPending reports whether a package is still waiting for this file,
// the question script.TriggerPackageDone is built on.
//
//   - A link the filter holds is not pending; it may never be decided. It still
//     counts in PackageView.Skipped.
//   - A disabled file is not pending, as in queueIdleForAction.
//   - A failed file with a retry armed is pending, like ClassifyTaskUpdate's
//     reading of task.failed.
//   - Everything else is pending, including a collected link that has not been
//     offered to the queue yet. Counters takes the opposite view of collected
//     links because it answers a different question.
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
// the two numbers that decide whether it fires.
type pkgTally struct {
	view    script.PackageView
	pending int
	settled int
}

// complete reports whether the package is done. Requiring a settled file keeps
// a package that is still being staged, or holds only filtered or disabled
// links, from counting as finished.
func (p pkgTally) complete() bool {
	return p.view.Files > 0 && p.pending == 0 && p.settled > 0
}

// scriptPackageTallies groups a task map by package name. It is pure over the
// map so it can be tested with struct literals.
//
// The counts do not partition Files (see script.PackageView): Disabled is
// counted alongside whatever else the file is.
//
// Caller holds a.mu, or owns the map outright.
func scriptPackageTallies(tasks map[string]*core.Task) map[string]pkgTally {
	out := make(map[string]pkgTally, len(tasks))
	for _, t := range tasks {
		name := strings.TrimSpace(t.Package)
		if name == "" {
			// Unpackaged downloads are not one package; grouping them would
			// fire package.done for the whole download folder.
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
			// Held by the filter and never tried: neither pending nor settled.
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

// watchPackagesForScripts fires script.TriggerPackageDone once the last part of
// a package has arrived.
//
// It polls rather than hooks because a package can finish in many ways: the
// last file completing, an archive settling later in settleExtraction, the last
// stuck file being removed or disabled. Two seconds of latency buys not missing
// any of them.
//
// It is edge-triggered per package: a package that gains a link after firing
// fires again once that link settles. The first pass fires nothing, so packages
// finished in an earlier run are not announced at boot.
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

			// Rebuilt each pass so removed packages drop out and a returning
			// name counts as a new package.
			next := make(map[string]bool, len(tallies))
			var done []script.PackageView
			for name, p := range tallies {
				next[name] = p.complete()
				if p.complete() && complete != nil && !complete[name] {
					done = append(done, p.view)
				}
			}
			// nil marks the first pass, which only seeds the map.
			complete = next

			// Sorted so packages finishing in the same pass reach scripts in a
			// stable order.
			sort.Slice(done, func(i, j int) bool { return done[i].Name < done[j].Name })
			for i := range done {
				v := done[i]
				a.publishEvent(script.Firing{Trigger: script.TriggerPackageDone, Package: &v})
			}
		}
	}
}
