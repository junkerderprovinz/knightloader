package api

// Whether requests reach this instance through a reverse proxy, which is when
// the trusted proxies setting means anything at all. The Security page shows
// that setting only then, or while it holds entries.

import (
	"net/http"
	"sync/atomic"
	"time"
)

// forwardedRecent is how long one forwarded request counts: a proxy that has
// forwarded nothing for a day has been taken away or is being bypassed.
const forwardedRecent = 24 * time.Hour

// forwardedHeaders are the headers a reverse proxy adds to name the client it
// forwards for.
var forwardedHeaders = []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip"}

// proxySeen remembers when a request last came in through a proxy.
type proxySeen struct{ at atomic.Int64 }

func (p *proxySeen) note(r *http.Request) {
	for _, h := range forwardedHeaders {
		if r.Header.Get(h) != "" {
			p.at.Store(time.Now().UnixNano())
			return
		}
	}
}

// recent reports whether a forwarded request arrived within forwardedRecent.
func (p *proxySeen) recent() bool {
	at := p.at.Load()
	return at != 0 && time.Since(time.Unix(0, at)) < forwardedRecent
}

// noteForwarded checks every request for a proxy's headers on its way to h.
func noteForwarded(p *proxySeen, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.note(r)
		h.ServeHTTP(w, r)
	})
}
