package app

// The seam between internal/script and the app: constructing the Host (see
// New), adapting *App to script.Actions, converting core.Task/QueueCounters
// into script's own TaskView/QueueView, and firing TriggerQueueIdle - the
// one trigger with no existing single broadcast site to hook (compare
// TriggerTaskDone/TriggerTaskFailed, fired from app_dispatch.go's onUpdate
// right next to the "task" broadcast that already exists there). Wiring
// internal/script's package doc comment explicitly leaves to whoever lands
// this wave - see its "wiring this in is deliberately not this package's
// job" section.

import (
	"errors"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
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
			a.Scripts.Fire(script.TriggerQueueIdle, nil, a.ScriptQueue())
		}
	}
}
