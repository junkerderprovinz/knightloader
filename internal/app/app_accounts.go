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
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// rewireBackends rebuilds the resolver routing table and the download backends
// from the credentials currently stored. It runs at startup and again whenever
// an account changes, so adding, removing or disabling a debrid account takes
// effect immediately instead of on the next restart. Everything is assembled
// into locals first and swapped in at the end, so a running download never
// sees a half-built table.
//
// ONE SLOT PER ACCOUNT, not one per service (resolver.SlotID). Until
// 2026-09-07 this file wired exactly one backend per service id and read only
// that service's DEFAULT account, so a second TorBox or AllDebrid key could be
// added, named and switched on from the accounts page and was then never asked
// for a single link. Every account of a service is registered here at the same
// priority, in routedAccounts' order, which is what makes the fallback chain
// try a person's second key before it moves on to the next service.
func (a *App) rewireBackends() {
	eng := a.Engine

	// Resolve which hoster backends are configured. Each debrid service brings
	// its own supported-host list; their union tells file hosters (→ debrid/JD)
	// from media pages (→ yt-dlp).
	//
	// The FIRST routed TorBox account's key, and only to ask TorBox which hosts
	// it supports: that list is the service's own answer and comes back the same
	// whichever of a person's keys asks for it, so it is fetched once here
	// rather than once per account. Deliberately no longer "the default
	// account's key" - an install whose only TorBox account is a named one would
	// otherwise fetch no host list at all and route nothing to a key that works.
	var torboxAccounts []routedAccount
	for _, acct := range a.routedAccounts("torbox") {
		// TorBox is an API-key service (accounts.KindAPIKey), so an account
		// whose stored credential carries no key can unlock nothing. Dropped
		// here rather than given a slot that would claim links and fail every
		// one of them, which is exactly what the one-shot services' own build
		// funcs do further down.
		if acct.cred.APIKey != "" {
			torboxAccounts = append(torboxAccounts, acct)
		}
	}
	torboxKey := ""
	if len(torboxAccounts) > 0 {
		torboxKey = torboxAccounts[0].cred.APIKey
	}
	jdBase := os.Getenv("KL_JD")

	var hosterSet map[string]bool
	// ytdlpExclude starts as the narrower, type:"hoster"-only view of the
	// same TorBox list (see fetchTorboxHosterOnlyHosts) - hosterSet itself
	// stays the FULL union (file hosters AND the "stream"/media sites TorBox
	// also unlocks) for torbox.Resolver and the debrid union just below,
	// since a real TorBox account genuinely can fetch from those too; only
	// yt-dlp's own resolver needs the narrower set, so it does not lose the
	// exact hosts it exists to serve to the JD catch-all's no-staging-name
	// path (see this function's own doc comment on hosterSet's other uses).
	var ytdlpExclude map[string]bool
	// The same narrower set, kept as its own map because ytdlpExclude is added
	// to below (every debrid service's own hosts join it) while this one must
	// stay exactly "the hosts TorBox itself calls file hosters" - see the
	// torbox.Resolver registration further down for what it decides.
	var torboxFileHosts map[string]bool
	if torboxKey != "" || jdBase != "" {
		hosterSet = a.fetchTorboxHosters(torboxKey)
		ytdlpExclude = a.fetchTorboxHosterOnlyHosts(torboxKey)
		torboxFileHosts = a.fetchTorboxHosterOnlyHosts(torboxKey)
	}

	// One-shot debrid services (AllDebrid, Real-Debrid): a single unlock call
	// yields a direct URL the engine downloads. One setup per (service,
	// ACCOUNT): a service with two configured accounts contributes two, both
	// carrying the same priority number, so they sort next to each other and
	// ahead of the next service - see routedAccounts for the order and
	// resolver.SlotID for the ids they register under.
	type debridSetup struct {
		svc     debrid.Service
		account string
		prio    int
	}
	//
	// THE NUMBERS. Every debrid service sits ABOVE resolver.Direct's 40, and
	// that changed on 2026-09-07 after jdp measured his own instance: 54
	// rapidgator links, 23 in JD's free mode, none through the Debrid-Link
	// account he had added for rapidgator ("Es lädt rapidgator und youtube
	// dateien nach wie vor nicht herunter obwohl ich premium accounts habe die
	// rapidgator abdecken"). Two things stood in the way, and both are about
	// how specifically a resolver claims a link:
	//
	//   - Direct claimed anything whose path ends in something file-shaped,
	//     knowing nothing about the host. A service that lists the host BY NAME
	//     knows more, so it now outranks a guess from the URL's shape.
	//   - JD's per-host boost was a single 41 for both "there is a login for
	//     this host" and "JD merely has a plugin", and the second case covers
	//     practically every hoster - see jd.PriorityFor, which now answers
	//     those two separately.
	//
	// Spread by one rather than by ten: the order among them is a preference,
	// not a statement about capability, and the gaps say nothing.
	//
	// One entry per service, in that priority order. build answers nil for a
	// credential this service cannot actually use (an empty key, a half-filled
	// Linksnappy login), which is how such an account contributes no slot at
	// all rather than a client that would fail on its first call.
	services := []struct {
		id    string
		prio  int
		build func(accounts.Credential) debrid.Service
	}{
		{"alldebrid", 49, func(c accounts.Credential) debrid.Service {
			if c.APIKey == "" {
				return nil
			}
			return debrid.NewAllDebrid(c.APIKey)
		}},
		{"realdebrid", 48, func(c accounts.Credential) debrid.Service {
			if c.APIKey == "" {
				return nil
			}
			return debrid.NewRealDebrid(c.APIKey)
		}},
		// Below the two that were here first, and in the order they were added -
		// the priority number is what settles which service claims a link both of
		// them support, and there is no reason to demote a working AllDebrid the
		// day somebody adds a second key.
		{"debridlink", 47, func(c accounts.Credential) debrid.Service {
			if c.APIKey == "" {
				return nil
			}
			return debrid.NewDebridLink(c.APIKey)
		}},
		{"premiumize", 46, func(c accounts.Credential) debrid.Service {
			if c.APIKey == "" {
				return nil
			}
			return debrid.NewPremiumize(c.APIKey)
		}},
		// Linksnappy is the one service here with no API key: it authenticates with
		// the website's own login, so it is read as a pair rather than a token.
		{"linksnappy", 45, func(c accounts.Credential) debrid.Service {
			if c.Username == "" || c.Password == "" {
				return nil
			}
			return debrid.NewLinksnappy(c.Username, c.Password)
		}},
		{"offcloud", 44, func(c accounts.Credential) debrid.Service {
			if c.APIKey == "" {
				return nil
			}
			return debrid.NewOffcloud(c.APIKey)
		}},
	}
	var configured []debridSetup
	for _, s := range services {
		for _, acct := range a.routedAccounts(s.id) {
			if svc := s.build(acct.cred); svc != nil {
				configured = append(configured, debridSetup{svc: svc, account: acct.account, prio: s.prio})
			}
		}
	}
	newDebrid := map[string]backend{}
	// Every slot this pass wired, so the sweep at the end can tell a slot that
	// is GONE from one that simply belongs to another service - see there.
	wired := map[string]bool{}
	// One host fetch per SERVICE rather than per account. What comes back is
	// the service's own list of hosters it supports, identical for both of a
	// person's two AllDebrid keys, and resolver.HostCache keeps exactly one set
	// per service id - so asking once per account would spend a second live
	// call to write the same answer into the same cache slot twice.
	hostsByService := map[string]map[string]bool{}
	for _, d := range configured {
		serviceID := d.svc.ID()
		hosts, fetched := hostsByService[serviceID]
		if !fetched {
			hosts = debridRoutingHosts(a, d.svc)
			hostsByService[serviceID] = hosts
		}
		slot := resolver.SlotID(serviceID, d.account)
		newDebrid[slot] = debrid.NewBackend(d.svc, eng, a.onUpdate)
		wired[slot] = true
		// Svc rides along so the routing entry can also answer "is this link still
		// there". Without it the resolver knows which links it claims and nothing
		// about them, and every debrid link stays at "not checked" for good.
		a.Registry.Register(debrid.Resolver{ServiceID: serviceID, Account: d.account, Prio: d.prio, Hosts: hosts, Svc: d.svc})
		for h := range hosts {
			if hosterSet == nil {
				hosterSet = map[string]bool{}
			}
			hosterSet[h] = true
			if ytdlpExclude == nil {
				ytdlpExclude = map[string]bool{}
			}
			ytdlpExclude[h] = true
		}
		log.Printf("%s%s debrid backend enabled (%d supported hosts)", d.svc.Label(), accountSuffix(d.account), len(hosts))
	}

	// The user's own servers: FTP, FTPS, SFTP and WebDAV.
	//
	// REGISTERED UNCONDITIONALLY, which no other backend in this function is,
	// and the reason is that this one is not gated on a credential existing.
	// A public FTP archive is fetched anonymously and a public WebDAV share
	// needs no login either, so a resolver that only appeared once somebody
	// had stored an account would leave "ftp://..." claimed by nothing at all
	// and settled as "no resolver matches". What the credentials decide here
	// is narrower and lives inside the resolver: which https:// hosts it
	// claims (remotefs.Resolver.Match) and which login each server gets.
	//
	// The snapshot is rebuilt on every call, like every host list above it, so
	// adding a seedbox takes effect on the next paste rather than at the next
	// restart - and it is a plain map rather than the store itself because
	// Match is called with the app's own lock held and must not go near an
	// encrypted file on disk.
	remoteDialer := remotefs.Dialer{KnownHostsFile: a.knownHostsPath()}
	remoteLogins := a.remotefsLogins()
	a.Registry.Register(remotefs.Resolver{Accounts: remoteLogins, Dialer: remoteDialer})
	remoteBackend := remotefs.NewBackend(remoteLogins, remoteDialer, eng, a.dlDir, a.onUpdate)
	remoteBackend.Dir = a.taskDir
	// The limit in force rather than the one in the settings file, for the
	// same reason yt-dlp's below reads it live: these bytes never pass through
	// the loopback proxy that meters everything else, so this closure is the
	// only thing that makes a nightly speed window true for an FTP transfer.
	remoteBackend.RateLimit = a.Throttle.Limit
	newRemoteFS := backend(remoteBackend)

	// The stored header profiles, and this single line is what arms the whole
	// feature: Registry.All walks only what Register has seen, so without it
	// Match and Resolve are never called and every link goes to Direct exactly
	// as before - the package would be complete, tested, and unreachable.
	//
	// Here rather than beside Direct and HTTPFallback in app.go, because
	// a.Accounts does not exist yet at that point. Rebuilt on every rewire like
	// the host lists above, so a profile saved for a new Nextcloud applies to
	// the next paste rather than after a restart.
	//
	// No Client handed in, deliberately. The obvious move is to pass a.Probe so
	// the outbound policy is shared, and it buys nothing: a.Probe is built with
	// httpx.New(httpx.Options{Timeout: probeTimeout}) and so is this package's
	// own fallback, the same call with the same options. The named connections
	// live on a.picker, which is per-App and reaches neither client, so passing
	// one would only look like it carried policy it does not have.
	a.Registry.Register(hostheaders.Resolver{Profiles: hostheaders.NewStore(a.Accounts)})

	// Optional yt-dlp media backend: when the yt-dlp binary is present, media
	// pages (non-hoster, non-file links) route through it.
	//
	// WHICH yt-dlp is no longer this file's decision. It used to be the two
	// lines `os.Getenv("KL_YTDLP")` and a `"yt-dlp"` fallback, which had no
	// third option; internal/mediatools adds one - a copy KnightLoader fetched
	// and verified itself - and owns the whole precedence question, including
	// the part that cannot be expressed here at all: a recorded copy that no
	// longer starts has to lose to KL_YTDLP rather than leaving a dead path in
	// the resolver table. See ResolveYtdlp's own doc comment for why the
	// fetched copy outranks an explicitly set KL_YTDLP, which is not the
	// obvious answer and was decided rather than assumed.
	var newYtdlp backend
	ytbin, ytsource, ytdetail := mediatools.ResolveYtdlp(a.DataDir)
	if yb := ytdlp.NewBackend(ytbin, a.dlDir, a.onUpdate); yb.Available() {
		// The limit in force rather than the one in the settings file. yt-dlp meters
		// itself because its bytes never pass through our loopback proxy, and the
		// limiter is what the timetable writes: reading the setting directly would
		// leave yt-dlp running at the daytime speed right through a nightly window.
		// The SHARE, not the whole limit (app_budget.go). This used to read the
		// engine's own throttle, which is a different meter: with the engine and
		// yt-dlp both working, each honoured the full limit and the two together
		// went at twice it.
		yb.RateLimit = a.budget.ytdlpLimit
		yb.Dir = a.taskDir
		// Read live rather than captured once: a settings save between two
		// downloads - or between a pause and its resume - must take effect on
		// the next spawn without a restart, the same reason RateLimit and Dir
		// just above are both closures rather than values copied in here.
		// Per-task now (jdp, 2026-08-25's "Variante" rows): the base is
		// still the instance-wide defaults (subtitle language, playlist,
		// output template - nothing this task's own Variant string
		// overrides), but Variant/Quality/AudioFormat come from THIS
		// task's own core.Task.Variant, set once at variant-expansion time
		// (see expandYtdlpVariants) and editable afterwards from the
		// list's own "Variante" column - see variantOptions's own doc
		// comment for the encoding.
		yb.Options = func(taskID string) ytdlp.Options {
			return a.ytdlpOptionsForTask(taskID)
		}
		// The stored cookie jars, and this line is the whole reason the feature
		// is reachable at all: CookieStore is complete and tested on its own,
		// but Backend.Cookies is nil until somebody hands it over, and a nil
		// hook means "no opinion" - every download would behave exactly as
		// before while the settings page cheerfully accepted jars nothing read.
		//
		// A closure over the store rather than the text, for the same reason
		// RateLimit and Options above are closures: a jar saved between two
		// downloads has to reach the next spawn without a restart.
		yb.Cookies = ytdlp.NewCookieStore(a.Accounts).Text
		newYtdlp = yb
		a.Registry.Register(ytdlp.Resolver{ExcludeHosts: ytdlpExclude})
		// The source, not only the path. "yt-dlp backend enabled:
		// /data/tools/yt-dlp" says nothing about why THAT one and not the
		// /usr/bin/yt-dlp the image installed, and that is the exact question
		// somebody reading this line in a bug report is trying to answer.
		log.Printf("yt-dlp backend enabled: %s (%s)", ytbin, ytsource)
		if ytdetail != "" {
			// Only when a higher-precedence copy was passed over, which is
			// never the ordinary case: a fetched copy that no longer starts is
			// something the operator needs told, and the settings page they
			// would read it on may not be open for weeks.
			log.Printf("yt-dlp: %s", ytdetail)
		}
	}

	// The same set, handed to the JD resolver as its own answer to "is this a
	// file hoster or a media site". JD has a plugin for YouTube too, and its
	// per-host boost would otherwise take a media link away from yt-dlp exactly
	// the way TorBox's host list did until 2026-09-06 - see jd.SetFileHosts.
	// Pushed even when yt-dlp is not running: an empty set means "nothing has
	// classified anything", and that is the case this must NOT be confused with.
	if newYtdlp != nil {
		jd.SetFileHosts(ytdlpExclude)
	} else {
		jd.SetFileHosts(nil)
	}

	// Optional TorBox debrid backend: when a key is present, supported hoster
	// links are unlocked into a direct CDN URL the engine then downloads. One
	// backend per configured TorBox account, exactly like the one-shot services
	// above - newTorbox itself stays the DEFAULT account's, because that is the
	// one backendFor's own "torbox" case answers with; every account, default
	// included, is also in newDebrid under its slot id, which is what
	// backendFor reads first.
	var newTorbox backend
	if len(torboxAccounts) > 0 {
		// The FULL list only when yt-dlp is not running. With yt-dlp there,
		// TorBox claims the hosts TorBox itself calls file hosters and leaves
		// the "stream" half - YouTube and its like - to yt-dlp.
		//
		// This is the actual cause of a complaint that looked like two others
		// (jdp, 2026-09-06: "wenn ich ein youtube link im sammler hinzufüge
		// heißt der ordner wieder watch und es wird nur ein link angezeigt,
		// nicht alle dateien"). Measured on his own instance, which has a
		// TorBox key, against the clean one, which does not: the same YouTube
		// link routes to ytdlp on the second and to TORBOX on the first,
		// because TorBox's host list covers streaming sites too and TorBox
		// outranks yt-dlp in the priority order. A TorBox-routed media link
		// gets no variant expansion (that is yt-dlp's, app_ytdlp_variants.go)
		// and no title probe, so it stays one nameless row in a folder called
		// "watch" - forever, not for fifteen seconds.
		//
		// TorBox genuinely CAN fetch those sites, which is why the full set was
		// right before yt-dlp existed and is still right when it is missing.
		// But it fetches one file, while yt-dlp is what turns the same link
		// into the video/audio/thumbnail/subtitle/description rows with a
		// quality to pick - the whole feature jdp asked for on 2026-08-25. For
		// a media page the better tool has to win, not the higher-priority one.
		torboxHosts := torboxRoutingHosts(hosterSet, torboxFileHosts, newYtdlp != nil)
		for _, acct := range torboxAccounts {
			be := torbox.NewBackend(torbox.NewClient(acct.cred.APIKey), eng, a.onUpdate)
			slot := resolver.SlotID("torbox", acct.account)
			newDebrid[slot] = be
			wired[slot] = true
			if acct.account == "" {
				newTorbox = be
			}
			a.Registry.Register(torbox.Resolver{Account: acct.account, Hosts: torboxHosts})
			log.Printf("TorBox%s debrid backend enabled (%d supported hosts)", accountSuffix(acct.account), len(torboxHosts))
		}
	}

	// Optional headless-JD backend: the lowest-priority catch-all for hoster
	// links nothing else claims, via JD's crawler and hoster plugins.
	var newJD backend
	if jdBase != "" {
		jb := jd.NewBackend(jdBase, a.onUpdate)
		// The same closure yt-dlp gets, for the same reason: a backend that writes
		// files has to be told where, and reading it live means a settings change
		// takes effect on the next download rather than at the next restart.
		jb.Dir = a.taskDir
		if err := jb.Reachable(); err != nil {
			log.Printf("KL_JD set but JD unreachable (%v); skipping JD backend", err)
		} else {
			// Before anything is handed to it. A JD pointed at a folder it cannot
			// write answers every package with "Invalid download directory" and
			// downloads nothing at all, silently - see Client.SetDownloadFolder for
			// what that cost. Logged rather than fatal: a JD somebody else runs may
			// refuse to be reconfigured, and a catch-all backend that only works for
			// containers still beats no catch-all backend.
			if err := jb.SetDownloadFolder(a.defaultDir()); err != nil {
				log.Printf("could not set JD's download folder to %s (%v); JD downloads may fail", a.defaultDir(), err)
			}
			newJD = jb
			a.Registry.Register(jd.Resolver{Backend: jb})
			log.Printf("headless JD backend enabled: %s (downloads to %s)", jdBase, a.defaultDir())
		}
	}

	// A credential that is gone - or an account that was switched off - must
	// stop claiming links, or those links would route to a service that can no
	// longer unlock them.
	//
	// Swept over what is actually registered rather than over a written-out
	// list of service ids, because a slot can now disappear for a reason such a
	// list could never name: a SECOND account was deleted while the service's
	// first one is still perfectly fine. isDebridService is what keeps the
	// sweep off jd/ytdlp/direct/http/torrent, which own their ids for reasons
	// that have nothing to do with a stored credential.
	for _, id := range a.Registry.IDs() {
		service, _ := resolver.SplitSlot(id)
		if isDebridService(service) && !wired[id] {
			a.Registry.Unregister(id)
		}
	}
	if newJD == nil {
		a.Registry.Unregister("jd")
	}
	if newYtdlp == nil {
		a.Registry.Unregister("ytdlp")
	}

	a.bmu.Lock()
	a.debrid, a.ytdlp, a.torbox, a.jd = newDebrid, newYtdlp, newTorbox, newJD
	a.remotefs = newRemoteFS
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

// torboxRoutingHosts is which hosts TorBox's resolver claims: every host it
// supports when nothing better is running, and only the ones TorBox itself
// calls file hosters once yt-dlp is there to take the rest. See the call site
// for the measurement behind it.
//
// A pure function so the decision can be tested without a live TorBox list or
// a spawned yt-dlp - see TestTorboxLeavesMediaSitesToYtdlp. fileOnly being
// empty falls back to the full set rather than to nothing: an empty answer
// there means the host list could not be read, not that TorBox supports no
// file hosters, and claiming nothing would silently stop routing through a
// working account.
func torboxRoutingHosts(all, fileOnly map[string]bool, ytdlpRunning bool) map[string]bool {
	if !ytdlpRunning || len(fileOnly) == 0 {
		return all
	}
	return fileOnly
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

// routedAccount is one account rewireBackends may build a backend for: which
// account of the service it is ("" for the default one) and the credential to
// build that backend with.
type routedAccount struct {
	account string
	cred    accounts.Credential
}

// routedAccounts lists every account of a service that may route right now, in
// the order the fallback chain will try them: the default account first, then
// each named one in AccountIDs' own sorted order. An account that is switched
// off (accountEnabled) or has no credential at all is left out entirely - the
// same thing routedCredential has always done for the default account, since
// Enabled gates routing exactly as a missing key does.
//
// THE ORDER IS THE FEATURE. Every account of a service is registered at the
// same priority, so the registry's tie-break - registration order (see
// resolver.Registry.Register) - is the whole of "which of my two AllDebrid
// keys is asked first", and it must not change between two restarts of the
// same container. That rules out iterating the credential map directly, which
// is why AccountIDs sorts.
func (a *App) routedAccounts(service string) []routedAccount {
	svc, ok := accounts.Lookup(service)
	if !ok {
		return nil
	}
	var out []routedAccount
	if cred := a.routedCredential(service); !cred.IsZero() {
		out = append(out, routedAccount{cred: cred})
	}
	for _, id := range a.Accounts.AccountIDs(service) {
		if !a.accountEnabled(service, id) {
			continue
		}
		if cred := a.credentialFor(svc, id); !cred.IsZero() {
			out = append(out, routedAccount{account: id, cred: cred})
		}
	}
	return out
}

// isDebridService reports whether a catalogue id is one of the services whose
// credential is a ROUTING decision (accounts.GroupDebrid) - exactly the set
// that owns slots in the resolver registry.
//
// Read off the catalogue rather than written out again here, so a service
// added there is covered by the sweep in rewireBackends and by
// accountForResolverLocked (app_health.go) without a second list to keep in
// step. The hardcoded list this replaced already had to be edited four times
// as services were added, and a sweep that forgets a service leaves its
// resolver claiming links with a credential that is gone.
func isDebridService(service string) bool {
	svc, ok := accounts.Lookup(service)
	return ok && svc.Group == accounts.GroupDebrid
}

// accountSuffix names a non-default account in a log line and says nothing at
// all for the default one, so the message a single-account install has always
// printed reads exactly as it did before slots existed.
func accountSuffix(account string) string {
	if account == "" {
		return ""
	}
	return "/" + account
}

// debridRoutingHosts is the seam rewireBackends reads a service's routing host
// list through, rather than calling fetchDebridHosts directly, so a test can
// drive the real wiring - which accounts get a slot, in which order, and what
// dispatch then does with them - without spending a live call against a real
// debrid API. The identical reason accountInfoFetcher below and probeCredential
// (app_health.go) exist. Swapped only by a test, and restored before it
// returns.
var debridRoutingHosts = (*App).fetchDebridHosts

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

// torboxHosterDomains folds a Hosters() response into a lowercase, "www."
// -stripped domain set - the one piece of both fetchTorboxHosters and
// fetchTorboxHosterOnlyHosts that has nothing to do with the network or the
// on-disk cache, pulled out so it can be tested directly against a plain
// []torbox.Hoster slice instead of only through a live API call.
//
// hosterOnly narrows the result to type:"hoster" entries - a real
// file-hosting service - and skips "stream" entries (media/social pages
// TorBox also unlocks by scraping them, e.g. YouTube, Twitch, TikTok,
// Instagram; live-confirmed 2026-08-25: 67 of TorBox's 161 public entries
// are type:"stream"). That distinction is what fetchTorboxHosterOnlyHosts
// needs and fetchTorboxHosters does not - see the doc comment there.
func torboxHosterDomains(hs []torbox.Hoster, hosterOnly bool) map[string]bool {
	set := map[string]bool{}
	add := func(d string) {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(d, "www.")))
		if d != "" {
			set[d] = true
		}
	}
	for _, h := range hs {
		if hosterOnly && h.Type != "hoster" {
			continue
		}
		add(h.Domain)
		for _, d := range h.Domains {
			add(d)
		}
	}
	return set
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
		return torboxHosterDomains(hs, false), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.Refresh(ctx); err != nil {
		log.Printf("TorBox hoster list unavailable (%v); keeping the last good list (%d hosts)", err, len(cache.Hosts()))
	}
	return cache.Hosts()
}

