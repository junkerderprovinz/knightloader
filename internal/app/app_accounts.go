package app

// Credentials and what they route: which accounts are configured, what they say
// when asked, and the resolver/backend table rebuilt whenever one changes.
//
// An "account" here is one (service, account-id) pair - the service is a
// catalogue entry (internal/accounts/catalogue.go), the account id is "" for
// the default/only account most services have, or a caller-chosen id for a
// second login on the same service. AccountStates lists one row per account
// that is actually configured (stored or env-supplied), never one row per
// catalogue entry - the catalogue itself is what the "new account" picker
// reads to offer a slot nothing has claimed yet (see /api/accounts/catalogue).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// rewireBackends rebuilds the resolver routing table and the download backends
// from the credentials currently stored. It runs at startup and again whenever
// an account changes, so adding, removing or disabling a debrid account takes
// effect immediately instead of on the next restart. Everything is assembled
// into locals first and swapped in at the end, so a running download never
// sees a half-built table.
func (a *App) rewireBackends() {
	eng := a.Engine

	// Resolve which hoster backends are configured. Each debrid service brings
	// its own supported-host list; their union tells file hosters (→ debrid/JD)
	// from media pages (→ yt-dlp).
	torboxKey := a.routedCredential("torbox").APIKey
	jdBase := os.Getenv("KL_JD")

	var hosterSet map[string]bool
	if torboxKey != "" || jdBase != "" {
		hosterSet = a.fetchTorboxHosters(torboxKey)
	}

	// One-shot debrid services (AllDebrid, Real-Debrid): a single unlock call
	// yields a direct URL the engine downloads. Only a service's default
	// account is ever wired into routing - a second, named account on the same
	// service (AccountIDs) is representable and manageable on the page, but
	// this app has exactly one debrid backend slot per service id, so it is
	// not a routing choice yet. That is a limitation for a later wave (see
	// docs/build-plan.md 6C, per-host limits and priority order), not a bug
	// this file introduces silently: only the default account can ever appear
	// here, named ones simply are never asked.
	type debridSetup struct {
		svc  debrid.Service
		prio int
	}
	var configured []debridSetup
	if k := a.routedCredential("alldebrid").APIKey; k != "" {
		configured = append(configured, debridSetup{debrid.NewAllDebrid(k), 34})
	}
	if k := a.routedCredential("realdebrid").APIKey; k != "" {
		configured = append(configured, debridSetup{debrid.NewRealDebrid(k), 33})
	}
	newDebrid := map[string]backend{}
	for _, d := range configured {
		hosts := a.fetchDebridHosts(d.svc)
		newDebrid[d.svc.ID()] = debrid.NewBackend(d.svc, eng, a.onUpdate)
		// Svc rides along so the routing entry can also answer "is this link still
		// there". Without it the resolver knows which links it claims and nothing
		// about them, and every debrid link stays at "not checked" for good.
		a.Registry.Register(debrid.Resolver{ServiceID: d.svc.ID(), Prio: d.prio, Hosts: hosts, Svc: d.svc})
		for h := range hosts {
			if hosterSet == nil {
				hosterSet = map[string]bool{}
			}
			hosterSet[h] = true
		}
		log.Printf("%s debrid backend enabled (%d supported hosts)", d.svc.Label(), len(hosts))
	}

	// Optional yt-dlp media backend: when the yt-dlp binary is present, media
	// pages (non-hoster, non-file links) route through it.
	var newYtdlp backend
	ytbin := os.Getenv("KL_YTDLP")
	if ytbin == "" {
		ytbin = "yt-dlp"
	}
	if yb := ytdlp.NewBackend(ytbin, a.dlDir, a.onUpdate); yb.Available() {
		// The limit in force rather than the one in the settings file. yt-dlp meters
		// itself because its bytes never pass through our loopback proxy, and the
		// limiter is what the timetable writes: reading the setting directly would
		// leave yt-dlp running at the daytime speed right through a nightly window.
		yb.RateLimit = a.Throttle.Limit
		yb.Dir = a.taskDir
		newYtdlp = yb
		a.Registry.Register(ytdlp.Resolver{ExcludeHosts: hosterSet})
		log.Printf("yt-dlp backend enabled: %s", ytbin)
	}

	// Optional TorBox debrid backend: when a key is present, supported hoster
	// links are unlocked into a direct CDN URL the engine then downloads.
	var newTorbox backend
	if torboxKey != "" {
		newTorbox = torbox.NewBackend(torbox.NewClient(torboxKey), eng, a.onUpdate)
		a.Registry.Register(torbox.Resolver{Hosts: hosterSet})
		log.Printf("TorBox debrid backend enabled (%d supported hosts)", len(hosterSet))
	}

	// Optional headless-JD backend: the lowest-priority catch-all for hoster
	// links nothing else claims, via JD's crawler and hoster plugins.
	var newJD backend
	if jdBase != "" {
		jb := jd.NewBackend(jdBase, a.onUpdate)
		if err := jb.Reachable(); err != nil {
			log.Printf("KL_JD set but JD unreachable (%v); skipping JD backend", err)
		} else {
			newJD = jb
			a.Registry.Register(jd.Resolver{})
			log.Printf("headless JD backend enabled: %s", jdBase)
		}
	}

	// A credential that is gone - or an account that was switched off - must
	// stop claiming links, or those links would route to a service that can no
	// longer unlock them.
	for _, id := range []string{"alldebrid", "realdebrid"} {
		if _, ok := newDebrid[id]; !ok {
			a.Registry.Unregister(id)
		}
	}
	if newTorbox == nil {
		a.Registry.Unregister("torbox")
	}
	if newJD == nil {
		a.Registry.Unregister("jd")
	}
	if newYtdlp == nil {
		a.Registry.Unregister("ytdlp")
	}

	a.bmu.Lock()
	a.debrid, a.ytdlp, a.torbox, a.jd = newDebrid, newYtdlp, newTorbox, newJD
	a.bmu.Unlock()

	// Starts the account-health ticker the first time this ever runs (New
	// calls this unconditionally) and is a no-op on every call after - see
	// healthState. Placed here rather than in New itself: rewireBackends is
	// this file's one guaranteed call site from New, so the ticker starts
	// without app.go needing a line for it.
	a.healthState()

	// Stamped on every call, success or failure - see hostRefreshAttempted's
	// own doc comment for why this is what refreshHostListsIfDue gates on
	// rather than the host caches' own FetchedAt.
	hostRefreshMu.Lock()
	hostRefreshAttempted[a] = time.Now()
	hostRefreshMu.Unlock()
}

