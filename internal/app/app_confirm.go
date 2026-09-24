package app

import (
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// globalConfirmConfig returns the instance's own OnDupes/OnOffline pair, which
// settings has already sanitized.
func globalConfirmConfig(s settings.Settings) confirm.Config {
	return confirm.Config{
		OnDupes:   confirm.Policy(s.OnDupes),
		OnOffline: confirm.Policy(s.OnOffline),
	}
}

// confirmItemsLocked reduces the tasks about to be confirmed to the two facts
// onDupes and onOffline act on. Caller holds a.mu.
//
// A task is a duplicate when duplicatesLocked finds it or when it is a kept
// mirror, which a name-or-size match may miss. Only a definite AvailOffline
// counts as offline, so an unknown or uncheckable link is never excluded.
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

// ConfirmTasks is StartTasks with onDupes and onOffline applied first. Excluded
// links stay in the collector, exclude-and-remove links are removed, and ask
// links come back in Result.Ask. An empty id list means every collected task
// the link filter does not hold, as for StartTasks.
//
// batch is the per-batch override; a field that is confirm.UseGlobal or empty
// falls back to the instance settings, so the zero Config means no override.
func (a *App) ConfirmTasks(ids []string, batch confirm.Config, trigger confirm.Trigger) confirm.Result {
	cfg := confirm.ResolveConfig(batch, globalConfirmConfig(a.Settings.Get()), trigger)

	a.mu.Lock()
	items := a.confirmItemsLocked(a.confirmableLocked(ids))
	a.mu.Unlock()

	// Background activity is shown only for triggers nobody is watching.
	// Evaluate is a quick synchronous pass, so a start/end pair is enough.
	if !trigger.Interactive() && len(items) > 0 {
		a.beginActivity(ActivityAutoConfirm, len(items))
		defer a.endActivity(ActivityAutoConfirm, len(items))
	}

	result := confirm.Evaluate(items, cfg)
	// StartTasks reads an empty list as "start everything collected".
	if len(result.Start) > 0 {
		a.StartTasks(result.Start)
	}
	if len(result.Remove) > 0 {
		a.RemoveTasks(result.Remove, false)
	}
	return result
}

// confirmableLocked lists the tasks a confirm of ids acts on: those still in
// the collector, with neither the link filter holding them nor a preset
// setting them aside. An empty ids means every such task. Caller holds a.mu.
func (a *App) confirmableLocked(ids []string) []*core.Task {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0
	// Callers name the links they staged, and staging hands back one row of a
	// yt-dlp link's family; the link is all of them.
	for _, id := range a.variantSiblingsLocked(ids) {
		want[id] = true
	}
	var out []*core.Task
	for id, t := range a.tasks {
		if t.Status == core.StatusCollected && !t.Skipped && !t.VariantOff && (all || want[id]) {
			out = append(out, t)
		}
	}
	return out
}
