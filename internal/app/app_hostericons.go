package app

// The site icon beside a hoster in the accounts list. Logos are not bundled:
// they are other people's trademarks and go stale when a site redesigns. Each
// instance fetches the icon from the site itself, once, and keeps it on disk,
// so the list of someone's hoster accounts is never handed to a third-party
// icon service.
//
// Candidates are the front page's own <link rel="icon"> hrefs, which may be on
// another host (alldebrid.com keeps its icon on a CDN), then the two well-known
// paths. Up to iconMaxCandidates are fetched and the largest real image wins,
// stopping early at iconGoodEnough, because /favicon.ico is usually 16x16.
//
// An href from someone else's HTML could name any address, so iconClient
// refuses to dial anything that is not public. The check happens at dial time,
// which also covers redirects and DNS answers.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // registers the GIF decoder for iconPixelSize
	_ "image/jpeg" // registers the JPEG decoder for iconPixelSize
	_ "image/png"  // registers the PNG decoder for iconPixelSize
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// iconMaxBytes caps what is read for one image, so a hostile host cannot make
// a decoration a memory problem.
const iconMaxBytes = 512 << 10

// iconHTMLMaxBytes is how much of a front page is read for its icon links,
// which can only be in the head.
const iconHTMLMaxBytes = 256 << 10

// iconMaxCandidates bounds how many images one host is asked for.
// iconGoodEnough is the pixel size that ends the search; 64 pixels is already
// sharp in the page's 18-pixel box at any density.
const (
	iconMaxCandidates = 4
	iconGoodEnough    = 64
)

// iconTTL is how long a fetched icon is trusted and iconMissTTL how long a
// failure is remembered: long enough not to ask on every render, short enough
// that a site that was briefly down is tried again.
const (
	iconTTL     = 30 * 24 * time.Hour
	iconMissTTL = 6 * time.Hour
)

// iconTypes is the allowlist of served types. SVG is left out because it can
// carry script and would be served from the instance's own origin.
var iconTypes = map[string]string{
	"image/x-icon":             "ico",
	"image/vnd.microsoft.icon": "ico",
	"image/png":                "png",
	"image/gif":                "gif",
	"image/jpeg":               "jpg",
	"image/webp":               "webp",
}

// HosterIcon returns a host's site icon and its content type, from disk when
// it was fetched before and from the site otherwise. An error means there is
// nothing to show, and the page falls back to a monogram.
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
		// The cached file is gone; fetch again.
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
		// The cache is an optimisation; serve the icon anyway.
		return body, ct, nil
	}
	// Named by a hash, because the host comes from an editable settings file
	// and must not become a path.
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

// normaliseIconHost reduces a URL or host to a bare hostname, or "" when the
// result is not a plain dotted hostname.
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
	// Letters, digits, dots and hyphens only, since this ends up in a URL.
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

// iconPaths are the well-known icon locations, the usually larger
// apple-touch-icon first.
var iconPaths = [...]string{"/apple-touch-icon.png", "/favicon.ico"}

// iconUserAgent is a browser's, because many hosters answer 403 to Go's
// default agent.
const iconUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

// iconClient is the client for every request in this file. Its dialer refuses
// non-public addresses.
var iconClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !iconDialAllowed(ip.IP) {
					return nil, fmt.Errorf("icon: refusing to fetch from a non-public address (%s)", ip.IP)
				}
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConnsPerHost:   2,
	},
}

// iconDialAllowed is the dialer's address policy, a variable so tests can
// reach an httptest server on 127.0.0.1.
var iconDialAllowed = publicIP

// publicIP reports whether ip is routable on the public internet. Anything
// unusual fails closed.
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10 (carrier-grade NAT, also Tailscale) is not covered by
	// IsPrivate.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

// iconCandidate is one image to try. declared is the size the HTML claimed, 0
// when none; it only orders the queue, and the real size comes from the bytes.
type iconCandidate struct {
	url      string
	declared int
}

// fetchFavicon returns the largest usable icon of host, always over https so
// the fact that someone has an account there does not travel in the clear.
func fetchFavicon(ctx context.Context, host string) ([]byte, string, error) {
	return fetchIconsFrom(ctx, "https://"+host)
}

// fetchIconsFrom is fetchFavicon for a full origin, which tests can point at a
// local server.
func fetchIconsFrom(ctx context.Context, origin string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	candidates := iconCandidates(ctx, origin)
	var (
		bestBody []byte
		bestCT   string
		bestSize = -1
		last     error
	)
	for i, c := range candidates {
		if i >= iconMaxCandidates {
			break
		}
		body, ct, err := fetchIconAt(ctx, c.url)
		if err != nil {
			last = err
			continue
		}
		size := iconPixelSize(body, ct)
		if size > bestSize {
			bestBody, bestCT, bestSize = body, ct, size
		}
		if size >= iconGoodEnough {
			break
		}
	}
	if bestBody == nil {
		if last == nil {
			last = errors.New("favicon: nothing to try")
		}
		return nil, "", last
	}
	return bestBody, bestCT, nil
}

