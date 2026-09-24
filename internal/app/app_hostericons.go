package app

// The site icon beside a hoster in the accounts list. Logos are not bundled:
// they are other people's trademarks and go stale when a site redesigns. Each
// instance fetches the icon from the site itself, once, and keeps it on disk,
// so the list of someone's hoster accounts is never handed to a third-party
// icon service.
//
// Candidates are the front page's own <link rel="icon"> hrefs, which may be on
// another host (alldebrid.com keeps its icon on a CDN), the icons its web app
// manifest lists, then the two well-known paths. Pages often declare sizes that
// were never uploaded, so failed requests have an allowance of their own
// (iconMaxAttempts) apart from the images compared (iconMaxImages). The largest
// image wins and the search stops early at iconGoodEnough, because
// /favicon.ico is usually 16x16.
//
// An href from someone else's HTML could name any address, so iconClient
// refuses to dial anything that is not public. The check happens at dial time,
// which also covers redirects and DNS answers.

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// iconMaxBytes caps what is read for one image, so a hostile host cannot make
// a decoration a memory problem.
const iconMaxBytes = 512 << 10

// iconHTMLMaxBytes is how much of a front page is read for its icon links,
// which can only be in the head. It also caps a web app manifest.
const iconHTMLMaxBytes = 256 << 10

// iconMaxImages bounds how many images are downloaded and compared for one
// host. iconMaxAttempts bounds the image requests one origin gets, failures
// included, and always leaves room for the well-known paths. iconGoodEnough is
// the pixel size that ends the search; 64 pixels is already sharp in the
// page's 18-pixel box at any density.
const (
	iconMaxImages   = 4
	iconMaxAttempts = 12
	iconGoodEnough  = 64
)

// iconTTL is how long a fetched icon is trusted and iconMissTTL how long a
// failure is remembered: long enough not to ask on every render, short enough
// that a site that was briefly down is tried again.
const (
	iconTTL     = 30 * 24 * time.Hour
	iconMissTTL = 6 * time.Hour
)

// iconTypes maps each served type to its cache file extension. An SVG can
// carry script, which the route's Content-Security-Policy keeps from running
// when someone opens an icon URL directly.
var iconTypes = map[string]string{
	"image/x-icon":  "ico",
	"image/png":     "png",
	"image/gif":     "gif",
	"image/jpeg":    "jpg",
	"image/webp":    "webp",
	"image/svg+xml": "svg",
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

	dir := filepath.Join(a.DataDir, "icons")
	if !ok {
		// The index lives in memory, so after a restart the files written
		// before it are found by name.
		if b, ct, e, found := readCachedIcon(dir, host); found {
			a.iconMu.Lock()
			a.icons[host] = e
			a.iconMu.Unlock()
			return b, ct, nil
		}
	}

	body, ct, err := fetchFavicon(ctx, host)
	if err != nil {
		// A page closed while the icon was loading says nothing about the site.
		if ctx.Err() == nil {
			a.iconMu.Lock()
			a.icons[host] = iconEntry{at: time.Now(), missing: true}
			a.iconMu.Unlock()
		}
		return nil, "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		// The cache is an optimisation; serve the icon anyway.
		return body, ct, nil
	}
	path := filepath.Join(dir, iconFileBase(host)+"."+iconTypes[ct])
	if err := os.WriteFile(path, body, 0o644); err == nil {
		a.iconMu.Lock()
		a.icons[host] = iconEntry{at: time.Now(), path: path, contentType: ct}
		a.iconMu.Unlock()
	}
	return body, ct, nil
}

// iconFileBase is the cache file name for host, without the extension. It is
// a hash because the host comes from an editable settings file and must not
// become a path.
func iconFileBase(host string) string {
	sum := sha256.Sum256([]byte(host))
	return hex.EncodeToString(sum[:8])
}

// readCachedIcon returns host's icon from the files an earlier run wrote, when
// one is younger than iconTTL. The extension gives the type back. A site that
// changed its icon's type leaves two files, and the newer one is the icon.
func readCachedIcon(dir, host string) ([]byte, string, iconEntry, bool) {
	base := filepath.Join(dir, iconFileBase(host))
	var newest iconEntry
	for ct, ext := range iconTypes {
		path := base + "." + ext
		info, err := os.Stat(path)
		if err != nil || time.Since(info.ModTime()) >= iconTTL || !info.ModTime().After(newest.at) {
			continue
		}
		newest = iconEntry{at: info.ModTime(), path: path, contentType: ct}
	}
	if newest.path == "" {
		return nil, "", iconEntry{}, false
	}
	b, err := os.ReadFile(newest.path)
	if err != nil {
		return nil, "", iconEntry{}, false
	}
	return b, newest.contentType, newest, true
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
		DialContext:           iconDialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConnsPerHost:   2,
	},
}

