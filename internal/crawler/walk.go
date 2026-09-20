package crawler

// The multi-page walk turns one pasted address into the files on that page
// and on the pages below it, down to a depth the user chose, so a collection
// thread's table of contents yields its downloads. The zero Options fetch the
// single page only.

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
)

const (
	// MaxDepth is the deepest walk: one page, its subpages, and theirs, the
	// "index, season, episode" shape of collection threads. A fourth level
	// turns a link grabber into a spider.
	MaxDepth = 3

	// DefaultMaxPages is the page budget when the caller names none.
	DefaultMaxPages = 20

	// MaxWalkPages caps the page budget whatever the caller asks for.
	MaxWalkPages = 200
)

// Options tunes a crawl beyond the single page. The zero value is that page,
// with no filtering and no host rule.
type Options struct {
	// Depth is how many pages deep the walk goes. 0 and 1 both mean the seed
	// page alone. A value above MaxDepth is clamped rather than refused, since
	// it comes from a settings file.
	Depth int

	// MaxPages caps how many pages the walk fetches. It counts requests, not
	// links found, because it bounds the load on somebody else's server;
	// results are bounded by MaxLinks. Zero means DefaultMaxPages, and values
	// above MaxWalkPages are clamped.
	MaxPages int

	// SameHost keeps the walk on the seed page's exact host. Matching "the
	// same site" would need the public suffix list, and the naive version
	// treats every *.github.io as one site; subdomains are therefore
	// excluded. It only limits which pages are followed, never which files
	// are kept, since downloads often sit on a CDN host.
	SameHost bool

	// Include and Exclude are regular expressions matched against the whole
	// absolute URL. Empty lists mean no filtering.
	//
	// Exclude applies to pages and files, so an excluded address is never
	// requested. Include applies to files only, because a pattern like
	// \.zip$ would otherwise stop the walk at the first page. Go's regexp
	// runs in linear time, so user-supplied patterns cannot pin a core.
	Include []string
	Exclude []string
}

// DeepCrawler is a Crawler that can also walk past the first page. Callers
// ask for it with a type assertion and fall back to Crawl.
type DeepCrawler interface {
	Crawler
	CrawlDeep(ctx context.Context, url string, opt Options) ([]Result, error)
}

var _ DeepCrawler = HTML{}

