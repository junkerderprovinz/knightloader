// Package crawler turns one page URL into the many file links it points at.
// It exists because resolver.Result describes exactly one file: without a crawl
// step a gallery, an "index of" listing or a link-list page could only ever
// become a single task. This is the step JDownloader users mean when they say
// the LinkGrabber crawled a page; a resolver then takes each link from here.
package crawler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"golang.org/x/net/html"
)

// Info identifies a crawler and sets its routing priority (higher wins). It
// mirrors resolver.Info so a site-specific crawler can outrank the generic HTML
// one the same way a hoster resolver outranks the direct downloader.
type Info struct {
	ID   string
	Prio int
}

// Result is one link a crawl found, with whatever the page said about it.
type Result struct {
	URL  string
	Name string // link text or the file name from the URL, may be empty
}

// Crawler turns a page into the links it points at.
type Crawler interface {
	Info() Info
	Match(url string) bool
	Crawl(ctx context.Context, url string) ([]Result, error)
}

// ErrPageTooLarge is returned when a page exceeds the buffering cap. It is a
// sentinel so a caller can tell "this host served us something absurd" apart
// from an ordinary network failure and skip the link instead of retrying it.
var ErrPageTooLarge = errors.New("crawler: page too large")

const (
	// maxBodyBytes caps how much of a page is ever buffered. A crawler that
	// streams a 4 GB "page" into memory is a denial of service against its own
	// host, and no genuine listing page comes anywhere near this size.
	maxBodyBytes = 8 << 20

	// defaultMaxLinks caps what a single page may produce. A link farm that
	// emits a hundred thousand anchors would otherwise turn one paste into a
	// task list nobody can undo.
	defaultMaxLinks = 2000

	// defaultTimeout bounds the whole fetch. A crawl that hangs forever pins
	// the worker that started it, and a page that takes this long to answer is
	// not going to produce a usable link list.
	defaultTimeout = 30 * time.Second

	// maxRedirects bounds the hop chain. An unbounded chain is a trivial way to
	// send a crawler in circles, and a legitimate page never needs this many.
	maxRedirects = 5

	// userAgent is sent because a fair number of hosts answer Go's default
	// agent with a 403, which would look like a dead page rather than a refusal.
	userAgent = "Mozilla/5.0 (compatible; KnightLoader; +https://github.com/junkerderprovinz/knightloader)"
)

// defaultClient is shared so crawls reuse connections. Both bounds it carries
// are guard rails rather than tuning: see defaultTimeout and maxRedirects.
var defaultClient = &http.Client{
	Timeout: defaultTimeout,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	},
}

// HTML is the generic crawler: it fetches a page and collects the links that
// look like files. It claims every http(s) URL, so it sits at the bottom of the
// priority list and only runs when no site-specific crawler wanted the page.
type HTML struct {
	Client *http.Client // nil means a default with a sane timeout
	// MaxLinks caps what one page can produce; zero means a default.
	MaxLinks int
}

// Info reports the generic crawler's ID and its deliberately low priority.
// HTML has to satisfy Crawler; asserting it here fails the build rather than a
// test if the interface and the implementation ever drift apart.
var _ Crawler = HTML{}

func (HTML) Info() Info { return Info{ID: "html", Prio: -100} }

// Match accepts any http(s) URL with a host. Everything else — mailto, magnet,
// data, ftp, a bare file path — is not something this crawler can fetch, and
// claiming it would only produce a confusing error much later.
func (HTML) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

// Crawl fetches the page and returns the file links it points at, in document
// order and deduplicated. A response that is not HTML is not a page at all, so
// it comes back as the single result it is.
func (h HTML) Crawl(ctx context.Context, raw string) ([]Result, error) {
	if !h.Match(raw) {
		return nil, fmt.Errorf("crawler: not an http(s) url: %q", raw)
	}

	// A caller-supplied client may carry no timeout of its own, so the deadline
	// is enforced here too; the guard rail must not depend on how HTML was
	// configured.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("crawler: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")

	resp, err := h.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("crawler: fetch %s: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("crawler: fetch %s: %s", raw, resp.Status)
	}

	// Redirects can land somewhere else entirely, so relative links have to
	// resolve against where the page actually came from, not where we asked.
	base := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		base = resp.Request.URL
	}

	// Only HTML is worth parsing. Anything else was a file all along, which is
	// also why the size cap below never applies to it: the body is not read.
	if !isHTML(resp.Header.Get("Content-Type")) {
		return []Result{{URL: base.String(), Name: fileName(base)}}, nil
	}

	body, err := readCapped(resp)
	if err != nil {
		return nil, err
	}
	return h.collect(base, body)
}

func (h HTML) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return defaultClient
}

