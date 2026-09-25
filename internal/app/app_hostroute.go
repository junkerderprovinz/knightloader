package app

// A host rule's say in which service fetches a link: the one it prefers and
// the ones it keeps away from the host (settings.HostRule's Prefer and
// Exclude). A pin outranks both, as it outranks the priority order.

import (
	"slices"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// hostChain is rankedChain with the rule for url's host applied: the services
// it excludes are left out, and the one it prefers moves to the front. Only a
// header profile and the user's own servers stay ahead of it, since they take
// nothing but the links they were set up for (see dynamicPrio).
func hostChain(chain []resolver.Resolver, url string, cfg settings.Settings) []resolver.Resolver {
	ranked := rankedChain(chain, url, cfg.ResolverOrder)
	rule := cfg.HostRuleFor(hostOf(url))
	if rule.Prefer == "" && len(rule.Exclude) == 0 {
		return ranked
	}
	var own, preferred, rest []resolver.Resolver
	for _, res := range ranked {
		id := res.Info().ID
		switch {
		case excluded(rule, id):
		case id == hostheaders.ResolverID || id == remotefs.ResolverID:
			own = append(own, res)
		case rule.Prefer != "" && pinMatches(rule.Prefer, id):
			preferred = append(preferred, res)
		default:
			rest = append(rest, res)
		}
	}
	return slices.Concat(own, preferred, rest)
}

// excluded reports whether rule keeps resolverID away from its host. An entry
// naming a service covers every account of it, as a pin does.
func excluded(rule settings.HostRule, resolverID string) bool {
	return slices.ContainsFunc(rule.Exclude, func(ex string) bool { return pinMatches(ex, resolverID) })
}

// unhandledError is the sentence for a link no backend takes: plain, unless
// the host rule's exclusions are what left nothing, which it says instead.
func (a *App) unhandledError(url, plain string) string {
	all := a.Registry.All(url)
	if len(all) == 0 || len(hostChain(all, url, a.Settings.Get())) > 0 {
		return plain
	}
	return "every backend that can fetch this link is excluded for " + hostOf(url)
}

// chainFor is the chain a task's backend is picked from: hostChain, or for a
// pinned task the plain ranking, since the pin outranks the host rule.
func (a *App) chainFor(t *core.Task) []resolver.Resolver {
	cfg := a.Settings.Get()
	if t.ResolverPin != "" {
		return rankedChain(a.Registry.All(t.URL), t.URL, cfg.ResolverOrder)
	}
	return hostChain(a.Registry.All(t.URL), t.URL, cfg)
}

// preferredLocked returns the first slot of the service t's host rule prefers
// that can take the link now, when t still sits on another service picked
// before the rule could have a say (see rerankLocked), or nil. Caller holds
// a.mu.
func (a *App) preferredLocked(chain []resolver.Resolver, t *core.Task) resolver.Resolver {
	want := a.Settings.Get().HostRuleFor(hostOf(t.URL)).Prefer
	if want == "" || pinMatches(want, t.Resolver) {
		return nil
	}
	for _, res := range chain {
		id := res.Info().ID
		if pinMatches(want, id) && !a.resolverOff(id) && !a.freeRefusedLocked(t, id) && a.routableForLocked(id, t.URL) {
			return res
		}
	}
	return nil
}
