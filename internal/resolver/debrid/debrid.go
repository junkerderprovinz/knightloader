// Package debrid is the shared seam for one-shot debrid providers: services
// that turn a supported file-hoster link into a direct URL in a single call
// (AllDebrid, Real-Debrid). TorBox has its own package because it runs an
// asynchronous fetch job instead.
package debrid

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Direct is a resolved, downloadable target.
type Direct struct {
	URL  string
	Name string
	Size int64
}

// AccountInfo is one account's plan, expiry and traffic, as read by
// AllDebrid.Account and RealDebrid.Account - the account-health ticker's
// source (see internal/app/app_accounts.go, docs/build-plan.md 6B). It is
// deliberately not part of the Service interface: not every provider this
// package might grow needs to answer it, the same reasoning LinkChecker
// below is kept separate for.
type AccountInfo struct {
	// Tier is the provider's own name for the plan - "premium", "free" or
	// "trial" for both AllDebrid and Real-Debrid today. The caller decides
	// how to default an account nothing has read yet; this type only ever
	// carries a real answer.
	Tier    string
	Traffic TrafficInfo
	// ExpiresAt is the zero time when there is nothing to expire (no premium
	// on the account), never a sentinel value a caller has to know about.
	ExpiresAt time.Time
}

// TrafficInfo is {used, limit, unlimited} for one account, folded by the
// caller into app.TrafficState. Unlimited is its own field rather than a
// sentinel in Limit - see that type's doc comment for why a caller must check
// it before ever dividing by Limit.
type TrafficInfo struct {
	UsedBytes  int64
	LimitBytes int64
	Unlimited  bool
}

// Service is one debrid provider.
type Service interface {
	ID() string    // stable resolver id, e.g. "alldebrid"
	Label() string // human name, e.g. "AllDebrid"
	// Hosts returns the set of supported hoster domains (lower-case, no "www.").
	Hosts(ctx context.Context) (map[string]bool, error)
	// Unlock turns a hoster link into a direct download target.
	Unlock(ctx context.Context, link string) (Direct, error)
}

// LinkChecker is the part of a provider that can say whether a link is still
// there without unlocking it. It is kept off Service on purpose: a provider that
// cannot check must not be forced to grow a method that lies, and the whole
// worth of the split is that "this one has no free check" is expressible.
//
// Free is the word that decides whether a provider implements this at all. The
// endpoint behind it must cost the user nothing - no unlock, no traffic against
// the account, no slot used up. Checking a link is a convenience; paying for it
// out of somebody's premium quota without being asked is not.
type LinkChecker interface {
	CheckLinks(ctx context.Context, links []string) ([]core.Availability, error)
}

// HostLimiter is the optional other half of a provider that can state a
// ceiling on how many chunks one download against a given host may safely
// open. Kept off Service for the same reason LinkChecker is: a provider with
// nothing to say about a host must not be forced to grow a method that
// invents a number.
//
// 0 means "no opinion", never "unlimited" and never "zero connections" - the
// same contract every ceiling in app.connsFor's chain already carries, and
// the one HostCap below exists to preserve on the way there.
//
// Real-Debrid is the only implementation today (see RealDebrid.HostLimit),
// and deliberately so: /hosts, /hosts/status and /hosts/domains were checked
// against the live API and none of them carry a per-host figure at all - the
// only place Real-Debrid ever states one is the "chunks" field on a response
// about a specific link it has just checked or unlocked, so that is where
// this learns it, opportunistically, host by host. AllDebrid's /hosts and
// /user/hosts were checked the same way and carry nothing comparable - so it
// does not implement this interface, rather than inventing a number nobody
// asked it for.
type HostLimiter interface {
	HostLimit(host string) int
}

// Downloader is the byte-transfer backend a resolved link is handed to.
type Downloader interface {
	Download(taskID, url string, headers map[string]string, conns int)
	Pause(taskID string)
	Resume(taskID string)
	Remove(taskID string, deleteFiles bool)
}

// Backend unlocks a link through a Service, then delegates the transfer to the
// engine. Once handed over, pause/resume/remove act on the engine.
type Backend struct {
	svc Service
	eng Downloader

	onUpdate func(taskID string, u core.Update)

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	link   map[string]string
	handed map[string]bool
	conns  map[string]int
}

func NewBackend(svc Service, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		svc: svc, eng: eng, onUpdate: onUpdate,
		cancel: map[string]context.CancelFunc{},
		link:   map[string]string{},
		handed: map[string]bool{},
		conns:  map[string]int{},
	}
}