// iconCandidates lists the images to try for one host: the front page's
// declared icons, largest first, then the well-known paths.
func iconCandidates(ctx context.Context, origin string) []iconCandidate {
	out := declaredIcons(ctx, origin+"/")
	for _, p := range iconPaths {
		out = append(out, iconCandidate{url: origin + p})
	}
	// Without duplicates, since the fetch budget is small.
	seen := map[string]bool{}
	uniq := out[:0]
	for _, c := range out {
		if seen[c.url] {
			continue
		}
		seen[c.url] = true
		uniq = append(uniq, c)
	}
	return uniq
}

// linkTag and the attribute patterns find icon links with regular expressions
// rather than an HTML parser. A bad match only wastes a candidate slot, since
// every href still goes through resolveIconURL and the dialer's guard.
var (
	linkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRelIn = regexp.MustCompile(`(?is)\brel\s*=\s*["']?([^"'>]*)`)
	attrHref  = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']+)["']`)
	attrSizes = regexp.MustCompile(`(?is)\bsizes\s*=\s*["']?\s*(\d+)\s*[xX]`)
)

// declaredIcons returns a page's <link rel="icon"> declarations, largest
// declared size first. A page that fails to load contributes nothing.
func declaredIcons(ctx context.Context, pageURL string) []iconCandidate {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", iconUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := iconClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, iconHTMLMaxBytes))
	if err != nil {
		return nil
	}
	// Relative hrefs resolve against where the page came from after redirects.
	base := resp.Request.URL

	var out []iconCandidate
	for _, tag := range linkTag.FindAll(body, -1) {
		rel := attrRelIn.FindSubmatch(tag)
		if rel == nil || !strings.Contains(strings.ToLower(string(rel[1])), "icon") {
			continue
		}
		href := attrHref.FindSubmatch(tag)
		if href == nil {
			continue
		}
		abs := resolveIconURL(base, string(href[1]))
		if abs == "" {
			continue
		}
		size := 0
		if m := attrSizes.FindSubmatch(tag); m != nil {
			size, _ = strconv.Atoi(string(m[1]))
		}
		// An apple-touch-icon is 180x180 whether or not it says so.
		if size == 0 && strings.Contains(strings.ToLower(string(rel[1])), "apple-touch") {
			size = 180
		}
		out = append(out, iconCandidate{url: abs, declared: size})
	}
	// Stable insertion sort, largest declared first.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].declared > out[j-1].declared; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// resolveIconURL makes href absolute against base, or returns "" for anything
// but http(s). A data: URI is not re-served from this origin.
func resolveIconURL(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	if abs.Hostname() == "" {
		return ""
	}
	return abs.String()
}

func fetchIconAt(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	req.Header.Set("User-Agent", iconUserAgent)
	res, err := iconClient.Do(req)
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
	// The header first, then sniffing: many hosts send their icon as
	// application/octet-stream.
	ct := strings.ToLower(strings.TrimSpace(strings.Split(res.Header.Get("Content-Type"), ";")[0]))
	if _, ok := iconTypes[ct]; !ok {
		ct = strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(body), ";")[0]))
	}
	if _, ok := iconTypes[ct]; !ok {
		return nil, "", errors.New("favicon: not an image this build serves")
	}
	return body, ct, nil
}

// iconPixelSize returns the image's smaller dimension in pixels, or 0 when it
// cannot be measured. An unmeasurable image is still used if nothing better
// turns up.
func iconPixelSize(body []byte, contentType string) int {
	if iconTypes[contentType] == "ico" {
		return icoLargestFrame(body)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0 // webp, or a file only a browser can read
	}
	return min(cfg.Width, cfg.Height)
}

// icoLargestFrame returns the largest frame in an .ico directory: a 6-byte
// header, then 16-byte entries whose first two bytes are width and height, 0
// meaning 256.
func icoLargestFrame(body []byte) int {
	if len(body) < 6 || binary.LittleEndian.Uint16(body[2:4]) != 1 {
		return 0
	}
	count := int(binary.LittleEndian.Uint16(body[4:6]))
	best := 0
	for i := range count {
		off := 6 + i*16
		if off+2 > len(body) {
			break
		}
		w, h := int(body[off]), int(body[off+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if s := min(w, h); s > best {
			best = s
		}
	}
	return best
}

// iconCache is embedded in App. The map is created on first use.
type iconCache struct {
	iconMu sync.Mutex
	icons  map[string]iconEntry
}
