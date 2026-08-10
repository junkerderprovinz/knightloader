package app

// The point where a batch leaving the collector is actually settled: what
// onDupes and onOffline do with a link that duplicates one already in the
// list or that a check has already found offline, evaluated together so the
// caller reports one sentence instead of running two prompts back to back.
//
// See internal/confirm for the policy itself - this file is only the glue
// that turns a Task into the two facts it cares about and turns its answer
// back into a StartTasks/RemoveTasks call.
//
// NOTHING IN THIS TREE CALLS ConfirmTasks YET. Every existing route to
// StartTasks - the manual "start" route (internal/api/routes_tasks.go), the
// auto-start branch inside AddLinksFrom and the watch folder's own
// auto-start (both internal/app/app_links.go, internal/app/app_watch.go) -
// still calls StartTasks directly, unfiltered, exactly as it did before
// this file existed. Wiring each of those to call ConfirmTasks instead, and
// to pass the confirm.Trigger that actually describes it, touches three
// files this wave's lane split hands to two other agents (8A/8B) - see this
// wave's own report for the exact call sites and the trigger each one
// wants.
import (
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// globalConfirmConfig is the instance's own OnDupes/OnOffline pair, already
// sanitized to a value confirm.Resolve can use directly - see
// internal/settings/settings_confirm.go's sanitizeConfirm.
func globalConfirmConfig(s settings.Settings) confirm.Config {
	return confirm.Config{
		OnDupes:   confirm.Policy(s.OnDupes),
		OnOffline: confirm.Policy(s.OnOffline),
	}
}

// confirmItemsLocked describes the collected tasks a confirm is about to
// settle, in exactly the two facts onDupes and onOffline act on. Caller
// holds a.mu.
//
// Duplicate reads two signals together. duplicatesLocked (app_bulk.go,
// CleanupDuplicates' own engine) already finds every task that is a second
// copy of another one already in the list by name+size or, failing that,
// URL - built for the cleanup menu, but the question it answers is exactly
// onDupes's own question, and unlike a mirror-set check it is not defeated
// by the one gap this wave's own report names: AddLinks refuses an exact
// repeat of a URL silently, at add time, so two collected tasks can never
// BOTH be pending copies of the very same paste - but a collected task
// duplicating one that has since finished, failed, or was kept as a mirror
// (Task.MirrorOf, app_mirror.go) reaches here every day, and duplicatesLocked
// already catches the first two; MirrorOf is checked too, belt and braces,
// for the one shape (a kept mirror still sitting on Hold) that a size-or-
// name match might not always agree with, depending on MirrorPolicy.
//
// Offline reads Online directly rather than any derived flag, so the
// contract "only a definite no may ever be excluded" is enforced by
// construction: AvailUnknown and AvailUncheckable both compare false here,
// exactly like AvailOnline does, and only AvailOffline ever compares true.
func (a *App) confirmItemsLocked(toStart []*core.Task) []confirm.Item {
	dupe := map[string]bool{}
	for _, id := range a.duplicatesLocked() {
		dupe[id] = true
	}
	items := make([]confirm.Item, 0, len(toStart))
	for _, t := range toStart {
		items = append(items, confirm.Item{
			ID:        t.ID,
			Duplicate: dupe[t.ID] || t.MirrorOf != "",
			Offline:   t.Online == core.AvailOffline,
		})
	}
	return items
}

// ConfirmTasks is StartTasks with onDupes and onOffline applied first: a
// batch's excluded links stay in the collector exactly as they were, its
// exclude-and-remove links are removed outright, and its ask links are
// neither - they come back in Result.Ask for whoever is watching to answer,
// untouched otherwise. An empty id list reaches every collected task not
// held by the link filter, exactly as an empty id list does for StartTasks.
//
// batch is the per-batch override this confirm carries, read against the
// instance's own OnDupes/OnOffline when either field is confirm.UseGlobal or
// its Go zero value "" - the two read identically, so a caller with no
// override at all can simply pass the zero Config{}.
func (a *App) ConfirmTasks(ids []string, batch confirm.Config, trigger confirm.Trigger) confirm.Result {
	cfg := confirm.ResolveConfig(batch, globalConfirmConfig(a.Settings.Get()), trigger)

	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	var candidates []*core.Task
	for id, t := range a.tasks {
		if t.Status == core.StatusCollected && !t.Skipped && (all || want[id]) {
			candidates = append(candidates, t)
		}
	}
	items := a.confirmItemsLocked(candidates)
	a.mu.Unlock()

	// Ambient activity only for the three triggers nobody is watching - see
	// confirm.Trigger.Interactive's own doc comment (internal/confirm/policy.go).
	// No production caller passes TriggerManual today (both real callers,
	// app_links.go's AddLinksFrom and app_watch.go's stageWatchJob, use
	// TriggerAutoConfirm/TriggerWatch) - the collector's own manual
	// Confirm/Start button calls StartTasks directly rather than through
	// here, since a person who is already looking at the staged batch has
	// made their own dupe/offline judgement and gains nothing from this
	// policy pass. TriggerManual and this guard exist for whichever future
	// caller needs ConfirmTasks run with a person actually watching -
	// confirm.Evaluate below is a pure, synchronous pass over already-known
	// facts - no network call, no goroutine - so this is a start/end pair
	// around one function call rather than per-item progress, the same way
	// a batch this fast has no meaningful "halfway" to report.
	if !trigger.Interactive() && len(items) > 0 {
		a.beginActivity(ActivityAutoConfirm, len(items))
		defer a.endActivity(ActivityAutoConfirm, len(items))
	}

	result := confirm.Evaluate(items, cfg)
	// Guarded rather than handed through unconditionally: StartTasks reads
	// an empty id list as "start everything collected", which is exactly
	// wrong here when every candidate this call actually considered was
	// held back - it must start nothing, not fall through to the wider
	// meaning of empty. RemoveTasks has no such second meaning for an empty
	// list, but the guard costs nothing and says the same thing either way.
	if len(result.Start) > 0 {
		a.StartTasks(result.Start)
	}
	if len(result.Remove) > 0 {
		a.RemoveTasks(result.Remove, false)
	}
	return result
}
