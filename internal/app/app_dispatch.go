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

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/engine"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
)

// modeForLocked reports whether a task routed to resolverID goes out on an
// account or anonymously. It labels the task, and premium only reads it to
// keep a link off a free download (see freeLocked); the ranking is
// jd.PriorityFor's. A plain file on an ordinary web server gets no label, since
// only hoster links are free or premium. Caller holds a.mu.
func (a *App) modeForLocked(t *core.Task, resolverID string) core.DownloadMode {
	if t == nil {
		return core.ModeUnknown
	}
	host := hostOf(t.URL)
	if host == "" || torrent.IsURI(t.URL) {
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
// whole chain for that host, its host rule applied. There is one row per
// service, not per account slot: the card saves the ids it shows back into
// ResolverOrder, and which account of a service goes first is decided by
// routedAccounts.
func (a *App) ResolverPriority(host string) []resolver.Info {
	host = strings.TrimSpace(host)
	url := ""
	chain := a.Registry.List()
	if host != "" {
		url = "https://" + host + "/"
		chain = a.Registry.All(url)
	}
	cfg := a.Settings.Get()
	order := cfg.ResolverOrder
	type row struct {
		info resolver.Info
		prio int
	}
	var rows []row
	seen := map[string]bool{}
	// No host means no host rule, which leaves hostChain the plain ranking.
	for _, res := range hostChain(chain, url, cfg) {
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
			// Ranked where dynamicPrio sends the login's links: at its own row,
			// else at JD's. A login the saved order does not name yet then sits
			// right below JD, and saving the card as shown keeps its links there.
			host := hostalias.Canonical(l.Host)
			prio := jd.ActiveLoginPrio
			if i := slices.IndexFunc(order, func(entry string) bool { return loginRowFor(entry, host) }); i >= 0 {
				prio = orderBase - i
			} else if i := slices.Index(order, "jd"); i >= 0 {
				prio = orderBase - i
			}
			rows = append(rows, row{resolver.Info{ID: loginRowID(l.Host), Prio: jd.ActiveLoginPrio}, prio})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].prio > rows[j].prio })
	}
	out := make([]resolver.Info, len(rows))
	for i, r := range rows {
		out[i] = r.info
	}
	return out
}

// barredPastOff reports whether a resolver may not take over the link of a
// switched-off backend ranked above it (see resolverForTaskLocked). JD and the
// HTTP fallback claim any http link and yt-dlp any host that is not a file
// hoster, so each would hand a video to JD or save a hoster's or a player's
// page. A service that lists the host, such as a debrid account, may take the
// link over, and so may the direct download: it claims only a path that ends
// in a file extension, and it leaves file hosters and, while yt-dlp is on,
// video sites alone (see hostClaims).
func barredPastOff(id string) bool {
	return id == "jd" || id == "ytdlp" || id == "http"
}

