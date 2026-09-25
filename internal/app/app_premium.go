package app

// Premium only: a link whose only way down is a free download waits for an
// account instead of starting (settings.Settings.PremiumOnly, and
// settings.Category.PremiumOnly per drawer).

import (
	"slices"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// freeLocked reports whether resolverID would fetch t without an account. JD
// tells that from its host lists, which are empty after a restart until the
// first pass over the hoster logins has read them, and until then JD counts
// as free rather than as a backend that has no free mode. Caller holds a.mu.
func (a *App) freeLocked(t *core.Task, resolverID string) bool {
	switch a.modeForLocked(t, resolverID) {
	case core.ModeFree:
		return true
	case core.ModeUnknown:
		return resolverID == "jd" && hostOf(t.URL) != "" && !a.hosterAuth().Listed()
	}
	return false
}

// freeRefusedLocked reports whether premium only keeps t off resolverID: the
// switch applies to t's category, and that backend would fetch the link
// without an account. Caller holds a.mu.
func (a *App) freeRefusedLocked(t *core.Task, resolverID string) bool {
	return a.Settings.Get().PremiumOnlyFor(t.Category) && a.freeLocked(t, resolverID)
}

// premiumHeldLocked reports whether premium only is what leaves t without a
// backend: something open to it would fetch the link in free mode, and nothing
// fetches it on an account. An account that is only benched is still a way
// down, which hasUnroutableMatchLocked holds the task for. It answers for a
// task resolverForTaskLocked found nothing for. Caller holds a.mu.
func (a *App) premiumHeldLocked(t *core.Task) bool {
	if !a.Settings.Get().PremiumOnlyFor(t.Category) {
		return false
	}
	chain := a.chainFor(t)
	if t.ResolverPin == "" {
		chain = a.chainFromLocked(t, chain)
	}
	free := false
	for _, res := range chain {
		id := res.Info().ID
		if t.ResolverPin != "" && !pinMatches(t.ResolverPin, id) {
			continue
		}
		if a.modeForLocked(t, id) == core.ModePremium {
			return false
		}
		free = free || a.freeLocked(t, id)
	}
	return free
}

// markPremiumLocked puts WaitingPremium on a collected link that premium only
// would hold once it is started, takes it off one it would not, and reports
// whether it changed anything. The checks run in the order dispatchLocked
// meets them. Caller holds a.mu.
func (a *App) markPremiumLocked(t *core.Task) bool {
	if t.Status != core.StatusCollected || t.Skipped {
		return false
	}
	want := core.WaitingNone
	if a.premiumHeldLocked(t) && a.switchedOffMatchLocked(t) == "" && a.resolverForTaskLocked(t) == nil {
		want = core.WaitingPremium
	}
	if t.Waiting == want {
		return false
	}
	t.Waiting = want
	return true
}

// refreshPremiumHolds brings the mark on collected links up to date, and gives
// the queue a pass when it holds a link for premium only. It runs after
// whatever can open or close a way down: a settings save, an account added or
// removed, a pass over the hoster logins that changed one.
func (a *App) refreshPremiumHolds() {
	// With the switch off everywhere no link needs the walk through its chain,
	// only the marks an earlier setting left need taking off.
	anywhere := premiumOnlyAnywhere(a.Settings.Get())
	a.mu.Lock()
	var changed []core.Task
	held := false
	for _, t := range a.tasks {
		if (anywhere || t.Waiting == core.WaitingPremium) && a.markPremiumLocked(t) {
			changed = append(changed, *t)
		}
		if t.Status == core.StatusQueued && t.Waiting == core.WaitingPremium {
			held = true
		}
	}
	if held {
		a.dispatchLocked()
	}
	a.mu.Unlock()
	a.publishTasks(changed)
}

// premiumOnlyAnywhere reports whether premium only can hold a link at all: the
// instance's switch is on, or a drawer's own is.
func premiumOnlyAnywhere(cfg settings.Settings) bool {
	return cfg.PremiumOnly || slices.ContainsFunc(cfg.Categories, func(c settings.Category) bool {
		return c.PremiumOnly != nil && *c.PremiumOnly
	})
}
