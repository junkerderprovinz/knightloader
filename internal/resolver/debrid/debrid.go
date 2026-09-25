// Package debrid is the shared seam for one-shot debrid providers: services
// that turn a supported file-hoster link into a direct URL in a single call
// (AllDebrid, Real-Debrid). TorBox has its own package because it runs an
// asynchronous fetch job instead.
package debrid

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
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
// Real-Debrid learns the figure host by host from the "chunks" field of its
// check and unlock answers. Other services take it from their host list or
// ask for one connection per file. AllDebrid's host endpoints carry nothing
// comparable.
type HostLimiter interface {
	HostLimit(host string) int
}

// Downloader is the byte-transfer backend a resolved link is handed to.
type Downloader interface {
	// Handover starts the transfer of url. relink unlocks the hoster link
	// again for a fresh url to the same file, for what is left when this one
	// stops working part way.
	Handover(taskID, url string, conns int, relink func(context.Context) (string, error))
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
	// timeout bounds one unlock, waits for a login or a pacer slot included.
	timeout time.Duration

	mu     sync.Mutex
	runs   map[string]*unlockRun
	link   map[string]string
	handed map[string]bool
	conns  map[string]int
}

// unlockRun is one unlock of a task. A pointer, so a run that ends after a
// Resume started the next one can tell the entry is no longer its own.
type unlockRun struct{ cancel context.CancelFunc }

func NewBackend(svc Service, eng Downloader, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		svc: svc, eng: eng, onUpdate: onUpdate,
		timeout: 2 * time.Minute,
		runs:    map[string]*unlockRun{},
		link:    map[string]string{},
		handed:  map[string]bool{},
		conns:   map[string]int{},
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
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	run := &unlockRun{cancel: cancel}
	b.mu.Lock()
	b.runs[taskID] = run
	conns := b.conns[taskID]
	b.mu.Unlock()
	go func() {
		defer cancel()
		defer func() {
			b.mu.Lock()
			if b.runs[taskID] == run {
				delete(b.runs, taskID)
			}
			b.mu.Unlock()
		}()
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: "unlocking via " + b.svc.Label() + "…"})
		d, err := b.svc.Unlock(ctx, link)
		if err != nil {
			// Pause and Remove cancel the unlock and set the task's state
			// themselves. A timeout is reported like any other failure, or the
			// task would stay unlocking.
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			b.onUpdate(taskID, core.Update{Status: core.StatusError, Speed: 0, Err: b.svc.ID() + ": " + err.Error()})
			return
		}
		// Pause and Remove cancel under b.mu, so one that came after the unlock
		// finished is seen here and the link is not handed on.
		b.mu.Lock()
		if errors.Is(ctx.Err(), context.Canceled) {
			b.mu.Unlock()
			return
		}
		b.handed[taskID] = true
		b.mu.Unlock()
		b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: d.Name, Size: d.Size})
		b.eng.Handover(taskID, d.URL, conns, b.relinker(link))
	}()
}

// relinker unlocks link again, within the unlock's own time limit.
func (b *Backend) relinker(link string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, b.timeout)
		defer cancel()
		d, err := b.svc.Unlock(ctx, link)
		if err != nil {
			return "", fmt.Errorf("%s: %w", b.svc.ID(), err)
		}
		return d.URL, nil
	}
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	handed := b.handed[taskID]
	if run := b.runs[taskID]; run != nil && !handed {
		run.cancel()
	}
	b.mu.Unlock()
	if handed {
		b.eng.Pause(taskID)
		return
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
	if run := b.runs[taskID]; run != nil {
		run.cancel()
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

// loginLock lets one caller at a time log in. A caller waiting for it gives up
// when its context ends, which sync.Mutex cannot do, so a login that hangs
// does not carry every queued unlock past its deadline.
type loginLock chan struct{}

func newLoginLock() loginLock { return make(loginLock, 1) }

func (l loginLock) lock(ctx context.Context) error {
	select {
	case l <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for the login: %w", ctx.Err())
	}
}

func (l loginLock) unlock() { <-l }

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
	// Torrents also claims magnet links and uploaded .torrent files, for a
	// service that takes them (see TorrentService).
	Torrents bool
}

func (r Resolver) Info() resolver.Info {
	return resolver.Info{ID: resolver.SlotID(r.ServiceID, r.Account), Prio: r.Prio}
}

// Match claims a hoster link the service supports, a torrent when it takes
// torrents, and a download imported from this very account.
func (r Resolver) Match(raw string) bool {
	if slot, _, ok := ParseJobLink(raw); ok {
		return slot == r.Info().ID
	}
	if r.Torrents && (torrent.Resolver{}).Match(raw) {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return HostInSet(u.Hostname(), r.Hosts)
}

// Resolve checks a torrent the way the built-in client does, so a malformed
// one is refused before it reaches the service, and passes a hoster link on
// as it is. An imported download keeps the name it was staged with.
func (r Resolver) Resolve(ctx context.Context, req resolver.Request) (resolver.Result, error) {
	if _, _, ok := ParseJobLink(req.URL); ok {
		return resolver.Result{DirectURL: req.URL}, nil
	}
	if r.Torrents && torrent.IsURI(req.URL) {
		return (torrent.Resolver{}).Resolve(ctx, req)
	}
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
	// A torrent or an imported download is no hoster link, and one in the
	// batch can make a service refuse all of it, so it stays uncheckable as a
	// torrent is with the built-in client.
	var links []string
	var at []int
	for i, u := range urls {
		if _, _, imported := ParseJobLink(u); !imported && !torrent.IsURI(u) {
			links = append(links, u)
			at = append(at, i)
		}
	}
	out := resolver.Answers(nil, len(urls))
	if len(links) == 0 {
		return out, nil
	}
	got, err := lc.CheckLinks(ctx, links)
	if err != nil {
		return nil, err
	}
	for j, v := range resolver.Answers(got, len(links)) {
		out[at[j]] = v
	}
	return out, nil
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