// fetchTorboxHosterOnlyHosts is fetchTorboxHosters narrowed to type:"hoster"
// entries only - the domains a real file-hosting service actually serves,
// not the "stream" ones TorBox also lists (YouTube, Twitch, TikTok,
// Instagram, ...; live-confirmed: 67 of TorBox's 161 public hoster entries
// are type:"stream"). This is the set yt-dlp's own resolver should be kept
// off of, not the wider union used elsewhere in this file: a "stream" entry
// names exactly the kind of page yt-dlp exists to serve directly, and
// dumping the unfiltered list into ExcludeHosts (the bug this fixes, jdp
// 2026-08-25: "Die ganzen links im linksammler zeigen noch immer nicht ihre
// namen richtig an") silently routed YouTube - and every other TorBox
// "stream" host - around yt-dlp and onto the JD catch-all instead, which
// (unlike yt-dlp's own async title probe) has no way to learn a link's real
// name before Start is pressed.
//
// A second live fetch rather than reusing fetchTorboxHosters' own cached
// result: resolver.HostCache caches one map[string]bool per service id, and
// splitting that into "both variants from one fetch" would mean rewriting
// the cache's own storage shape for a distinction only this one call site
// needs. The two calls share the same 6-hour refresh cadence and TorBox's
// own /hosters response is small (161 entries), so the extra request is
// cheap - and each keeps its own on-disk keep-last-good copy, so an outage
// affecting one still leaves the other's most recent good answer in place.
func (a *App) fetchTorboxHosterOnlyHosts(key string) map[string]bool {
	cache := a.hostCacheFor("torbox-hoster-only", func(ctx context.Context) (map[string]bool, error) {
		hs, err := torbox.NewClient(key).Hosters(ctx)
		if err != nil {
			return nil, err
		}
		return torboxHosterDomains(hs, true), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.Refresh(ctx); err != nil {
		log.Printf("TorBox hoster-only list unavailable (%v); keeping the last good list (%d hosts)", err, len(cache.Hosts()))
	}
	return cache.Hosts()
}

// remotefsLogins is the host-to-login snapshot the remote-server resolver and
// its backend both read - see the registration in rewireBackends for why it is
// a snapshot and not the store itself.
//
// KEYED BY THE ACCOUNT ID, WHICH IS THE HOSTNAME (see
// accounts.GroupRemoteServer). Nothing enforces that at storage time, because
// accounts.Store seals whatever it is given and validates nothing by design -
// so a credential stored under a name that is not a host simply never matches
// a link, which is a configuration mistake and not a state this function can
// repair. It is lower-cased here rather than trusted, because a hostname is
// case-insensitive and a person typing one into a form is not.
//
// A disabled account is skipped, exactly as routedCredential skips one for the
// debrid services: switching a server off has to stop links routing to it, and
// for this resolver "routing" also means whether an https:// link on that host
// is claimed at all.
func (a *App) remotefsLogins() remotefs.Logins {
	out := remotefs.Logins{}
	svc := "remotefs"
	for _, host := range a.Accounts.AccountIDs(svc) {
		if !a.accountEnabled(svc, host) {
			continue
		}
		cred, err := a.Accounts.GetCredential(svc, host)
		if err != nil || cred.IsZero() {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(host))] = remotefs.Login{
			Username: cred.Username,
			Password: cred.Password,
		}
	}
	return out
}

// knownHostsPath is where SSH host keys this install has accepted are
// remembered. Beside accounts.json and account_meta.json, derived from dlDir
// the same way acctMetaPath is and for the same reason - accounts.Store keeps
// its own directory private.
//
// Deliberately NOT the user's own ~/.ssh/known_hosts, which is what
// remotefs.Dialer falls back to when this is empty: the container build runs
// as a user with no home directory worth writing to, and an app that silently
// edits a person's ssh configuration is doing something they did not ask for.
func (a *App) knownHostsPath() string {
	return filepath.Join(filepath.Dir(a.dlDir), "known_hosts")
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
	// UsedPercent is how much of the allowance is spent, 0-100, for a service
	// that meters in a fraction instead of in bytes - Premiumize's fair-use
	// limit_used and Debrid-Link's usagePercent. Read only after Unlimited and
	// Limit (bytes are the better answer wherever there are any) and only
	// together with PercentKnown: 0 is an ordinary reading, not an absent one.
	UsedPercent float64 `json:"usedPercent,omitempty"`
	// PercentKnown says whether UsedPercent came from the service at all. It is
	// the field the "verbleibendes Volumen bleibt leer" report turned on: a
	// fresh Debrid-Link account had used 0%, which omitempty then dropped from
	// the answer entirely, and the column could not tell an intact allowance
	// from a service that had said nothing.
	PercentKnown bool `json:"percentKnown,omitempty"`
	// ResetsAt is RFC3339, or "" when the service does not say when the
	// traffic figure above resets. Debrid-Link is the one provider that does
	// (nextResetSeconds), which is what the field was originally reserved for.
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
// tierUnknown is the tier of an account nothing has read yet - the one value
// that means "no reading", as opposed to a plan name a provider chose.
const tierUnknown = "unknown"

func (a *App) accountHealth(service, account string) AccountHealth {
	st := a.healthState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	if h, ok := st.rows[metaKey(service, account)]; ok {
		return h
	}
	return AccountHealth{Tier: tierUnknown}
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
	// A reading IS a check. The ticker asks the provider for this account and
	// gets back a tier, an expiry and a traffic figure - which is the same
	// question the manual Refresh asks - but only Refresh used to write OK and
	// Detail, so the page printed "not checked" in the status column beside
	// the very numbers that check had just produced (measured live,
	// 2026-09-05). A row saying two contradictory things about itself is
	// worse than a row saying nothing.
	//
	// Detail is left alone once something has written one: Refresh calls this
	// before its own live check and overwrites it afterwards, so an error from
	// that check still wins.
	if h.Tier != "" && h.Tier != tierUnknown && st.Detail == "" {
		st.OK, st.Detail = true, "credential accepted"
	}
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
	if t.Limit > 0 {
		remaining := t.Limit - t.Used
		if remaining < 0 {
			remaining = 0
		}
		return fmtBinaryBytes(remaining)
	}
	// A service that meters in a fraction rather than in bytes (jdp,
	// 2026-09-06: "kann man bei debrid konten das verbleibende volume nicht
	// anzeigen lassen?" - for two of the four providers there is no byte
	// figure to show, and printing nothing at all was why the column looked
	// broken rather than honest). Stated as what is LEFT, because that is what
	// the column is headed, and rounded down so 99.6% spent reads "0 %" rather
	// than a reassuring "1 %".
	if t.PercentKnown {
		left := 100 - t.UsedPercent
		if left < 0 {
			left = 0
		}
		return fmt.Sprintf("%d %%", int(left))
	}
	return ""
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
// StartAccountHealthNow runs one sweep straight away, off the ticker's own
// schedule.
//
// The loop below does nothing until its first tick, fifteen minutes in, and
// that wait is right for the reason its own comment gives. It is wrong for a
// container somebody just started: until that first tick the accounts page
// says "not checked" with an empty expiry and an empty traffic figure for a
// credential that is perfectly good. Measured on a test instance restarted
// several times in one afternoon (2026-09-05) - the table was blank every
// single time, and the data was there the moment a tick finally landed.
//
// Called from main, never from New, for exactly the reason the loop waits:
// New is what every test in this package builds an App with, and a sweep is a
// real HTTP request to a real debrid API.
func (a *App) StartAccountHealthNow() {
	a.spawn(a.refreshAccountHealth)
}

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
	prev, had := st.rows[metaKey(svc.ID, account)]
	st.rows[metaKey(svc.ID, account)] = health
	st.mu.Unlock()
	// Both readings, so the decision to fire can be about the CROSSING and
	// not about the state - see fireAccountExpiry (app_script.go) for why a
	// sweep that runs every fifteen minutes for the life of the process
	// cannot fire on "this account is expired". Outside st.mu: publishing
	// delivers on this goroutine, and a subscriber must never inherit a lock
	// the read path (accountHealth) takes on every accounts-page render.
	a.fireAccountExpiry(svc.ID, account, prev, had, health)
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
	case "debridlink":
		info, err := debrid.NewDebridLink(cred.APIKey).Account(ctx)
		if err != nil {
			return AccountHealth{}, true, err
		}
		return healthFromDebrid(info), true, nil
	case "premiumize":
		info, err := debrid.NewPremiumize(cred.APIKey).Account(ctx)
		if err != nil {
			return AccountHealth{}, true, err
		}
		return healthFromDebrid(info), true, nil
	case "linksnappy":
		info, err := debrid.NewLinksnappy(cred.Username, cred.Password).Account(ctx)
		if err != nil {
			return AccountHealth{}, true, err
		}
		return healthFromDebrid(info), true, nil
	default:
		// Offcloud lands here on purpose: it documents no account or quota
		// endpoint at all, so there is nothing to read. ok=false means "this
		// account has no health reading to offer", which leaves the row saying
		// nothing rather than saying something invented - and the manual
		// Refresh still confirms the key through checkCredential.
		return AccountHealth{}, false, nil
	}
}

// healthFromDebrid folds any of the four one-shot debrid services' answers into
// the cache's own shape - they all speak debrid.AccountInfo, so one function
// covers every caller above.
func healthFromDebrid(info debrid.AccountInfo) AccountHealth {
	return AccountHealth{
		Tier: info.Tier,
		Traffic: TrafficState{
			Used:         info.Traffic.UsedBytes,
			Limit:        info.Traffic.LimitBytes,
			Unlimited:    info.Traffic.Unlimited,
			UsedPercent:  info.Traffic.UsedPercent,
			PercentKnown: info.Traffic.PercentKnown,
			ResetsAt:     formatExpiry(info.Traffic.ResetsAt),
		},
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
	case "debridlink":
		// Deliberately the AUTHENTICATED host list rather than the account
		// call: /downloader/hosts refuses a bad token the same way every other
		// endpoint does, and it answers the second half of this function's
		// contract (how many hosts this key actually buys) in the same round
		// trip. /account/infos would confirm the key and say nothing about
		// reach.
		hosts, err := debrid.NewDebridLink(cred.APIKey).Hosts(ctx)
		if err != nil {
			return false, 0, err
		}
		return true, len(hosts), nil
	case "premiumize":
		hosts, err := debrid.NewPremiumize(cred.APIKey).Hosts(ctx)
		if err != nil {
			return false, 0, err
		}
		return true, len(hosts), nil
	case "linksnappy":
		// The login FIRST, and that ordering is the whole point here:
		// Linksnappy's host list needs no account at all, so checking only that
		// would report a wrong password as a working credential.
		ls := debrid.NewLinksnappy(cred.Username, cred.Password)
		if err := ls.Authenticate(ctx); err != nil {
			return false, 0, err
		}
		hosts, err := ls.Hosts(ctx)
		if err != nil {
			return false, 0, err
		}
		return true, len(hosts), nil
	case "offcloud":
		hosts, err := debrid.NewOffcloud(cred.APIKey).Hosts(ctx)
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
