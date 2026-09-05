package api

// The site icon beside a hoster row (app_hostericons.go owns the fetching and
// the cache). One route, GET only, and a 404 that the page is expected to
// handle: a missing icon is the ordinary case, not an error worth a toast.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func registerHosterIcons(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/hosters/icon", "one host's own site icon, fetched by this instance and cached on disk",
		func(w http.ResponseWriter, r *http.Request) {
			host := r.URL.Query().Get("host")
			body, ct, err := a.HosterIcon(r.Context(), host)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			// An ETag over the bytes, so a browser that already has this icon
			// gets a 304 instead of the file - forty rows on a page that
			// reloads often is forty requests worth answering cheaply.
			sum := sha256.Sum256(body)
			etag := `"` + hex.EncodeToString(sum[:16]) + `"`
			w.Header().Set("ETag", etag)
			// Long, because a site icon is not something a person waits for an
			// update of, and the server's own cache refreshes it monthly
			// anyway. Private: this answers which hosters somebody uses, and
			// that is not a thing to let a shared proxy keep.
			w.Header().Set("Cache-Control", "private, max-age=86400")
			w.Header().Set("Content-Type", ct)
			// Belt and braces on a byte stream that came from somebody else's
			// server: nothing here should ever be sniffed into something
			// executable, whatever the allowlist let through.
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if match := r.Header.Get("If-None-Match"); match == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			_, _ = w.Write(body)
		})
}
