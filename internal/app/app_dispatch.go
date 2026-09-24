package app

// The handover to a download backend and what comes back: which backend a
// task goes to, when it may go, and what happens when it finishes, fails or
// asks to be retried.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// modeForLocked reports whether a task routed to resolverID goes out on an
// account or anonymously. It is for display only; routing is decided by
// jd.PriorityFor. A plain file on an ordinary web server gets no label, since
// only hoster links are free or premium. Caller holds a.mu.
func (a *App) modeForLocked(t *core.Task, resolverID string) core.DownloadMode {
	if t == nil {
		return core.ModeUnknown
	}
	host := hostOf(t.URL)
	if host == "" {
		return core.ModeUnknown
	}
	// A debrid service is itself the account.
	if _, _, ok := a.accountForResolverLocked(resolverID); ok {
		return core.ModePremium
	}
	if resolverID == "jd" {
		// An active login on the sidecar is premium; JD knowing the host
		// without one is free mode.
		if jd.HostActive(host) {
			return core.ModePremium
		}
		if jd.HostKnown(host) {
			return core.ModeFree
		}
	}
	return core.ModeUnknown
}

// dynamicPrio returns res's priority for this url. Usually that is the static
// Info().Prio, but JD's priority depends on the host (see jd.PriorityFor), so
// it is asked per URL.
//
// order is settings.Settings.ResolverOrder, the user's hand-arranged ranking,
// and it wins outright; unnamed resolvers keep their automatic numbers, which
// orderBase keeps below every named one. It only reorders resolvers that can
// take the URL at all.
//
// An order entry names a service and moves all of its account slots, so a
// second key is not left behind at its automatic priority. An entry naming a
// full slot id is honoured as written.
func dynamicPrio(res resolver.Resolver, url string, order []string) int {
	id := res.Info().ID
	switch {
	case id == hostheaders.ResolverID || id == remotefs.ResolverID:
		// A header profile and the user's own servers take only the links they
		// were set up for, so they stay above every row of the card, and an
		// order entry naming one, which the card cannot write, is ignored.
		return orderBase + res.Info().Prio
	case id == "http":
		// The fallback is last whatever an order entry says.
		return res.Info().Prio
	case id == "jd":
		// A connected hoster login is a row of its own on the priority card, so
		// the order decides whether a link to that host goes out on the login
		// or through a debrid service that carries the host too. The login's
		// row wins over JD's own, which ranks JD for every other host.
		if host := jd.LoginHost(url); host != "" {
			if i := slices.IndexFunc(order, func(entry string) bool { return loginRowFor(entry, host) }); i >= 0 {
				return orderBase - i
			}
		}
	}
	for i, want := range order {
		if want == id {
			return orderBase - i
		}
	}
	if service, _ := resolver.SplitSlot(id); service != id {
		for i, want := range order {
			if want == service {
				return orderBase - i
			}
		}
	}
	if id == "jd" {
		return jd.PriorityFor(url)
	}
	return res.Info().Prio
}

// loginRowID is the order entry for one host's hoster login.
func loginRowID(host string) string { return "login:" + host }

// loginRowFor reports whether an order entry is the login row for host, given
// by its main domain. A login saved under an alias such as rg.to has its row
// under that alias.
func loginRowFor(entry, host string) bool {
	saved, ok := strings.CutPrefix(entry, "login:")
	return ok && hostalias.Canonical(saved) == host
}

// orderBase is the priority of the first entry in a hand-arranged order; each
// following entry gets one less. It is far above the automatic band (JD's
// highest is 60), so a long list never mixes with it.
const orderBase = 1000

// rankedChain returns chain stably sorted by dynamicPrio, so resolvers
// dynamicPrio does not distinguish keep the registry's order.
func rankedChain(chain []resolver.Resolver, url string, order []string) []resolver.Resolver {
	out := make([]resolver.Resolver, len(chain))
	copy(out, chain)
	sort.SliceStable(out, func(i, j int) bool {
		return dynamicPrio(out[i], url, order) > dynamicPrio(out[j], url, order)
	})
	return out
}

// ResolverPriority returns the order services are actually asked in, for the
// priority card on the Accounts page. The registry's own order ignores the
// hand-arranged order and JD's per-host boost, so it would show a ladder the
// downloader does not use.
//
// An empty host lists what the card orders: every registered service offCard
// does not leave out, and one row per switched-on hoster login; otherwise the
// whole chain for that host. There is one row per service, not per account
// slot: the card saves the ids it shows back into ResolverOrder, and which
// account of a service goes first is decided by routedAccounts.
func (a *App) ResolverPriority(host string) []resolver.Info {
	host = strings.TrimSpace(host)
	url := ""
	chain := a.Registry.List()
	if host != "" {
		url = "https://" + host + "/"
		chain = a.Registry.All(url)
	}
	order := a.Settings.Get().ResolverOrder
	type row struct {
		info resolver.Info
		prio int
	}
	var rows []row
	seen := map[string]bool{}
	for _, res := range rankedChain(chain, url, order) {
		info := res.Info()
		service, _ := resolver.SplitSlot(info.ID)
		if seen[service] || (host == "" && offCard(service)) {
			continue
		}
		seen[service] = true
		prio := dynamicPrio(res, url, order)
		// The service id, so a drag saves an order dynamicPrio matches.
		info.ID = service
		rows = append(rows, row{info, prio})
	}
	if host == "" {
		for _, l := range a.HosterLogins() {
			if !l.Enabled {
				continue
			}
			id := loginRowID(l.Host)
			prio := jd.ActiveLoginPrio
			if i := slices.Index(order, id); i >= 0 {
				prio = orderBase - i
			}
			rows = append(rows, row{resolver.Info{ID: id, Prio: jd.ActiveLoginPrio}, prio})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].prio > rows[j].prio })
	}
	out := make([]resolver.Info, len(rows))
	for i, r := range rows {
		out[i] = r.info
	}
	return out
}

