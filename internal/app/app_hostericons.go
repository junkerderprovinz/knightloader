package app

// app_hostericons.go: the little site icon beside a hoster in the accounts
// list (jdp, 2026-09-05: "Bei allen Hostern bzw. Accounts soll das logo mit in
// der liste sein. wie bei JD").
//
// JDownloader ships those logos as files in its own package. This does not,
// for two reasons that both matter here: a hoster's logo is its trademark, and
// this repository is public, so a folder of forty of them is a folder of forty
// other people's marks committed to somebody else's account. And a bundled set
// is wrong the moment a site redesigns, with nobody to notice.
//
// So the icon is fetched from the site itself, by this instance, once, and
// kept on disk. That is the same thing the browser showing this page would do
// with a favicon, done by the server so a page listing somebody's hoster
// accounts does not hand that list to a third-party icon service - which is
// what every "just use s2/favicons" shortcut actually does.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// iconMaxBytes is a hard cap on what is read from a host. A favicon is a few
// kilobytes; anything past this is either not a favicon or not something worth
// keeping, and reading it into memory unbounded is how a hostile host turns a
// cosmetic feature into a memory problem.
const iconMaxBytes = 128 << 10

// iconTTL is how long a fetched icon is trusted before the next request
// refreshes it, and iconMissTTL how long a failure is remembered. The miss is
// deliberately short-lived but not absent: a site that was down when the page
// first loaded should not be asked again on every render, and should not be
// written off for a month either.
const (
	iconTTL     = 30 * 24 * time.Hour
	iconMissTTL = 6 * time.Hour
)

// iconTypes is the allowlist. Everything else a host might answer with is
// refused rather than passed through, including SVG: an SVG is a document that
// can carry script, and this one would be served from the instance's own
// origin (see the inline-content-type rule this project already follows for
// captcha images and embedded assets).
var iconTypes = map[string]string{
	"image/x-icon":             "ico",
	"image/vnd.microsoft.icon": "ico",
	"image/png":                "png",
	"image/gif":                "gif",
	"image/jpeg":               "jpg",
	"image/webp":               "webp",
}

// HosterIcon is one host's site icon, from disk when it was fetched before and
// from the host itself when it was not.
//
// Returns the bytes, the content type to serve them as, and an error when
// there is nothing to show - a caller (routes_hostericons.go) answers 404 to
// that, and the page falls back to a monogram rather than a broken image.
func (a *App) HosterIcon(ctx context.Context, host string) ([]byte, string, error) {
	host = normaliseIconHost(host)
	if host == "" {
		return nil, "", errors.New("no host")
	}

	a.iconMu.Lock()
	if a.icons == nil {
		a.icons = map[string]iconEntry{}
	}
	e, ok := a.icons[host]
	a.iconMu.Unlock()
	if ok && time.Since(e.at) < e.ttl() {
		if e.missing {
			return nil, "", errors.New("no icon")
		}
		if b, err := os.ReadFile(e.path); err == nil {
			return b, e.contentType, nil
		}
		// The file was there when it was cached and is not now. Fall through
		// and fetch again rather than reporting a miss for a month.
	}

	body, ct, err := fetchFavicon(ctx, host)
	if err != nil {
		a.iconMu.Lock()
		a.icons[host] = iconEntry{at: time.Now(), missing: true}
		a.iconMu.Unlock()
		return nil, "", err
	}

	dir := filepath.Join(a.DataDir, "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Serve it anyway: the cache is an optimisation, not the feature.
		return body, ct, nil
	}
	// Named by a hash of the host, not by the host itself: a host string
	// reaches this from a settings file somebody can edit by hand, and a name
	// that becomes a path is a name that can escape the directory.
	sum := sha256.Sum256([]byte(host))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+"."+iconTypes[ct])
	if err := os.WriteFile(path, body, 0o644); err == nil {
		a.iconMu.Lock()
		a.icons[host] = iconEntry{at: time.Now(), path: path, contentType: ct}
		a.iconMu.Unlock()
	}
	return body, ct, nil
}

type iconEntry struct {
	at          time.Time
	path        string
	contentType string
	missing     bool
}

func (e iconEntry) ttl() time.Duration {
	if e.missing {
		return iconMissTTL
	}
	return iconTTL
}

// normaliseIconHost reduces whatever the page had on screen to a bare
// hostname. A catalogue row carries a full URL, a hoster login carries a plain
// host, and a person can type either.
func normaliseIconHost(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	// A hostname and nothing else: letters, digits, dots and hyphens, with at
	// least one dot. Everything else is refused rather than passed to a
	// request, because this string comes from stored settings and ends up in a
	// URL.
	if !strings.Contains(s, ".") || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return ""
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
		default:
			return ""
		}
	}
	return s
}

// iconPaths are the two well-known places a site keeps its icon, tried in
// order. No HTML parsing for <link rel="icon">: that means fetching and
// parsing a hoster's front page, which is a much larger request against a site
// that did not ask for it, for a decoration. Two known paths cover most of
// what one page of a hoster list shows; a host with neither gets a monogram
// and nothing is lost.
var iconPaths = [...]string{"/favicon.ico", "/apple-touch-icon.png"}

// iconUserAgent is a browser's, because a good number of hosters answer 403 to
// anything else and this request is doing exactly what a browser would do with
// the same URL. Measured 2026-09-05: alldebrid.com among others refused the
// default Go agent and served the icon happily to this one.
const iconUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

// fetchFavicon asks one host for its icon and reads the answer only if it is
// an image this build is willing to serve.
func fetchFavicon(ctx context.Context, host string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var letzter error
	for _, pfad := range iconPaths {
		body, ct, err := fetchIconAt(ctx, host, pfad)
		if err == nil {
			return body, ct, nil
		}
		letzter = err
	}
	return nil, "", letzter
}

func fetchIconAt(ctx context.Context, host, pfad string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+pfad, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	req.Header.Set("User-Agent", iconUserAgent)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", errors.New("favicon: " + res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, iconMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 || len(body) > iconMaxBytes {
		return nil, "", errors.New("favicon: unusable size")
	}
	// The header first, the bytes second. A host that answers
	// application/octet-stream for its own icon is common enough that
	// refusing it would lose real icons, and a host that CLAIMS image/png for
	// something else is exactly what sniffing is for.
	ct := strings.ToLower(strings.TrimSpace(strings.Split(res.Header.Get("Content-Type"), ";")[0]))
	if _, ok := iconTypes[ct]; !ok {
		ct = strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(body), ";")[0]))
	}
	if _, ok := iconTypes[ct]; !ok {
		return nil, "", errors.New("favicon: not an image this build serves")
	}
	return body, ct, nil
}

// iconCache is embedded into App (see its own struct), so the cache's two
// fields live in the file that owns them. The map is created on first use, so
// there is nothing to wire in New.
type iconCache struct {
	iconMu sync.Mutex
	icons  map[string]iconEntry
}
