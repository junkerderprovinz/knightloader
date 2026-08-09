// Package resolver is KnightLoader's plugin seam. Everything that turns a
// pasted link into a concrete, downloadable target — a direct URL, a premium
// hoster, a debrid unlock, yt-dlp, or a headless-JD delegation — implements
// Resolver. v1 ships built-in resolvers; native hoster plugins come later.
package resolver

import (
	"context"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// Info identifies a resolver and sets its routing priority (higher wins).
type Info struct {
	ID   string
	Prio int
}

// Request is what the resolver is asked to resolve.
type Request struct {
	URL string
	// Account and Captcha providers are added when premium/debrid land.
}

// Result is a concrete download target the engine can fetch.
type Result struct {
	Name        string
	DirectURL   string
	Headers     map[string]string
	Size        int64
	Connections int
}

// Resolver turns a link into a downloadable Result.
type Resolver interface {
	Info() Info
	Match(url string) bool
	Resolve(ctx context.Context, req Request) (Result, error)
}

// Checker is the optional other half of a resolver: a backend that can be asked
// whether a link is still there without fetching it. Implementing it is what
// moves a service's links off core.AvailUnknown, which says "nobody has looked"
// and is a lie the moment somebody presses Check.
//
// The batch is the interface and not an optimisation inside it. Every service
// that answers this question answers it for a list, and a caller holding fifty
// links that asks fifty times is a caller whose key gets rate-limited - so the
// one-link form is deliberately absent, because it is the shape that would get
// written by accident.
//
// The contract is one verdict per URL, in the order they were given. A service
// that cannot answer for a particular link returns core.AvailUncheckable for it
// rather than dropping it, because a short slice silently re-aligns every
// verdict after the gap onto the wrong link. Callers should still run the answer
// through Answers, which is the only cheap defence against a service that
// changes its mind about that.
//
// An error means the batch was not answered at all - a refused key, a service
// that is down. It never means "these links are gone": the caller files the
// whole batch as uncheckable and says so.
type Checker interface {
	Check(ctx context.Context, urls []string) ([]core.Availability, error)
}

// Answers squares what a Checker returned against the number of links it was
// asked about, filling anything missing with core.AvailUncheckable and dropping
// anything extra.
//
// It exists because the alternative is an index-out-of-range in the caller, and
// the input is a remote service's JSON: the day a provider adds an entry for a
// link it expanded, or omits one it did not recognise, is a day this app must
// still be able to draw its list.
func Answers(got []core.Availability, want int) []core.Availability {
	out := make([]core.Availability, want)
	for i := range out {
		if i < len(got) && got[i] != "" {
			out[i] = got[i]
			continue
		}
		// AvailUncheckable and not AvailUnknown, including for an empty string the
		// service did send: a link that went out in a check request has been looked
		// at, whatever came back. Leaving it "" would put it back among the links
		// nobody has touched and hide it from the person who just asked.
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
