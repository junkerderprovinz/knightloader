// Package resolver is KnightLoader's plugin seam. Everything that turns a
// pasted link into a concrete, downloadable target — a direct URL, a premium
// hoster, a debrid unlock, yt-dlp, or a headless-JD delegation — implements
// Resolver. v1 ships built-in resolvers; native hoster plugins come later.
package resolver

import (
	"context"
	"sync"
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