// perLinkResolvers claim nearly any link: JD and the HTTP fallback any http
// link, yt-dlp any host that is not a file hoster, and direct anything that
// looks like a file. Which hosts they leave alone is part of their claim (see
// hostClaims), so the priority card can order them like any other service.
// Past a switched-off backend they may not take its link over (see
// resolverForTaskLocked).
var perLinkResolvers = map[string]bool{"direct": true, "jd": true, "ytdlp": true, "http": true}

// offCard reports whether the priority card leaves a resolver out: a stored
// header profile, which goes first for its origin, the user's own servers and
// the HTTP fallback. The first two take only links the user pointed them at,
// so there is nothing to rank, and the fallback is by definition last.
// dynamicPrio ignores an order entry naming any of them.
func offCard(id string) bool {
	return id == "http" || id == hostheaders.ResolverID || id == remotefs.ResolverID
}

// SaveResolverOrder stores the order the priority card sends and answers with
// the card's rows as re-read. What the card leaves out is dropped on the way
// in.
func (a *App) SaveResolverOrder(order []string) ([]resolver.Info, error) {
	kept := make([]string, 0, len(order))
	for _, id := range order {
		if !offCard(strings.TrimSpace(id)) {
			kept = append(kept, id)
		}
	}
	raw, _ := json.Marshal(kept)
	if _, err := a.PatchSettings(map[string]json.RawMessage{"resolverOrder": raw}); err != nil {
		return nil, err
	}
	return a.ResolverPriority(""), nil
}

// setWaitingLocked sets each listed task's waiting reason: per[id] when given,
// else only. It broadcasts only tasks that changed, since dispatch runs on
// nearly every event. Caller holds a.mu.
func (a *App) setWaitingLocked(ids []string, only core.Waiting, per ...map[string]core.Waiting) {
	var changed []core.Task
	for _, id := range ids {
		t := a.tasks[id]
		if t == nil {
			continue
		}
		want := only
		for _, m := range per {
			if w, ok := m[id]; ok {
				want = w
			}
		}
		if t.Waiting == want {
			continue
		}
		t.Waiting = want
		changed = append(changed, *t)
	}
	if len(changed) > 0 {
		// Published off this goroutine, since the caller holds a.mu.
		a.spawn(func() { a.publishTasks(changed) })
	}
}

// hostCapFor returns the per-host connection limit the resolver reports for
// host, or 0 (see resolver.HostCapper). It is one more ceiling for connsFor.
func hostCapFor(res resolver.Resolver, host string) int {
	hc, ok := res.(resolver.HostCapper)
	if !ok {
		return 0
	}
	return hc.HostCap(host)
}

// maxPerHostFor returns how many transfers one host may have in flight: its
// settings.HostRules entry, else the global MaxPerHost. The entry may be
// larger or smaller than the global value; MaxConcurrent still applies.
func maxPerHostFor(cfg settings.Settings, host string) int {
	if r := cfg.HostRuleFor(host); r.MaxPerHost > 0 {
		return r.MaxPerHost
	}
	return cfg.MaxPerHost
}

// resolverForTaskLocked picks the resolver for a task: its pin if it has one,
// else the recorded resolver if its account is usable, else the first usable
// entry of rankedChain. A benched account stays registered and is skipped
// here, so a second account of the same service is simply the next entry in
// the chain. Caller holds a.mu.
func (a *App) resolverForTaskLocked(t *core.Task) resolver.Resolver {
	// A pin is the whole answer and may be nil (see pinnedResolverLocked);
	// the caller reports that rather than falling back to the chain.
	if t.ResolverPin != "" {
		return a.pinnedResolverLocked(t)
	}
	chain := rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder)
	// A fallback may have recorded the direct download or yt-dlp while a
	// backend above it was switched off; that backend decides, not the fallback.
	if t.Resolver != "" && a.accountRoutableLocked(t.Resolver) && !a.resolverOff(t.Resolver) &&
		!(perLinkResolvers[t.Resolver] && a.switchedOffAboveLocked(chain, t.Resolver)) {
		for _, res := range a.Registry.All(t.URL) {
			if res.Info().ID == t.Resolver {
				return res
			}
		}
	}
	// A task whose recorded backend is switched off stands at that backend's
	// place in the chain. The backends above it have had the link already, and
	// asking them again would loop between a decline and the switch.
	if a.resolverOff(t.Resolver) {
		if i := slices.IndexFunc(chain, func(r resolver.Resolver) bool { return r.Info().ID == t.Resolver }); i >= 0 {
			chain = chain[i:]
		}
	}
	passedOff := false
	for _, res := range chain {
		id := res.Info().ID
		if a.resolverOff(id) {
			passedOff = true
			continue
		}
		// Past a switched-off backend only a service that lists the host, such
		// as a debrid account, may take the link over. The backends that take
		// any link would fetch the hoster's page or hand a video to JD.
		if passedOff && perLinkResolvers[id] {
			return nil
		}
		if a.accountRoutableLocked(id) {
			return res
		}
	}
	return nil
}