// credentialFor reads one account's secret: the catalogue's env var for the
// default account when it is set (a container's KL_TORBOX and friends always
// win over whatever is in the encrypted store, so a redeploy with a new env
// value is never shadowed by a stale saved key), the encrypted store
// otherwise. It does not consider whether the account is enabled - that is
// routedCredential's job, kept separate so a disabled account still shows up
// on the page as "configured, off" rather than "not configured".
func (a *App) credentialFor(svc accounts.Service, account string) accounts.Credential {
	if account == "" && svc.Env != "" {
		if v := os.Getenv(svc.Env); v != "" {
			return accounts.Credential{APIKey: v}
		}
	}
	cred, _ := a.Accounts.GetCredential(svc.ID, account)
	return cred
}

// routedCredential is the credential rewireBackends may actually use for a
// service's default account: zero when the account is switched off, exactly
// as zero when nothing is configured at all - Enabled gates routing the same
// way a missing credential always has (see accountEnabled).
func (a *App) routedCredential(service string) accounts.Credential {
	if !a.accountEnabled(service, "") {
		return accounts.Credential{}
	}
	svc, ok := accounts.Lookup(service)
	if !ok {
		return accounts.Credential{}
	}
	return a.credentialFor(svc, "")
}

// fetchDebridHosts returns a service's supported-host set through its
// resolver.HostCache (see hostCacheFor): the freshly fetched set on success,
// or - and this is the fix, not a detail - the LAST GOOD set on a transient
// failure, never nil.
//
// Before HostCache existed, a fetch error here returned nil straight into
// debrid.Resolver{Hosts: hosts}, and HostInSet treats a nil or empty set as
// "matches nothing" - so one timeout at the wrong moment silently stopped
// every one of that service's links from routing until the process
// restarted and asked again: the resolver stayed fully registered, in the
// Prio order, and simply claimed nothing. rewireBackends runs on every
// account change, so this ran on the ordinary "add a second account" path
// too, not only at boot.
func (a *App) fetchDebridHosts(svc debrid.Service) map[string]bool {
	cache := a.hostCacheFor(svc.ID(), svc.Hosts)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.Refresh(ctx); err != nil {
		log.Printf("%s host list unavailable (%v); keeping the last good list (%d hosts)", svc.Label(), err, len(cache.Hosts()))
	}
	return cache.Hosts()
}

// fetchTorboxHosters is fetchDebridHosts for TorBox, which speaks a different
// client shape (Hosters, not Hosts) but gets the identical fix: the union of
// every hoster's Domain/Domains through the same keep-last-good cache, never
// nil on a transient failure.
func (a *App) fetchTorboxHosters(key string) map[string]bool {
	cache := a.hostCacheFor("torbox", func(ctx context.Context) (map[string]bool, error) {
		hs, err := torbox.NewClient(key).Hosters(ctx)
		if err != nil {
			return nil, err
		}
		set := map[string]bool{}
		add := func(d string) {
			d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(d, "www.")))
			if d != "" {
				set[d] = true
			}
		}
		for _, h := range hs {
			add(h.Domain)
			for _, d := range h.Domains {
				add(d)
			}
		}
		return set, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.Refresh(ctx); err != nil {
		log.Printf("TorBox hoster list unavailable (%v); keeping the last good list (%d hosts)", err, len(cache.Hosts()))
	}
	return cache.Hosts()
}

// ---- routing host-list cache -----------------------------------------------
//
// Same shape of problem as acctMeta just below, and the same answer: a small
// plaintext sidecar beside accounts.json, read and written whole. What is
// cached here is never a secret - it is the set of hoster domains a service
// says it publicly supports, the same list GET /hosts on every one of these
// APIs answers with no credential at all - so it earns none of the care
// accounts.Store takes with a real credential, and none of acctMeta's either
// (Enabled/Label are a user's own choices; this is only ever what a service
// last said about itself).