// readCapped buffers the response body, refusing anything past maxBodyBytes.
func readCapped(resp *http.Response) ([]byte, error) {
	// A declared length over the cap is refused before a single byte of body is
	// read; there is no reason to pull the whole thing down to learn that.
	if resp.ContentLength > maxBodyBytes {
		return nil, fmt.Errorf("%w: %d bytes declared", ErrPageTooLarge, resp.ContentLength)
	}
	// Chunked responses declare no length, so the read is bounded as well. One
	// byte over the cap and the page is refused with the remainder left unread,
	// which tears the connection down instead of politely draining gigabytes.
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("crawler: read page: %w", err)
	}
	if len(buf) > maxBodyBytes {
		return nil, fmt.Errorf("%w: over %d bytes", ErrPageTooLarge, maxBodyBytes)
	}
	return buf, nil
}

// isHTML reports whether the content type is something worth parsing as a page.
// A missing type counts as not-HTML: guessing wrong turns a file into a parse.
func isHTML(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "text/html" || mt == "application/xhtml+xml"
}

// collect walks the document once, in order, gathering the links that look like
// files. Order is preserved because a listing page is usually already sorted
// the way the user expects the downloads to queue.
func (h HTML) collect(base *url.URL, body []byte) ([]Result, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("crawler: parse %s: %w", base, err)
	}

	limit := h.MaxLinks
	if limit <= 0 {
		limit = defaultMaxLinks
	}

	// Non-nil even when nothing matches: "no links here" is an empty list, not
	// a missing one, and callers range over it either way.
	out := make([]Result, 0, 16)
	seen := make(map[string]bool)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(out) >= limit {
			return
		}
		if n.Type == html.ElementNode {
			if r, ok := link(base, n); ok && !seen[r.URL] {
				seen[r.URL] = true
				out = append(out, r)
			}
		}
		for c := n.FirstChild; c != nil && len(out) < limit; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out, nil
}

// link turns a single element into a Result, or reports that it is not a link
// to a file.
func link(base *url.URL, n *html.Node) (Result, bool) {
	switch n.Data {
	case "a":
		u, ok := absolute(base, attr(n, "href"))
		if !ok || !fileLink(u) {
			return Result{}, false
		}
		// The anchor text is what the page called the file, which beats a
		// cryptic URL segment; the file name is only the fallback.
		name := text(n)
		if name == "" {
			name = fileName(u)
		}
		return Result{URL: u.String(), Name: name}, true

	case "video", "audio", "source":
		// Media sources are taken at face value rather than run through the
		// file-extension rule: a <video src> is the file whether or not its URL
		// happens to end in .mp4, and streaming hosts routinely serve these
		// from extensionless, query-driven paths.
		//
		// <img> is deliberately NOT in this list. Every page has images, and
		// none of them is what anyone pasted a link for: collecting them turns
		// an ordinary hoster page into a pile of logos, sprites and tracking
		// pixels while the real link is pushed out of the way.
		u, ok := absolute(base, attr(n, "src"))
		if !ok {
			return Result{}, false
		}
		return Result{URL: u.String(), Name: fileName(u)}, true
	}
	return Result{}, false
}

// fileLink reports whether an anchor target names a file rather than another
// page. The rule lives in the direct resolver, and reusing it keeps a link the
// crawler collects and a link the user pastes by hand on the same footing.
func fileLink(u *url.URL) bool { return (resolver.Direct{}).Match(u.String()) }

// absolute resolves a reference against the page URL and rejects everything the
// engine could not fetch afterwards.
func absolute(base *url.URL, ref string) (*url.URL, bool) {
	ref = strings.TrimSpace(ref)
	// A bare fragment is a jump inside the same page, so it is dropped before
	// resolution: otherwise it would inherit the page URL and look like a hit.
	if ref == "" || strings.HasPrefix(ref, "#") {
		return nil, false
	}
	u, err := base.Parse(ref)
	if err != nil {
		return nil, false
	}
	// Resolution happily yields mailto:, javascript:, data: and ftp: targets,
	// none of which are downloads.
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, false
	}
	// The fragment is not part of a file's identity; keeping it would let
	// file.zip and file.zip#top survive deduplication as two downloads.
	u.Fragment, u.RawFragment = "", ""
	return u, true
}

// attr returns the value of an attribute, or "" if the element has none.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// text collects the visible text under a node with its whitespace collapsed.
// Listing pages wrap link text across lines and pad it into columns, so the raw
// text node would carry the table layout into the task name.
func text(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

// fileName is the last path segment of a URL, or "" when there is none.
func fileName(u *url.URL) string {
	b := path.Base(u.Path)
	if b == "/" || b == "." || b == ".." {
		return ""
	}
	return b
}