// pinnedResolverLocked returns the first entry of the ranked chain that the
// pin names and whose account is usable, or nil. A pin naming a service is met
// by any of its accounts. Account health is not bypassed, and nothing falls
// through to another service. Caller holds a.mu.
func (a *App) pinnedResolverLocked(t *core.Task) resolver.Resolver {
	for _, res := range rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder) {
		id := res.Info().ID
		if !pinMatches(t.ResolverPin, id) || a.resolverOff(id) {
			continue
		}
		if a.accountRoutableLocked(id) {
			return res
		}
	}
	return nil
}

// pinFailureLocked explains why a pinned task has nowhere to go. A pinned
// backend that does not handle the link is ReasonUnsupported and needs the
// user. One that handles it but has no usable account is ReasonAuth: unlike an
// unpinned task, which is held quietly, a pinned task fails visibly, and the
// ordinary retry picks it up if the account recovers. Caller holds a.mu.
func (a *App) pinFailureLocked(t *core.Task) (string, core.Reason) {
	for _, res := range a.Registry.All(t.URL) {
		if pinMatches(t.ResolverPin, res.Info().ID) {
			return "pinned to " + t.ResolverPin + ", and that backend's account is not usable right now", core.ReasonAuth
		}
	}
	return "pinned to " + t.ResolverPin + ", which does not handle this link", core.ReasonUnsupported
}

// pinMatches reports whether a resolver id satisfies a pin. A pin may name a
// full slot ("alldebrid#work") or a service ("alldebrid"), the same rule
// ResolverOrder uses.
func pinMatches(pin, resolverID string) bool {
	if pin == resolverID {
		return true
	}
	service, _ := resolver.SplitSlot(resolverID)
	return pin == service
}

// ResolverPinnable reports whether resolverID can be pinned, so a request for
// a backend this instance does not have is refused up front. The empty id,
// which removes a pin, is always valid. It checks the registry, which only
// holds the backends currently wired up.
func (a *App) ResolverPinnable(resolverID string) error {
	id := strings.TrimSpace(resolverID)
	if id == "" {
		return nil
	}
	for _, registered := range a.Registry.IDs() {
		if pinMatches(id, registered) {
			return nil
		}
	}
	return fmt.Errorf("%q is not a download backend this instance has", id)
}

// PinResolver pins each task to one backend, or unpins it when resolverID is
// empty, and dispatches right away so the result shows at once. A running
// transfer is not moved; the pin decides where the next attempt goes.
func (a *App) PinResolver(ids []string, resolverID string) error {
	id := strings.TrimSpace(resolverID)
	// Validated before any task is touched, so a refusal never leaves half a
	// selection edited.
	if err := a.ResolverPinnable(id); err != nil {
		return err
	}
	a.mu.Lock()
	var touched []string
	for _, taskID := range ids {
		t := a.tasks[taskID]
		if t == nil || t.ResolverPin == id {
			continue
		}
		t.ResolverPin = id
		touched = append(touched, taskID)
	}
	if len(touched) > 0 {
		a.dispatchLocked()
	}
	// Copied after the dispatch, which may have failed one of these tasks.
	changed := make([]core.Task, 0, len(touched))
	for _, taskID := range touched {
		if t := a.tasks[taskID]; t != nil {
			changed = append(changed, *t)
		}
	}
	a.mu.Unlock()
	for i := range changed {
		_ = a.Store.Save(&changed[i])
		a.Hub.Broadcast("task", &changed[i])
	}
	return nil
}

// nextResolverLocked returns the resolver to try after the one the task just
// used, following rankedChain, or "" when the chain is exhausted. Caller holds
// a.mu.
func (a *App) nextResolverLocked(t *core.Task) string {
	// A pinned task has no next resolver. Every fallback path asks here, so
	// this one check keeps a pin from being bypassed.
	if t.ResolverPin != "" {
		return ""
	}
	chain := rankedChain(a.Registry.All(t.URL), t.URL, a.Settings.Get().ResolverOrder)
	for i, res := range chain {
		if res.Info().ID == t.Resolver {
			if i+1 < len(chain) {
				return chain[i+1].Info().ID
			}
			return "" // the chain is exhausted
		}
	}
	// The recorded backend has been unregistered (a credential removed, a
	// binary missing), so start over from the top.
	for _, res := range chain {
		if res.Info().ID != t.Resolver {
			return res.Info().ID
		}
	}
	return ""
}

