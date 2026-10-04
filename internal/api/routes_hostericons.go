package api

// The site icon beside a hoster row (app_hostericons.go owns the fetching and
// the cache). One route, GET only. Neither answer short of an icon is an error
// status, because a browser logs each one as a failed load: a host without an
// icon gets a 204, and one the instance is still fetching gets a 202.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// iconCSP applies to every icon. An SVG can carry script, which an <img> never
// runs but a direct visit to the icon URL would, on the instance's own origin.
const iconCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

func registerHosterIcons(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/hosters/icon", "one host's own site icon, fetched by this instance and cached on disk",
		hosterIconHandler(a.HosterIcon))
}

// hosterIconHandler answers from lookup, which is App.HosterIcon outside tests.
func hosterIconHandler(lookup func(host string) ([]byte, string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, ct, err := lookup(r.URL.Query().Get("host"))
		if errors.Is(err, app.ErrIconPending) {
			// The fetch goes on without this request; asking again soon
			// finds the outcome cached.
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		serveHosterIcon(w, r, body, ct)
	}
}

// serveHosterIcon writes an icon with the headers that bytes from somebody
// else's server need.
func serveHosterIcon(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	// An ETag over the bytes, so a browser that has the icon gets a 304: forty
	// rows on a page that reloads often is forty requests worth answering
	// cheaply.
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", etag)
	// Long, since the server's own cache refreshes the icon monthly. Private,
	// because the answer says which hosters somebody uses and a shared proxy
	// has no business keeping that.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", contentType)
	// Nothing gets sniffed into something executable whatever the allowlist
	// let through.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", iconCSP)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(body)
}