// CrawlDeep fetches the seed page and, at a depth above one, the pages it
// links to, returning the file links found across all of them. Results come
// back breadth first, so a sorted listing keeps its order.
func (h HTML) CrawlDeep(ctx context.Context, raw string, opt Options) ([]Result, error) {
	if !h.Match(raw) {
		return nil, fmt.Errorf("crawler: not an http(s) url: %q", raw)
	}
	// A bad pattern fails the crawl rather than being dropped, since a filter
	// that silently is not there returns more links than asked for.
	inc, err := compile("include", opt.Include)
	if err != nil {
		return nil, err
	}
	exc, err := compile("exclude", opt.Exclude)
	if err != nil {
		return nil, err
	}
	seed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("crawler: %w", err)
	}

	pages := DefaultMaxPages
	if opt.MaxPages > 0 {
		pages = clamp(opt.MaxPages, 1, MaxWalkPages)
	}
	links := defaultMaxLinks
	if h.MaxLinks > 0 {
		links = h.MaxLinks
	}
	w := &walk{
		html:     h,
		inc:      inc,
		exc:      exc,
		host:     strings.ToLower(seed.Hostname()),
		sameHost: opt.SameHost,
		depth:    clamp(opt.Depth, 1, MaxDepth),
		pages:    pages,
		links:    links,
		seen:     map[string]bool{},
		visited:  map[string]bool{},
	}
	return w.run(ctx, seed)
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// compile turns one pattern list into matchers, naming the list in the error.
func compile(list string, pats []string) ([]*regexp.Regexp, error) {
	if len(pats) == 0 {
		return nil, nil
	}
	out := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		if strings.TrimSpace(p) == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("crawler: %s pattern %q: %w", list, p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// walk is one CrawlDeep run. It is not safe for concurrent use.
type walk struct {
	html     HTML
	inc, exc []*regexp.Regexp
	host     string
	sameHost bool
	depth    int
	pages    int
	links    int

	// seen is every result URL already returned, so twenty pages linking the
	// same archive give one task.
	seen map[string]bool
	// visited is every page already queued or fetched, which breaks link
	// loops. A page is added when queued, so two pages pointing at a third do
	// not queue it twice. Query strings are not normalised, since ?page=2 and
	// ?page=2&sort=asc can be different listings.
	visited map[string]bool
}

type queued struct {
	url   *url.URL
	depth int
}

func (w *walk) run(ctx context.Context, seed *url.URL) ([]Result, error) {
	out := make([]Result, 0, 16)
	w.visited[seed.String()] = true
	queue := []queued{{url: seed, depth: 1}}
	// requested counts queued pages, which equals pages fetched: anything
	// that would be skipped is skipped before it is queued.
	requested := 1

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			// A cancelled crawl returns nothing, so a partial list is not
			// mistaken for a complete one.
			return nil, fmt.Errorf("crawler: crawl %s: %w", seed, err)
		}
		if len(out) >= w.links {
			break
		}

		cur := queue[0]
		queue = queue[1:]

		page, err := w.html.fetch(ctx, cur.url.String())
		if err != nil {
			// Cancellation usually surfaces as a failed request. It ends the
			// whole walk rather than skipping one page.
			if cerr := ctx.Err(); cerr != nil {
				return nil, fmt.Errorf("crawler: crawl %s: %w", seed, cerr)
			}
			if cur.depth == 1 {
				// The seed is what the user pasted, so its failure is the answer.
				return nil, err
			}
			// One dead subpage must not discard the others; log it so a short
			// result can be explained.
			log.Printf("crawl %s: %v", cur.url, err)
			continue
		}

		// Mark the redirect target visited too, or a page linking it directly
		// fetches the same document again.
		w.visited[page.base.String()] = true

		if page.file {
			// Not HTML, so this is the download itself: a pasted file at depth
			// 1, or a /download.php?id=7 style link deeper down.
			w.keep(&out, Result{URL: page.base.String(), Name: fileName(page.base)})
			continue
		}

		// A page at the depth limit, or with the page budget spent, is scanned
		// for files only.
		room := 0
		if cur.depth < w.depth {
			room = w.pages - requested
			if room < 0 {
				room = 0
			}
		}
		found, err := scan(page.base, page.body, w.links-len(out), room)
		if err != nil {
			if cur.depth == 1 {
				return nil, err
			}
			log.Printf("crawl %s: %v", cur.url, err)
			continue
		}
		for _, r := range found.files {
			w.keep(&out, r)
		}
		for _, next := range found.pages {
			if requested >= w.pages {
				break
			}
			if !w.follow(next) {
				continue
			}
			w.visited[next.String()] = true
			queue = append(queue, queued{url: next, depth: cur.depth + 1})
			requested++
		}
	}
	return out, nil
}

// keep adds one result unless it is a duplicate, over the link budget, or
// filtered out.
func (w *walk) keep(out *[]Result, r Result) {
	if len(*out) >= w.links || w.seen[r.URL] {
		return
	}
	if !w.wanted(r.URL) {
		return
	}
	w.seen[r.URL] = true
	*out = append(*out, r)
}

// wanted reports whether a file link passes both filters.
func (w *walk) wanted(u string) bool {
	if matchesAny(w.exc, u) {
		return false
	}
	return len(w.inc) == 0 || matchesAny(w.inc, u)
}

// follow reports whether a page may be fetched: not seen before, on the right
// host, and not excluded.
func (w *walk) follow(u *url.URL) bool {
	if w.visited[u.String()] {
		return false
	}
	if w.sameHost && !strings.EqualFold(u.Hostname(), w.host) {
		return false
	}
	return !matchesAny(w.exc, u.String())
}

func matchesAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