// hostCacheEntry is one service's last known-good routing host set, as
// written to disk.
type hostCacheEntry struct {
	Hosts     []string  `json:"hosts"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// hostCacheMu serializes every read-modify-write of host_cache.json, the same
// reasoning acctMetaMu documents for account_meta.json - a mutex on App
// itself would be app.go's struct to change, which is not this file's this
// wave either.
var hostCacheMu sync.Mutex

// hostCachePath sits beside accounts.json and account_meta.json for the same
// reason acctMetaPath does: derived from dlDir rather than asked of
// accounts.Store, which keeps its own directory private.
func (a *App) hostCachePath() string {
	return filepath.Join(filepath.Dir(a.dlDir), "host_cache.json")
}

func (a *App) loadHostCacheFileLocked() map[string]hostCacheEntry {
	m := map[string]hostCacheEntry{}
	if b, err := os.ReadFile(a.hostCachePath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (a *App) saveHostCacheFileLocked(m map[string]hostCacheEntry) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(a.hostCachePath(), b, 0o600)
}

// loadHostCacheEntry is a resolver.HostCache's Load hook for one service:
// what was persisted the last time a fetch for it actually succeeded, or
// ok=false for a service that has never once succeeded - see
// resolver.HostCache.Load's own doc comment for why that is a different
// answer from an empty set.
func (a *App) loadHostCacheEntry(serviceID string) (map[string]bool, time.Time, bool) {
	hostCacheMu.Lock()
	e, ok := a.loadHostCacheFileLocked()[serviceID]
	hostCacheMu.Unlock()
	if !ok || len(e.Hosts) == 0 {
		return nil, time.Time{}, false
	}
	set := make(map[string]bool, len(e.Hosts))
	for _, h := range e.Hosts {
		set[h] = true
	}
	return set, e.FetchedAt, true
}

// saveHostCacheEntry is a resolver.HostCache's Save hook: called only after a
// live fetch actually succeeds (see resolver.HostCache.Refresh), never after
// a failure - a failed fetch has nothing new to persist, and rewriting the
// same bytes back on every failed retry would be pure wear for no reason.
func (a *App) saveHostCacheEntry(serviceID string, hosts map[string]bool, fetchedAt time.Time) {
	list := make([]string, 0, len(hosts))
	for h := range hosts {
		list = append(list, h)
	}
	sort.Strings(list)
	hostCacheMu.Lock()
	m := a.loadHostCacheFileLocked()
	m[serviceID] = hostCacheEntry{Hosts: list, FetchedAt: fetchedAt}
	a.saveHostCacheFileLocked(m)
	hostCacheMu.Unlock()
}

// hostCacheFor builds a fresh resolver.HostCache wired to this service's slot
// in host_cache.json. Fresh rather than kept on App, on purpose: rewireBackends
// already reconstructs a fresh debrid.Service (NewAllDebrid/NewRealDebrid) on
// every call, so a HostCache that only lived as long as one call would have
// nothing to remember between them - Load re-seeds it from disk instead,
// which is what makes "keep the last good list" survive not just one failed
// Refresh but every rewireBackends call after it, however many credentials
// away, until the next one actually succeeds.
func (a *App) hostCacheFor(serviceID string, fetch func(context.Context) (map[string]bool, error)) *resolver.HostCache {
	return &resolver.HostCache{
		Fetch: fetch,
		Load:  func() (map[string]bool, time.Time, bool) { return a.loadHostCacheEntry(serviceID) },
		Save:  func(hosts map[string]bool, at time.Time) { a.saveHostCacheEntry(serviceID, hosts, at) },
	}
}

// hostRefreshInterval is how often the routing host lists refresh themselves
// unasked, on top of rewireBackends' existing "on demand" trigger (every
// account add/edit/enable/disable already forces a fresh attempt). Long,
// because what changes here is a hoster gaining or losing support at a
// debrid service - something that moves in weeks, not minutes - and hitting
// three external APIs on every tick just to notice that is the kind of
// polite-until-it-isn't behaviour that earns a key a slow-down response.
const hostRefreshInterval = 6 * time.Hour

// hostRefreshAttempted is the last time rewireBackends ran, for ANY reason -
// boot, an account change, or refreshHostListsIfDue itself. Package-level and
// keyed by *App rather than a field on App (app.go's struct is not this
// file's to grow this wave) or reference-counted/cleaned up on Close - the
// same trade app_hosterauth.go's Reconciler registry documents: production
// runs exactly one App for the life of the process, and a test suite that
// constructs many discards each one quickly enough that the accumulated
// entries cost nothing that matters.
//
// This is deliberately NOT keyed off HostCache.FetchedAt, which only moves on
// SUCCESS (see resolver.HostCache) - gating the timer on that would mean a
// service stuck failing (a bad key, a real outage) is retried on upkeep's own
// 1-minute tick forever instead of backing off to hostRefreshInterval like a
// healthy one, which is the one behaviour "on a timer" must not have: hitting
// a paid API once a minute for as long as it stays down.
var (
	hostRefreshMu        sync.Mutex
	hostRefreshAttempted = map[*App]time.Time{}
)

// refreshHostListsIfDue re-runs rewireBackends once hostRefreshInterval has
// passed since the last attempt, so a hoster a debrid account gains support
// for is picked up without the user ever touching the accounts page again.
// Called from sweep (app_boot.go), which already runs on upkeep's own ticker -
// a second goroutine here would duplicate that ticker for no reason, and the
// cost of being asked every minute is one map read except on the tick that is
// actually due.
func (a *App) refreshHostListsIfDue() {
	hostRefreshMu.Lock()
	last, ok := hostRefreshAttempted[a]
	hostRefreshMu.Unlock()
	if ok && time.Since(last) < hostRefreshInterval {
		return
	}
	a.rewireBackends()
}

// ---- non-secret per-account metadata -------------------------------------
//
// Enabled and Label are not secrets: accounts.Store (internal/accounts) seals
// a Credential and nothing else, on purpose - see that package's doc comment.
// Rather than ask it to grow fields it was not designed to hold, this rides in
// its own small, plaintext JSON file beside accounts.json. It is read and
// written whole on every call, which is fine at this size: the file holds one
// tiny record per configured account, and it is touched on a page load or a
// button click, never in a hot path.

// acctMeta is one account's non-secret metadata.
type acctMeta struct {
	// Enabled is read as true when the key is entirely absent from the file -
	// see accountEnabled. That default is load-bearing: the three env-keyed
	// debrid secrets migrate straight into account rows with this change, and
	// a default of false would silently stop routing through every one of
	// them the first time this file runs, the same hazard Task.Enabled has
	// been bitten by before.
	Enabled bool   `json:"enabled"`
	Label   string `json:"label,omitempty"`
}

// acctMetaMu serializes every read-modify-write of account_meta.json. A mutex
// living on App itself would need a change to app.go's struct, which is
// another agent's file this wave; a package-level lock gives the same
// guarantee against concurrent writers of one app's file. Different App
// instances (as in tests) use different paths, so contention between them
// never happens in practice.
var acctMetaMu sync.Mutex

// acctMetaPath sits beside accounts.json without asking the accounts package
// for its directory - accounts.Store keeps that private, so this is derived
// from dlDir instead, which App already computes as filepath.Join(dataDir,
// "downloads").
func (a *App) acctMetaPath() string {
	return filepath.Join(filepath.Dir(a.dlDir), "account_meta.json")
}

func (a *App) loadAcctMetaLocked() map[string]acctMeta {
	m := map[string]acctMeta{}
	if b, err := os.ReadFile(a.acctMetaPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (a *App) saveAcctMetaLocked(m map[string]acctMeta) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(a.acctMetaPath(), b, 0o600)
}

// metaKey mirrors the shape accounts.Store's own accountKey builds (service,
// or service+NUL+account) closely enough that the two files address the same
// pair the same way, without this package reaching into accounts' unexported
// helper. It doubles as AccountState.ID: opaque to the frontend, and stable
// for one (service, account) pair.
func metaKey(service, account string) string {
	if account == "" {
		return service
	}
	return service + "\x00" + account
}

// accountEnabled reports whether one account is switched on, defaulting to
// true when nothing was ever recorded for it - see acctMeta.Enabled.
func (a *App) accountEnabled(service, account string) bool {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	meta, ok := a.loadAcctMetaLocked()[metaKey(service, account)]
	if !ok {
		return true
	}
	return meta.Enabled
}

// accountLabel returns the caller-chosen display label for one account, or ""
// if none was ever set.
func (a *App) accountLabel(service, account string) string {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	return a.loadAcctMetaLocked()[metaKey(service, account)].Label
}

// SetAccountEnabled persists whether one account participates in routing and
// re-wires the backends so the switch takes effect right away - the same
// contract SetAccountCredential has always had for the credential itself. It
// works for a credential the container supplied too: Enabled is a routing
// decision independent of where the secret came from, and stopping an
// env-supplied account from routing without touching the container's
// configuration is a legitimate thing to want.
func (a *App) SetAccountEnabled(service, account string, enabled bool) {
	acctMetaMu.Lock()
	m := a.loadAcctMetaLocked()
	key := metaKey(service, account)
	meta := m[key]
	meta.Enabled = enabled
	m[key] = meta
	a.saveAcctMetaLocked(m)
	acctMetaMu.Unlock()
	a.rewireBackends()
}

// SetAccountLabel persists the display label a user gave one account. It
// never touches the credential and never re-wires anything - a rename must
// not be able to interrupt routing.
func (a *App) SetAccountLabel(service, account, label string) {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	m := a.loadAcctMetaLocked()
	key := metaKey(service, account)
	meta := m[key]
	meta.Label = label
	m[key] = meta
	a.saveAcctMetaLocked(m)
}

// deleteAccountMeta removes a row's metadata once its credential is cleared,
// so a stale "disabled" or an old label does not linger and resurface if the
// same service/account id is ever configured again later.
func (a *App) deleteAccountMeta(service, account string) {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	m := a.loadAcctMetaLocked()
	delete(m, metaKey(service, account))
	a.saveAcctMetaLocked(m)
}

// ---- account health: tier, traffic, expiry --------------------------------
//
// The account-health ticker's cached answer for one account - never fetched
// on a page load. TestAccount already makes a live call with a 15s timeout
// per service (see checkCredential); if AccountStates or the shell-bar strip
// did that too, three third-party calls would fire on every page load and a
// slow debrid host would stall every route, not only the Accounts page. So
// there is exactly one path to a live tier/traffic/expiry read -
// refreshOneAccountHealth below - and everything else in this file only ever
// reads what it last found.

// TrafficState is one account's traffic allowance as last read from its
// service - see debrid.TrafficInfo and torbox.TrafficInfo, which this is
// folded from.
//
// Unlimited is checked FIRST, always, by every reader of this type. Used and
// Limit are the zero value and mean nothing while it is true: a progress bar
// fed a zero maximum renders 0% used, which reads as "out of traffic" - the
// exact opposite of what an unlimited account means to show. A reader that
// divides Used by Limit before checking Unlimited is the bug this field
// exists to make unreachable.
type TrafficState struct {
	Used      int64 `json:"used"`
	Limit     int64 `json:"limit"`
	Unlimited bool  `json:"unlimited"`
	// ResetsAt is RFC3339, or "" when the service does not say when the
	// traffic figure above resets - true of every provider wired in today;
	// the field exists for one that does.
	ResetsAt string `json:"resetsAt,omitempty"`
}

// AccountHealth is the account-health ticker's cached reading for one
// account. The zero value - Tier "" - is never handed to a caller directly;
// accountHealth below turns a missing cache entry into Tier "unknown"
// instead, so "nothing has confirmed this account yet" can never be read as
// "confirmed free", which is the complaint a wrong default here would earn.
type AccountHealth struct {
	Tier    string       `json:"tier"`
	Traffic TrafficState `json:"traffic"`
	// Expiry is RFC3339, or "" when the account has nothing to expire (a free
	// tier) or has not been read yet - the same "empty means unknown" rule
	// TrafficLeft below already used before this file could back it with real
	// data.
	Expiry    string    `json:"expiry,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// accountHealthMu guards accountHealthByApp. Package-level and keyed by the
// owning *App - not a field on App itself - for the same reason acctMetaMu
// above is package-level: a field there is app.go's struct to change, and
// this wave that file is not this one's to grow. Keying by the App pointer
// rather than a path (the way acctMetaPath keys the metadata file) is what
// keeps two App instances - as in every table-driven test in this package -
// from ever reading each other's cache: no two live Apps ever share a
// pointer, and accountHealthLoop deletes its own entry on the way out, so a
// closed App's reading does not linger for the next one a test happens to
// allocate at the same address.
var (
	accountHealthMu    sync.Mutex
	accountHealthByApp = map[*App]*accountHealthState{}
)

// accountHealthState is one App's account-health cache: the rows the ticker
// has read so far, and the guard that starts the ticker exactly once no
// matter how many times rewireBackends runs (every credential save, every
// enable toggle calls it again).
type accountHealthState struct {
	startOnce sync.Once

	mu   sync.RWMutex
	rows map[string]AccountHealth // keyed by metaKey(service, account)
}

// healthState returns this App's account-health cache, creating it and
// starting its background ticker on the first call - see rewireBackends,
// this file's one call site.
func (a *App) healthState() *accountHealthState {
	accountHealthMu.Lock()
	st, ok := accountHealthByApp[a]
	if !ok {
		st = &accountHealthState{rows: map[string]AccountHealth{}}
		accountHealthByApp[a] = st
	}
	accountHealthMu.Unlock()
	st.startOnce.Do(func() { a.spawn(a.accountHealthLoop) })
	return st
}

// accountHealth is the cache read: exactly what fillHealth, and through it
// every AccountState this file ever returns, is built from. A missing entry -
// nothing has completed a read for this account yet - answers Tier "unknown"
// rather than a zero-value AccountHealth, so a caller can tell "not checked"
// from "checked, free" without inspecting FetchedAt itself.
func (a *App) accountHealth(service, account string) AccountHealth {
	st := a.healthState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	if h, ok := st.rows[metaKey(service, account)]; ok {
		return h
	}
	return AccountHealth{Tier: "unknown"}
}

// fillHealth stamps a row with the cached reading: Tier, Traffic, and the two
// presentation fields the accounts page already renders (Expiry,
// TrafficLeft). Called by both accountRow and TestAccount, so every
// AccountState this package ever returns carries the same answer for the
// same account - TestAccount's own live hosts-check does not touch this, by
// design (see that function's doc comment).
func (a *App) fillHealth(st *AccountState) {
	h := a.accountHealth(st.Service, st.Account)
	st.Tier = h.Tier
	st.Traffic = h.Traffic
	st.Expiry = h.Expiry
	st.TrafficLeft = fmtTrafficLeft(h.Traffic)
}

// fmtTrafficLeft is TrafficLeft's value: a plain-text column the accounts
// page prints verbatim with no translation applied (see Accounts.tsx's
// AccountsTable), so nothing here may be a sentence - only digits, a unit and
// the "∞" symbol this app already uses unlocalized for "no limit"
// (QueueBar.tsx's speed-limit placeholder). "" keeps its established meaning,
// "not fetched yet" - Accounts.tsx already renders that as a dash.
func fmtTrafficLeft(t TrafficState) string {
	if t.Unlimited {
		return "∞"
	}
	if t.Limit <= 0 {
		return ""
	}
	remaining := t.Limit - t.Used
	if remaining < 0 {
		remaining = 0
	}
	return fmtBinaryBytes(remaining)
}

// fmtBinaryBytes mirrors web/src/lib/format.ts's fmtBytes unit table exactly
// (binary units, one decimal below 10) so a byte figure reads the same
// whether it was formatted here or in the browser.
func fmtBinaryBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := [...]string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v < 10 && i > 0 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}

