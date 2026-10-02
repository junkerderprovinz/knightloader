package app

// Further sources for one file (settings.MultiSource): the task's link
// unlocked by another debrid account, and the parked copies the mirror set
// kept from other hosters. The engine only uses a link that serves the same
// bytes as the task's own (see engine.Engine.vetSources).

import (
	"cmp"
	"context"
	"log"
	"slices"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// maxFurtherSources caps the links asked for beside a task's own. Each costs
// an unlock and a check before the transfer starts.
const maxFurtherSources = 3

// sourceAsk is one further link: unlocked through svc, or used as it is when
// svc is nil.
type sourceAsk struct {
	svc  debrid.Service
	link string
}

// sourcesLocked is how the engine asks for t's further sources, or nil when
// the setting is off or t's backend hands over no plain link to share the
// file with. Caller holds a.mu.
func (a *App) sourcesLocked(t *core.Task, cfg settings.Settings) func(context.Context) []string {
	if !cfg.MultiSource || torrent.IsURI(t.URL) || !sharesSources(t.Resolver) {
		return nil
	}
	asks := a.unlockersLocked(t)
	for _, c := range a.parkedCopiesLocked(t) {
		if ask, ok := a.copyAskLocked(c); ok {
			asks = append(asks, ask)
		}
	}
	if len(asks) == 0 {
		return nil
	}
	asks = asks[:min(len(asks), maxFurtherSources)]
	id := t.ID
	return func(ctx context.Context) []string {
		links := make([]string, len(asks))
		var wg sync.WaitGroup
		for i, ask := range asks {
			if ask.svc == nil {
				links[i] = ask.link
				continue
			}
			wg.Go(func() {
				d, err := ask.svc.Unlock(ctx, ask.link)
				if err != nil {
					log.Printf("task %s: %s gave no further source: %v", id, ask.svc.Label(), err)
					return
				}
				links[i] = d.URL
			})
		}
		wg.Wait()
		return links
	}
}

// sharesSources reports whether a task on this backend reaches the engine
// with a link of its own that further links can stand beside: a debrid
// service's or TorBox's unlock, or a plain file link. The user's own servers
// and header profiles log in with headers, and JD and yt-dlp fetch for
// themselves.
func sharesSources(resolverID string) bool {
	service, _ := resolver.SplitSlot(resolverID)
	return isDebridService(service) || resolverID == "direct" || resolverID == "http"
}

// unlockersLocked asks every other usable one-shot debrid account that takes
// t's link to unlock it too. Caller holds a.mu.
func (a *App) unlockersLocked(t *core.Task) []sourceAsk {
	var asks []sourceAsk
	for _, res := range a.chainFor(t) {
		if res.Info().ID == t.Resolver {
			continue
		}
		if svc := a.unlockerLocked(t, res); svc != nil {
			asks = append(asks, sourceAsk{svc, t.URL})
		}
	}
	return asks
}

// unlockerLocked is res's debrid service when it is a one-shot service that
// may take t's link now, else nil. Caller holds a.mu.
func (a *App) unlockerLocked(t *core.Task, res resolver.Resolver) debrid.Service {
	dr, ok := res.(debrid.Resolver)
	id := res.Info().ID
	if !ok || dr.Svc == nil || a.resolverOff(id) || a.freeRefusedLocked(t, id) || !a.routableForLocked(id, t.URL) {
		return nil
	}
	return dr.Svc
}

// parkedCopiesLocked is every parked copy of t's file, oldest first, as
// parkedMirrorLocked picks them. Caller holds a.mu.
func (a *App) parkedCopiesLocked(t *core.Task) []*core.Task {
	root := a.mirrorRootLocked(t)
	var out []*core.Task
	for id, c := range a.tasks {
		if id == t.ID || c.MirrorOf == "" || c.Enabled || c.Skipped {
			continue
		}
		if c.Status != core.StatusCollected && c.Status != core.StatusQueued {
			continue
		}
		if a.mirrorRootLocked(c) == root {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(x, y *core.Task) int {
		return cmp.Or(x.CreatedAt.Compare(y.CreatedAt), strings.Compare(x.ID, y.ID))
	})
	return out
}

// copyAskLocked is how a parked copy's link becomes a source: unlocked by the
// first debrid account that takes it, or as it is when it is a plain file
// link. A copy only JD or yt-dlp can fetch gives none. Caller holds a.mu.
func (a *App) copyAskLocked(c *core.Task) (sourceAsk, bool) {
	chain := a.chainFor(c)
	for _, res := range chain {
		if svc := a.unlockerLocked(c, res); svc != nil {
			return sourceAsk{svc, c.URL}, true
		}
	}
	if slices.ContainsFunc(chain, func(r resolver.Resolver) bool { return r.Info().ID == "direct" }) {
		return sourceAsk{nil, c.URL}, true
	}
	return sourceAsk{}, false
}