// routeForLocked picks the outbound connection for a task and returns its id.
// The task's own choice wins; otherwise the picker applies this host's list,
// filters, per-connection limits and bans. No picker, no usable entry, or an
// entry whose Route fails all mean the machine's own address. Caller holds
// a.mu.
func (a *App) routeForLocked(t *core.Task, host string) (proxycfg.Route, string) {
	p := a.picker
	if p == nil {
		return proxycfg.Route{}, ""
	}
	// Current load per connection, counted over running tasks, so limits hold.
	inUse := map[string]int{}
	for id := range a.active {
		if other := a.tasks[id]; other != nil && other.Connection != "" {
			inUse[other.Connection]++
		}
	}
	e, ok := p.PickFor(t.Connection, host, inUse)
	if !ok {
		return proxycfg.Route{}, ""
	}
	r, err := e.Route()
	if err != nil {
		// Sanitize already refuses malformed entries, so this is a bug; it
		// should not take the download down with it.
		log.Printf("connection %s is unusable, going out directly: %v", e.ID, err)
		return proxycfg.Route{}, ""
	}
	return r, e.ID
}

// maxForcedDownloads bounds the pool forced tasks run in, on top of the
// ordinary limit, so forcing a large selection does not open hundreds of
// transfers (JDownloader's MaxForcedDownloads).
const maxForcedDownloads = 3

// countStartLocked books a starting task into the right slot count, so the two
// start sites cannot drift apart. Caller holds a.mu.
func (a *App) countStartLocked(t *core.Task, host string, perHost map[string]int, forced, normal *int) {
	if t.Forced {
		*forced++
		return
	}
	*normal++
	perHost[host]++
}

