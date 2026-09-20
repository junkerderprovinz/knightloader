package app

// Credentials and what they route: which accounts are configured, what they
// report when asked, and the resolver and backend table rebuilt whenever one
// changes.
//
// An account is a (service, account id) pair: the service is a catalogue
// entry (internal/accounts/catalogue.go), and the account id is "" for a
// service's default account or a chosen id for a further login. AccountStates
// lists configured accounts only; the catalogue is what offers new ones.

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

// rewireBackends rebuilds the resolver routing table and download backends
// from the stored credentials. It runs at startup and on every account change,
// so changes apply without a restart. Everything is built into locals and
// swapped in at the end, so a running download never sees a half-built table.
//
// There is one slot per account (resolver.SlotID), all accounts of a service
// at the same priority in routedAccounts' order, so the fallback chain tries a
// second key of a service before moving on to the next service.
func (a *App) rewireBackends() {
	eng := a.Engine

	// The first routed TorBox key is used to ask which hosts TorBox supports.
	// The list is the same for every key, so it is fetched once.
	var torboxAccounts []routedAccount
	for _, acct := range a.routedAccounts("torbox") {
		// TorBox needs an API key; an account without one gets no slot.
		if acct.cred.APIKey != "" {
			torboxAccounts = append(torboxAccounts, acct)
		}
	}
	torboxKey := ""
	if len(torboxAccounts) > 0 {
		torboxKey = torboxAccounts[0].cred.APIKey
	}
	jdBase := os.Getenv("KL_JD")

	// hosterSet is every host a hoster backend claims, TorBox's streaming
	// sites included, since TorBox can fetch those too.
	var hosterSet map[string]bool
	// ytdlpExclude keeps yt-dlp off file hosters only. It starts from TorBox's
	// type:"hoster" entries, so yt-dlp keeps the streaming sites, and every
	// debrid service's hosts are added below.
	var ytdlpExclude map[string]bool
	// torboxFileHosts stays exactly TorBox's own file hosters; it decides what
	// the TorBox resolver claims while yt-dlp is running.
	var torboxFileHosts map[string]bool
	if torboxKey != "" || jdBase != "" {
		hosterSet = a.fetchTorboxHosters(torboxKey)
		ytdlpExclude = a.fetchTorboxHosterOnlyHosts(torboxKey)
		torboxFileHosts = a.fetchTorboxHosterOnlyHosts(torboxKey)
	}

	// One-shot debrid services: one unlock call yields a direct URL the engine
	// downloads. There is one setup per account, and accounts of one service
	// share its priority, so they sort together ahead of the next service.
	type debridSetup struct {
		svc     debrid.Service
		account string
		prio    int
	}
	// Every debrid service ranks above resolver.Direct (40): a service that
	// lists a host by name knows more than Direct's guess from the URL's shape.
	// The order among them is a preference, hence gaps of one.
	//
	// build returns nil for a credential the service cannot use, such as an
	// empty key, so that account gets no slot.
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
		// Linksnappy logs in with the website's username and password.
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
	// Every slot wired in this pass, for the sweep at the end.
	wired := map[string]bool{}
	// Host lists are per service, not per account, and HostCache keeps one
	// set per service id.
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
		// Svc lets the resolver also check whether a link is still available.
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

	// The user's own servers: FTP, FTPS, SFTP and WebDAV. Registered
	// unconditionally, because public FTP and WebDAV need no login; the stored
	// credentials only decide which https:// hosts it claims and which login
	// each server gets. The logins are a plain map because Match runs with
	// the app's lock held and must not read the encrypted store.
	remoteDialer := remotefs.Dialer{KnownHostsFile: a.knownHostsPath()}
	remoteLogins := a.remotefsLogins()
	a.Registry.Register(remotefs.Resolver{Accounts: remoteLogins, Dialer: remoteDialer})
	remoteBackend := remotefs.NewBackend(remoteLogins, remoteDialer, eng, a.dlDir, a.onUpdate)
	remoteBackend.Dir = a.taskDir
	// These bytes bypass the metering proxy, so the backend reads the limit in
	// force itself.
	remoteBackend.RateLimit = a.Throttle.Limit
	newRemoteFS := backend(remoteBackend)

	// Stored header profiles. Registered here rather than in app.go because
	// a.Accounts does not exist there yet. It builds its own client, which has
	// the same options as a.Probe.
	a.Registry.Register(hostheaders.Resolver{Profiles: hostheaders.NewStore(a.Accounts)})

	// Optional yt-dlp media backend for media pages. mediatools decides which
	// binary to use (see ResolveYtdlp).
	var newYtdlp backend
	ytbin, ytsource, ytdetail := mediatools.ResolveYtdlp(a.DataDir)
	if yb := ytdlp.NewBackend(ytbin, a.dlDir, a.onUpdate); yb.Available() {
		// yt-dlp meters itself, so it gets its share of the budget
		// (app_budget.go), read live so schedule windows apply.
		yb.RateLimit = a.budget.ytdlpLimit
		yb.Dir = a.taskDir
		// Read on every spawn so settings changes apply without a restart. The
		// instance defaults are combined with the task's own Variant (see
		// expandYtdlpVariants and variantOptions).
		yb.Options = func(taskID string) ytdlp.Options {
			return a.ytdlpOptionsForTask(taskID)
		}
		// Stored cookie jars, read on every spawn. Without this hook the
		// backend would ignore saved jars.
		yb.Cookies = ytdlp.NewCookieStore(a.Accounts).Text
		newYtdlp = yb
		a.Registry.Register(ytdlp.Resolver{ExcludeHosts: ytdlpExclude})
		// The source explains why this binary was chosen over the others.
		log.Printf("yt-dlp backend enabled: %s (%s)", ytbin, ytsource)
		if ytdetail != "" {
			// A higher-precedence copy was passed over, usually because it no
			// longer starts.
			log.Printf("yt-dlp: %s", ytdetail)
		}
	}

	// The same file-hoster set tells JD's resolver not to take media links
	// from yt-dlp (see jd.SetFileHosts). Without yt-dlp nil is pushed, which
	// means nothing was classified rather than an empty classification.
	if newYtdlp != nil {
		jd.SetFileHosts(ytdlpExclude)
	} else {
		jd.SetFileHosts(nil)
	}

	// Optional TorBox backend, one per account like the one-shot services.
	// newTorbox is the default account's, which backendFor's "torbox" case
	// returns; every account is also in newDebrid under its slot id.
	var newTorbox backend
	if len(torboxAccounts) > 0 {
		// With yt-dlp running, TorBox claims only its file hosters and leaves
		// streaming sites to yt-dlp. TorBox outranks yt-dlp and would otherwise
		// take YouTube links, which then get no variant rows and no title.
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
	// links nothing else claims.
	var newJD backend
	if jdBase != "" {
		jb := jd.NewBackend(jdBase, a.onUpdate)
		jb.Dir = a.taskDir
		if err := jb.Reachable(); err != nil {
			log.Printf("KL_JD set but JD unreachable (%v); skipping JD backend", err)
		} else {
			// JD with an unwritable download folder silently downloads nothing.
			// A failure is only logged, since a JD run by someone else may
			// refuse the change and is still useful for containers.
			if err := jb.SetDownloadFolder(a.defaultDir()); err != nil {
				log.Printf("could not set JD's download folder to %s (%v); JD downloads may fail", a.defaultDir(), err)
			}
			newJD = jb
			a.Registry.Register(jd.Resolver{Backend: jb})
			log.Printf("headless JD backend enabled: %s (downloads to %s)", jdBase, a.defaultDir())
		}
	}

	// Slots whose credential is gone or switched off stop claiming links. The
	// sweep walks what is registered, since a single deleted second account is
	// enough to remove a slot; isDebridService keeps it off the built-in ids.
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

	// Starts the account-health ticker on the first call (New always calls
	// rewireBackends); later calls do nothing.
	a.healthState()

	// Stamped on success and failure alike (see hostRefreshAttempted).
	hostRefreshMu.Lock()
	hostRefreshAttempted[a] = time.Now()
	hostRefreshMu.Unlock()
}

// torboxRoutingHosts returns the hosts the TorBox resolver claims: all of them
// without yt-dlp, only TorBox's file hosters with it. An empty fileOnly means
// the list could not be read, so it falls back to all rather than claiming
// nothing.
func torboxRoutingHosts(all, fileOnly map[string]bool, ytdlpRunning bool) map[string]bool {
	if !ytdlpRunning || len(fileOnly) == 0 {
		return all
	}
	return fileOnly
}

// credentialFor reads one account's secret. For the default account the
// catalogue's environment variable wins over the encrypted store, so a
// redeploy with a new value is never shadowed by a stale saved key. Whether
// the account is enabled is not considered here, so a disabled account still
// shows as configured.
func (a *App) credentialFor(svc accounts.Service, account string) accounts.Credential {
	if account == "" && svc.Env != "" {
		if v := os.Getenv(svc.Env); v != "" {
			return accounts.Credential{APIKey: v}
		}
	}
	cred, _ := a.Accounts.GetCredential(svc.ID, account)
	return cred
}

// routedCredential returns the default account's credential if it may route,
// or the zero credential when it is switched off or not configured.
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

// routedAccount is one account rewireBackends may build a backend for.
type routedAccount struct {
	account string
	cred    accounts.Credential
}

// routedAccounts lists every account of a service that may currently route,
// default first, then named accounts in AccountIDs' sorted order. Disabled
// accounts and accounts without a credential are left out. The order decides
// which key is tried first, so it must be stable across restarts.
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

// isDebridService reports whether a catalogue id belongs to accounts.GroupDebrid,
// the services that own resolver slots. It reads the catalogue so new services
// need no second list.
func isDebridService(service string) bool {
	svc, ok := accounts.Lookup(service)
	return ok && svc.Group == accounts.GroupDebrid
}

// accountSuffix names a non-default account in a log line and returns "" for
// the default account.
func accountSuffix(account string) string {
	if account == "" {
		return ""
	}
	return "/" + account
}

// debridRoutingHosts is replaced in tests so the wiring can be exercised
// without calling a real debrid API.
var debridRoutingHosts = (*App).fetchDebridHosts

// fetchDebridHosts returns a service's supported hosts through its HostCache:
// fresh on success, the last good set on a failure, never nil. A nil set would
// make the resolver claim nothing until the next restart.
func (a *App) fetchDebridHosts(svc debrid.Service) map[string]bool {
	cache := a.hostCacheFor(svc.ID(), svc.Hosts)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.Refresh(ctx); err != nil {
		log.Printf("%s host list unavailable (%v); keeping the last good list (%d hosts)", svc.Label(), err, len(cache.Hosts()))
	}
	return cache.Hosts()
}

// torboxHosterDomains folds a Hosters() response into a set of lower-case
// domains without "www.". With hosterOnly it keeps only type:"hoster" entries
// and skips the "stream" ones (YouTube, Twitch and the like).
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

// fetchTorboxHosters is fetchDebridHosts for TorBox's client: every hoster's
// domains through the same keep-last-good cache.
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

// fetchTorboxHosterOnlyHosts is fetchTorboxHosters limited to type:"hoster"
// entries. yt-dlp must be kept off only these, not off the streaming sites it
// exists to serve. It is a separate fetch and cache entry because HostCache
// stores one set per id, and the TorBox list is small.
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

// remotefsLogins returns the host-to-login snapshot for the remote-server
// resolver and backend. Accounts of the "remotefs" service are keyed by host
// name, lower-cased here since host names are case-insensitive. Disabled
// accounts are skipped, which also stops their https:// links being claimed.
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

// knownHostsPath is where accepted SSH host keys are kept, beside
// accounts.json. It is not ~/.ssh/known_hosts: the container user has no
// useful home, and the app should not edit a person's ssh configuration.
func (a *App) knownHostsPath() string {
	return filepath.Join(filepath.Dir(a.dlDir), "known_hosts")
}

// Routing host lists are cached in host_cache.json beside accounts.json. They
// are public information, so the file is plain JSON.

// hostCacheEntry is one service's last good routing host set, as stored.
type hostCacheEntry struct {
	Hosts     []string  `json:"hosts"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// hostCacheMu serialises read-modify-writes of host_cache.json.
var hostCacheMu sync.Mutex

// hostCachePath is derived from dlDir, since accounts.Store keeps its own
// directory private.
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

// loadHostCacheEntry is a HostCache Load hook: the last successfully fetched
// set, or ok=false for a service that never succeeded (see
// resolver.HostCache.Load).
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

// saveHostCacheEntry is a HostCache Save hook, called only after a successful
// fetch.
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

// hostCacheFor builds a HostCache backed by serviceID's entry in
// host_cache.json. It is rebuilt on every rewire; Load reseeds it from disk,
// which is what keeps the last good list across rewires.
func (a *App) hostCacheFor(serviceID string, fetch func(context.Context) (map[string]bool, error)) *resolver.HostCache {
	return &resolver.HostCache{
		Fetch: fetch,
		Load:  func() (map[string]bool, time.Time, bool) { return a.loadHostCacheEntry(serviceID) },
		Save:  func(hosts map[string]bool, at time.Time) { a.saveHostCacheEntry(serviceID, hosts, at) },
	}
}

// hostRefreshInterval is how often the routing host lists refresh on their
// own, besides every account change. Supported hosts change over weeks, and
// polling paid APIs often earns rate limits.
const hostRefreshInterval = 6 * time.Hour

// hostRefreshAttempted is when rewireBackends last ran, for any reason, keyed
// by *App. HostCache.FetchedAt only moves on success, so gating on it would
// retry a failing service every minute.
var (
	hostRefreshMu        sync.Mutex
	hostRefreshAttempted = map[*App]time.Time{}
)

// refreshHostListsIfDue reruns rewireBackends once hostRefreshInterval has
// passed since the last attempt. It is called from sweep on the upkeep ticker
// and costs one map read when not due.
func (a *App) refreshHostListsIfDue() {
	hostRefreshMu.Lock()
	last, ok := hostRefreshAttempted[a]
	hostRefreshMu.Unlock()
	if ok && time.Since(last) < hostRefreshInterval {
		return
	}
	a.rewireBackends()
}

// Enabled and Label are not secrets, and accounts.Store only seals
// credentials, so they live in account_meta.json beside accounts.json. The
// file is small and read whole, never on a hot path.

// acctMeta is one account's non-secret metadata.
type acctMeta struct {
	// Enabled counts as true when the account has no entry at all (see
	// accountEnabled), so accounts that never had metadata keep routing.
	Enabled bool   `json:"enabled"`
	Label   string `json:"label,omitempty"`
}

// acctMetaMu serialises read-modify-writes of account_meta.json.
var acctMetaMu sync.Mutex

// acctMetaPath is derived from dlDir, since accounts.Store keeps its own
// directory private.
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

// metaKey mirrors accounts.Store's key shape (service, or service+NUL+account)
// so both files address a pair the same way. It is also AccountState.ID.
func metaKey(service, account string) string {
	if account == "" {
		return service
	}
	return service + "\x00" + account
}

// accountEnabled reports whether one account is switched on, true when nothing
// was ever recorded.
func (a *App) accountEnabled(service, account string) bool {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	meta, ok := a.loadAcctMetaLocked()[metaKey(service, account)]
	if !ok {
		return true
	}
	return meta.Enabled
}

// accountLabel returns the display label set for one account, or "".
func (a *App) accountLabel(service, account string) string {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	return a.loadAcctMetaLocked()[metaKey(service, account)].Label
}

// SetAccountEnabled stores whether one account routes and rewires the backends
// so it applies at once. It works for environment-supplied credentials too.
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

// SetAccountLabel stores one account's display label. It never rewires, so a
// rename cannot interrupt routing.
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

// deleteAccountMeta removes an account's metadata when its credential is
// cleared, so an old state does not resurface if the id is reused.
func (a *App) deleteAccountMeta(service, account string) {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	m := a.loadAcctMetaLocked()
	delete(m, metaKey(service, account))
	a.saveAcctMetaLocked(m)
}

// Tier, traffic and expiry come from a background ticker and are only ever
// read from its cache. refreshOneAccountHealth is the single place that asks
// the services, so a page load never waits on a slow debrid API.

// TrafficState is one account's traffic allowance as last read (see
// debrid.TrafficInfo and torbox.TrafficInfo). Readers check Unlimited first:
// Used and Limit are zero then, and 0 of 0 would render as "out of traffic".
type TrafficState struct {
	Used      int64 `json:"used"`
	Limit     int64 `json:"limit"`
	Unlimited bool  `json:"unlimited"`
	// UsedPercent is the spent share, 0-100, for services that report a
	// fraction rather than bytes (Premiumize, Debrid-Link). It is only
	// meaningful with PercentKnown, since 0 is a real reading.
	UsedPercent float64 `json:"usedPercent,omitempty"`
	// PercentKnown says whether the service reported UsedPercent at all.
	PercentKnown bool `json:"percentKnown,omitempty"`
	// ResetsAt is RFC3339, or "" when the service does not say when traffic
	// resets (only Debrid-Link does).
	ResetsAt string `json:"resetsAt,omitempty"`
}

// AccountHealth is the ticker's cached reading for one account. A missing
// entry is reported as tier "unknown", never as a zero value, so an unchecked
// account is not shown as free.
type AccountHealth struct {
	Tier    string       `json:"tier"`
	Traffic TrafficState `json:"traffic"`
	// Expiry is RFC3339, or "" when nothing expires or nothing was read yet.
	Expiry    string    `json:"expiry,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// accountHealthByApp holds each App's health cache. It is keyed by pointer so
// test Apps never share a cache, and accountHealthLoop removes its entry on
// exit so a reused address starts clean.
var (
	accountHealthMu    sync.Mutex
	accountHealthByApp = map[*App]*accountHealthState{}
)

// accountHealthState is one App's health cache and the guard that starts its
// ticker once, however often rewireBackends runs.
type accountHealthState struct {
	startOnce sync.Once

	mu   sync.RWMutex
	rows map[string]AccountHealth // keyed by metaKey(service, account)
}

// healthState returns this App's health cache, creating it and starting its
// ticker on the first call.
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

// tierUnknown is the tier of an account nothing has read yet, as opposed to a
// plan name a provider chose.
const tierUnknown = "unknown"

// accountHealth reads the cache, answering tier "unknown" for an account no
// sweep has read yet.
func (a *App) accountHealth(service, account string) AccountHealth {
	st := a.healthState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	if h, ok := st.rows[metaKey(service, account)]; ok {
		return h
	}
	return AccountHealth{Tier: tierUnknown}
}

// fillHealth copies the cached reading onto a row. accountRow and TestAccount
// both use it, so every row for an account carries the same answer.
func (a *App) fillHealth(st *AccountState) {
	h := a.accountHealth(st.Service, st.Account)
	st.Tier = h.Tier
	st.Traffic = h.Traffic
	st.Expiry = h.Expiry
	st.TrafficLeft = fmtTrafficLeft(h.Traffic)
	// A successful reading is a check too, so the row should not say "not
	// checked" beside its numbers. An existing Detail, such as an error from
	// TestAccount's live check, is kept.
	if h.Tier != "" && h.Tier != tierUnknown && st.Detail == "" {
		st.OK, st.Detail = true, "credential accepted"
	}
}

// fmtTrafficLeft formats the TrafficLeft column, which the page prints
// verbatim: digits, a unit or "∞", never a sentence. "" means not fetched yet.
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
	// Services that meter in percent show what is left, rounded down so 99.6%
	// spent reads "0 %".
	if t.PercentKnown {
		left := 100 - t.UsedPercent
		if left < 0 {
			left = 0
		}
		return fmt.Sprintf("%d %%", int(left))
	}
	return ""
}

// fmtBinaryBytes matches web/src/lib/format.ts's fmtBytes (binary units, one
// decimal below 10), so byte figures read the same wherever they are formatted.
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

// accountHealthInterval is how often the ticker rereads every enabled
// account. Tier and expiry change over days, traffic over hours.
var accountHealthInterval = 15 * time.Minute

// accountHealthTimeout bounds one service's call, so one unreachable service
// cannot stall the sweep for the others.
const accountHealthTimeout = 15 * time.Second

// StartAccountHealthNow runs one sweep right away. Without it the accounts
// page shows no tier or traffic until the first tick, fifteen minutes after
// start. It is called from main rather than New, because tests build Apps with
// New and a sweep is a real API call.
func (a *App) StartAccountHealthNow() {
	a.spawn(a.refreshAccountHealth)
}

// accountHealthLoop is the ticker. It does no work before the first tick, so
// the Apps tests build never call a real debrid API in the background.
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

// refreshAccountHealth is one sweep over every service's default and named
// accounts, one after another; it runs off every request path.
func (a *App) refreshAccountHealth() {
	st := a.healthState()
	for _, svc := range accounts.Catalogue {
		a.refreshOneAccountHealth(st, svc, "")
		for _, id := range a.Accounts.AccountIDs(svc.ID) {
			a.refreshOneAccountHealth(st, svc, id)
		}
	}
}

// refreshOneAccountHealth reads one account's tier, traffic and expiry into
// the cache. It is the only writer of accountHealthState.rows. Disabled or
// unconfigured accounts are skipped, but their last reading is kept.
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
		return // the service offers no health reading
	}
	if err != nil {
		// The last good reading stays; one failed request says nothing about
		// the account.
		log.Printf("%s account health unavailable (%v); keeping the last reading", svc.Label, err)
		return
	}
	st.mu.Lock()
	prev, had := st.rows[metaKey(svc.ID, account)]
	st.rows[metaKey(svc.ID, account)] = health
	st.mu.Unlock()
	// Both readings, so the trigger fires on the crossing rather than on the
	// state (see fireAccountExpiry). Outside st.mu, since subscribers run on
	// this goroutine.
	a.fireAccountExpiry(svc.ID, account, prev, had, health)
}

