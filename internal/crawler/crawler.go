// Package crawler turns one page URL into the file links it points at, so a
// gallery, an "index of" listing or a link-list page becomes many tasks
// rather than one. A resolver then takes each link.
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

// Info identifies a crawler and sets its routing priority (higher wins), as
// resolver.Info does for resolvers.
type Info struct {
	ID   string
	Prio int
}

// Result is one link a crawl found, with whatever the page said about it.
type Result struct {
	URL  string
	Name string // link text or the file name from the URL, may be empty
	// Title is what the page called itself, repeated on every result from
	// that page, so a batch can be named after the page rather than a URL
	// segment such as "index".
	Title string
	// Size is the byte count the source stated, or 0 when it stated none. The
	// HTML crawler never sets it; a remote directory listing does. It is a
	// hint that a real resolve may replace.
	Size int64
}

// Crawler turns a page into the links it points at. It takes no options; the
// one crawler that can walk deeper also implements DeepCrawler, and callers
// ask for that with a type assertion.
type Crawler interface {
	Info() Info
	Match(url string) bool
	Crawl(ctx context.Context, url string) ([]Result, error)
}

// ErrPageTooLarge is returned when a page exceeds the buffering cap, so a
// caller can skip the link instead of retrying it.
var ErrPageTooLarge = errors.New("crawler: page too large")

const (
	// maxBodyBytes caps how much of a page is buffered. No real listing page
	// comes near it.
	maxBodyBytes = 8 << 20

	// defaultMaxLinks caps what one crawl may produce across the whole walk,
	// so a link farm cannot turn one paste into a list nobody can undo.
	defaultMaxLinks = 2000

	// defaultTimeout bounds one page fetch, not a whole walk.
	defaultTimeout = 30 * time.Second

	// maxRedirects bounds the redirect chain.
	maxRedirects = 5

	// maxTitleRunes caps the page title, which is copied onto every result.
	// It counts runes so the cut does not split a character.
	maxTitleRunes = 200

	// userAgent is sent because many hosts answer Go's default agent with a
	// 403, which would look like a dead page.
	userAgent = "Mozilla/5.0 (compatible; KnightLoader; +https://github.com/junkerderprovinz/knightloader)"
)

// defaultClient is shared so crawls reuse connections. It uses the httpx
// policy because a crawl follows redirects chosen by a pasted page, and httpx
// keeps credentials from following a hop to another host.
var defaultClient = httpx.New(httpx.Options{
	Timeout:      defaultTimeout,
	MaxRedirects: maxRedirects,
})

// HTML is the generic crawler: it fetches a page and collects the links that
// look like files. It claims every http(s) URL, so it has the lowest priority
// and runs only when no site-specific crawler wants the page.
type HTML struct {
	Client *http.Client // nil means a default with a sane timeout
	// MaxLinks caps what one crawl can produce; zero means a default.
	MaxLinks int
}

var _ Crawler = HTML{}

func (HTML) Info() Info { return Info{ID: "html", Prio: -100} }

// Match accepts any http(s) URL with a host.
func (HTML) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

// Crawl fetches the page and returns the file links it points at, in
// document order and deduplicated. A response that is not HTML comes back as
// the single file it is. It is CrawlDeep with the zero Options.
func (h HTML) Crawl(ctx context.Context, raw string) ([]Result, error) {
	return h.CrawlDeep(ctx, raw, Options{})
}

// fetched is one page as it came back.
type fetched struct {
	// base is where the page came from after redirects; relative links
	// resolve against it.
	base *url.URL
	body []byte
	// file is set when the response was not HTML. body is then unread, since
	// the URL is the download itself.
	file bool
}