// iconDialer checks each address right before connecting to it, after the name
// has been resolved, so a DNS answer that changes between a check and the dial
// cannot point the request into the local network.
var iconDialer = &net.Dialer{
	Timeout: 10 * time.Second,
	ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if !iconDialAllowed(net.ParseIP(host)) {
			return fmt.Errorf("icon: refusing to fetch from a non-public address (%s)", host)
		}
		return nil
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

// fetchFavicon returns the largest usable icon of host from the first of
// iconOrigins that yields one.
func fetchFavicon(ctx context.Context, host string) ([]byte, string, error) {
	return fetchIconsFrom(ctx, iconOrigins(host)...)
}

// iconOrigins lists where to look for host's icon, in order: https, https on
// the www. name, which is sometimes the only one with a valid certificate
// (jianguoyun.com), and plain http for hosts without working TLS. Plain http
// comes last because an icon fetched over it can be swapped in transit; it
// gives nothing new away, since DNS and the TLS handshake already carry the
// host name in the clear.
func iconOrigins(host string) []string {
	out := []string{"https://" + host}
	if !strings.HasPrefix(host, "www.") {
		out = append(out, "https://www."+host)
	}
	return append(out, "http://"+host)
}

// fetchIconsFrom is fetchFavicon for explicit origins, which tests can point at
// local servers. The first origin that yields an image ends the search.
func fetchIconsFrom(ctx context.Context, origins ...string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var (
		bestBody []byte
		bestCT   string
		bestSize = -1
		images   int
		last     error
	)
	tried := map[string]bool{}
	for _, origin := range origins {
		candidates, err := iconCandidates(ctx, origin, tried)
		if err != nil {
			last = err
			continue
		}
		for _, c := range candidates {
			body, ct, err := fetchIconAt(ctx, c.url)
			if err != nil {
				last = err
				continue
			}
			images++
			size := iconPixelSize(body, ct)
			if size > bestSize {
				bestBody, bestCT, bestSize = body, ct, size
			}
			if size >= iconGoodEnough || images == iconMaxImages {
				break
			}
		}
		if bestBody != nil {
			return bestBody, bestCT, nil
		}
	}
	if last == nil {
		last = errors.New("favicon: nothing to try")
	}
	return nil, "", last
}

// iconCandidates lists the untried images of one origin, its front page's
// declared icons and then the well-known paths beside wherever that page ended
// up, and adds them to tried. It fails only when the origin does not answer.
func iconCandidates(ctx context.Context, origin string, tried map[string]bool) ([]iconCandidate, error) {
	page := origin + "/"
	if tried[page] {
		// An earlier origin redirected here, so its candidates are in tried.
		return nil, nil
	}
	declared, base, err := declaredIcons(ctx, page)
	if err != nil {
		return nil, err
	}
	// This origin redirected to a page an earlier one already went through,
	// whose allowance is spent.
	seen := tried[base.String()]
	tried[page], tried[base.String()] = true, true
	if seen || parkedHost(base.Hostname()) {
		return nil, nil
	}

	var out []iconCandidate
	add := func(c iconCandidate) {
		if !tried[c.url] {
			tried[c.url] = true
			out = append(out, c)
		}
	}
	// A page declaring a dozen sizes it never uploaded must not crowd out the
	// well-known paths.
	for _, c := range declared {
		if len(out) == iconMaxAttempts-len(iconPaths) {
			break
		}
		add(c)
	}
	for _, p := range iconPaths {
		add(iconCandidate{url: resolveIconURL(base, p)})
	}
	return out, nil
}

// parkedHosts are where the domain of a hoster that closed tends to redirect:
// domain marketplaces, parking services and the anti-piracy alliance's seizure
// notice. Their icon would stand in for the hoster's, which is worse than the
// monogram. A rebranded hoster redirects to its new site, which is followed.
var parkedHosts = []string{
	"above.com",
	"afternic.com",
	"alliance4creativity.com",
	"atom.com",
	"bodis.com",
	"buydomains.com",
	"dan.com",
	"domainmarket.com",
	"dynadot.com",
	"efty.com",
	"hugedomains.com",
	"parkingcrew.net",
	"sedo.com",
	"undeveloped.com",
}

func parkedHost(host string) bool {
	host = strings.ToLower(host)
	return slices.ContainsFunc(parkedHosts, func(p string) bool {
		return host == p || strings.HasSuffix(host, "."+p)
	})
}

// linkTag and the attribute patterns find icon links with regular expressions
// rather than an HTML parser. A bad match only wastes an attempt, since every
// href still goes through resolveIconURL and the dialer's guard.
var (
	linkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRel   = attrPattern("rel")
	attrHref  = attrPattern("href")
	attrSizes = attrPattern("sizes")
)

// charRef matches one character reference. A named one needs its semicolon,
// since in an attribute a browser leaves the "&region" of "?a=1&region=eu"
// alone where html.UnescapeString would read it as "&reg".
var charRef = regexp.MustCompile(`&(?:#[0-9]+;?|#[xX][0-9a-fA-F]+;?|[A-Za-z][A-Za-z0-9]*;)`)

// attrPattern matches one attribute, quoted either way or bare as minified
// pages write it.
func attrPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)\s` + name + `\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
}