// accountHealthInterval is how often the ticker re-reads every configured,
// enabled account's tier, traffic and expiry. Tier and expiry move on the
// order of days, traffic on the order of hours - fifteen minutes keeps the
// strip honestly current without leaning on a paid API any harder than that.
//
// A var, not a const, only so a future test could shorten it; nothing in
// this package's own tests does - see accountHealthLoop's doc comment for why
// none of them may.
var accountHealthInterval = 15 * time.Minute

// accountHealthTimeout bounds one service's Account call - the same 15s
// TestAccount already spends on checkCredential, long enough for a slow
// debrid host, short enough that one unreachable service cannot stall the
// whole sweep for the others behind it.
const accountHealthTimeout = 15 * time.Second

// accountHealthLoop is the ticker. It mirrors upkeep's own shape in
// app_boot.go on purpose - ctx-aware, no work until the first tick - and that
// last part is load-bearing here in a way it is merely tidy there: every
// table-driven test in this package builds an App with New, which calls
// rewireBackends, which starts this goroutine unconditionally. An immediate
// first sweep would mean every one of those tests - most of which configure
// no credential, and a few of which configure a fake one specifically to stay
// clear of the real debrid APIs on purpose (see this file's package doc
// comment) - could fire a real HTTP request in the background before the test
// finishes. Waiting for the first tick instead means the interval above would
// have to be shorter than a test's own lifetime for that to happen, which it
// never is by five orders of magnitude.
func (a *App) accountHealthLoop() {
	defer func() {
		accountHealthMu.Lock()
		delete(accountHealthByApp, a)
		accountHealthMu.Unlock()
	}()
	tick := time.NewTicker(accountHealthInterval)
	defer tick.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			a.refreshAccountHealth()
		}
	}
}