// dispatchLocked starts queued tasks while slots are free, in queue order,
// letting tasks for other hosts skip ahead of a host at its limit. Caller holds
// a.mu.
func (a *App) dispatchLocked() {
	// The background watchers start here, once each, because dispatchLocked
	// runs at start-up and on nearly every change. They start before the halt
	// check, since captchas, stalls, disk space and the volume counter matter
	// while the queue is halted too, and the volume gate below reads the
	// counter's cached answer.
	a.ensureCaptchaPoller()
	a.ensureStallWatcher()
	a.ensureDiskWatcher()
	a.ensureVolumeCapWatcher()
	if a.halted {
		// Every queued row says why, so a stopped queue does not look like a
		// full one.
		a.setWaitingLocked(a.queue, core.WaitingHalted)
		return
	}
	// The volume allowance, when set to hold the queue. It is a gate rather
	// than a.halted, which the next settings save would undo (see
	// app_volumecap.go). Running transfers continue.
	if a.volumeCapHolds() {
		a.setWaitingLocked(a.queue, core.WaitingVolume)
		return
	}
	// The settings in force, since quiet mode overrides the slot counts.
	cfg := a.cfgInForceLocked()
	// Ordered by Task.Priority, then Position. A category's priority is
	// applied only when a task is created (see packagize), never here, or
	// every pass would undo manual reordering.
	a.sortQueueLocked()
	// settled collects tasks turned down here. The caller's copy was taken
	// earlier, so without a copy of its own the refusal would never reach the
	// store or the browsers.
	var settled []core.Task
	// Why each task does not start, applied at the end (see setWaitingLocked).
	waiting := map[string]core.Waiting{}
	perHost := map[string]int{}
	// Forced tasks are counted apart, so a forced download cannot push an
	// ordinary one out of its slot.
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
	space := newSpaceCheck(cfg)
	// The queue as the pass began, so tasks that start in this pass get their
	// waiting reason cleared too.
	before := append([]string(nil), a.queue...)
	for _, id := range a.queue {
		t := a.tasks[id]
		if t == nil {
			continue // removed while queued
		}
		// Nothing in the queue has been given up on. Cleared here, where every
		// requeue path (fallback, Resume, boot, RestartTasks) meets.
		t.GaveUp = false
		// Disabled, held and captcha-blocked tasks keep their place but do not
		// start. A captcha-blocked task is normally active, not queued; this
		// guards against a requeue handing JD the same link twice.
		if !t.Enabled || t.Hold || a.captchaWaitingLocked(id) {
			switch {
			case !t.Enabled:
				waiting[id] = core.WaitingDisabled
			case t.Hold:
				waiting[id] = core.WaitingHold
			default:
				waiting[id] = core.WaitingCaptcha
			}
			rest = append(rest, id)
			continue
		}
		h := hostOf(t.URL)
		// dirFor expands templates, so it is computed once per task.
		dir := a.dirFor(t)
		// Disk space is checked before the slot limits: on a full disk, "all
		// slots busy" would point at the wrong fix. See app_diskguard.go.
		if w := space.admit(dir, t); w != core.WaitingNone {
			waiting[id] = w
			rest = append(rest, id)
			continue
		}
		// Forced tasks bypass the ordinary limits but share a small pool of
		// their own.
		if t.Forced {
			if forcedActive >= maxForcedDownloads {
				waiting[id] = core.WaitingForced
				rest = append(rest, id)
				continue
			}
		} else {
			if normalActive >= cfg.MaxConcurrent {
				waiting[id] = core.WaitingSlot
				rest = append(rest, id)
				continue
			}
			if perHost[h] >= maxPerHostFor(cfg, h) {
				// Separate from WaitingSlot: raising MaxConcurrent does not help.
				waiting[id] = core.WaitingHost
				rest = append(rest, id)
				continue
			}
		}
		if a.started[id] && a.resolverOff(t.Resolver) {
			waiting[id] = core.WaitingModule
			rest = append(rest, id)
			continue
		}
		if a.started[id] {
			a.active[id] = true
			a.countStartLocked(t, h, perHost, &forcedActive, &normalActive)
			space.commit(dir, t)
			go a.backendFor(t.Resolver).Resume(id)
			continue
		}
		// The filter is asked once more before bytes move, since rules may
		// have changed since staging. A link the user restored from the
		// holding area is exempt.
		if v := a.filter(candidateOf(t)); v.Rejected && !filterWaived(t) {
			t.Status = core.StatusError
			t.Online = core.AvailOffline
			t.Error = rejection(v)
			// Cleared so an earlier attempt's reason does not label a rule
			// rejection.
			t.Reason = core.ReasonUnknown
			settled = append(settled, *t)
			continue
		}
		// The recorded resolver is honoured, since after a fallback it is not
		// the best match.
		res := a.resolverForTaskLocked(t)
		if res == nil {
			if off := a.switchedOffMatchLocked(t); off != "" {
				// Recorded, so the task goes back to that backend once it is
				// switched on rather than to a fallback it reached meanwhile.
				t.Resolver = off
				waiting[id] = core.WaitingModule
				rest = append(rest, id)
				continue
			}
			if t.ResolverPin != "" {
				// A pinned task fails visibly instead of waiting; see
				// pinFailureLocked.
				t.Status = core.StatusError
				t.Error, t.Reason = a.pinFailureLocked(t)
				settled = append(settled, *t)
				continue
			}
			if a.hasUnroutableMatchLocked(t.URL) {
				// A backend does claim the link, but its account is benched;
				// hold the task rather than call it unsupported.
				waiting[id] = core.WaitingAccount
				rest = append(rest, id)
				continue
			}
			t.Status = core.StatusError
			t.Error = "no resolver matches"
			t.Reason = core.ReasonUnsupported
			settled = append(settled, *t)
			continue
		}
		t.Resolver = res.Info().ID
		t.Mode = a.modeForLocked(t, t.Resolver)
		// a.ctx, because a.mu is held: a hanging resolver must not keep the
		// lock past shutdown.
		result, err := res.Resolve(a.ctx, resolver.Request{URL: t.URL})
		if err != nil {
			t.Status = core.StatusError
			t.Error = err.Error()
			// Classified from the error value, which is still available here.
			t.Reason = classify(failure{err: err})
			settled = append(settled, *t)
			continue
		}
		be := a.backendFor(t.Resolver)
		// One collision policy for both the skip check and the engine.
		// CollisionFor resolves the category's rule; ParsePolicy alone would
		// turn an unset category rule into rename.
		policy := collide.ParsePolicy(cfg.CollisionFor(t.Category))
		// Skip is the one policy decidable here, since it only refuses to
		// start; rename and overwrite need to name the file, which only the
		// engine can be told. Without a resolved name there is nothing to
		// check.
		if policy == collide.Skip && filename(t) != "" {
			// The sanitised name is what would land on disk.
			target := filepath.Join(dir, collide.SafeName(t.Name))
			if taken, err := collide.Check(target); err == nil && taken {
				// Availability is untouched: this is about the folder, not the
				// link.
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
		space.commit(dir, t)
		conns := connsFor(t, cfg, result.Connections, hostCapFor(res, h))
		route, chosen := a.routeForLocked(t, h)
		if chosen != t.Connection {
			t.Connection = chosen
		}
		if be == a.Engine {
			// Only the engine can be told a name, so only it gets the
			// collision policy.
			go a.Engine.Start(engine.Job{
				TaskID: id, URL: result.DirectURL, Headers: result.Headers,
				Conns: conns, Dir: dir, WorkDir: a.stagedDirFor(t), Route: route,
				Collision: policy, MaxCollisionAttempts: cfg.CollisionMaxAttempts,
				// nil for non-torrent tasks, and read only for torrent URLs.
				// Without it, files unticked in the collector would be
				// downloaded anyway, since an empty selection means everything.
				TorrentSelect: core.SelectedTorrentIndices(t.TorrentFiles),
			})
		} else {
			// Delegated backends reach the internet their own way, so they get
			// no route.
			go be.Download(id, result.DirectURL, result.Headers, conns)
		}
	}
	a.queue = rest
	// Reasons are recomputed for everything queued at the start, so a stale
	// one never survives.
	a.setWaitingLocked(before, core.WaitingNone, waiting)
	if len(settled) > 0 {
		// Off this goroutine, since a.mu is held. A caller publishing the same
		// state again afterwards is harmless.
		a.spawn(func() { a.publishTasks(settled) })
	}
}

// defaultConns is the connection count when neither task, rule nor settings
// say otherwise. The global setting defaults to zero so that this is the only
// copy of the number.
const defaultConns = 4

// connsFor decides how many connections one download opens:
//
//	value = first of (per-task, matching rule, host table, global setting, defaultConns)
//	conns = min(value, every ceiling that applies, rules.MaxChunks)
//
// Rule and hand edit share t.Chunks: the Packagizer writes it at staging and a
// hand edit overwrites it later. Zero always means "use the next one down".
//
// Ceilings only lower the count. A resolver's Connections says what the host
// tolerates, so a user's 1 for a hoster that bans multiple connections stays
// 1. A ceiling of 0 means no opinion. rules.MaxChunks is gopeed's limit.
//
// The host table is a value, not a ceiling, so it can ask for more than the
// global setting; ceilings still cut it.
func connsFor(t *core.Task, cfg settings.Settings, ceilings ...int) int {
	conns := defaultConns
	// hostOf, the same key the dispatcher counts transfers per host by.
	hostChunks := cfg.HostRuleFor(hostOf(t.URL)).Chunks
	switch {
	case t.Chunks > 0:
		conns = t.Chunks
	case hostChunks > 0:
		conns = hostChunks
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

// Pause stops one task and takes it out of the wait queue until it is
// resumed.
func (a *App) Pause(id string) { a.stop(id, false) }

// StopBack stops one task's transfer but puts the task back into the wait
// queue. The hard stop uses it, so releasing the halt is all it takes to start
// everything again; a task paused by hand stays paused.
func (a *App) StopBack(id string) { a.stop(id, true) }

func (a *App) stop(id string, requeue bool) {
	a.mu.Lock()
	// The slot is freed before the nil check: StopAll releases the lock between
	// tasks, and a task removed in that window would otherwise leak its slot.
	wasActive := a.active[id]
	delete(a.active, id)
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	if requeue {
		// Added back explicitly: dispatchLocked removed the task from the
		// queue when it started it.
		if !slices.Contains(a.queue, id) {
			a.queue = append(a.queue, id)
		}
	} else {
		a.dequeueLocked(id)
	}
	// The app records the state it commanded rather than waiting for the
	// backend; the engine's pause, for one, reports nothing. A later backend
	// event can still override it.
	if requeue {
		t.Status = core.StatusQueued
	} else {
		t.Status = core.StatusPaused
	}
	t.Speed = 0
	// The stall mark describes a running transfer; clear it here rather than
	// on the watcher's next tick.
	t.StalledSince = time.Time{}
	c := *t
	if !wasActive {
		a.mu.Unlock()
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
		return
	}
	a.dispatchLocked()
	a.mu.Unlock()
	_ = a.Store.Save(&c)
	a.Hub.Broadcast("task", &c)
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

// HonoursCollisionPolicy reports whether the collision policy reaches the file
// a task on this resolver writes. Delegated backends name files themselves, so
// only skip applies to them. The interface uses this to avoid offering
// controls that would be ignored.
func (a *App) HonoursCollisionPolicy(resolverID string) bool {
	return a.backendFor(resolverID) == a.Engine
}

func (a *App) backendFor(resolverID string) backend {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	if b, ok := a.debrid[resolverID]; ok && b != nil {
		return b
	}
	switch {
	case resolverID == remotefs.ResolverID && a.remotefs != nil:
		// The remotefs backend, even for WebDAV links the engine could fetch,
		// so a task pauses through the same object it started on.
		return a.remotefs
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
	// Configured is whether a reconnect could run at all, not whether it
	// would succeed.
	Configured bool `json:"configured"`
}

// ReconnectState reports whether a reconnect is configured and whether one is
// running.
func (a *App) ReconnectState() ReconnectState {
	return ReconnectState{Busy: a.Reconnector.Busy(), Configured: a.reconnectConfigured()}
}

// reconnectConfigured reports whether reconnect is fully set up. It is checked
// before every automatic attempt.
func (a *App) reconnectConfigured() bool {
	return a.Settings.Get().Reconnect.Validate() == nil
}

// Reconnect runs one reconnect immediately. Every run goes through here, so
// reconnect.done is published in one place.
func (a *App) Reconnect(ctx context.Context) (reconnect.Result, error) {
	res, err := a.Reconnector.Do(ctx)
	a.fireReconnectDone(res, err)
	return res, err
}

// reconnectThenRetry asks the router for a new address and, if it changed,
// brings the pending retry forward. Any error, ErrUnchanged included, leaves
// the ordinary backoff to run.
func (a *App) reconnectThenRetry(id string) {
	if _, err := a.Reconnect(a.ctx); err != nil {
		log.Printf("reconnect after task %s hit a limit: %v", id, err)
		return
	}
	// Read now: the task may have been restarted while the line was down,
	// and that retry is not this reconnect's to move. Zero means nothing is
	// pending.
	a.mu.Lock()
	var due time.Time
	if t := a.tasks[id]; t != nil {
		due = t.NextTry
	}
	a.mu.Unlock()
	if due.IsZero() {
		return
	}
	a.retryAfter(id, 0, due)
}

func (a *App) onUpdate(id string, u core.Update) {
	a.mu.Lock()
	t := a.tasks[id]
	if t == nil {
		a.mu.Unlock()
		return
	}
	// The dispatcher decides what is running. A non-terminal update for a task
	// that is not active is stale: JD's poller keeps reporting "running" for a
	// moment after a pause. Done and error are facts about the file and always
	// apply.
	stale := u.Status != core.StatusDone && u.Status != core.StatusError && !a.active[id]
	if u.Name != "" {
		t.Name = u.Name
	}
	if u.Size > 0 {
		t.Size = u.Size
	}
	if u.Status != "" && !stale {
		t.Status = u.Status
	}
	if u.Loaded > 0 {
		t.Loaded = u.Loaded
	}
	// A stopped task keeps its bytes but not a speed or a live note.
	if stale {
		t.Speed = 0
	} else {
		t.Speed = u.Speed
	}
	if stale {
		t.Note = ""
	} else {
		t.Note = u.Note
	}
	if u.Torrent != nil {
		u.Torrent.ApplyTo(t)
	}
	if u.Err != "" {
		t.Error = u.Err
		// Classified here so every backend's failures get the same labels. A
		// backend that knows the cause better sets u.Reason, which wins: this
		// only sees a truncated sentence.
		if u.Reason != "" {
			t.Reason = u.Reason
		} else {
			t.Reason = classify(failure{text: u.Err})
		}
	}
	// Account health learns of the outcome before the freed slot is
	// redispatched, so queued tasks on the same account see the new verdict.
	// Resolvers without a tracked account are unaffected.
	var accountUnroutable bool
	if svc, acct, ok := a.accountForResolverLocked(t.Resolver); ok {
		switch u.Status {
		case core.StatusError:
			accountUnroutable = a.reportAccountFailure(svc, acct, t.Reason, u.Err)
		case core.StatusDone:
			a.reportAccountSuccess(svc, acct)
		}
	}
	// Terminal states free the slot for the next queued task.
	if u.Status == core.StatusDone || u.Status == core.StatusError {
		// A settled task is not stalled; cleared before this update goes out.
		t.StalledSince = time.Time{}
		delete(a.active, id)
		if u.Status == core.StatusDone {
			// Counted before the pass below reuses the slot, so the volume cap
			// is right at the moment it matters.
			a.volumeCapRecordLocked(t)
		}
		a.dispatchLocked()
	}
	var hitStopMark bool
	if u.Status == core.StatusDone {
		t.Online = core.AvailOnline
		t.Retries = 0
		t.NextTry = time.Time{}
		t.MaxTries = 0
		t.StallRestarts = 0
		// Renamed before anything below builds a path from t.Name.
		a.renameFinishedLocked(t)
		if a.stopMark == id {
			// Also recorded as a manual halt, so the next schedule boundary
			// does not restart the queue.
			a.manualHalt = true
			a.halted = true
			a.stopMark = ""
			hitStopMark = true
		}
		// Verified where it was written, then delivered (see app_deliver.go).
		path := filepath.Join(a.workDirFor(t), t.Name)
		verify := a.Settings.Get().VerifyChecksums
		a.spawn(func() {
			if verify {
				a.verifyTask(id, path)
			}
			a.deliverDownload(id)
		})
	}
	// A backend that says the link is not its business hands the task to the
	// next one in the chain. Only this explicit signal advances the chain, and
	// it only moves down, so it terminates.
	var fallbackTo backend
	if u.Status == core.StatusError && u.Unsupported {
		if next := a.nextResolverLocked(t); next != "" {
			fallbackTo = a.backendFor(t.Resolver)
			log.Printf("task %s: %s could not fetch the link, trying %s", id, t.Resolver, next)
			t.Resolver = next
			t.Mode = a.modeForLocked(t, next)
			t.Status = core.StatusQueued
			t.Error = ""
			t.Reason = core.ReasonUnknown
			t.Loaded = 0
			t.Speed = 0
			delete(a.started, id)
			a.queue = append(a.queue, id)
			a.dispatchLocked()
		} else {
			// Every matching backend has declined the link.
			t.Reason = core.ReasonUnsupported
		}
	} else if u.Status == core.StatusError && accountUnroutable && t.ResolverPin != "" {
		// Pinned, so no move to another backend: the failure stands, named
		// after the pin, and the ordinary retry below applies.
		t.Error, t.Reason = a.pinFailureLocked(t)
	} else if u.Status == core.StatusError && accountUnroutable {
		// The account failed, not the link, so the task is requeued for the
		// next backend like u.Unsupported rather than failed.
		fallbackTo = a.backendFor(t.Resolver)
		next := a.nextResolverLocked(t)
		if next != "" {
			log.Printf("task %s: the account behind %s is unavailable, trying %s", id, t.Resolver, next)
		} else {
			// With the resolver cleared, the next pass searches again, and
			// hasUnroutableMatchLocked holds the task if nothing is usable.
			log.Printf("task %s: the account behind %s is unavailable, holding for it to recover", id, t.Resolver)
		}
		t.Resolver = next
		t.Mode = a.modeForLocked(t, next)
		t.Status = core.StatusQueued
		t.Error = ""
		t.Reason = core.ReasonUnknown
		t.Loaded = 0
		t.Speed = 0
		delete(a.started, id)
		a.queue = append(a.queue, id)
		a.dispatchLocked()
	}

	// The mirror set follows the task's own status, since a task handed to the
	// next backend is queued again, not settled.
	switch {
	case t.Status == core.StatusDone || t.Status == core.StatusError:
		// A settled link does not block being added again.
		a.forgetLinkLocked(t)
	case u.Name != "" || u.Size > 0:
		// Refiled with the name and size the backend reported, so mirrors can
		// be matched on them.
		a.dupes.Add(linkEntry(t))
	}

	// Failures are retried a bounded number of times with growing delays,
	// except where waiting cannot help.
	var retryIn time.Duration
	if u.Status == core.StatusError && fallbackTo == nil {
		cfg := a.Settings.Get()
		// This failure's retry plan for this host (see settings.RetryFor).
		plan := cfg.RetryFor(string(t.Reason), hostOf(t.URL))
		switch {
		case plan.Never:
			// Configured never to retry: GaveUp marks a decision rather than
			// an exhausted counter.
			t.GaveUp = true
			t.NextTry = time.Time{}
		case t.Reason == core.ReasonCaptcha:
			// Time does not answer a captcha, and headless JD may never offer
			// it over the API at all.
			t.GaveUp = true
			t.NextTry = time.Time{}
		case t.Reason == core.ReasonDiskFull:
			// Waiting frees no space. NextTry is cleared so the row does not
			// claim a pending retry.
			t.GaveUp = true
			t.NextTry = time.Time{}
		case retryCannotHelp(t.Reason):
			// Causes a backend names itself; none is a matter of time. After a
			// bot check, further requests only strengthen the block.
			t.GaveUp = true
			t.NextTry = time.Time{}
		case t.Retries < plan.Tries:
			t.Retries++
			// Recorded here, the only place the merged plan is known.
			t.MaxTries = plan.Tries
			retryIn = u.Retry
			if retryIn <= 0 {
				retryIn = retryDelay(t.Retries, plan.Delay, plan.Max)
			}
			t.NextTry = time.Now().Add(retryIn)
		default:
			// Out of attempts: a plain failure that more retries could mend,
			// so GaveUp stays false and the ceiling is shown.
			t.MaxTries = plan.Tries
			t.NextTry = time.Time{}
		}
	}
	// A kept mirror takes over when the source has nothing left to try. It
	// reads retryIn and may cancel the retry just armed (see
	// handOverToMirrorLocked).
	var mirrorCopy *core.Task
	if u.Status == core.StatusError && fallbackTo == nil {
		mirrorCopy, retryIn = a.handOverToMirrorLocked(t, retryIn)
	}
	// A hoster limit tied to this address is what a reconnect can fix. It is
	// signalled by u.Retry or by ReasonLimit, and addressMayHelp can veto it.
	// ReasonLimit also covers used-up account allowances, where a reconnect
	// does not help, but a wasted reconnect is the cheaper mistake. Never while
	// the queue is halted.
	limitHit := u.Retry > 0 || t.Reason == core.ReasonLimit
	reconnectFor := ""
	if retryIn > 0 && limitHit && !a.halted && addressMayHelp(t.Reason) && a.reconnectConfigured() {
		reconnectFor = id
	}
	// A finished download that completes an archive continues as an
	// extraction (see extractionDueLocked).
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
		// Drop the old backend's state so a restart does not resume there.
		fallbackTo.Remove(id, true)
	}
	// An empty status is a torrent's periodic seeding poll. It is broadcast
	// for the live peer counts but not saved, and must not fire task scripts
	// on every poll.
	if u.Status != "" {
		_ = a.Store.Save(&c)
	}
	a.Hub.Broadcast("task", &c)
	if u.Status != "" {
		// NextTry is already set when a retry is pending, so task.failed fires
		// only on the final failure.
		tv := scriptTaskView(c)
		if trig, ok := script.ClassifyTaskUpdate(tv); ok {
			a.publishEvent(script.Firing{Trigger: trig, Task: &tv})
		}
	}
	if extractCopy != nil {
		_ = a.Store.Save(extractCopy)
		a.Hub.Broadcast("task", extractCopy)
	}
	if mirrorCopy != nil {
		_ = a.Store.Save(mirrorCopy)
		a.Hub.Broadcast("task", mirrorCopy)
	}
	if retryIn > 0 {
		// The deadline ties the timer to this failure (see retryAfter).
		a.retryAfter(id, retryIn, c.NextTry)
	}
	if reconnectFor != "" {
		// Do can block for its whole timeout, so it runs off the lock.
		a.spawn(func() { a.reconnectThenRetry(reconnectFor) })
	}
	if hitStopMark {
		log.Printf("stop mark reached at %s; the queue is halted", c.Name)
		a.Hub.Broadcast("queue", a.Queue())
	}
}

// retryDelay doubles from base per attempt up to ceiling, which come from
// settings.RetryFor; zero values fall back to the defaults. attempt is clamped
// so the shift cannot overflow.
func retryDelay(attempt int, base, ceiling time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 32 {
		attempt = 32
	}
	if base <= 0 {
		base = settings.DefaultRetryDelay
	}
	if ceiling <= 0 {
		ceiling = settings.DefaultRetryMax
	}
	d := base * time.Duration(uint64(1)<<uint(attempt-1))
	if d <= 0 || d > ceiling {
		d = ceiling
	}
	return d
}

// retryAfter restarts a failed task after d, but only while the task still
// carries the deadline this timer was armed for. Otherwise an older timer
// could restart a task in the middle of a newer, longer wait.
func (a *App) retryAfter(id string, d time.Duration, due time.Time) {
	time.AfterFunc(d, func() {
		a.mu.Lock()
		t := a.tasks[id]
		mine := t != nil && t.Status == core.StatusError && !t.NextTry.IsZero() && t.NextTry.Equal(due)
		a.mu.Unlock()
		if mine {
			a.RestartTasks([]string{id})
		}
	})
}
