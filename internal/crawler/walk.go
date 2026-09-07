package crawler

// The multi-page walk: one pasted address becomes the files on that page AND on
// the pages below it, down to a depth the user chose.
//
// It exists because a collection thread is a table of contents. The single-page
// crawl next door turns that table into nothing at all - none of its entries is
// a file - so twenty subpages meant twenty rounds of copy, paste, wait.
//
// Everything here is off by default. The zero Options is the single page this
// package fetched before the walk was written, byte for byte, because a build
// that suddenly followed links on its own would be a three-level crawl of
// somebody's forum that nobody asked for.

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
)

const (
	// MaxDepth is the deepest walk this crawler will do. Three is not a round
	// number: one is a page, two is a page and its subpages, three is the
	// "index -> season -> episode" shape that collection threads actually have.
	// A fourth level multiplies the request count by the average link-out of an
	// arbitrary page, which is where a link grabber stops being one and becomes
	// a spider pointed at somebody else's server.
	MaxDepth = 3

	// DefaultMaxPages is the page budget when the caller names none. Twenty
	// covers the thread-with-subpages case that motivated the walk and is small
	// enough that a wrong pattern costs seconds, not an afternoon.
	DefaultMaxPages = 20

	// MaxWalkPages is the ceiling on the page budget whatever the caller asks
	// for. Two hundred requests fired at one host from a single paste is
	// already at the edge of what a link grabber may do without being asked
	// twice; past it, the honest description of the program has changed.
	MaxWalkPages = 200
)

// Options tunes a crawl beyond the one page HTML has always fetched.
//
// The zero value is that single page, with no filtering and no host rule. It is
// what a caller that has never heard of this struct gets, and what an install
// that never opens the crawl settings keeps.
type Options struct {
	// Depth is how many pages deep the walk goes. 0 and 1 both mean the seed
	// page alone, 2 follows the pages it links to, 3 follows theirs.
	//
	// A value above MaxDepth is clamped rather than refused. The number arrives
	// from a settings file that another build, or a hand edit, may have
	// written, and refusing the crawl over it would turn one bad integer into
	// "pasting pages stopped working".
	Depth int

	// MaxPages caps how many pages the walk FETCHES, and counts requests rather
	// than links found.
	//
	// That is the decision, and it is deliberate: this cap exists to bound what
	// the crawl does to somebody else's server, and a request is the unit of
	// that. Counting links instead would make one number mean five requests on
	// a sparse site and five hundred on a dense one, which is a cap the person
	// setting it cannot reason about. What comes back is bounded separately and
	// already was, by MaxLinks (crawler.go).
	//
	// Zero means DefaultMaxPages; anything above MaxWalkPages is clamped to it.
	MaxPages int

	// SameHost keeps the walk on the seed page's own host.
	//
	// Host, exactly, and not "the same site": working out that
	// forum.example.co.uk and www.example.co.uk are one site needs the public
	// suffix list, and the naive stand-in for it (compare the last two labels)
	// makes every *.github.io and every *.blogspot.com page one site - so the
	// first shortcut anyone reaches for is the one that walks off the user's
	// page into a stranger's. Exact host needs no list and can only ever be too
	// narrow, which for "fetch pages nobody vouched for" is the right side to
	// be wrong on. Subdomains are therefore NOT included: news.example.com is a
	// different host from www.example.com and the walk stops at the boundary.
	//
	// It governs which pages are FOLLOWED and never which files are kept. A
	// listing on example.com whose downloads sit on dl3.example-cdn.net is the
	// ordinary case, not the exception, so filtering the results by host would
	// make this setting silently produce nothing on the sites it is aimed at.
	SameHost bool

	// Include and Exclude are regular expressions matched against the whole
	// absolute URL. Empty lists mean no filtering, which is the default.
	//
	// They are not symmetric, and each half has its own reason:
	//
	// Exclude applies to pages AND to files. A pattern saying "not this" is an
	// instruction not to go near it, and a filter that only dropped results has
	// already sent the request to the address the user filtered out - the same
	// argument app_links.go makes for asking the link filter before the crawl
	// rather than only after it.
	//
	// Include applies to files only. An include pattern is written about the
	// downloads somebody wants (\.zip$, 1080p, S02E) and applying it to the
	// pages on the way there would cut the walk off at the first hop, so the
	// depth they set in the same settings block would silently do nothing.
	//
	// Go's regexp is RE2: no backtracking, linear in the input by construction.
	// That is why a pattern typed into a settings box is allowed to be a real
	// expression at all - the usual objection to user-supplied regexps is a
	// pattern that pins a core, and this engine has no such pattern.
	Include []string
	Exclude []string
}

// DeepCrawler is a Crawler that can also walk past the first page.
//
// Optional on purpose: a caller asks for it with a type assertion and falls
// back to plain Crawl, so a site-specific crawler and every test stand-in go on
// satisfying Crawler alone. See that interface's own comment.
type DeepCrawler interface {
	Crawler
	CrawlDeep(ctx context.Context, url string, opt Options) ([]Result, error)
}

// HTML is the one crawler in this package that can walk. Asserted here so a
// signature drift fails the build rather than quietly falling back to the
// single-page path at runtime, where nothing would report it.
var _ DeepCrawler = HTML{}