// refreshAccountHealth is one sweep: every catalogue service's default
// account plus every named one, each asked in turn. Sequential rather than
// fanned out - three services today is not enough concurrency to be worth
// the complexity, and asking all of them at once buys no real wall-clock
// saving here, since nothing waits on this: it runs on its own goroutine, off
// every request path.
func (a *App) refreshAccountHealth() {
	st := a.healthState()
	for _, svc := range accounts.Catalogue {
		a.refreshOneAccountHealth(st, svc, "")
		for _, id := range a.Accounts.AccountIDs(svc.ID) {
			a.refreshOneAccountHealth(st, svc, id)
		}
	}
}

// refreshOneAccountHealth reads one account's tier, traffic and expiry and
// updates the cache - the only function in this file that makes an outbound
// call for this purpose, and the only one that ever writes accountHealthState.rows.
//
// A disabled or unconfigured account is skipped rather than read: there is no
// reason to spend a call on an account that is not backing any download right
// now, and the last reading a since-disabled account had is not deleted -
// switching it back on should not have to wait a full interval before the
// strip has something to show again.
func (a *App) refreshOneAccountHealth(st *accountHealthState, svc accounts.Service, account string) {
	if !a.accountEnabled(svc.ID, account) {
		return
	}
	cred := a.credentialFor(svc, account)
	if cred.IsZero() {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, accountHealthTimeout)
	defer cancel()
	health, ok, err := accountInfoFetcher(ctx, svc.ID, cred)
	if !ok {
		return // this service has no health reading to offer - not an error
	}
	if err != nil {
		// The last good reading stays rather than being blanked - the same
		// rule the host-list refresh follows (docs/build-plan.md 6C): a
		// transient error must not erase a number that was correct fifteen
		// minutes ago, and dropping to "unknown" would read as the account
		// itself having gone bad when only this one request failed.
		log.Printf("%s account health unavailable (%v); keeping the last reading", svc.Label, err)
		return
	}
	st.mu.Lock()
	st.rows[metaKey(svc.ID, account)] = health
	st.mu.Unlock()
}

