package app

// Module switches: the modules page can switch off a subsystem that has no
// setting of its own to clear. The list lives in settings.ModulesOff; this
// file answers the questions every gate asks of it.

import (
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// resolverModule maps the backends that belong to a switchable module to that
// module's id on the modules page.
var resolverModule = map[string]string{
	"jd":      "jd",
	"ytdlp":   "ytdlp",
	"torrent": "torrents",
}

// ModuleOff reports whether the module with this id is switched off.
func (a *App) ModuleOff(id string) bool {
	return a.Settings.Get().ModuleOff(id)
}

// applyModuleSwitches hands a switch to the subsystem that keeps its own copy
// instead of reading the settings on every use.
func (a *App) applyModuleSwitches(s settings.Settings) {
	if a.Scripts != nil {
		a.Scripts.SetOff(s.ModuleOff("scripting"))
	}
}

// resolverOff reports whether the backend with this resolver id belongs to a
// switched-off module. The backend stays registered, so a transfer already
// running in it can still be paused or removed.
func (a *App) resolverOff(resolverID string) bool {
	m, ok := resolverModule[resolverID]
	return ok && a.ModuleOff(m)
}

// switchedOffAboveLocked reports whether a switched-off backend ranks above
// resolverID in chain. Caller holds a.mu.
func (a *App) switchedOffAboveLocked(chain []resolver.Resolver, resolverID string) bool {
	for _, res := range chain {
		id := res.Info().ID
		if id == resolverID {
			return false
		}
		if a.resolverOff(id) {
			return true
		}
	}
	return false
}

// switchedOffMatchLocked returns the highest-ranked switched-off backend that
// claims the task's link, or that its pin names, and "" when there is none.
// Dispatch then holds the task until that module is back on instead of
// failing it. Caller holds a.mu.
func (a *App) switchedOffMatchLocked(t *core.Task) string {
	for _, res := range rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder) {
		id := res.Info().ID
		if t.ResolverPin != "" && !pinMatches(t.ResolverPin, id) {
			continue
		}
		if a.resolverOff(id) {
			return id
		}
	}
	return ""
}
