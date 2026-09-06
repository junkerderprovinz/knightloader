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
//
// HOW IT FINDS ONE (rewritten 2026-09-06, jdp: "sehr viele logos der hoster
// werden nicht angezeigt. das von rapidgator ist zu klein"). The first version
// tried exactly two paths, /favicon.ico then /apple-touch-icon.png, and took
// whichever answered first. Measured against the real list, that loses on both
// counts:
//
//   - It misses every site that keeps its icon somewhere else and says so in
//     its HTML. alldebrid.com is the case that proved it: both well-known paths
//     answer 404, and the front page carries
//     <link rel="shortcut icon" href="https://cdn.alldebrid.com/lib/images/default/favicon.png">
//     - a different path AND a different host. A debrid account with no logo
//     was not a site without one, it was a site this never asked properly.
//   - Taking the FIRST answer means /favicon.ico always wins, and a favicon.ico
//     is usually 16x16 while the apple-touch-icon beside it is 180x180. The row
//     then draws a 16-pixel image in an 18-pixel box, which is exactly the
//     "zu klein" complaint.
//
// So it now collects candidates - the HTML's own <link rel="icon"> hrefs first,
// then the two well-known paths - fetches up to iconMaxCandidates of them,
// decodes each one's real pixel size, and keeps the largest. It stops early at
// iconGoodEnough, so the common case is still one or two requests.
//
// Reading a hoster's front page is a bigger request than fetching a fixed path,
// and the first version's comment said that was not worth it for a decoration.
// That judgement was made before anyone counted how many hosts it loses; it is
// capped (iconHTMLMaxBytes), it happens once per host per month, and it is the
// only way to find an icon a site chose to put somewhere else.
//
// SSRF: an href out of somebody else's HTML is an address this app would
// otherwise fetch on command. iconClient below refuses to dial any private,
// loopback or link-local address, at the DIAL, so a redirect chain cannot walk
// around it either - the homelab this runs in is full of things that answer on
// 192.168.x.x, and none of them is a favicon.

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

// iconMaxBytes is a hard cap on what is read from a host. A favicon is a few
// kilobytes; anything past this is either not a favicon or not something worth
// keeping, and reading it into memory unbounded is how a hostile host turns a
// cosmetic feature into a memory problem.
const iconMaxBytes = 512 << 10

// iconHTMLMaxBytes is how much of a front page is read while looking for its
// <link rel="icon">. The head is the only part that can carry one, and a head
// past this size is a page doing something other than declaring an icon.
const iconHTMLMaxBytes = 256 << 10

// iconMaxCandidates bounds how many images one host is asked for, and
// iconGoodEnough is the pixel size at which the search stops being worth
// another round trip - a 64-pixel image already draws sharply in the 18-pixel
// box the page uses, on any display density a browser will ask for.
const (
	iconMaxCandidates = 4
	iconGoodEnough    = 64
)

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

// iconPaths are the two well-known places a site keeps its icon. The
// apple-touch-icon comes first now: where a site has both, it is the larger of
// the two by a wide margin (180x180 against 16x16), and the whole point of
// ordering candidates is that the first good one ends the search.
var iconPaths = [...]string{"/apple-touch-icon.png", "/favicon.ico"}

// iconUserAgent is a browser's, because a good number of hosters answer 403 to
// anything else and this request is doing exactly what a browser would do with
// the same URL. Measured 2026-09-05: alldebrid.com among others refused the
// default Go agent and served the icon happily to this one.
const iconUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"

// iconClient is the one client every request in this file goes through. Its
// dialer is the SSRF guard: an <link rel="icon"> href comes out of a remote
// page and can name any address at all, including one inside the network this
// process is running in. Checking the address at DIAL time rather than the URL
// beforehand is what makes it hold through a redirect chain and through DNS
// answers that resolve to a private address.
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

// iconDialAllowed is the address policy iconClient's dialer applies, as a
// variable purely so a test can point this file at an httptest server - which
// listens on 127.0.0.1, an address the real policy exists to refuse. Swapped
// only by app_hostericons_test.go, and restored before it returns, the same
// seam accountInfoFetcher (app_accounts.go) already uses for the same reason.
var iconDialAllowed = publicIP

// publicIP is the whole of the address policy: everything that is not routable
// on the public internet is refused. Written as an allowlist of "is this
// ordinary" rather than a blocklist of ranges, so a range nobody thought of
// fails closed.
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10, carrier-grade NAT - not covered by IsPrivate, and the
	// range Tailscale hands out, which makes it very much a local address on a
	// machine like the one this runs on.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