// accountInfoFetcher is the seam refreshOneAccountHealth calls through rather
// than calling fetchAccountInfoLive directly, so a test can prove the read
// path the strip actually uses (accountRow/fillHealth/accountHealth) never
// reaches it - see TestAccountHealthStripNeverBlocksOnLiveCall. Swapped only
// by that test, and restored before it returns.
var accountInfoFetcher = fetchAccountInfoLive

// fetchAccountInfoLive asks one already-configured account's service for its
// tier, traffic and expiry - the one live call refreshOneAccountHealth makes.
// ok is false for a service this function does not know how to read (there is
// nothing wrong with that account; it simply has no health reading to offer),
// which the caller treats as "leave the cache alone", not as an error to log
// on every sweep.
func fetchAccountInfoLive(ctx context.Context, service string, cred accounts.Credential) (health AccountHealth, ok bool, err error) {
	switch service {
	case "torbox":
		info, err := torbox.NewClient(cred.APIKey).Account(ctx)
		if err != nil {
			return AccountHealth{}, true, err
		}
		return AccountHealth{
			Tier:      info.Tier,
			Traffic:   TrafficState{Used: info.Traffic.UsedBytes, Limit: info.Traffic.LimitBytes, Unlimited: info.Traffic.Unlimited},
			Expiry:    formatExpiry(info.ExpiresAt),
			FetchedAt: time.Now(),
		}, true, nil
	case "alldebrid":
		info, err := debrid.NewAllDebrid(cred.APIKey).Account(ctx)
		if err != nil {
			return AccountHealth{}, true, err
		}
		return healthFromDebrid(info), true, nil
	case "realdebrid":
		info, err := debrid.NewRealDebrid(cred.APIKey).Account(ctx)
		if err != nil {
			return AccountHealth{}, true, err
		}
		return healthFromDebrid(info), true, nil
	default:
		return AccountHealth{}, false, nil
	}
}

// healthFromDebrid folds either AllDebrid's or Real-Debrid's answer into the
// cache's own shape - both speak debrid.AccountInfo, so one function covers
// both callers above.
func healthFromDebrid(info debrid.AccountInfo) AccountHealth {
	return AccountHealth{
		Tier:      info.Tier,
		Traffic:   TrafficState{Used: info.Traffic.UsedBytes, Limit: info.Traffic.LimitBytes, Unlimited: info.Traffic.Unlimited},
		Expiry:    formatExpiry(info.ExpiresAt),
		FetchedAt: time.Now(),
	}
}

// formatExpiry is AccountHealth.Expiry's one source: RFC3339 for a real
// timestamp, "" for the zero time - never a formatted sentence, so the
// browser's own fmtDate (web/src/lib/format.ts, already used throughout this
// app) is what a future reader of the accounts page formats it with, in the
// reader's own locale, not this file's.
func formatExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ---- the accounts page's view of the world --------------------------------