// CrawlDeep fetches the seed page and, at a depth above one, the pages it links
// to in turn, returning the file links found across all of them.
//
// Results come back breadth first: the seed page's own links in document order,
// then the pages it pointed at in the order it pointed at them. A listing is
// usually already sorted the way the user expects the downloads to queue, and
// depth-first ordering would bury the second half of that listing behind
// everything reachable from its first entry.
func (h HTML) CrawlDeep(ctx context.Context, raw string, opt Options) ([]Result, error) {
	if !h.Match(raw) {
		return nil, fmt.Errorf("crawler: not an http(s) url: %q", raw)
	}
	// Both pattern lists are compiled before a single request goes out. A
	// pattern that does not parse fails the crawl loudly instead of being
	// dropped: a filter that quietly is not there returns MORE links than the
	// user asked for, which is the failure nobody inspects.
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

// clamp folds a configured number into the range the walk can honour. Zero is
// treated as "the low end" rather than as an error for the reason Options.Depth
// gives: these numbers come from a file, not from a caller who can be told off.
func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// compile turns one pattern list into matchers, naming the list in the error so
// the log line says which box the bad pattern was typed into.
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

// walk is one run: the budgets, the filters, and what has already been seen.
// It is not safe for concurrent use and is never shared - a walk is created
// per CrawlDeep call and dies with it.
type walk struct {
	html     HTML
	inc, exc []*regexp.Regexp
	host     string
	sameHost bool
	depth    int
	pages    int
	links    int

	// seen is every result URL already returned, across the whole walk. Without
	// it the twenty pages of a thread that all link the same archive produce
	// twenty tasks for one file.
	seen map[string]bool
	// visited is every page already queued or fetched, across the whole walk.
	// It is the answer to the loop: page A links B, B links back to A, and a
	// walk without this set bounces between them until the page budget runs out
	// and reports the same handful of links over and over. A page is entered
	// into it when it is QUEUED, not when it is fetched, so two pages both
	// pointing at a third do not queue it twice.
	//
	// Keyed by the absolute URL with its fragment already removed (absolute
	// does that), and otherwise verbatim. Query strings are deliberately not
	// normalised: ?page=2 and ?page=2&sort=asc are genuinely different listings
	// on most forum software, and reordering or dropping keys to make two
	// spellings match is a guess that silently loses half a thread. The cost of
	// not guessing is fetching one page twice under two spellings, which the
	// page budget already bounds.
	visited map[string]bool
}

// queued is one page waiting to be fetched and how deep it sits.
type queued struct {
	url   *url.URL
	depth int
}

func (w *walk) run(ctx context.Context, seed *url.URL) ([]Result, error) {
	out := make([]Result, 0, 16)
	w.visited[seed.String()] = true
	queue := []queued{{url: seed, depth: 1}}
	// Counts pages ENTERED INTO THE QUEUE, which is the same number as pages
	// requested: everything queued is fetched unless the run is cancelled, and
	// everything skipped (wrong host, excluded, already visited) is skipped
	// before it gets here. Queuing what can never be fetched would also hold a
	// depth-3 walk's worth of URLs in memory for nothing.
	requested := 1

	for len(queue) > 0 {
		// Checked before each fetch rather than only inside the HTTP client, so
		// an abort between pages stops the run instead of paying for one more
		// page first.
		if err := ctx.Err(); err != nil {
			// Nothing is kept. A cancelled crawl means "as if it never ran",
			// the same answer AbortExtraction gives when it takes a half-written
			// extraction back off the disk: half a crawl staged as though it
			// were the whole one is a list somebody trusts to be complete.
			return nil, fmt.Errorf("crawler: crawl %s: %w", seed, err)
		}
		// Checked here rather than after the next page is parsed, so a full
		// list does not cost one more request at somebody else's host.
		if len(out) >= w.links {
			break
		}

		cur := queue[0]
		queue = queue[1:]

		page, err := w.html.fetch(ctx, cur.url.String())
		if err != nil {
			// An abort usually lands here rather than at the top of the loop:
			// the run spends nearly all of its time inside a request, so
			// cancelling it fails that request first. It is the whole walk
			// being called off and never "one bad page", so it must not take
			// the skip path below - which would let a cancelled crawl finish
			// its queue and hand back whatever it had.
			if cerr := ctx.Err(); cerr != nil {
				return nil, fmt.Errorf("crawler: crawl %s: %w", seed, cerr)
			}
			if cur.depth == 1 {
				// The seed is the address the user pasted. Its failure is the
				// answer to what they asked for and belongs in the caller's
				// error, exactly as it did before there was a walk.
				return nil, err
			}
			// A page the walk chose for itself is a different matter: one dead
			// subpage out of twenty must not throw away the nineteen that
			// answered. Logged rather than swallowed, because "the crawl found
			// less than it should have" is otherwise unanswerable afterwards.
			log.Printf("crawl %s: %v", cur.url, err)
			continue
		}

		// Where the page came from counts as visited too, not only where we
		// asked. A redirect means the two are different strings, and without
		// this a second page linking the redirect TARGET directly fetches the
		// same document again under its other spelling.
		w.visited[page.base.String()] = true

		if page.file {
			// Not HTML, so it was the download all along. At depth 1 this is
			// the "you pasted a file, not a page" case the single-page crawl
			// has always answered with the link itself. Deeper it is the
			// /download.php?id=7 shape: an anchor no extension rule can call a
			// file, which turns out to serve one.
			w.keep(&out, Result{URL: page.base.String(), Name: fileName(page.base)})
			continue
		}

		// Only ask the parser for what there is still room for. maxPages here
		// is the page budget, not the depth: a page whose links can never be
		// followed (the walk is as deep as it goes, or the budget is spent) is
		// scanned for files alone.
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

// keep adds one result unless it is a duplicate, is over the link budget, or
// the filters turned it down.
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

// wanted reports whether a file link passes both filters. See Options.Include
// for why include is asked here and not of the pages.
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
