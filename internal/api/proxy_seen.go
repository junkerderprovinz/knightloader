package api

import (
	"net/http"
	"sync/atomic"
	"time"
)

// proxyRecent is how long one forwarded request keeps the trusted proxies
// field on the Security page.
const proxyRecent = 24 * time.Hour

// proxySeen remembers when a request last arrived through a reverse proxy, so
// the Security page offers the trusted proxies field only where one is in
// front.
type proxySeen struct {
	// last is in Unix nanoseconds, 0 for never.
	last atomic.Int64
	now  func() time.Time
}

func newProxySeen() *proxySeen { return &proxySeen{now: time.Now} }

// note marks every request that carries a forwarding header. A sibling's call
// over the relay reaches the same handler without passing any proxy in front
// of this instance, so it does not count.
func (p *proxySeen) note(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if forwarded(r) && !fromRelayGroup(r) {
			p.last.Store(p.now().UnixNano())
		}
		next.ServeHTTP(w, r)
	})
}

// recently reports whether a forwarded request came in within proxyRecent.
func (p *proxySeen) recently() bool {
	last := p.last.Load()
	return last != 0 && p.now().Sub(time.Unix(0, last)) < proxyRecent
}

func forwarded(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}