// iconCandidate is one image worth trying, and how promising it looked before
// anything was fetched. declared is the size the HTML claimed (sizes="32x32"),
// 0 when nothing claimed one - it only orders the queue; the real size comes
// from the bytes.
type iconCandidate struct {
	url      string
	declared int
}

// fetchFavicon asks one host for its icon and returns the largest usable image
// it found. https, always: a hoster asked over plain http would have its icon
// - and the fact that somebody has an account there - travel in the clear.
func fetchFavicon(ctx context.Context, host string) ([]byte, string, error) {
	return fetchIconsFrom(ctx, "https://"+host)
}

// fetchIconsFrom is fetchFavicon with the origin spelled out rather than built
// from a hostname, which is the only thing a test can point at a local server.
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

// iconCandidates is the ordered list of images to try for one host: whatever
// the front page declares, largest first, then the two well-known paths. A
// front page that cannot be read costs nothing - the well-known paths are
// always appended, so this degrades exactly to the behaviour it replaces.
func iconCandidates(ctx context.Context, origin string) []iconCandidate {
	out := declaredIcons(ctx, origin+"/")
	for _, p := range iconPaths {
		out = append(out, iconCandidate{url: origin + p})
	}
	// Stable and duplicate-free: a site that declares /favicon.ico explicitly
	// must not have it fetched twice, and the fetch budget is small enough
	// that one wasted slot is a real loss.
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

// linkTag matches one <link ...> element. Deliberately a regex over the first
// few hundred kilobytes rather than a real HTML parse: this is looking for one
// attribute on one kind of tag in a document nobody here renders, and pulling
// in a parser to read a decoration would be the larger risk, not the smaller
// one. A malformed match costs a wasted candidate slot, nothing else, because
// every href still goes through resolveIconURL and the dialer's own guard.
var (
	linkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRelIn = regexp.MustCompile(`(?is)\brel\s*=\s*["']?([^"'>]*)`)
	attrHref  = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']+)["']`)
	attrSizes = regexp.MustCompile(`(?is)\bsizes\s*=\s*["']?\s*(\d+)\s*[xX]`)
)

// declaredIcons reads a site's own <link rel="icon"> declarations, largest
// declared size first. Errors are not errors here: a page that will not load,
// or carries no such tag, simply contributes nothing.
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
	// resp.Request.URL, not pageURL: a site that redirected to another host
	// declares its icon relative to where the page actually came from.
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
		// An apple-touch-icon carries no sizes attribute nearly as often as it
		// carries one, and is 180x180 either way. Ranking it above an
		// undeclared favicon costs nothing when the guess is wrong: the real
		// size still decides which image is kept.
		if size == 0 && strings.Contains(strings.ToLower(string(rel[1])), "apple-touch") {
			size = 180
		}
		out = append(out, iconCandidate{url: abs, declared: size})
	}
	// Largest declared first, stable within equal sizes so the document's own
	// order breaks ties.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].declared > out[j-1].declared; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// resolveIconURL turns an href into an absolute https URL, or "" for one this
// build will not fetch. Only http(s) survives: a data: URI is not something to
// re-serve from this origin, and every other scheme is not a fetch at all.
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

// iconPixelSize is the image's own width in pixels, or 0 when this build
// cannot tell. Zero is a real answer and not a failure: an image nobody could
// measure is still served if it is the only one, it just loses to any image
// that could be.
func iconPixelSize(body []byte, contentType string) int {
	if iconTypes[contentType] == "ico" {
		return icoLargestFrame(body)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0 // webp, or a file only the browser can read
	}
	return min(cfg.Width, cfg.Height)
}

// icoLargestFrame reads an ICO's directory - the only part of the format this
// needs - and answers the largest frame in it. An .ico is a container: 6 bytes
// of header, then one 16-byte entry per image, whose first two bytes are the
// width and height with 0 meaning 256. Browsers pick a frame themselves, so
// what matters here is the best size the FILE can offer, not the first one.
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

// iconCache is embedded into App (see its own struct), so the cache's two
// fields live in the file that owns them. The map is created on first use, so
// there is nothing to wire in New.
type iconCache struct {
	iconMu sync.Mutex
	icons  map[string]iconEntry
}
