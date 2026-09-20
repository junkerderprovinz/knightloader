// Package resolver turns a pasted link into a downloadable target. Direct
// URLs, premium hosters, debrid unlocks, yt-dlp and headless JD each
// implement Resolver.
package resolver

import (
	"context"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Info identifies a resolver and sets its routing priority (higher wins).
// It carries JSON tags because PriorityFor shows the order to the user.
type Info struct {
	ID   string `json:"id"`
	Prio int    `json:"prio"`
}

// AccountSep separates a service id from an account id inside a resolver id.
//
// A resolver id names one backend slot. A service's default account keeps the
// bare catalogue id ("alldebrid"), which stored tasks, orders and logs already
// use; every further login on the same service gets its own slot behind this
// separator, so it is registered, benched and tried in the fallback chain on
// its own. Service ids come from the fixed catalogue in internal/accounts and
// never contain "#", while account ids are typed by people, so the first "#"
// is the separator and everything after it belongs to the account id.
const AccountSep = "#"

// SlotID is the resolver id one (service, account) pair registers under: the
// bare service id for a service's default account, service + AccountSep +
// account for a named one.
func SlotID(service, account string) string {
	if account == "" {
		return service
	}
	return service + AccountSep + account
}

// SplitSlot reads a slot id back into the pair SlotID built it from. An id
// without a separator yields (id, ""), which covers default accounts and
// resolvers that have no account at all (jd, ytdlp, direct, http, torrent).
func SplitSlot(id string) (service, account string) {
	if i := strings.Index(id, AccountSep); i >= 0 {
		return id[:i], id[i+len(AccountSep):]
	}
	return id, ""
}

// Request is what the resolver is asked to resolve.
type Request struct {
	URL string
	// Headers names the stored header profile this task was given, by a
	// Packagizer rule or by hand. Empty means none was named, which
	// internal/resolver/hostheaders reads as "use the profile for this link's
	// origin" and every other resolver ignores.
	//
	// It is a name and never the header values: those stay sealed in
	// accounts.Store, while a Request is built from a task that gets persisted
	// and serialised.
	Headers string
}

// Result is a concrete download target the engine can fetch.
type Result struct {
	Name        string
	DirectURL   string
	Headers     map[string]string
	Size        int64
	Connections int
	// Available is set when resolving already produced an availability
	// verdict, as JD's crawl of a link container does. Empty means nothing was
	// learned, which is the normal case.
	Available core.Availability
}

// Resolver turns a link into a downloadable Result.
type Resolver interface {
	Info() Info
	Match(url string) bool
	Resolve(ctx context.Context, req Request) (Result, error)
}

// Checker is implemented by a backend that can tell whether links are still
// online without fetching them.
//
// It takes a batch because every service answers for a list, and asking once
// per link gets an API key rate-limited. The answer holds one verdict per URL
// in input order, with core.AvailUncheckable for a link the service cannot
// judge; callers still pass it through Answers. An error means the batch was
// not answered at all, never that the links are gone.
type Checker interface {
	Check(ctx context.Context, urls []string) ([]core.Availability, error)
}

// HostCapper is implemented by a resolver that can cap how many chunks one
// download from a given host may open, such as a multihoster with per-host
// limits (see debrid.HostLimiter). A result of 0 means no opinion, not zero
// connections.
type HostCapper interface {
	HostCap(host string) int
}

// Answers squares what a Checker returned against the number of links it was
// asked about, filling anything missing with core.AvailUncheckable and dropping
// anything extra. The input is a remote service's JSON, so its length cannot
// be trusted.
func Answers(got []core.Availability, want int) []core.Availability {
	out := make([]core.Availability, want)
	for i := range out {
		if i < len(got) && got[i] != "" {
			out[i] = got[i]
			continue
		}
		// A link that went out in a check request has been looked at, so it
		// must not fall back among the links nobody has checked.
		out[i] = core.AvailUncheckable
	}
	return out
}

// Registry keeps resolvers ordered by descending priority. It is safe for
// concurrent use: adding an account rebuilds the routing table while downloads
// are running.
type Registry struct {
	mu   sync.RWMutex
	list []Resolver
}

func NewRegistry() *Registry { return &Registry{} }

// Register adds a resolver, keeping the list sorted by priority (highest first).
// A resolver with an ID that is already registered replaces it, so re-wiring
// after a credential change cannot leave two of the same backend behind.
func (r *Registry) Register(res Resolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := res.Info().ID
	for i, existing := range r.list {
		if existing.Info().ID == id {
			r.list = append(r.list[:i], r.list[i+1:]...)
			break
		}
	}
	r.list = append(r.list, res)
	for i := len(r.list) - 1; i > 0 && r.list[i].Info().Prio > r.list[i-1].Info().Prio; i-- {
		r.list[i], r.list[i-1] = r.list[i-1], r.list[i]
	}
}

// Unregister drops the resolver with this ID, if present. Removing a credential
// has to actually stop routing links to that service.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, res := range r.list {
		if res.Info().ID == id {
			r.list = append(r.list[:i], r.list[i+1:]...)
			return
		}
	}
}

// IDs lists the registered resolver IDs, highest priority first.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.list))
	for _, res := range r.list {
		out = append(out, res.Info().ID)
	}
	return out
}

// All returns every resolver that matches the URL, highest priority first. It
// is what makes a fallback chain possible: when the first backend cannot
// actually fetch the link, the next one gets a turn.
func (r *Registry) All(url string) []Resolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Resolver
	for _, res := range r.list {
		if res.Match(url) {
			out = append(out, res)
		}
	}
	return out
}

// For returns the highest-priority resolver that matches the URL, or nil.
func (r *Registry) For(url string) Resolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, res := range r.list {
		if res.Match(url) {
			return res
		}
	}
	return nil
}

// List returns every registered resolver in registry order, unfiltered by URL.
func (r *Registry) List() []Resolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Resolver, len(r.list))
	copy(out, r.list)
	return out
}

// AllInfo lists every registered resolver's identity in the order dispatch
// would try them for a URL all of them match: highest priority first, ties
// broken by registration order.
func (r *Registry) AllInfo() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.list))
	for _, res := range r.list {
		out = append(out, res.Info())
	}
	return out
}

// PriorityFor narrows AllInfo to the resolvers that would be asked for a link
// on host, in the order dispatch asks them. Resolvers only inspect the scheme
// and hostname in Match, so a synthetic "https://<host>/" stands in for a real
// link.
func (r *Registry) PriorityFor(host string) []Info {
	list := r.All("https://" + host + "/")
	out := make([]Info, 0, len(list))
	for _, res := range list {
		out = append(out, res.Info())
	}
	return out
}
