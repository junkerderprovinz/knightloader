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

// AccountInfo is one account's plan, expiry and traffic, read by the
// account-health ticker. It stays off Service because not every provider can
// answer it.
type AccountInfo struct {
	// Tier is the provider's own name for the plan, such as "premium",
	// "free" or "trial".
	Tier    string
	Traffic TrafficInfo
	// ExpiresAt is the zero time when the account has no premium to expire.
	ExpiresAt time.Time
}

// TrafficInfo is one account's traffic, folded by the caller into
// app.TrafficState. Check Unlimited before dividing by LimitBytes.
type TrafficInfo struct {
	UsedBytes  int64
	LimitBytes int64
	Unlimited  bool
	// UsedPercent is how much of the allowance is spent, 0-100, for services
	// that meter in a fraction rather than bytes (Premiumize's fair-use
	// fraction, Debrid-Link's usagePercent). It is valid only when
	// PercentKnown is set, since 0 is an ordinary answer.
	UsedPercent float64
	// PercentKnown is whether UsedPercent came from the service at all.
	PercentKnown bool
	// ResetsAt is when the figure above rolls over, or the zero time when the
	// service does not say.
	ResetsAt time.Time
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

// LinkChecker is implemented by a provider that can tell whether links are
// still online without unlocking them. A provider implements it only when the
// check is free: no unlock, no traffic and no slot taken from the account.
type LinkChecker interface {
	CheckLinks(ctx context.Context, links []string) ([]core.Availability, error)
}

// HostLimiter is implemented by a provider that can cap how many chunks one
// download from a given host may open. 0 means no opinion, never unlimited
// and never zero connections.
//
// Only Real-Debrid implements it: its host lists carry no per-host figure, but
// the "chunks" field on a check or unlock answer does, so RealDebrid learns
// the limit host by host. AllDebrid's host endpoints carry nothing comparable.
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
	// Kept because Resume unlocks again and has to hand the same count on.
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
	// Account is the stored account this entry routes through, "" for the
	// default one. It does not change which links are claimed; it gives each
	// login its own routing slot (see resolver.SlotID).
	Account string
	Prio    int
	Hosts   map[string]bool
	// Svc is the provider behind this entry, used by Check and HostCap. Nil
	// behaves like a provider without a free check.
	Svc Service
}

func (r Resolver) Info() resolver.Info {
	return resolver.Info{ID: resolver.SlotID(r.ServiceID, r.Account), Prio: r.Prio}
}

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
// of them when this provider has no free way to ask, so callers need no list
// of which services can check.
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

// HostCap satisfies resolver.HostCapper. It answers 0 (no opinion) both for a
// provider without HostLimiter and for a host nothing is known about yet.
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
