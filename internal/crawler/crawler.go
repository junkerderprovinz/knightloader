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

	"github.com/junkerderprovinz/knightloader/internal/httpx"
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
	// Title is what the page called itself, repeated on every result from that
	// page. It is a page-level fact riding on a per-link struct because Crawl
	// answers with a flat slice and there is nowhere else for it to sit — and
	// widening the Crawler interface to carry it would break every site-specific
	// crawler and every stand-in a test has written.
	//
	// It exists so a batch of links crawled off one page can be named after the
	// page. The alternative is a URL segment, which on listing pages is "index",
	// "download" or a bare number for a good half of the web.
	Title string
	// Size is the byte count the source already stated, 0 when it stated none.
	//
	// The HTML crawler never fills it - an anchor on a page says nothing
	// trustworthy about how large the file behind it is - but a remote
	// directory listing does (internal/resolver/remotefs), and it is the same
	// answer the collector would otherwise have to make a second round trip to
	// learn. 0 is "not stated", never "an empty file": the staging path applies
	// it as a hint that a real resolve is still free to replace.
	Size int64
}

// Crawler turns a page into the links it points at.
//
// Crawl deliberately takes no options. A site-specific crawler knows its own
// site and has nothing to tune, and every stand-in a test has written
// implements exactly these three methods - widening this interface to carry a
// depth would break all of them to serve the one generic crawler that has any
// use for it. That one implements DeepCrawler (walk.go) alongside this, and a
// caller asks for it with a type assertion.
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

	// defaultMaxLinks caps what a single crawl may produce. A link farm that
	// emits a hundred thousand anchors would otherwise turn one paste into a
	// task list nobody can undo.
	//
	// It is the budget for the WHOLE walk, not for each page in it. Applied per
	// page it would multiply by the page cap, and a depth-3 crawl of twenty
	// dense pages is forty thousand tasks - the same list nobody can undo, one
	// multiplication further along.
	defaultMaxLinks = 2000

	// defaultTimeout bounds ONE page fetch, not a whole walk - see fetch for
	// why the two are different budgets. A crawl that hangs forever pins the
	// worker that started it, and a page that takes this long to answer is not
	// going to produce a usable link list.
	defaultTimeout = 30 * time.Second

	// maxRedirects bounds the hop chain. An unbounded chain is a trivial way to
	// send a crawler in circles, and a legitimate page never needs this many.
	maxRedirects = 5

	// maxTitleRunes caps the page title. Nothing stops a page declaring a title
	// the length of its body, and the title is copied onto every result — so an
	// uncapped one turns a two-thousand-link crawl into megabytes of the same
	// sentence. Counted in runes, because cutting UTF-8 by byte produces a title
	// ending in a broken glyph.
	maxTitleRunes = 200

	// userAgent is sent because a fair number of hosts answer Go's default
	// agent with a 403, which would look like a dead page rather than a refusal.
	userAgent = "Mozilla/5.0 (compatible; KnightLoader; +https://github.com/junkerderprovinz/knightloader)"
)

// defaultClient is shared so crawls reuse connections, and is only reached when
// no client was injected. Both bounds it carries are guard rails rather than
// tuning: see defaultTimeout and maxRedirects.
//
// It comes from httpx rather than being assembled here. The hop cap was the
// only rule this file used to enforce, and a crawl follows redirects chosen by
// a page somebody pasted - which is the exact shape that wants the rest of the
// policy too, above all the one that stops a credential following a hop onto a
// host it was never meant for.
var defaultClient = httpx.New(httpx.Options{
	Timeout:      defaultTimeout,
	MaxRedirects: maxRedirects,
})