// fetch gets one page, refusing anything that is not a successful response.
func (h HTML) fetch(ctx context.Context, raw string) (fetched, error) {
	// Each request gets its own deadline, so a slow but healthy site does not
	// exhaust a shared budget. The caller's context still bounds the whole
	// run, and the deadline applies even to an injected client without a
	// timeout.
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

	base := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		base = resp.Request.URL
	}

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
	if resp.ContentLength > maxBodyBytes {
		return nil, fmt.Errorf("%w: %d bytes declared", ErrPageTooLarge, resp.ContentLength)
	}
	// Chunked responses declare no length. Reading one byte past the cap and
	// leaving the rest tears the connection down rather than draining it.
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("crawler: read page: %w", err)
	}
	if len(buf) > maxBodyBytes {
		return nil, fmt.Errorf("%w: over %d bytes", ErrPageTooLarge, maxBodyBytes)
	}
	return buf, nil
}

// isHTML reports whether the content type is worth parsing as a page. A
// missing type counts as not HTML.
func isHTML(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "text/html" || mt == "application/xhtml+xml"
}

// scanned is what one document yielded.
type scanned struct {
	files []Result
	// pages are the anchors that are not files, followed only by a deeper
	// walk.
	pages []*url.URL
	title string
}

// scan walks the document once, in order, since a listing is usually sorted
// the way the user expects the downloads to queue. maxFiles and maxPages are
// what the caller still has room for, so a huge page is abandoned at the
// budget rather than collected in full.
func scan(base *url.URL, body []byte, maxFiles, maxPages int) (scanned, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return scanned{}, fmt.Errorf("crawler: parse %s: %w", base, err)
	}

	out := scanned{files: make([]Result, 0, 16), title: pageTitle(doc)}
	// Per document; the walk keeps its own set across pages.
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

type linkKind int

const (
	kindNone linkKind = iota // not a link this crawler can use at all
	kindFile                 // a download
	kindPage                 // another page, worth following only in a deep walk
)

// classify turns a single element into a Result, a page to follow, or
// neither.
func classify(base *url.URL, n *html.Node) (Result, *url.URL, linkKind) {
	switch n.Data {
	case "a":
		u, ok := absolute(base, attr(n, "href"))
		if !ok {
			return Result{}, nil, kindNone
		}
		if !fileLink(u) {
			// Anything that is not a file counts as a page, since many listing
			// pages are /thread/1234 or ?page=2 without an extension. The page
			// cap bounds the cost.
			return Result{}, u, kindPage
		}
		// The anchor text usually names the file better than the URL does.
		name := text(n)
		if name == "" {
			name = fileName(u)
		}
		return Result{URL: u.String(), Name: name}, u, kindFile

	case "video", "audio", "source":
		// Media sources count as files whatever their URL ends in. <img> is
		// left out: every page has images, and none is what the link was
		// pasted for.
		u, ok := absolute(base, attr(n, "src"))
		if !ok {
			return Result{}, nil, kindNone
		}
		return Result{URL: u.String(), Name: fileName(u)}, u, kindFile
	}
	return Result{}, nil, kindNone
}

// fileLink reports whether an anchor names a file rather than a page, using
// the direct resolver's rule so crawled and pasted links are treated alike.
func fileLink(u *url.URL) bool { return (resolver.Direct{}).Match(u.String()) }

// absolute resolves a reference against the page URL and rejects anything the
// engine could not fetch.
func absolute(base *url.URL, ref string) (*url.URL, bool) {
	ref = strings.TrimSpace(ref)
	// A bare fragment is a jump within the page and would otherwise resolve
	// to the page URL.
	if ref == "" || strings.HasPrefix(ref, "#") {
		return nil, false
	}
	u, err := base.Parse(ref)
	if err != nil {
		return nil, false
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, false
	}
	// Drop the fragment so file.zip and file.zip#top deduplicate.
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

// text collects the visible text under a node with its whitespace collapsed,
// so a listing's table layout does not end up in the task name.
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

// pageTitle returns the document's <title>, whitespace collapsed and capped.
// Only a title directly inside <head> counts, since SVG icons have a <title>
// element too.
func pageTitle(doc *html.Node) string {
	var found string
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "title" &&
			n.Parent != nil && n.Parent.Type == html.ElementNode && n.Parent.Data == "head" {
			found = text(n)
			return true
		}
		// html.Parse always builds a head before the body, so the body never
		// needs walking.
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