// attrValue returns the attribute re finds in tag with its character
// references decoded, such as the &#x2F; rapidb.it writes for every slash.
func attrValue(re *regexp.Regexp, tag []byte) string {
	m := re.FindSubmatch(tag)
	if m == nil {
		return ""
	}
	for _, v := range m[1:] {
		if v != nil {
			return charRef.ReplaceAllStringFunc(string(v), html.UnescapeString)
		}
	}
	return ""
}

// declaredIcons returns the icons a page declares, largest first, and the URL
// it was finally served from. An error means the page did not answer at all;
// an error status or a body that is not HTML declares nothing.
func declaredIcons(ctx context.Context, pageURL string) ([]iconCandidate, *url.URL, error) {
	resp, err := iconGet(ctx, pageURL, "text/html,application/xhtml+xml")
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	// Relative hrefs and the well-known paths resolve against where the page
	// came from after redirects.
	base := resp.Request.URL
	if resp.StatusCode != http.StatusOK {
		return nil, base, nil
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		return nil, base, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, iconHTMLMaxBytes))
	if err != nil {
		return nil, base, nil
	}

	var (
		out      []iconCandidate
		manifest string
	)
	for _, tag := range linkTag.FindAll(body, -1) {
		abs := resolveIconURL(base, attrValue(attrHref, tag))
		if abs == "" {
			continue
		}
		rel := strings.ToLower(attrValue(attrRel, tag))
		switch {
		case slices.Contains(strings.Fields(rel), "manifest"):
			manifest = abs
		// A mask-icon is Safari's one-colour silhouette for tinting.
		case strings.Contains(rel, "icon") && !strings.Contains(rel, "mask-icon"):
			size := largestSize(attrValue(attrSizes, tag))
			// An apple-touch-icon is 180x180 whether or not it says so.
			if size == 0 && strings.Contains(rel, "apple-touch") {
				size = 180
			}
			out = append(out, iconCandidate{url: abs, declared: size})
		}
	}
	largestFirst(out)
	if manifest != "" {
		out = append(out, manifestIcons(ctx, manifest)...)
	}
	return out, base, nil
}

// manifestIcons returns the icons a web app manifest lists, largest first. It
// is the only place some single-page apps name a working icon (alsscan.com).
func manifestIcons(ctx context.Context, manifestURL string) []iconCandidate {
	resp, err := iconGet(ctx, manifestURL, "application/manifest+json,application/json")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var m struct {
		Icons []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, iconHTMLMaxBytes)).Decode(&m); err != nil {
		return nil
	}
	var out []iconCandidate
	for _, ic := range m.Icons {
		// A monochrome icon is a mask for the system to tint.
		if strings.Contains(ic.Purpose, "monochrome") {
			continue
		}
		if abs := resolveIconURL(resp.Request.URL, ic.Src); abs != "" {
			out = append(out, iconCandidate{url: abs, declared: largestSize(ic.Sizes)})
		}
	}
	largestFirst(out)
	return out
}

// largestSize returns the largest width in a sizes value such as "16x16
// 32x32", or 0 for "any" and anything unreadable.
func largestSize(sizes string) int {
	best := 0
	for _, s := range strings.Fields(strings.ToLower(sizes)) {
		w, _, _ := strings.Cut(s, "x")
		if n, err := strconv.Atoi(w); err == nil && n > best {
			best = n
		}
	}
	return best
}

// largestFirst orders candidates by declared size and keeps the page's order
// among equals.
func largestFirst(cs []iconCandidate) {
	slices.SortStableFunc(cs, func(a, b iconCandidate) int { return cmp.Compare(b.declared, a.declared) })
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

// iconGet sends a GET through iconClient with a browser's User-Agent.
func iconGet(ctx context.Context, rawURL, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", iconUserAgent)
	return iconClient.Do(req)
}

// svgRoot matches the start of an SVG document: an optional byte order mark,
// any declarations, comments or doctype, then the <svg> root element.
var svgRoot = regexp.MustCompile(`^\x{FEFF}?(?:\s*<[?!][^>]*>)*\s*<svg[\s/>]`)

func fetchIconAt(ctx context.Context, rawURL string) ([]byte, string, error) {
	res, err := iconGet(ctx, rawURL, "image/*,*/*;q=0.8")
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
	// The bytes decide the type, not the header: hosts send icons as
	// application/octet-stream or a GIF as image/x-icon (gigapeta.com), and an
	// error page labelled image/svg+xml would end the search as an SVG.
	ct, _, _ := strings.Cut(http.DetectContentType(body), ";")
	// DetectContentType calls an SVG text/xml or text/plain.
	if svgRoot.Match(body) {
		ct = "image/svg+xml"
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
	switch contentType {
	case "image/svg+xml":
		// Sharp at any size, so good enough, but no better: at 18 pixels a
		// vector adds nothing over a large raster found first, and a site's SVG
		// often recolours itself for the browser's dark mode rather than for
		// this page.
		return iconGoodEnough
	case "image/x-icon":
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