// HTML is the generic crawler: it fetches a page and collects the links that
// look like files. It claims every http(s) URL, so it sits at the bottom of the
// priority list and only runs when no site-specific crawler wanted the page.
type HTML struct {
	Client *http.Client // nil means a default with a sane timeout
	// MaxLinks caps what one crawl can produce; zero means a default.
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
//
// It is CrawlDeep with the zero Options, which is one page and no filtering:
// the behaviour this crawler had before a walk existed, kept as the answer any
// caller gets who never asked for anything else.
func (h HTML) Crawl(ctx context.Context, raw string) ([]Result, error) {
	return h.CrawlDeep(ctx, raw, Options{})
}

// fetched is one page as it came back.
type fetched struct {
	// base is where the page actually came from, which after a redirect is not
	// where we asked. Relative links resolve against it.
	base *url.URL
	body []byte
	// file is set when the response was not HTML, in which case body was never
	// read: whatever sits at this URL, it is the download itself.
	file bool
}

// fetch gets one page, refusing anything that is not a successful response.
func (h HTML) fetch(ctx context.Context, raw string) (fetched, error) {
	// One deadline per request rather than one for the whole run. Thirty
	// seconds means "this host is not answering", not "this crawl has gone on
	// long enough", and a twenty-page walk sharing a single 30-second budget
	// would abandon page four of a slow but perfectly healthy site.
	//
	// The caller's context still bounds the run as a whole, because a derived
	// timeout can only ever shorten one: that is what the app's own budget and
	// the abort button in the status strip both rely on. It is also why the
	// old "only if the caller set no deadline" test is gone - a caller-supplied
	// client may carry no timeout of its own, so this guard rail must not
	// depend on how HTML was configured or on what the caller passed.
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return fetched{}, fmt.Errorf("crawler: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")

	resp, err := h.client().Do(req)
	if err != nil {
		return fetched{}, fmt.Errorf("crawler: fetch %s: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fetched{}, fmt.Errorf("crawler: fetch %s: %s", raw, resp.Status)
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
		return fetched{base: base, file: true}, nil
	}

	body, err := readCapped(resp)
	if err != nil {
		return fetched{}, err
	}
	return fetched{base: base, body: body}, nil
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

// scanned is what one document yielded: the links that look like files, the
// links that look like more pages, and what the document called itself.
type scanned struct {
	files []Result
	// pages are the anchors that are not files. They are only ever fetched by a
	// walk deeper than one page; a plain Crawl throws them away, which is
	// exactly what it did before they were collected at all.
	pages []*url.URL
	title string
}

// scan walks the document once, in order, splitting the links it finds. Order
// is preserved because a listing page is usually already sorted the way the
// user expects the downloads to queue.
//
// maxFiles and maxPages are what the caller still has room for, so a page with
// a hundred thousand anchors is abandoned at the budget instead of being
// collected in full and truncated afterwards.
func scan(base *url.URL, body []byte, maxFiles, maxPages int) (scanned, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return scanned{}, fmt.Errorf("crawler: parse %s: %w", base, err)
	}

	// Non-nil even when nothing matches: "no links here" is an empty list, not
	// a missing one, and callers range over it either way.
	out := scanned{files: make([]Result, 0, 16), title: pageTitle(doc)}
	// Per document, not per walk: the walk keeps its own set across pages (see
	// walk.seen), and this one only stops the same anchor being counted twice
	// against the budgets below.
	seen := make(map[string]bool)
	full := func() bool { return len(out.files) >= maxFiles && len(out.pages) >= maxPages }

	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if full() {
			return
		}
		if n.Type == html.ElementNode {
			switch r, u, kind := classify(base, n); kind {
			case kindFile:
				if !seen[r.URL] && len(out.files) < maxFiles {
					seen[r.URL] = true
					r.Title = out.title
					out.files = append(out.files, r)
				}
			case kindPage:
				if s := u.String(); !seen[s] && len(out.pages) < maxPages {
					seen[s] = true
					out.pages = append(out.pages, u)
				}
			}
		}
		for c := n.FirstChild; c != nil && !full(); c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return out, nil
}

// linkKind is what one element turned out to be.
type linkKind int

const (
	kindNone linkKind = iota // not a link this crawler can use at all
	kindFile                 // a download
	kindPage                 // another page, worth following only in a deep walk
)

// classify turns a single element into a Result, into a page to follow, or into
// neither.
func classify(base *url.URL, n *html.Node) (Result, *url.URL, linkKind) {
	switch n.Data {
	case "a":
		u, ok := absolute(base, attr(n, "href"))
		if !ok {
			return Result{}, nil, kindNone
		}
		if !fileLink(u) {
			// Everything an anchor points at that is not a file is treated as a
			// page. That is broader than "it ends in .html" on purpose: half the
			// listing pages on the web are /thread/1234 or ?page=2 with no
			// extension at all, and a rule that needed one would follow nothing
			// on exactly the sites a deep crawl exists for. What it costs is
			// bounded by the page cap, which is what that cap is for.
			return Result{}, u, kindPage
		}
		// The anchor text is what the page called the file, which beats a
		// cryptic URL segment; the file name is only the fallback.
		name := text(n)
		if name == "" {
			name = fileName(u)
		}
		return Result{URL: u.String(), Name: name}, u, kindFile

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
			return Result{}, nil, kindNone
		}
		return Result{URL: u.String(), Name: fileName(u)}, u, kindFile
	}
	return Result{}, nil, kindNone
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

// pageTitle is what the document calls itself, whitespace collapsed and capped.
//
// Only a <title> directly inside <head> counts. SVG has an element of the same
// name, and an inline icon in a page's navigation would otherwise name the whole
// crawl after whatever its designer wrote in there.
func pageTitle(doc *html.Node) string {
	var found string
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "title" &&
			n.Parent != nil && n.Parent.Type == html.ElementNode && n.Parent.Data == "head" {
			found = text(n)
			return true
		}
		// The body is never entered: html.Parse always builds a head, so a title
		// that exists has been passed before the first body node, and walking a
		// whole listing page looking for one that is not there costs a second full
		// traversal of the document on every crawl.
		if n.Type == html.ElementNode && n.Data == "body" {
			return false
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(doc)
	if r := []rune(found); len(r) > maxTitleRunes {
		found = strings.TrimSpace(string(r[:maxTitleRunes]))
	}
	return found
}

// fileName is the last path segment of a URL, or "" when there is none.
func fileName(u *url.URL) string {
	b := path.Base(u.Path)
	if b == "/" || b == "." || b == ".." {
		return ""
	}
	return b
}