// accountInfoFetcher is replaced in a test that proves page reads never make
// a live call.
var accountInfoFetcher = fetchAccountInfoLive

// fetchAccountInfoLive asks a service for an account's tier, traffic and
// expiry. ok is false for services that offer no such reading, which the
// caller treats as nothing to update rather than an error.
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
		// Offcloud documents no account endpoint. Refresh still confirms the
		// key through checkCredential.
		return AccountHealth{}, false, nil
	}
}

// healthFromDebrid converts a debrid.AccountInfo into the cache's shape.
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

// formatExpiry returns t as RFC3339, or "" for the zero time. The browser
// formats it in the reader's locale.
func formatExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// AccountState is one row of the accounts page: one configured account, never
// its secret.
type AccountState struct {
	// ID is what the client sends back to act on the row (see metaKey).
	ID      string `json:"id"`
	Service string `json:"service"` // catalogue id: accounts.Lookup(Service)
	Account string `json:"account"` // "" for a service's default account
	// Label is the chosen label, else the account id, else "" for an unnamed
	// default account. The service's own name is a separate column.
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`

	Configured bool `json:"configured"`
	// FromEnv and EnvVar explain why a credential is read-only on the page.
	FromEnv bool   `json:"fromEnv"`
	EnvVar  string `json:"envVar,omitempty"`

	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Hosts  int    `json:"hosts"` // supported hosters the service reports, once tested
	// HostsFetchedAt is when the routing host list was last fetched
	// successfully, RFC3339 or "". Hosts is the count from the last manual
	// check; this can be older if the service has failed since.
	HostsFetchedAt string `json:"hostsFetchedAt,omitempty"`

	// Tier, Traffic, Expiry and TrafficLeft are the ticker's cached reading
	// (see fillHealth), never fetched for this request.
	//
	// Tier defaults to "unknown", never "free", so an unchecked account is not
	// shown as free.
	Tier string `json:"tier"`
	// Traffic's zero value means no data, not 0% used.
	Traffic TrafficState `json:"traffic"`
	// Expiry and TrafficLeft are empty until fetched. Expiry is RFC3339;
	// TrafficLeft is preformatted for a plain-text column (see fmtTrafficLeft).
	Expiry      string `json:"expiry,omitempty"`
	TrafficLeft string `json:"trafficLeft,omitempty"`
}

// AccountStates lists every configured account, stored or from the
// environment. Services without a configured account have no row.
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

// accountRow builds the row for (svc, account), or false when nothing is
// configured there. The environment is checked first for the default account,
// as credentialFor does.
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
	a.fillHealth(&st)
	st.HostsFetchedAt = a.hostsFetchedAtField(svc.ID)
	return st, true
}

// hostsFetchedAtField returns when serviceID's routing host list was last
// fetched, RFC3339 or "". It is a local read.
func (a *App) hostsFetchedAtField(serviceID string) string {
	_, at, ok := a.loadHostCacheEntry(serviceID)
	if !ok {
		return ""
	}
	return formatExpiry(at)
}

// SetAccountCredential stores, or with a zero Credential clears, one account's
// secret and rewires the backends at once. Clearing also drops the account's
// metadata.
func (a *App) SetAccountCredential(service, account string, cred accounts.Credential) error {
	if err := a.Accounts.SetCredential(service, account, cred); err != nil {
		return err
	}
	if cred.IsZero() {
		a.deleteAccountMeta(service, account)
	}
	// A new credential starts without the old one's health verdict.
	a.acctHealthTracker().Reset(service, account)
	a.rewireBackends()
	return nil
}

// checkCredential asks a service whether cred works, storing nothing. It backs
// both VerifyCredential and TestAccount.
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
		// The authenticated host list rejects a bad token and also counts the
		// hosts in one round trip.
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
		// Log in first: Linksnappy's host list needs no account and would
		// accept a wrong password.
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

// VerifyCredential checks a credential before it is saved, so a typo shows up
// in the dialog. The caller decides whether a failure blocks saving.
func (a *App) VerifyCredential(service string, cred accounts.Credential) (ok bool, hosts int, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, hosts, err := checkCredential(ctx, service, cred)
	if err != nil {
		return false, 0, err.Error()
	}
	return true, hosts, "credential accepted"
}

// TestAccount rechecks a stored account against its service: the per-row
// Refresh. It changes no routing and does not touch the ticker's cache, but
// feeds the result into the account health tracker.
func (a *App) TestAccount(service, account string) AccountState {
	svc, known := accounts.Lookup(service)
	st := AccountState{
		ID: metaKey(service, account), Service: service, Account: account,
		Label: account, Enabled: a.accountEnabled(service, account),
	}
	if lbl := a.accountLabel(service, account); lbl != "" {
		st.Label = lbl
	}
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
		a.reportAccountFailure(service, account, classify(failure{err: err, text: err.Error()}), err.Error())
		return st
	}
	a.reportAccountSuccess(service, account)
	st.OK, st.Hosts, st.Detail = ok, hosts, "credential accepted"
	return st
}

// JDStatus is what the interface shows for the headless-JD sidecar. JD is not
// an account: KL_JD is a URL, not a secret.
type JDStatus struct {
	Configured bool `json:"configured"`
	Reachable  bool `json:"reachable"`
	// Version is JDownloader's revision number (see jd.Client.Version), 0 when
	// not reachable.
	Version int64  `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// JDStatus asks the sidecar live, so an unreachable JD shows up at once. It
// uses a fresh jd.Client because the backend interface has no Version method.
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