func (b *Backend) Download(taskID, link string, _ map[string]string, conns int) {
	b.mu.Lock()
	b.link[taskID] = link
	b.handed[taskID] = false
	// Kept beside the link rather than used and dropped: the unlock happens
	// first, and Resume re-enters start from the other side, so the count has to
	// survive the round trip through the service. A flat number written at the
	// handover instead - which is what stood here - meant a debrid download
	// ignored the task, the rule and the setting alike, and did it silently.
	b.conns[taskID] = conns
	b.mu.Unlock()
	b.start(taskID, link)
}

func (b *Backend) start(taskID, link string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	b.mu.Lock()
	b.cancel[taskID] = cancel
	conns := b.conns[taskID]
	b.mu.Unlock()
	go func() {
		defer cancel()
		defer func() {
			b.mu.Lock()
			delete(b.cancel, taskID)
			b.mu.Unlock()
		}()
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: "unlocking via " + b.svc.Label() + "…"})
		d, err := b.svc.Unlock(ctx, link)
		if err != nil {
			if ctx.Err() != nil {
				return // cancelled by Pause/Remove
			}
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Speed: 0, Err: b.svc.ID() + ": " + err.Error()})
			return
		}
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: d.Name, Size: d.Size})
		b.mu.Lock()
		b.handed[taskID] = true
		b.mu.Unlock()
		b.eng.Download(taskID, d.URL, nil, conns)
	}()
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	handed, cancel := b.handed[taskID], b.cancel[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Pause(taskID)
		return
	}
	if cancel != nil {
		cancel()
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusPaused, Speed: 0})
}

func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	handed, link := b.handed[taskID], b.link[taskID]
	b.mu.Unlock()
	if handed {
		b.eng.Resume(taskID)
		return
	}
	if link != "" {
		b.start(taskID, link)
	}
}

func (b *Backend) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	if c, ok := b.cancel[taskID]; ok {
		c()
	}
	handed := b.handed[taskID]
	delete(b.link, taskID)
	delete(b.handed, taskID)
	delete(b.conns, taskID)
	b.mu.Unlock()
	if handed {
		b.eng.Remove(taskID, deleteFiles)
	}
}

// Resolver claims links whose host the service supports.
type Resolver struct {
	ServiceID string
	Prio      int
	Hosts     map[string]bool
	// Svc is the provider the routing table was built from, kept here so a check
	// can reach it. Nil is allowed and means the same as a provider with no free
	// check: every link comes back uncheckable. It is nil in every test that only
	// cares about which links this resolver claims.
	Svc Service
}

func (r Resolver) Info() resolver.Info { return resolver.Info{ID: r.ServiceID, Prio: r.Prio} }

func (r Resolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return HostInSet(u.Hostname(), r.Hosts)
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// Check asks the provider about a batch of links, or answers uncheckable for all
// of them when this provider has no free way to ask.
//
// The method is present either way, which is the point: the caller reads "this
// resolver was asked and could not say" off the verdicts, and does not have to
// keep its own list of which services can check. Answering uncheckable is not a
// failure - it is the fourth state doing exactly the job it was added for.
func (r Resolver) Check(ctx context.Context, urls []string) ([]core.Availability, error) {
	lc, ok := r.Svc.(LinkChecker)
	if !ok {
		return resolver.Answers(nil, len(urls)), nil
	}
	got, err := lc.CheckLinks(ctx, urls)
	if err != nil {
		return nil, err
	}
	return resolver.Answers(got, len(urls)), nil
}

// HostCap satisfies resolver.HostCapper: 0 - "no opinion" - for a provider
// that does not implement HostLimiter at all, exactly as for one that does
// but has not learned anything about this host yet. The two cases are
// indistinguishable on purpose, because connsFor treats them identically.
func (r Resolver) HostCap(host string) int {
	hl, ok := r.Svc.(HostLimiter)
	if !ok {
		return 0
	}
	return hl.HostLimit(host)
}

// HostInSet reports whether host or any parent domain is in set.
func HostInSet(host string, set map[string]bool) bool {
	if len(set) == 0 {
		return false
	}
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	for host != "" {
		if set[host] {
			return true
		}
		i := strings.IndexByte(host, '.')
		if i < 0 {
			break
		}
		host = host[i+1:]
	}
	return false
}

// NormalizeHost lower-cases a domain and strips a leading "www.".
func NormalizeHost(d string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "www.")))
}