// offCard reports whether the priority card leaves a resolver out: a stored
// header profile, which goes first for its origin, the user's own servers, the
// files an .nzb came back as, and the HTTP fallback. The first three take only
// links that were pointed at them, so there is nothing to rank, and the
// fallback is by definition last. dynamicPrio ignores an order entry naming
// any of them.
func offCard(id string) bool {
	return id == "http" || id == hostheaders.ResolverID || id == remotefs.ResolverID || id == usenet.ResolverID
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
	var changed []taskCopy
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
		changed = append(changed, a.copyLocked(t))
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
// else the service its host rule prefers or a usable debrid service ranked
// above the recorded resolver where rerankLocked allows it, else the recorded
// resolver if its account is usable, else the first usable entry of the chain
// chainFromLocked leaves. A benched account stays registered and is skipped
// here, so a second account of the same service is simply the next entry in
// the chain. A backend premium only keeps the task off is passed over like a
// switched-off one. Caller holds a.mu.
func (a *App) resolverForTaskLocked(t *core.Task) resolver.Resolver {
	// A pin is the whole answer and may be nil (see pinnedResolverLocked);
	// the caller reports that rather than falling back to the chain.
	if t.ResolverPin != "" {
		return a.pinnedResolverLocked(t)
	}
	chain := a.chainFor(t)
	if a.rerankLocked(t) {
		if res := a.preferredLocked(chain, t); res != nil {
			return res
		}
		if res := a.debridAboveLocked(chain, t); res != nil {
			return res
		}
	}
	// A fallback may have recorded JD, yt-dlp or the HTTP fallback while a
	// backend above it was switched off; that backend decides, not the fallback.
	if t.Resolver != "" && !a.torrentOpenLocked(t) && a.routableForLocked(t.Resolver, t.URL) && !a.resolverOff(t.Resolver) &&
		!a.freeRefusedLocked(t, t.Resolver) &&
		!(barredPastOff(t.Resolver) && a.switchedOffAboveLocked(chain, t.Resolver)) {
		// Looked up in the chain, which a backend the host rule excludes is
		// not in.
		for _, res := range chain {
			if res.Info().ID == t.Resolver {
				return res
			}
		}
	}
	passedOff := false
	for _, res := range a.chainFromLocked(t, chain) {
		id := res.Info().ID
		if a.resolverOff(id) || a.freeRefusedLocked(t, id) {
			passedOff = true
			continue
		}
		if passedOff && barredPastOff(id) {
			return nil
		}
		if a.routableForLocked(id, t.URL) {
			return res
		}
	}
	return nil
}

// chainFromLocked leaves out of the ranked chain the backends above the task's
// recorded one when that backend is switched off or the task was handed to it
// down the chain. Those have had the link already, and asking them again
// would loop between a decline and the switch or the refusal. For a task
// handed down, they are the ones ranked above at that moment, so a backend
// wired since, such as a debrid account added for the host, is still asked.
// Caller holds a.mu.
func (a *App) chainFromLocked(t *core.Task, chain []resolver.Resolver) []resolver.Resolver {
	if passed, fell := a.fellBack[t.ID]; fell {
		return slices.DeleteFunc(slices.Clone(chain), func(r resolver.Resolver) bool { return passed[r.Info().ID] })
	}
	if !a.resolverOff(t.Resolver) {
		return chain
	}
	if i := slices.IndexFunc(chain, func(r resolver.Resolver) bool { return r.Info().ID == t.Resolver }); i >= 0 {
		return chain[i:]
	}
	return chain
}

// rerankLocked reports whether a task's recorded backend is only a pick from
// staging or an earlier run, which a usable debrid service ranked above it
// overrules: JD or a debrid slot, with nothing fetched yet, and not where the
// chain led in this process. A yt-dlp row keeps its backend, since it is part
// of what it is, and a torrent is placed by torrentOpenLocked. Caller holds
// a.mu.
func (a *App) rerankLocked(t *core.Task) bool {
	service, _ := resolver.SplitSlot(t.Resolver)
	if t.Resolver != "jd" && !isDebridService(service) {
		return false
	}
	_, fell := a.fellBack[t.ID]
	return t.Loaded == 0 && !fell &&
		t.Variant == "" && t.InfoHash == "" && len(t.TorrentFiles) == 0
}

// torrentOpenLocked reports whether a torrent goes wherever the ranked chain
// puts it rather than to the backend it was collected for: nothing of it has
// come in, no debrid service holds a job for it, and it was not handed down
// the chain. The built-in client and every debrid service that takes torrents
// compete for it on the priority card, and the order may have changed since it
// was collected. Caller holds a.mu.
func (a *App) torrentOpenLocked(t *core.Task) bool {
	_, fell := a.fellBack[t.ID]
	return torrent.IsURI(t.URL) && t.Loaded == 0 && t.ServiceJob == nil && !fell
}

// debridAboveLocked returns the first usable debrid slot chain ranks above the
// task's recorded backend, or nil. Caller holds a.mu.
func (a *App) debridAboveLocked(chain []resolver.Resolver, t *core.Task) resolver.Resolver {
	for _, res := range chain {
		id := res.Info().ID
		if id == t.Resolver {
			return nil
		}
		if service, _ := resolver.SplitSlot(id); isDebridService(service) && a.routableForLocked(id, t.URL) {
			return res
		}
	}
	return nil
}

// pinnedResolverLocked returns the first entry of the ranked chain that the
// pin names and whose account is usable, or nil. A pin naming a service is met
// by any of its accounts. Account health and premium only are not bypassed,
// and nothing falls through to another service. Caller holds a.mu.
func (a *App) pinnedResolverLocked(t *core.Task) resolver.Resolver {
	for _, res := range a.chainFor(t) {
		id := res.Info().ID
		if !pinMatches(t.ResolverPin, id) || a.resolverOff(id) || a.freeRefusedLocked(t, id) {
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

// PinChoice is one backend a selection of tasks can be pinned to.
type PinChoice struct {
	ID string `json:"id"`
	// Label is the service's name from the account catalogue, and empty for a
	// backend the interface names itself, such as JDownloader.
	Label string `json:"label,omitempty"`
}

// PinChoices lists the backends every one of the given tasks can be pinned
// to, one per service, in the order the first task's chain ranks them. What
// the priority card leaves out is left out here too, since it takes only the
// links it was set up for, and so is a backend whose module is switched off.
func (a *App) PinChoices(ids []string) []PinChoice {
	a.mu.Lock()
	urls := make([]string, 0, len(ids))
	for _, id := range ids {
		if t := a.tasks[id]; t != nil {
			urls = append(urls, t.URL)
		}
	}
	a.mu.Unlock()
	order := a.Settings.Get().ResolverOrder
	var out []PinChoice
	for i, u := range urls {
		here := a.pinChoicesFor(u, order)
		if i == 0 {
			out = here
			continue
		}
		out = slices.DeleteFunc(out, func(c PinChoice) bool {
			return !slices.ContainsFunc(here, func(h PinChoice) bool { return h.ID == c.ID })
		})
	}
	return out
}

func (a *App) pinChoicesFor(url string, order []string) []PinChoice {
	var out []PinChoice
	for _, res := range rankedChain(a.Registry.All(url), url, order) {
		service, _ := resolver.SplitSlot(res.Info().ID)
		if offCard(service) || a.resolverOff(service) ||
			slices.ContainsFunc(out, func(c PinChoice) bool { return c.ID == service }) {
			continue
		}
		c := PinChoice{ID: service}
		if svc, ok := accounts.Lookup(service); ok {
			c.Label = svc.Label
		}
		out = append(out, c)
	}
	return out
}

// PinResolver pins each task to one backend, or unpins it when resolverID is
// empty, and dispatches right away so the result shows at once. A running
// transfer is not moved; the pin decides where the next attempt goes.
//
// A paused or requeued task would resume on the backend it started on and
// pass the pin by, so a pin naming another backend takes it off that one
// first: it drops its partial file, JD its package, and the task starts afresh
// on the pinned backend.
func (a *App) PinResolver(ids []string, resolverID string) error {
	id := strings.TrimSpace(resolverID)
	// Validated before any task is touched, so a refusal never leaves half a
	// selection edited.
	if err := a.ResolverPinnable(id); err != nil {
		return err
	}
	type leaving struct {
		id string
		be backend
	}
	a.mu.Lock()
	var touched []string
	var moved []leaving
	for _, taskID := range ids {
		t := a.tasks[taskID]
		if t == nil || t.ResolverPin == id {
			continue
		}
		t.ResolverPin = id
		touched = append(touched, taskID)
		stopped := t.Status == core.StatusPaused || t.Status == core.StatusQueued
		if id != "" && stopped && a.started[taskID] && !a.active[taskID] && !pinMatches(id, t.Resolver) {
			moved = append(moved, leaving{taskID, a.backendFor(t.Resolver)})
			a.moving[taskID] = true
			delete(a.started, taskID)
			t.Resolver = ""
			t.Mode = core.ModeUnknown
			t.Loaded = 0
		}
	}
	if len(moved) > 0 {
		a.mu.Unlock()
		for _, m := range moved {
			m.be.Remove(m.id, true)
		}
		a.mu.Lock()
		for _, m := range moved {
			delete(a.moving, m.id)
		}
	}
	if len(touched) > 0 {
		a.dispatchLocked()
	}
	// Copied after the dispatch, which may have failed one of these tasks.
	changed := make([]taskCopy, 0, len(touched))
	for _, taskID := range touched {
		if t := a.tasks[taskID]; t != nil {
			changed = append(changed, a.copyLocked(t))
		}
	}
	a.mu.Unlock()
	a.publishTasks(changed)
	return nil
}

// leavesItsBackendLocked reports whether a started task has to leave the backend
// it started on: its host rule excludes that backend, which a pin there
// outranks, or premium only refuses it as a free download. Caller holds a.mu.
func (a *App) leavesItsBackendLocked(t *core.Task) bool {
	if a.freeRefusedLocked(t, t.Resolver) {
		return true
	}
	return t.ResolverPin == "" && excluded(a.Settings.Get().HostRuleFor(hostOf(t.URL)), t.Resolver)
}

// leaveBackendLocked takes a stopped task off the backend it started on, as
// PinResolver does: its partial file goes, JD's package too, and the task
// starts afresh on another backend once the old one has let go. Caller holds
// a.mu.
func (a *App) leaveBackendLocked(t *core.Task) {
	id, old := t.ID, a.backendFor(t.Resolver)
	a.moving[id] = true
	delete(a.started, id)
	t.Resolver = ""
	t.Mode = core.ModeUnknown
	t.Loaded = 0
	a.spawn(func() {
		old.Remove(id, true)
		if c := a.handedOn(id); c != nil {
			a.publish(c)
		}
	})
}

// nextResolverLocked returns the resolver to try after the one the task just
// used, following chainFor, or "" when the chain is exhausted. A backend the
// task was handed down from before is not asked again. Caller holds a.mu.
func (a *App) nextResolverLocked(t *core.Task) string {
	// A pinned task has no next resolver. Every fallback path asks here, so
	// this one check keeps a pin from being bypassed.
	if t.ResolverPin != "" {
		return ""
	}
	chain := a.chainFor(t)
	// A recorded backend that has been unregistered (a credential removed, a
	// binary missing) is not found, and the search starts over from the top.
	if i := slices.IndexFunc(chain, func(r resolver.Resolver) bool { return r.Info().ID == t.Resolver }); i >= 0 {
		chain = chain[i+1:]
	}
	passed := a.fellBack[t.ID]
	for _, res := range chain {
		if id := res.Info().ID; !passed[id] {
			return id
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
	if a.halted && len(a.startNow) == 0 {
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
	var settled []taskCopy
	// Why each task does not start, applied at the end (see setWaitingLocked).
	waiting := map[string]core.Waiting{}
	perHost := map[string]int{}
	// Forced tasks are counted apart, so a forced download cannot push an
	// ordinary one out of its slot. This recount is where a forced start stands
	// against the limits for as long as it is active: it holds a place in the
	// forced pool, never one of MaxConcurrent's or its host's. The flag
	// outlives the start, so a forced download paused, retried or restarted
	// comes back past the limits too. Unforced while it runs (SetForced), it
	// counts as ordinary from the next pass on, and nothing new starts until
	// the ordinary pool is below its limit again.
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
		// A stopped queue lets out only what "Start now" was pressed for, and
		// that still through the forced pool below.
		if a.halted && !(t.Forced && a.startNow[id]) {
			waiting[id] = core.WaitingHalted
			rest = append(rest, id)
			continue
		}
		if a.moving[id] {
			rest = append(rest, id)
			continue
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
		if a.started[id] && a.leavesItsBackendLocked(t) {
			if a.premiumHeldLocked(t) {
				// Kept where it is rather than started over, so a login for
				// JD's hoster resumes it.
				waiting[id] = core.WaitingPremium
			} else {
				a.leaveBackendLocked(t)
			}
			rest = append(rest, id)
			continue
		}
		if a.started[id] {
			a.active[id] = true
			a.countStartLocked(t, h, perHost, &forcedActive, &normalActive)
			space.commit(dir, t)
			// A retry date belongs to the failure, not to the run it led to.
			t.NextTry = time.Time{}
			go a.backendFor(t.Resolver).Resume(id)
			continue
		}
		// The filter is asked once more before bytes move, since rules may
		// have changed since staging, and so is the list of banned trackers.
		// A link the user restored from the holding area is past the filter,
		// and past the ban it was held for (see trackerBan).
		v := a.filter(candidateOf(t))
		if filterWaived(t) {
			v = rules.Verdict{}
		}
		if !v.Rejected {
			v = trackerBan(t, cfg.Torrent)
		}
		if v.Rejected {
			t.Status = core.StatusError
			t.Online = core.AvailOffline
			t.Error = rejection(v)
			// Cleared so an earlier attempt's reason does not label a rule
			// rejection.
			t.Reason = core.ReasonUnknown
			settled = append(settled, a.copyLocked(t))
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
			if a.premiumHeldLocked(t) {
				// Held rather than failed, pinned or not: an account for the
				// host, or a debrid service that carries it, lets it start.
				waiting[id] = core.WaitingPremium
				rest = append(rest, id)
				continue
			}
			if t.ResolverPin != "" {
				// A pinned task fails visibly instead of waiting; see
				// pinFailureLocked.
				t.Status = core.StatusError
				t.Error, t.Reason = a.pinFailureLocked(t)
				settled = append(settled, a.copyLocked(t))
				continue
			}
			if a.hasUnroutableMatchLocked(t) {
				// A backend does claim the link, but its account is benched;
				// hold the task rather than call it unsupported.
				waiting[id] = core.WaitingAccount
				rest = append(rest, id)
				continue
			}
			t.Status = core.StatusError
			t.Error = a.unhandledError(t.URL, "no resolver matches")
			t.Reason = core.ReasonUnsupported
			settled = append(settled, a.copyLocked(t))
			continue
		}
		prev := t.Resolver
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
			settled = append(settled, a.copyLocked(t))
			continue
		}
		be := a.backendFor(t.Resolver)
		// One collision policy for both the skip check and the engine.
		// CollisionFor resolves the category's rule; ParsePolicy alone would
		// turn an unset category rule into rename.
		policy := collide.ParsePolicy(cfg.CollisionFor(t.Category))
		// What an earlier attempt of this task left. The library would find it
		// under the task's name and write this attempt beside it as
		// "name (1).ext", once per restart, so it goes before the backend
		// starts.
		own := a.ownFileLocked(t)
		// Skip is the one policy decidable here, since it only refuses to
		// start; rename and overwrite need to name the file, which only the
		// engine can be told. Without a resolved name there is nothing to
		// check.
		if policy == collide.Skip && filename(t) != "" {
			// The sanitised name is what would land on disk. The task's own
			// leftover is not in its way.
			target := filepath.Join(dir, collide.SafeName(t.Name))
			if taken, err := collide.Check(target); err == nil && taken && !own.at(target) {
				// Availability is untouched: this is about the folder, not the
				// link.
				t.Status = core.StatusError
				t.Error = "not downloaded: " + target + " already exists"
				t.Reason = core.ReasonUnknown
				settled = append(settled, a.copyLocked(t))
				continue
			}
		}
		a.active[id] = true
		a.started[id] = true
		a.countStartLocked(t, h, perHost, &forcedActive, &normalActive)
		space.commit(dir, t)
		t.NextTry = time.Time{}
		// The backend the task leaves may still hold it, as JD keeps its
		// download list across restarts, and would fetch the file a second
		// time. The bytes it counted go with it. Nothing of this task runs
		// there in this process, so the new start does not wait for it.
		if prev != "" && prev != t.Resolver {
			if old := a.backendFor(prev); old != be {
				t.Loaded = 0
				t.ServiceJob = nil
				go old.Remove(id, true)
			}
		}
		conns := connsFor(t, cfg, result.Connections, hostCapFor(res, h))
		route, chosen := a.routeForLocked(t, h)
		if chosen != t.Connection {
			t.Connection = chosen
		}
		// The new attempt reports the file it writes itself.
		t.File = ""
		if be == a.Engine {
			job := a.engineJobLocked(t, cfg, result.DirectURL, result.Headers, conns)
			job.Route = route
			// nil for non-torrent tasks, and read only for torrent URLs.
			// Without it, files unticked in the collector would be downloaded
			// anyway, since an empty selection means everything.
			job.TorrentSelect = core.SelectedTorrentIndices(t.TorrentFiles)
			a.torrentJobLocked(&job, t, cfg)
			go func() {
				own.drop(id)
				a.Engine.Start(job)
			}()
		} else {
			// Delegated backends reach the internet their own way, so they get
			// no route. Those that pass their link on to the engine go through
			// engineHandoff, which gives the engine the same job as above.
			go func() {
				own.drop(id)
				be.Download(id, result.DirectURL, result.Headers, conns)
			}()
		}
	}
	a.queue = rest
	// A link turned down here has had its start.
	for _, t := range settled {
		delete(a.startNow, t.ID)
	}
	// Reasons are recomputed for everything queued at the start, so a stale
	// one never survives.
	a.setWaitingLocked(before, core.WaitingNone, waiting)
	if len(settled) > 0 {
		// Off this goroutine, since a.mu is held. A caller publishing the same
		// state again afterwards is harmless.
		a.spawn(func() { a.publishTasks(settled) })
	}
}

// engineJobLocked is t's transfer as the engine takes it: written into t's
// folder, or its working folder, under the collision policy of t's category.
// Caller holds a.mu.
func (a *App) engineJobLocked(t *core.Task, cfg settings.Settings, url string, headers map[string]string, conns int) engine.Job {
	return engine.Job{
		TaskID: t.ID, URL: url, Headers: headers, Conns: conns,
		Dir: a.dirFor(t), WorkDir: a.stagedDirFor(t),
		Collision: collide.ParsePolicy(cfg.CollisionFor(t.Category)), MaxCollisionAttempts: cfg.CollisionMaxAttempts,
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
	c := a.copyLocked(t)
	if !wasActive {
		a.mu.Unlock()
		a.publish(&c)
		return
	}
	a.dispatchLocked()
	a.mu.Unlock()
	a.publish(&c)
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
	c := a.copyLocked(t)
	a.dequeueLocked(id)
	a.queue = append(a.queue, id)
	a.dispatchLocked()
	a.mu.Unlock()
	a.publish(&c)
}

// HonoursCollisionPolicy reports whether the collision policy reaches the file
// a task on this resolver writes: the engine's own downloads and those a
// debrid service or TorBox passes on to it (see engineHandoff). The other
// backends name files themselves, so only skip applies to them. The interface
// uses this to avoid offering controls that would be ignored.
func (a *App) HonoursCollisionPolicy(resolverID string) bool {
	switch a.backendFor(resolverID).(type) {
	case *engine.Engine, *debrid.Backend, *debrid.TorrentBackend, *torbox.Backend, *usenet.Files:
		return true
	}
	return false
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
	case resolverID == usenet.ResolverID:
		return a.usenetStateFor().files
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
	// A fact about the disk, so a stale update still counts.
	a.recordFileLocked(t, u.File)
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
		t.Remote = nil
	} else {
		t.Note = u.Note
		t.Remote = u.Remote
	}
	if u.Torrent != nil {
		u.Torrent.ApplyTo(t)
	}
	// A fact about the service, so a stale update still counts.
	applyServiceJobLocked(t, u.Job)
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
	// Resolvers without a tracked account are unaffected. A site the service
	// has switched off is benched on its own and says nothing about the
	// account.
	siteDown := u.Status == core.StatusError && u.HostDown
	if siteDown {
		a.benchSiteLocked(t.Resolver, t.URL)
	}
	var accountUnroutable bool
	if svc, acct, ok := a.accountForResolverLocked(t.Resolver); ok && !siteDown {
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
		_ = a.renameFinishedLocked(t)
		if a.stopMark == id {
			// Also recorded as a manual halt, so the next schedule boundary
			// does not restart the queue.
			a.manualHalt = true
			a.halted = true
			a.stopMark = ""
			hitStopMark = true
		}
		// Verified where it was written, then delivered (see app_deliver.go).
		path := a.fileOfLocked(t)
		verify := a.Settings.Get().VerifyChecksums
		a.spawn(func() {
			a.delivering.Add(1)
			defer a.delivering.Add(-1)
			if verify {
				a.verifyTask(id, path)
			}
			a.deliverDownload(id)
		})
	}
	// A backend that says the link is not its business hands the task to the
	// next one in the chain. Only this explicit signal advances the chain, and
	// it only moves down, so it terminates.
	//
	// The task waits in the queue until the backend it leaves has let go of
	// it (see handOnLocked).
	var fallbackTo backend
	if u.Status == core.StatusError && u.Unsupported {
		if next := a.nextResolverLocked(t); next != "" {
			fallbackTo = a.backendFor(t.Resolver)
			log.Printf("task %s: %s could not fetch the link, trying %s", id, t.Resolver, next)
			t.Resolver = next
			t.Mode = a.modeForLocked(t, next)
			a.handOnLocked(t)
		} else {
			// Every matching backend has declined the link.
			t.Reason = core.ReasonUnsupported
		}
	} else if u.Status == core.StatusError && accountUnroutable && t.ResolverPin != "" {
		// Pinned, so no move to another backend: the failure stands, named
		// after the pin, and the ordinary retry below applies.
		t.Error, t.Reason = a.pinFailureLocked(t)
	} else if u.Status == core.StatusError && (accountUnroutable || (siteDown && t.ResolverPin == "")) {
		// The account failed, or the service has switched off this site, not
		// the link, so the task is requeued for the next backend like
		// u.Unsupported rather than failed. A pinned task that met a
		// switched-off site keeps the service's own words and the ordinary
		// retry.
		fallbackTo = a.backendFor(t.Resolver)
		why := "the account behind " + t.Resolver + " is unavailable"
		if siteDown {
			why = t.Resolver + " has switched off " + siteOf(t.URL)
		}
		next := a.nextResolverLocked(t)
		if next != "" {
			log.Printf("task %s: %s, trying %s", id, why, next)
		} else {
			// With the resolver cleared, the next pass searches again, and
			// hasUnroutableMatchLocked holds the task if nothing is usable.
			log.Printf("task %s: %s, holding it until that changes", id, why)
		}
		t.Resolver = next
		t.Mode = a.modeForLocked(t, next)
		a.handOnLocked(t)
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
	var mirrorCopy *taskCopy
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
	// "Start now" holds through a fallback and a retry, which put the link back
	// in the queue by themselves, and ends with the download.
	if u.Status == core.StatusDone || (t.Status == core.StatusError && retryIn == 0) {
		delete(a.startNow, id)
	}
	// A finished download that completes an archive continues as an
	// extraction (see extractionDueLocked).
	var extractCopy *taskCopy
	if u.Status == core.StatusDone {
		if target := a.extractNowLocked(t, a.Settings.Get()); target != nil && target != t {
			c := a.copyLocked(target)
			extractCopy = &c
		}
	}
	c := a.copyLocked(t)
	a.mu.Unlock()
	if fallbackTo != nil {
		// The old backend lets go of the task, its partial file included, before
		// the task starts anywhere else. Two debrid services both hand their
		// link to the engine under the task's id, and a Remove after the new
		// start would take the new transfer with it.
		fallbackTo.Remove(id, true)
		moved := a.handedOn(id)
		if moved == nil {
			// Removed from the list while the old backend let go.
			return
		}
		c = *moved
	}
	// An empty status is a torrent's periodic seeding poll. It is broadcast
	// for the live peer counts but not saved, and must not fire task scripts
	// on every poll. A debrid job is saved with or without one.
	if u.Status != "" || u.Job != nil {
		a.publish(&c)
	} else {
		a.show(&c)
	}
	if u.Status != "" {
		// NextTry is already set when a retry is pending, so task.failed fires
		// only on the final failure.
		tv := scriptTaskView(c.Task)
		if trig, ok := script.ClassifyTaskUpdate(tv); ok {
			a.publishEvent(script.Firing{Trigger: trig, Task: &tv})
		}
	}
	if extractCopy != nil {
		a.publish(extractCopy)
	}
	if mirrorCopy != nil {
		a.publish(mirrorCopy)
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

// handOnLocked queues t for the backend onUpdate has just recorded, as
// moving: the dispatcher leaves it alone until handedOn, once the backend it
// leaves has let go. Caller holds a.mu.
func (a *App) handOnLocked(t *core.Task) {
	t.Status = core.StatusQueued
	t.Error = ""
	t.Reason = core.ReasonUnknown
	t.Loaded = 0
	t.Speed = 0
	// The backend it leaves lets go of its job on the service too.
	t.ServiceJob = nil
	delete(a.started, t.ID)
	passed := a.fellBack[t.ID]
	if passed == nil {
		passed = map[string]bool{}
	}
	// An empty Resolver, an account that failed with nothing below it, is not
	// in the chain, and the whole chain stays open to the search.
	chain := a.chainFor(t)
	if i := slices.IndexFunc(chain, func(r resolver.Resolver) bool { return r.Info().ID == t.Resolver }); i >= 0 {
		for _, res := range chain[:i] {
			passed[res.Info().ID] = true
		}
	}
	a.fellBack[t.ID] = passed
	a.moving[t.ID] = true
	a.queue = append(a.queue, t.ID)
}

// handedOn lets the dispatcher start a task handOnLocked held back and returns
// the task as the dispatch left it, or nil when it was removed meanwhile.
func (a *App) handedOn(id string) *taskCopy {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.moving, id)
	a.dispatchLocked()
	t := a.tasks[id]
	if t == nil {
		return nil
	}
	c := a.copyLocked(t)
	return &c
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
