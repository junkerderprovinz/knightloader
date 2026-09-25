package api

// Running under a path prefix, for a reverse proxy that mounts KnightLoader at
// https://example.com/kl/ instead of giving it a host of its own.
//
// The prefix is KL_BASE_PATH (buildinfo.BasePath) or, while that is unset, the
// proxy's X-Forwarded-Prefix. Honouring the header is safe: another site cannot
// add it to a request without a CORS preflight this server never answers, and
// it is only accepted as a plain path on this origin, so whoever sends a false
// one breaks nothing but their own page. The index page, the one response
// built from it that a cache may keep, names it in Vary.
//
// A path is served with the prefix and without it. Some proxies strip it
// (Traefik's StripPrefix, nginx with a URI on proxy_pass) and some pass it on,
// and the container's health check and the LAN address reach the process
// directly.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// pathChars is what a base path segment may consist of: the characters RFC
// 3986 leaves unreserved, which need no escaping in a URL or in HTML.
const pathChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// ParseBasePath checks a base path and returns it without the trailing slash,
// "" for the root.
func ParseBasePath(v string) (string, error) {
	p := strings.TrimRight(strings.TrimSpace(v), "/")
	if p == "" {
		return "", nil
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%q does not start with a slash, as in /kl", v)
	}
	for i, seg := range strings.Split(p[1:], "/") {
		if seg == "" || seg == "." || seg == ".." || strings.Trim(seg, pathChars) != "" {
			return "", fmt.Errorf("%q is not a plain path: use letters, digits and - . _ ~ between the slashes", v)
		}
		// Unprefixed requests are still served, so a prefix of /api would
		// make /api/health mean two different things, and one of /relay would
		// hide the /relay/connect a sibling on the LAN dials.
		if i == 0 && (seg == "api" || seg == "relay") {
			return "", fmt.Errorf("a base path cannot begin with /%s, where this instance's own routes live", seg)
		}
	}
	return p, nil
}

type basePathKeyType struct{}

var basePathKey basePathKeyType

// requestBasePath is the prefix the browser sees in front of this request's
// path, "" at the root.
func requestBasePath(r *http.Request) string {
	p, _ := r.Context().Value(basePathKey).(string)
	return p
}

// forwardedBasePath is the proxy's X-Forwarded-Prefix when it is a usable base
// path, and "" otherwise.
func forwardedBasePath(r *http.Request) string {
	p, err := ParseBasePath(r.Header.Get("X-Forwarded-Prefix"))
	if err != nil {
		return ""
	}
	return p
}

// withBasePath takes the base path off the request before the guard and the
// routes see it, and keeps it on the context for whatever builds a link.
func withBasePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := buildinfo.BasePath
		if base == "" {
			base = forwardedBasePath(r)
		}
		if base == "" {
			next.ServeHTTP(w, r)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), basePathKey, base))
		r.URL = stripBasePath(r.URL, base)
		next.ServeHTTP(redirectsUnderBase{ResponseWriter: w, base: base}, r)
	})
}

// stripBasePath returns u without the base in front of its path, or u itself
// when the path does not start with it.
func stripBasePath(u *url.URL, base string) *url.URL {
	rest, ok := strings.CutPrefix(u.Path, base)
	if !ok || (rest != "" && rest[0] != '/') {
		return u
	}
	out := *u
	out.Path = rest
	// The base is all unreserved characters, so it opens the escaped form
	// exactly as it opens the decoded one.
	out.RawPath = strings.TrimPrefix(u.RawPath, base)
	if rest == "" {
		out.Path, out.RawPath = "/", ""
	}
	return &out
}

// redirectsUnderBase puts the base path in front of a redirect to a path from
// the root, which is what http.ServeMux sends when it tidies a path such as
// /kl//downloads.
type redirectsUnderBase struct {
	http.ResponseWriter
	base string
}

func (w redirectsUnderBase) WriteHeader(code int) {
	if loc := w.Header().Get("Location"); strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") {
		w.Header().Set("Location", w.base+loc)
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets the WebSocket upgrade and http.ResponseController reach the
// connection underneath.
func (w redirectsUnderBase) Unwrap() http.ResponseWriter { return w.ResponseWriter }