// AccountState is one row the accounts page shows: one configured (stored or
// env-supplied) account, never the secret itself.
type AccountState struct {
	// ID is what the frontend sends back to act on this row - see metaKey.
	ID      string `json:"id"`
	Service string `json:"service"` // catalogue id: accounts.Lookup(Service)
	Account string `json:"account"` // "" for a service's default account
	// Label is the account's display name: a caller-chosen label if one was
	// set (SetAccountLabel), the account id otherwise, or "" for a default
	// account nobody has named. It deliberately does not fall back to the
	// catalogue's service label - that is a separate column on the page,
	// looked up from Service against the catalogue the frontend already has.
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`

	Configured bool `json:"configured"`
	// FromEnv and EnvVar together are the reason a credential is read-only on
	// the page: FromEnv alone does not say why, and a read-only field with no
	// stated reason reads as a bug rather than a deliberate choice.
	FromEnv bool   `json:"fromEnv"`
	EnvVar  string `json:"envVar,omitempty"`

	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Hosts  int    `json:"hosts"` // supported hosters the service reports, once tested
	// HostsFetchedAt is when this service's ROUTING host list (the set
	// debrid.Resolver/torbox.Resolver actually match against, refreshed by
	// fetchDebridHosts/fetchTorboxHosters) was last actually obtained -
	// RFC3339, or "" before the very first successful fetch. It answers a
	// different question from Hosts: Hosts is a count from the last live
	// check this row's own "Refresh" ran; this is when the number ROUTING
	// is using was last confirmed current, which can be older than that if
	// the service has been failing since - see resolver.HostCache.
	HostsFetchedAt string `json:"hostsFetchedAt,omitempty"`

	// Tier, Traffic, Expiry and TrafficLeft are the account-health ticker's
	// cached reading (docs/build-plan.md, agent 6B) - see fillHealth, which
	// every row below is built through. None of the four is ever the result
	// of a call made while answering this request; they are what the ticker
	// last found, which can be a few minutes stale but is never what stalls a
	// page load.
	//
	// Tier defaults to "unknown", NEVER "free" - an account nothing has
	// checked yet is unconfirmed, not confirmed-free, and those two must not
	// render the same or a paying user watches this column call their
	// account "Free".
	Tier string `json:"tier"`
	// Traffic is {used, limit, unlimited, resetsAt} - see TrafficState. Its
	// zero value (Unlimited false, Limit 0) is what "not fetched yet" looks
	// like here too, and a renderer must treat that as no data rather than
	// 0% used.
	Traffic TrafficState `json:"traffic"`
	// Expiry and TrafficLeft: empty here means "not fetched yet", not "none
	// at all". The table renders a dash rather than treating empty as a real
	// answer, and the "Buy Premium / Renew" link stays disabled until Expiry
	// says there is something to renew. Expiry is RFC3339 for the browser's
	// own fmtDate to format; TrafficLeft is pre-formatted for the one place
	// (Accounts.tsx's plain-text column) that prints it as-is with no
	// formatter of its own - see fmtTrafficLeft for why it carries no prose.
	Expiry      string `json:"expiry,omitempty"`
	TrafficLeft string `json:"trafficLeft,omitempty"`
}

// AccountStates lists every configured account - one row per stored or
// env-supplied credential, never one row per catalogue entry. A service with
// nothing configured has no row at all; the "new account" dialogue is what
// offers its catalogue slot instead (GET /api/accounts/catalogue).
func (a *App) AccountStates() []AccountState {
	var out []AccountState
	for _, svc := range accounts.Catalogue {
		if row, ok := a.accountRow(svc, ""); ok {
			out = append(out, row)
		}
		for _, id := range a.Accounts.AccountIDs(svc.ID) {
			if row, ok := a.accountRow(svc, id); ok {
				out = append(out, row)
			}
		}
	}
	return out
}

// accountRow builds the row for (svc, account), or false if nothing is
// actually configured there. Env is checked first for the default account,
// the same precedence credentialFor uses for routing, so the page never shows
// "not configured" for an account rewireBackends is in fact using.
func (a *App) accountRow(svc accounts.Service, account string) (AccountState, bool) {
	st := AccountState{
		ID: metaKey(svc.ID, account), Service: svc.ID, Account: account,
		Label: account, Enabled: a.accountEnabled(svc.ID, account),
	}
	if lbl := a.accountLabel(svc.ID, account); lbl != "" {
		st.Label = lbl
	}
	if account == "" && svc.Env != "" {
		if v := os.Getenv(svc.Env); v != "" {
			st.Configured, st.FromEnv, st.EnvVar = true, true, svc.Env
		}
	}
	if !st.Configured {
		cred, err := a.Accounts.GetCredential(svc.ID, account)
		if err != nil || cred.IsZero() {
			return st, false
		}
		st.Configured = true
	}
	// One tail both branches converge on, so env-supplied and stored rows
	// carry the same health reading through the same call - see fillHealth.
	a.fillHealth(&st)
	st.HostsFetchedAt = a.hostsFetchedAtField(svc.ID)
	return st, true
}

// hostsFetchedAtField is AccountState.HostsFetchedAt's one source: the
// persisted fetchedAt behind serviceID's routing host list (see
// loadHostCacheEntry), formatted the same way formatExpiry already does -
// RFC3339 for the browser's own fmtDate, "" when nothing has ever succeeded.
// A cheap local read, not a network call, so calling it once per row on
// every AccountStates poll costs nothing worth avoiding.
func (a *App) hostsFetchedAtField(serviceID string) string {
	_, at, ok := a.loadHostCacheEntry(serviceID)
	if !ok {
		return ""
	}
	return formatExpiry(at)
}

// SetAccountCredential stores (or, with a zero Credential, clears) one
// account's secret and re-wires the backends so it takes effect right away -
// a saved key that only works after a restart is a key that looks broken.
// Clearing also drops the account's Enabled/Label metadata (deleteAccountMeta):
// a slot that no longer has a credential should not be able to resurface as
// "disabled" or keep an old label if the same id is configured again later.
func (a *App) SetAccountCredential(service, account string, cred accounts.Credential) error {
	if err := a.Accounts.SetCredential(service, account, cred); err != nil {
		return err
	}
	if cred.IsZero() {
		a.deleteAccountMeta(service, account)
	}
	// A freshly typed credential deserves its own trial, not the verdict on
	// whatever secret used to live in this slot - see
	// internal/accounts/health.go's Tracker.Reset. Also drops a stale
	// benched/invalid record when the credential is cleared, the same
	// reasoning deleteAccountMeta already applies to Enabled/Label above.
	a.acctHealthTracker().Reset(service, account)
	a.rewireBackends()
	return nil
}

// checkCredential asks a service whether cred actually works, without storing
// anything. It is the shared logic behind both VerifyCredential (a credential
// not yet saved) and TestAccount (one already stored) - the network call is
// identical either way, only what happens to the answer differs.
func checkCredential(ctx context.Context, service string, cred accounts.Credential) (ok bool, hosts int, err error) {
	switch service {
	case "torbox":
		list, err := torbox.NewClient(cred.APIKey).Hosters(ctx)
		if err != nil {
			return false, 0, err
		}
		set := map[string]bool{}
		for _, h := range list {
			for _, d := range h.Domains {
				set[d] = true
			}
		}
		return true, len(set), nil
	case "alldebrid":
		hosts, err := debrid.NewAllDebrid(cred.APIKey).Hosts(ctx)
		if err != nil {
			return false, 0, err
		}
		return true, len(hosts), nil
	case "realdebrid":
		hosts, err := debrid.NewRealDebrid(cred.APIKey).Hosts(ctx)
		if err != nil {
			return false, 0, err
		}
		return true, len(hosts), nil
	default:
		return false, 0, errors.New("accounts: unknown service " + service)
	}
}

// VerifyCredential checks a credential against its service without storing it
// anywhere - what the "new account" dialogue calls before persisting, so a
// typo in a key is visible before it is saved rather than on the first
// download. The caller decides whether a failure blocks the save ("save
// anyway" exists precisely because an offline service must not be able to).
func (a *App) VerifyCredential(service string, cred accounts.Credential) (ok bool, hosts int, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, hosts, err := checkCredential(ctx, service, cred)
	if err != nil {
		return false, 0, err.Error()
	}
	return true, hosts, "credential accepted"
}

// TestAccount re-checks an already-stored account against its service and
// reports what came back, so a stored key that stopped working is visible
// here instead of on the next download. This is the per-row "Refresh": it
// does not change what routes, does not touch the account-health ticker's own
// cached snapshot (see docs/build-plan.md 6B), and never persists anything -
// it is a read, same as the old TestAccount always was.
func (a *App) TestAccount(service, account string) AccountState {
	svc, known := accounts.Lookup(service)
	st := AccountState{
		ID: metaKey(service, account), Service: service, Account: account,
		Label: account, Enabled: a.accountEnabled(service, account),
	}
	if lbl := a.accountLabel(service, account); lbl != "" {
		st.Label = lbl
	}
	// A pure cache read, on every return path below including the two early
	// ones - see this function's own doc comment: Refresh never touches what
	// the ticker wrote, only reports it alongside the live hosts-check this
	// function itself performs.
	a.fillHealth(&st)
	st.HostsFetchedAt = a.hostsFetchedAtField(service)
	if !known {
		st.Detail = "unknown service"
		return st
	}
	cred := a.credentialFor(svc, account)
	if cred.IsZero() {
		st.Detail = "no credential stored"
		return st
	}
	st.Configured = true
	if account == "" && svc.Env != "" && os.Getenv(svc.Env) != "" {
		st.FromEnv, st.EnvVar = true, svc.Env
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, hosts, err := checkCredential(ctx, service, cred)
	if err != nil {
		st.Detail = err.Error()
		// The manual "Refresh" is just as informative a signal as a real
		// download's failure, so it feeds the same account-health state
		// machine - see internal/accounts/health.go and app_health.go's
		// reportAccountFailure/reportAccountSuccess.
		a.reportAccountFailure(service, account, classify(failure{err: err, text: err.Error()}), err.Error())
		return st
	}
	a.reportAccountSuccess(service, account)
	st.OK, st.Hosts, st.Detail = ok, hosts, "credential accepted"
	return st
}

// ---- the JD sidecar's own status -------------------------------------------
//
// JD is not an account: KL_JD names a URL, not a secret, so it has no
// catalogue entry and never appears in AccountStates. Its own identity - is
// it configured, is it reachable, which revision is it running - is worth
// surfacing beside the accounts it feeds the same way a real account's does,
// which is what this answers.

// JDStatus is what the interface shows for the headless-JD sidecar: whether
// it is configured at all, whether it answered, and its own revision number
// if it did.
type JDStatus struct {
	Configured bool `json:"configured"`
	Reachable  bool `json:"reachable"`
	// Version is JDownloader's own revision number (see jd.Client.Version) -
	// 0 when Reachable is false, since there is nothing to report.
	Version int64  `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// JDStatus reports the sidecar's own status. It is a live call, not a cache -
// unlike the routing host lists, asking JD for its revision costs nothing
// worth polling on a timer, so an unreachable sidecar reports itself as such
// right now rather than serving a stale number from before it went away.
//
// A fresh jd.Client rather than a.jd (the registered backend): the backend
// interface carries Download/Pause/Resume/Remove, not Version, and growing
// it to expose one method that only every other backend would have to
// stub is the wrong trade for a status line nobody is on the byte path for.
func (a *App) JDStatus() JDStatus {
	base := os.Getenv("KL_JD")
	if base == "" {
		return JDStatus{}
	}
	c := jd.NewClient(base)
	if err := c.Ping(); err != nil {
		return JDStatus{Configured: true, Detail: err.Error()}
	}
	v, err := c.Version()
	if err != nil {
		return JDStatus{Configured: true, Reachable: true, Detail: err.Error()}
	}
	return JDStatus{Configured: true, Reachable: true, Version: v}
}
