package app

// Everything between a pasted string and a staged task: the entrance it came
// in by, the filter, the crawl, the Packagizer, the package it lands in, the
// mirror set, and the links that never made it.

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// The five entrances a link can arrive by.
//
// They are declared here rather than beside the type in core because core owns
// the type and nothing else: every one of these names a funnel in this file, and
// the only way the set stays honest is if adding an entrance means editing the
// same file that has to set the value. A link with no origin at all is the state
// this exists to end — "why is this here" is unanswerable weeks later, and a rule
// keyed on where something came from has nothing to read.
const (
	// OriginPaste is the collector's paste box, which is also what a bare
	// AddLinks means.
	OriginPaste core.Origin = "paste"
	// OriginCrawl is a link a page pointed at. The page itself is on Source.
	OriginCrawl core.Origin = "crawl"
	// OriginCnL is a Click'n'Load submission from a browser.
	OriginCnL core.Origin = "cnl"
	// OriginWatch is a job file dropped into the watched folder.
	OriginWatch core.Origin = "watch"
	// OriginContainer is a .dlc/.ccf/.rsdf/.txt container, whether it was read
	// here or opened by the JD backend on our behalf.
	OriginContainer core.Origin = "container"
)

// KnownOrigin turns an entrance a caller names into one of the five, and refuses
// anything else.
//
// It exists for the relays. A Click'n'Load bridge decodes a submission on the
// user's own desktop — because CnL is hard-wired to the browser's loopback and
// cannot reach a NAS — and then forwards it over the ordinary link route. The
// entrance is known to that bridge and to nobody downstream of it, so without a
// way to say so those links are filed as pasted: wrong precisely for the
// deployment the bridge exists to serve, and wrong in the one column somebody
// opens the holding area to read.
//
// An unrecognised value is refused rather than stored, because a free-text
// origin is a column that stops being answerable — which is the state the five
// constants above exist to end.
func KnownOrigin(s string) (core.Origin, bool) {
	switch o := core.Origin(strings.ToLower(strings.TrimSpace(s))); o {
	case OriginPaste, OriginCrawl, OriginCnL, OriginWatch, OriginContainer:
		return o, true
	}
	return "", false
}

// intake is what an entrance knows about the links it is handing over. It is a
// struct rather than four more parameters because stage is called from five
// places and a bare string in the fourth position is how "source" ended up
// carrying a package name once already.
type intake struct {
	pkg    string
	origin core.Origin
	// source is the page a crawl found the link on; empty for everything else.
	source string
	// waived is the reason the filter gave when it held this link, handed back in
	// by RestoreFiltered. Non-empty means the user has read that reason and
	// decided anyway, so the filter is not asked at staging time — and, because
	// the reason is kept on the task, not asked again at the queue either. See
	// filterWaived.
	waived string

	// priority, autoExtract and comment are the add-links form's own per-batch
	// options (§8A), carried from LinkBatchOptions through addLinksFrom into
	// every task the batch creates - including the ones a crawled page yields,
	// which is why they live here rather than being applied once after
	// addLinksFrom returns. stage writes them onto the task BEFORE
	// finishStaging runs the Packagizer, which is what makes a matching rule
	// win over them by default: packagize() already overwrites a field a rule
	// has an opinion about, exactly as it would for a plain paste with no form
	// involved at all. AddLinksWithOptions applies them a second time,
	// afterwards, when the form itself is meant to have the last word - see its
	// own comment for why Dir and the two passwords never come through here.
	priority    *int
	autoExtract *bool
	comment     string
}

// AddLinks stages links pasted into the collector. Every other entrance calls
// AddLinksFrom with an origin of its own; this is the paste box, and it keeps
// the short name because it is also what an unadorned "add these links" means.
func (a *App) AddLinks(urls []string, pkg string) []*core.Task {
	return a.AddLinksFrom(urls, pkg, OriginPaste)
}

// AddLinksFrom resolves each URL and stages it in the link collector (JD-style):
// tasks are created "collected" (analysed but not started). StartTasks moves
// them into the download queue.
//
// origin is written onto every task this creates. It is the difference between a
// list of links and a list of links you can account for: it answers "why is this
// here" without a memory of what happened last Tuesday, and it is what a rule
// keyed on the entrance has to read.
func (a *App) AddLinksFrom(urls []string, pkg string, origin core.Origin) []*core.Task {
	return a.detached(a.addLinksFrom(urls, pkg, origin, LinkBatchOptions{}))
}

// AddResolvedLinksFrom stages links whose name and, where known, size have
// already been found — a container's crawl (internal/resolver/jd's
// AddContainer/AddCryptedV1) learns both while opening the container, because
// opening it IS crawling it. Routing those links back through AddLinksFrom
// would throw that answer away and stage bare URLs instead, leaving the
// collector to show the raw link and no size until the user starts the
// download and JD crawls the very same links a second time.
//
// It skips crawl(): a link a container already named is a resolved file, not
// a page that might point at more of them, which page-crawling that link
// again would only risk mistaking it for. Everything else - the link filter,
// the packagizer, the duplicate check, batch naming and auto-confirm - runs
// exactly as it does for AddLinksFrom, because a container is a delivery
// mechanism and none of those decisions is about how a link arrived.
func (a *App) AddResolvedLinksFrom(links []resolver.Result, pkg string, origin core.Origin) []*core.Task {
	return a.detached(a.addResolvedLinksFrom(links, pkg, origin))
}

func (a *App) addResolvedLinksFrom(links []resolver.Result, pkg string, origin core.Origin) []*core.Task {
	var created []*core.Task
	seen := map[string]bool{}
	b := &bucket{}
	for _, l := range links {
		u := strings.TrimSpace(l.DirectURL)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		cand := rules.Candidate{URL: u, Package: pkg, Added: time.Now()}
		if v := a.filter(cand); v.Rejected {
			if t := a.hold(cand, v, origin, cand.Added); t != nil {
				created = append(created, t)
			}
			continue
		}
		if t := a.stage(u, l.Name, l.Size, intake{pkg: pkg, origin: origin}); t != nil {
			b.tasks = append(b.tasks, t)
			created = append(created, t)
		}
	}
	if strings.TrimSpace(pkg) == "" {
		a.nameBucket(b)
	}
	a.catchAll(created)
	if len(created) > 0 && a.Settings.Get().AutoConfirm {
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerAutoConfirm)
	}
	return created
}

// addLinksFrom is AddLinksFrom without the copy at the end.
//
// The copy has to happen once, at the outermost exported call, and not here: a
// caller that still has work to do - AddLinksWithPasswords writes the archive
// password onto these tasks straight afterwards - must be holding the real ones.
// Detaching here instead cost exactly that, and the test that caught it said so
// plainly: the response carried an empty password because the write had landed
// on the live task while the caller was returning a copy taken before it.
//
// batch is the add-links form's own per-batch options, zero-valued for every
// caller but AddLinksWithOptions - see intake's own comment for where they are
// applied and app_links_batch.go for why applying them here is what lets a
// Packagizer rule win by default.
func (a *App) addLinksFrom(urls []string, pkg string, origin core.Origin, batch LinkBatchOptions) []*core.Task {
	var created []*core.Task
	// seen is about the pasted text and nothing else: it stops one page being
	// fetched twice when it appears twice in the same box. Whether a *link* is
	// already in the list is the mirror set's answer alone. Two ideas of "we
	// already have this" that normalise URLs differently disagree sooner or later,
	// and then a link a raw string comparison let through comes back reported as a
	// duplicate of itself.
	seen := map[string]bool{}
	// One bucket per crawled page, plus one for everything that was already a
	// link. Naming used to run once across the whole call, so two pages pasted
	// together were both named after whichever came first — and the second page's
	// links sat under a title that was never about them.
	var buckets []*bucket
	loose := &bucket{}
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		// Asked here as well as inside stage, because the crawl below fetches the
		// page. A rule naming a host is an instruction not to talk to it, and a
		// filter that only refuses the links a page yielded has already sent a
		// request to the address the user filtered out. stage keeps its own pass:
		// it is the only way a link enters the list, and the links a crawl produces
		// never come past this point.
		cand := rules.Candidate{URL: u, Package: pkg, Added: time.Now()}
		if v := a.filter(cand); v.Rejected {
			if t := a.hold(cand, v, origin, cand.Added); t != nil {
				created = append(created, t)
			}
			continue
		}
		// A page that points at files becomes those files, not one task for the
		// page. Without this a gallery or an index listing can only ever be a
		// single unusable download.
		if crawled := a.crawl(u); len(crawled) > 0 {
			b := &bucket{title: crawlTitle(crawled)}
			for _, c := range crawled {
				if c.URL == "" {
					continue
				}
				// OriginCrawl rather than the caller's origin, whatever brought the
				// page in: a link nobody typed did not arrive by the path the page
				// did. Which page it was is on Source right beside it, which is also
				// the only place a rule keyed on "where did this link come from" can
				// get it.
				if t := a.stage(c.URL, c.Name, 0, intake{
					pkg: pkg, origin: OriginCrawl, source: u,
					priority: batch.Priority, autoExtract: batch.AutoExtract, comment: batch.Comment,
				}); t != nil {
					b.tasks = append(b.tasks, t)
					created = append(created, t)
				}
			}
			buckets = append(buckets, b)
			continue
		}
		if t := a.stage(u, "", 0, intake{
			pkg: pkg, origin: origin,
			priority: batch.Priority, autoExtract: batch.AutoExtract, comment: batch.Comment,
		}); t != nil {
			loose.tasks = append(loose.tasks, t)
			created = append(created, t)
		}
	}

	buckets = append(buckets, loose)

	// Naming, in two passes: each bucket gets the best name its own links agree
	// on, and whatever is still nameless afterwards goes in the catch-all rather
	// than into the blank that is not a package at all.
	if strings.TrimSpace(pkg) == "" {
		for _, b := range buckets {
			a.nameBucket(b)
		}
	}
	a.catchAll(created)

	// Auto-confirm hands everything straight past the collector for users who
	// don't want the staging step - AutoConfirm, not AutoStart: the latter now
	// answers a different question (settings.go's own doc comment on the
	// three-way split) and reading it here was Wave 8's own regression, caught
	// by that wave's adversarial review before it shipped - a fresh install
	// defaults AutoConfirm false and AutoStart true, so this branch was firing
	// on every paste regardless of the collector setting anyone actually chose.
	// Routed through ConfirmTasks, not a raw StartTasks, so onDupes/onOffline
	// apply here exactly as build-plan.md section 8's Wave 8 note asks - this
	// is the one caller app_confirm.go's own package comment named as still
	// missing. What the link filter is holding is not in ConfirmTasks' own
	// StatusCollected scan (a held link never reaches that status), which is
	// the whole reason the flag is on the task rather than a note somewhere
	// else.
	if len(created) > 0 && a.Settings.Get().AutoConfirm {
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerAutoConfirm)
	}
	return created
}

// detached copies the tasks out of the live map before they leave this package.
//
// Staging returns pointers INTO a.tasks, and one of the last things stage does
// is start the availability probe on a goroutine - so the caller was handed a
// task that another goroutine is already writing to. The API encodes that slice
// straight into the response, which the race detector caught exactly there:
// json.Encoder reading Task.Name while setAvailability wrote Task.Error.
//
// The copy is at this boundary rather than in the handler because every caller
// inherits the hazard, and the next one will not know to look. App.Tasks has
// copied for the same reason since it was written.
//
// Under mu, which is the whole point: `c := *t` reads every field of the struct,
// so copying without the lock is the same race one step further along. It must
// therefore never be called by anything already holding mu; the three callers
// are exported methods returning to their own caller, and none of them does.
func (a *App) detached(in []*core.Task) []*core.Task {
	if in == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*core.Task, len(in))
	for i, t := range in {
		c := *t
		out[i] = &c
	}
	return out
}

// snapshotTasks is detached without the nil-passthrough: nameBucket always
// has at least one task (its own caller checks len(b.tasks) == 0 first), and
// every caller here is, unlike detached's, still inside the same method that
// goes on to read the copies afterwards - so there is always a real slice to
// copy, not an optional one to pass along.
func (a *App) snapshotTasks(in []*core.Task) []*core.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*core.Task, len(in))
	for i, t := range in {
		c := *t
		out[i] = &c
	}
	return out
}

// bucket is one group of links that will be named together: the yield of a
// single crawl, or everything in a paste that was already a link.
type bucket struct {
	// title is what the crawled page called itself, empty for the loose bucket.
	title string
	tasks []*core.Task
}

// crawlTitle is what the crawled page called itself. Every result from one page
// carries the same title, but a site-specific crawler is free to fill it on some
// results and not others, so the first non-empty one wins rather than the first.
func crawlTitle(found []crawler.Result) string {
	for _, c := range found {
		if s := strings.TrimSpace(c.Title); s != "" {
			return s
		}
	}
	return ""
}

// nameBucket gives one batch a package derived from the links in it. It runs
// only when the user named no package, because a name typed into the box is a
// more specific answer than anything guessable from a file list.
//
// A task a Packagizer rule already named is left out. The rule is the more
// specific answer and it ran first, so overwriting it here would make a rule that
// works look like one that does nothing.
//
// b.tasks are live pointers into a.tasks: stage has already handed the newest
// of them to a background probe (probeYtdlpTitle or analyze) before returning
// them here, so derivePackage's read of a task's Name is racing that probe's
// own locked write the same way a caller reading Tasks() would be without its
// copy - see detached's own comment just above for the general shape of the
// hazard. snapshotTasks takes the same locked copy detached and Tasks already
// take, so derivePackage and unpackagedIDs read a name that can no longer
// change under them instead of the live one a probe might be mid-write on.
func (a *App) nameBucket(b *bucket) {
	if b == nil || len(b.tasks) == 0 {
		return
	}
	snap := a.snapshotTasks(b.tasks)
	derived := derivePackage(snap, b.title)
	if derived == "" {
		return
	}
	ids := unpackagedIDs(snap)
	if len(ids) > 0 {
		a.SetPackage(ids, derived)
	}
}

// catchAllPackage is where a link with no name of its own ends up.
//
// It is a real package rather than the blank one because "ungrouped" is not a
// group: a collector holding forty unrelated links and no packages is exactly the
// list this app set out to replace, and a bucket with a name can be collapsed,
// moved, started and emptied like any other.
//
// It is deliberately not translated. A package name is data, not interface text:
// it is written to the store, it becomes a folder name when SubfolderByPackage is
// on, and rules match on it — so a name that changed with the interface language
// would rename folders on disk when somebody switched to German.
const catchAllPackage = "Various"

// catchAll files whatever is still nameless.
func (a *App) catchAll(created []*core.Task) {
	if ids := unpackagedIDs(created); len(ids) > 0 {
		a.SetPackage(ids, catchAllPackage)
	}
}

// unpackagedIDs is the tasks nothing has filed yet. ManualPackage is checked as
// well as the name because a package the user chose by hand is the one answer
// nothing derived here may overwrite — including the empty one, which from a
// person is a deliberate "leave this ungrouped" rather than a gap.
func unpackagedIDs(tasks []*core.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t != nil && !t.ManualPackage && strings.TrimSpace(t.Package) == "" {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// derivePackage guesses a name for a batch that arrived without one: the shared
// stem of the file names if the links look like parts of one thing, then what
// the page they were crawled off called itself, else the host they came from. It
// returns "" when none of the three is worth using, because a bad guess is worse
// than no group at all — and what is left over lands in the catch-all instead.
//
// title is empty for a pasted batch. It sits between the two because it is the
// more specific answer of the pair — a listing page's own address is "pub/",
// "index" or a bare number for a good half of the web, and a package named after
// the host groups everything anybody ever fetched from that host together.
func derivePackage(tasks []*core.Task, title string) string {
	if len(tasks) == 0 {
		return ""
	}
	names := make([]string, 0, len(tasks))
	hosts := map[string]bool{}
	for _, t := range tasks {
		hosts[hostOf(t.URL)] = true
		if n := fileStem(t); n != "" {
			names = append(names, n)
		}
	}
	if stem := commonStem(names); len(stem) >= 3 {
		return sanitizeSegment(stem)
	}
	// Emptiness is tested before sanitizing, not after: sanitizeSegment answers
	// "package" for an empty string, so sanitizing first would name every batch
	// with no shared stem "package" and the host fallback below would be dead.
	if title = strings.TrimSpace(title); title != "" {
		return sanitizeSegment(title)
	}
	// One host and nothing else in common: the source is the only honest label.
	if len(hosts) == 1 {
		for h := range hosts {
			if h != "" && !strings.HasPrefix(h, "http") {
				return sanitizeSegment(h)
			}
		}
	}
	return ""
}

// fileStem is the part of a task's file name that identifies the thing rather
// than the part: "film.part03.rar" and "film.r02" both reduce to "film".
func fileStem(t *core.Task) string {
	name := t.Name
	if name == "" || strings.Contains(name, "://") {
		// Nothing resolved yet, so the URL's last segment is the best we have.
		if u, err := url.Parse(t.URL); err == nil {
			name = path.Base(u.Path)
		}
	}
	if name == "" || name == "." || name == "/" {
		return ""
	}
	if key, ok := extract.SetKey(name); ok {
		// SetKey lower-cases its base because it is a grouping key. A package
		// name is read by a person, so take only the LENGTH from it and slice
		// the original, which keeps the capitalisation the release came with.
		if base, _, cut := strings.Cut(key, "|"); cut && len(base) <= len(name) {
			return name[:len(base)]
		}
	}
	return strings.TrimSuffix(name, path.Ext(name))
}

// commonStem is the longest prefix every name shares. When that prefix cuts a
// name short it is trimmed back to a separator, because half a word
// ("Movie.S01E0") is a worse label than the shorter whole one. When every name
// is identical nothing was cut, so nothing is trimmed either.
func commonStem(names []string) string {
	if len(names) == 0 {
		return ""
	}
	stem := names[0]
	truncated := false
	for _, n := range names[1:] {
		i := 0
		for i < len(stem) && i < len(n) && stem[i] == n[i] {
			i++
		}
		if i < len(stem) || i < len(n) {
			truncated = true
		}
		stem = stem[:i]
		if stem == "" {
			return ""
		}
	}
	if truncated {
		if i := strings.LastIndexAny(stem, ".-_ "); i > 0 {
			stem = stem[:i]
		}
	}
	return strings.Trim(stem, ".-_ ")
}

// crawl asks the page crawler what a link points at. It returns nothing when
// crawling is off, when the link is already a file, or when the page yielded
// nothing — in every one of those cases the link is staged as itself.
func (a *App) crawl(u string) []crawler.Result {
	if !a.Settings.Get().Crawl {
		return nil
	}
	// Which backends mean "this might be a page". The HTTP fallback means
	// nobody recognised the link at all. yt-dlp is in the set because it claims
	// by exclusion rather than by knowledge — it takes every http link that is
	// not a known hoster — so gating on the fallback alone meant the crawler
	// never ran at all on any install that has yt-dlp, which is all of them.
	//
	// Everything else stays out: a direct file link is already a download, and
	// a debrid or JD link belongs to a hoster whose page holds nothing we could
	// fetch ourselves.
	if res := a.Registry.For(u); res != nil {
		switch res.Info().ID {
		case "http", "ytdlp":
		default:
			return nil
		}
	}
	// Begun here, after the settings/registry gates above rather than at the
	// top of the function: those two return instantly with no network call
	// made, and counting them as "ambient activity" would flash the status
	// strip on for zero-cost, zero-duration work.
	a.beginActivity(ActivityCrawl, 1)
	defer a.endActivity(ActivityCrawl, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	found, err := a.Crawler.Crawl(ctx, u)
	if err != nil {
		log.Printf("crawl %s: %v", u, err)
		return nil
	}
	// One result that is the page itself is not a crawl, it is the same link
	// back; staging it through the normal path keeps the resolver choice honest.
	if len(found) == 1 && found[0].URL == u {
		return nil
	}
	return found
}

// stage creates one collected task for a URL and is the only way a link enters
// the list — the pasted path and the crawled path both come through here, so a
// filter one of them honours cannot be the one the other walks past.
//
// Everything that decides whether a link may exist at all happens before put.
// A filter that ran afterwards would already have leaked the link into the task
// map, into the store and onto every connected screen, and taking it away again
// is a flicker and a store round trip, not a filter.
//
// It returns nil when the link never became a task, and the held task when the
// filter refused it.
//
// sizeHint is what the caller already knows about the byte count - a
// container's own crawl, for the same reason name can arrive pre-known (see
// addResolvedLinksFrom) - and 0 for every caller that does not. It is applied
// before the resolver runs and, like name, is not allowed to be overwritten by
// a resolver's placeholder answer of 0 - see the guard below.
func (a *App) stage(u, name string, sizeHint int64, in intake) *core.Task {
	// One clock reading for the whole link, in local time: it is what CreatedAt
	// gets and what pathvars formats for <jd:date>, and a UTC reading here would
	// flip a dated folder name a day early for everyone east of Greenwich.
	now := time.Now()
	cand := rules.Candidate{URL: u, Source: in.source, Package: in.pkg, Added: now}
	if n := strings.TrimSpace(name); n != "" {
		cand.Filename = n
	}
	// First pass, before anything is fetched. The byte count and the file type
	// are still unknown, so a rule keyed on those cannot fire yet — but a reject
	// on the URL, the hoster or the source page saves the whole network round
	// trip, which on a paste of several thousand links is the entire cost.
	if in.waived == "" {
		if v := a.filter(cand); v.Rejected {
			return a.hold(cand, v, in.origin, now)
		}
	}
	// An advisory look before the expensive part, so an obvious duplicate costs
	// nothing. The binding check is in put, under the lock that inserts the task.
	//
	// A mirror the user has asked to keep is deliberately NOT short-circuited
	// here: a sibling staged at this point would be a bare URL with no name and
	// no byte count, because nothing has resolved it yet. It goes the long way
	// round and is caught by the binding check instead, which runs after the
	// resolver has filled those in.
	if m := a.mirror(dedupe.Entry{URL: u, Name: cand.Filename}); m.Seen() && !a.keepsAsSibling(m) {
		a.recordSkipped(u, m)
		return nil
	}

	t := &core.Task{
		URL:     u,
		Name:    u,
		Package: in.pkg,
		Status:  core.StatusCollected,
		// Set here and not left to the zero value: a link nobody has switched off
		// is on, and a Task built without this would be staged already disabled.
		Enabled: true,
		Source:  in.source,
		Origin:  in.origin,
		// The reason the user overruled, kept on a link they let through. It is
		// what tells the queue apart from a link the filter has never seen — see
		// filterWaived — and it is empty for everything that was never held.
		SkipReason: in.waived,
		// torrentHost, not the bare hostOf every other link here gets: a magnet
		// names no single host either, and falling back to hostOf's raw-string
		// answer for one is exactly the bug torrentHost's own comment describes
		// for an uploaded .torrent, just smaller - see that comment for why
		// "smaller" does not mean "fine".
		Host:      torrentHost(u),
		CreatedAt: now,
	}
	// The add-links form's own batch options, seeded before ANY of the three
	// finishStaging calls below - which is what runs the Packagizer - so that a
	// matching rule overwrites them exactly as it would overwrite a value set
	// no other way. See intake's own comment and app_links_batch.go for the
	// other half: applying them again once staging is done, when the form is
	// meant to win instead.
	if in.priority != nil {
		p := *in.priority
		if p < rules.PriorityMin {
			p = rules.PriorityMin
		} else if p > rules.PriorityMax {
			p = rules.PriorityMax
		}
		t.Priority = p
	}
	if in.autoExtract != nil {
		v := *in.autoExtract
		t.AutoExtract = &v
	}
	if in.comment != "" {
		t.Comment = in.comment
	}
	if cand.Filename != "" {
		t.Name = cand.Filename
	}
	if sizeHint > 0 {
		t.Size = sizeHint
	}
	res := a.Registry.For(u)
	if res == nil {
		// A link is never dropped on the floor. If nothing can handle it, or
		// resolving fails, it is still staged — with the reason on it — so the
		// user can see what happened instead of watching links vanish.
		t.Error = "no backend handles this link"
		t.Reason = core.ReasonUnsupported
		t.Online = core.AvailOffline
		return a.finishStaging(t, cand)
	}
	t.Resolver = res.Info().ID
	result, err := res.Resolve(context.Background(), resolver.Request{URL: u})
	if err != nil {
		t.Error = err.Error()
		t.Reason = classify(failure{err: err})
		return a.finishStaging(t, cand)
	}
	// result.Name != u is deliberate, not a stray strictness. A resolver that
	// does not yet know the real name answers with the URL itself rather than
	// leaving Name blank - jd, ytdlp, debrid and torbox's own Resolve methods
	// all do this, by their own doc comments, so a task always has something to
	// show. filename() (below) already reads that exact convention the other
	// way, treating Name == URL as "nothing resolved yet". That placeholder
	// must not be allowed to overwrite a real name this link arrived with (a
	// container's own crawl - see addResolvedLinksFrom), or the one useful
	// answer staging already had is thrown away for the one that means "I don't
	// know" - which is the bug a DLC's name and size were disappearing to.
	if result.Name != "" && result.Name != u {
		t.Name = result.Name
	}
	if result.Size > 0 {
		t.Size = result.Size
	}
	if t.Resolver == "torrent" {
		// resolver.Result (above) has no room for these - it is the one shape
		// every resolver answers with, and InfoHash/Trackers mean nothing to
		// the other five. A second, torrent-specific Describe call gets them
		// the same way app_torrents.go's AddTorrent already does for an
		// uploaded .torrent - cheap for the magnet case this path actually
		// handles: checkMagnet is metainfo.ParseMagnetV2Uri, a local parse of
		// the URI's own text, never a network call, so this is not a second
		// real resolve.
		if md, err := (torrent.Resolver{}).Describe(u); err == nil {
			t.InfoHash = md.InfoHash
			t.Trackers = md.Trackers
		}
	}

	// Second pass, now that the name and the byte count exist. This is the one
	// that can act on a size or a file-type condition, and it still runs before
	// the task is staged.
	cand.Filename, cand.Filesize = filename(t), t.Size
	if in.waived == "" {
		if v := a.filter(cand); v.Rejected {
			return a.hold(cand, v, in.origin, now)
		}
	}
	staged := a.finishStaging(t, cand)
	// Lightweight analysis for plain file links: a HEAD gives size + an online
	// check while the task waits in the collector.
	if staged != nil && res.Info().ID == "direct" {
		a.spawn(func() { a.analyze(t.ID, result.DirectURL) })
	} else if staged != nil && res.Info().ID == "ytdlp" {
		// Same shape, yt-dlp's own version: a title probe while the task waits
		// in the collector, so a YouTube (etc.) link shows the video's real
		// name instead of its own URL before anybody presses Start - see
		// probeYtdlpTitle's own doc comment (app_tasks.go) for why this is
		// silent on failure, the same as analyze's HEAD probe above.
		a.spawn(func() { a.probeYtdlpTitle(t.ID, result.DirectURL) })
	}
	return staged
}

// finishStaging applies the Packagizer and stages the task, or reports the link
// as one the list already covers.
//
// The Packagizer runs here, before put and therefore before anything has asked
// dirFor where the file goes. Run it afterwards and its folder action names a
// folder nothing writes to, and the user watches a link land in one package and
// jump to another a moment later.
func (a *App) finishStaging(t *core.Task, cand rules.Candidate) *core.Task {
	cand.Filename, cand.Filesize, cand.Package = filename(t), t.Size, t.Package
	a.packagize(t, cand)
	if m, ok := a.put(t); !ok {
		// The refusal is where a kept mirror is staged instead, and it has to be
		// here rather than at the advisory check: this is the first point at which
		// the sibling has a name and a size of its own, and a second copy nobody
		// can tell apart from the first is not worth keeping.
		if a.stageSibling(t, m) {
			return t
		}
		a.recordSkipped(t.URL, m)
		return nil
	}
	return t
}

// filename is a task's file name, or empty while nothing has resolved one. A
// task that has not been looked at yet carries its own URL as a name, and
// handing that to a rule as a file name makes "filename contains" answer about
// the URL instead.
func filename(t *core.Task) string {
	if t.Name == t.URL {
		return ""
	}
	return t.Name
}

// candidateOf describes an existing task to the rule engine. The source page is
// left out although the task now carries it: a rule keyed on the source decides
// at staging time today, and feeding it to the second pass in the dispatcher
// would change what an existing rule set does to links already in the list. That
// is a decision for the wave that owns the link filter, not a side effect of
// recording the field.
func candidateOf(t *core.Task) rules.Candidate {
	return rules.Candidate{
		URL:      t.URL,
		Filename: filename(t),
		Filesize: t.Size,
		Package:  t.Package,
		Added:    t.CreatedAt,
	}
}

// filter asks the link filter about a candidate. A set with no usable rule is
// never consulted, so an install that has never opened the page does no work per
// link at all.
func (a *App) filter(cand rules.Candidate) rules.Verdict {
	_, f := a.matchers()
	if f == nil || f.Empty() {
		return rules.Verdict{}
	}
	return f.Check(cand)
}

// filterWaived reports a link the user has already overruled the filter for.
//
// It is the pair (not held, but carrying the reason it was held for): Skipped
// says the filter is holding it now, and a SkipReason that outlives the flag is
// the record that somebody read that reason and restored the link anyway. The
// queue asks the filter one last time before any bytes move, and without this
// Restore would be a button that puts a link back so the same rule can refuse it
// again with the same sentence.
func filterWaived(t *core.Task) bool { return t != nil && !t.Skipped && t.SkipReason != "" }

// packagize applies the Packagizer's answer to a task that has not been staged
// yet. Only what a rule actually set is applied: an empty field means "no rule
// had an opinion", never "clear it".
//
// The rename action is deliberately not applied. Nothing here can tell a backend
// which file name to write — the engine is handed a directory and names the file
// itself — so putting a rule's name on the task would leave the list showing one
// name while the disk holds another, and extraction and checksum verification
// both build their path by joining the folder with that name.
func (a *App) packagize(t *core.Task, cand rules.Candidate) {
	pkg, _ := a.matchers()
	if pkg == nil || pkg.Empty() {
		return
	}
	e := pkg.Apply(cand)
	if e.Package != "" {
		t.Package = e.Package
	}
	if e.Dir != "" {
		// Already expanded by the rules package, and dirFor takes a task's own
		// folder verbatim. Expanding it a second time would resolve placeholders
		// the first pass deliberately left standing so the user could see them.
		t.Dir = e.Dir
	}
	if e.Comment != "" {
		t.Comment = e.Comment
	}
	if e.Priority != nil {
		// Already clamped to the same range SetPriority uses, so a rule cannot
		// hand a task a priority the interface has no way to undo.
		t.Priority = *e.Priority
	}
	if e.Chunks != nil {
		t.Chunks = *e.Chunks
	}
	if e.AutoExtract != nil {
		v := *e.AutoExtract
		t.AutoExtract = &v
	}
	t.MatchedRules = e.Matched
}

// hold parks a link the filter refused, in the holding area rather than in the
// collector.
//
// It is still a task, and it is still kept. Eating links in silence is
// JDownloader's single most complained-about behaviour, and a link that
// disappears without a trace is indistinguishable from a bug in the paste box.
// But Skipped keeps it out of the list, out of the queue and out of the counters
// — because a filter that is working would otherwise fill the collector with
// exactly the junk it just caught, and a collector full of junk reads as a filter
// that does nothing. That is the whole of the difference from staging it: same
// record, different list.
//
// A task rather than a note in memory, because the holding area has to survive a
// restart. A list of links that quietly empties itself overnight is the silent
// loss this feature exists to prevent, moved one reboot along.
//
// Nothing is resolved. A refused link must not cost a network round trip — on a
// paste of several thousand links that saving is the entire cost — and a rule
// written to keep this box away from a host must not make it talk to that host
// on the way to saying so.
func (a *App) hold(cand rules.Candidate, v rules.Verdict, origin core.Origin, now time.Time) *core.Task {
	t := &core.Task{
		URL:     cand.URL,
		Name:    cand.URL,
		Package: cand.Package,
		Status:  core.StatusCollected,
		Skipped: true,
		// The sentence the person reads. rejection() has already folded the rule's
		// name into it where the rule's own words did not carry it.
		SkipReason: rejection(v),
		// A refused link is still an enabled one: what stopped it was the filter,
		// and a user who fixes the rule expects to be able to start it.
		Enabled:   true,
		Source:    cand.Source,
		Origin:    origin,
		Host:      hostOf(cand.URL),
		CreatedAt: now,
		// Online is left unset on purpose. It used to be filed as offline so the
		// collector would show the link was not going to be taken, which Skipped
		// now says properly — and "offline" is a claim about the link that nobody
		// checked. Filing a live link as dead is how a user learns to ignore the
		// column.
	}
	if cand.Filename != "" {
		t.Name = cand.Filename
	}
	if v.Rule != "" {
		// Which rule caught it, as data and not only inside a sentence. The first
		// question anyone asks of the holding area is which rule to go and edit,
		// and a client that has to parse the name back out of prose that will
		// eventually be translated will get it wrong.
		t.MatchedRules = []string{v.Rule}
	}
	if m, ok := a.put(t); !ok {
		a.recordSkipped(t.URL, m)
		return nil
	}
	return t
}

// rejection is what the user reads on a refused link. The rule package writes a
// reason that already names the rule when the user gave none of their own, so
// the name is added only where it would otherwise be missing. The test is on the
// quoted name because a reason written in the user's own words routinely
// contains the same word the rule is named after.
func rejection(v rules.Verdict) string {
	if v.Rule == "" || strings.Contains(v.Reason, strconv.Quote(v.Rule)) {
		return v.Reason
	}
	return fmt.Sprintf("%s (link filter rule %q)", v.Reason, v.Rule)
}

// FilteredLinks is the holding area: the links the filter refused, oldest first.
//
// It is derived from the task list rather than kept beside it. The tasks are
// already persisted, already broadcast and already sent to every client, so a
// second list would be a second thing to keep in step with the first — and the
// browser can answer this question from the stream it is holding anyway. This
// exists for the clients that are not a browser.
func (a *App) FilteredLinks() []*core.Task {
	a.mu.Lock()
	held := make([]core.Task, 0, 8)
	for _, t := range a.tasks {
		if t.Skipped {
			held = append(held, *t)
		}
	}
	a.mu.Unlock()
	sort.Slice(held, func(i, j int) bool { return held[i].CreatedAt.Before(held[j].CreatedAt) })
	out := make([]*core.Task, 0, len(held))
	for i := range held {
		out = append(out, &held[i])
	}
	return out
}

// RestoreFiltered puts links the filter is holding back into the collector, with
// the filter waived for exactly those links. An empty id list restores the whole
// holding area.
//
// Waived, and not merely un-held. The commonest reason to open this list at all
// is that the rule turned out to be too broad, and the queue asks the filter one
// final time before any bytes move — so a Restore that only cleared the flag
// would be a button that hands the link straight back to the rule that caught it.
// What was overruled stays recorded on the task (see filterWaived), so this is a
// decision about these links and not a hole in the filter.
func (a *App) RestoreFiltered(ids []string) []*core.Task {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	var freed []core.Task
	for id, t := range a.tasks {
		if !t.Skipped || !(all || want[id]) {
			continue
		}
		t.Skipped = false
		// Status and Error are untouched: a held link was never started, so there
		// is nothing to reset, and SkipReason is deliberately kept.
		freed = append(freed, *t)
	}
	a.mu.Unlock()

	sort.Slice(freed, func(i, j int) bool { return freed[i].CreatedAt.Before(freed[j].CreatedAt) })
	out := make([]*core.Task, 0, len(freed))
	restored := make([]string, 0, len(freed))
	for i := range freed {
		c := freed[i]
		_ = a.Store.Save(&c)
		a.Hub.Broadcast("task", &c)
		out = append(out, &c)
		restored = append(restored, c.ID)
	}
	// Nothing was resolved while the link was held, so a restored link would
	// otherwise sit in the collector as a bare URL with no name and no size. The
	// recheck is the same one the interface offers by hand; off the caller's
	// goroutine because it is one network round trip per link and the browser is
	// waiting for this response.
	if len(restored) > 0 {
		a.spawn(func() { a.RecheckTasks(restored) })
	}
	return out
}

// ClearFiltered deletes the links the filter is holding, and only those. An
// empty id list empties the whole holding area. The downloaded files are never
// touched, because a held link has never downloaded anything.
func (a *App) ClearFiltered(ids []string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	doomed := make([]string, 0, 8)
	for id, t := range a.tasks {
		if t.Skipped && (all || want[id]) {
			doomed = append(doomed, id)
		}
	}
	a.mu.Unlock()
	// Through RemoveTasks rather than by deleting from the map here: it is what
	// takes the link back out of the mirror set and off every open screen, and a
	// second removal path is a second place for those two to be forgotten.
	return a.RemoveTasks(doomed, false)
}

// heldLink reports whether a task id belongs to a link the filter is holding.
// Caller must not hold mu.
func (a *App) heldLink(id string) bool {
	if id == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tasks[id]
	return t != nil && t.Skipped
}

// SkippedLink is a link that never became a task. It is kept so the interface
// can say what happened to it: a link folded away with nothing to show for it
// looks exactly like a bug in the paste box, and gets reported as one.
//
// This is not the holding area. A link the filter refused is a task with Skipped
// on it, because it can be restored and therefore has to survive a restart; this
// is the trace of links that were folded into a copy already in the list, where
// there is nothing to restore and nothing was lost.
type SkippedLink struct {
	URL string `json:"url"`
	// Kind is what the mirror set decided: "duplicate" or "mirror".
	Kind   string    `json:"kind"`
	Reason string    `json:"reason"`
	OfID   string    `json:"ofId,omitempty"`
	Signal string    `json:"signal,omitempty"`
	At     time.Time `json:"at"`
}

// maxSkipped caps the trace. A watch folder re-reading one list is exactly the
// shape that would otherwise grow it for the life of the process.
const maxSkipped = 500

// mirror asks the set whether a link is already covered. It is a separate
// critical section from the one put uses because the caller has a network round
// trip to make in between, and holding mu across a resolver call would serialise
// every paste behind one HTTP request — which on a large paste looks like the
// app hanging.
func (a *App) mirror(e dedupe.Entry) dedupe.Match {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dupes.Check(e)
}

// recordSkipped keeps and broadcasts a link that was folded into one already in
// the list.
func (a *App) recordSkipped(u string, m dedupe.Match) {
	// OfID and Signal are the whole point of a duplicate's trace: "already have
	// it" is not something a user can check, and "the same file name and byte
	// count as this task" is.
	a.pushSkipped(SkippedLink{
		URL:    u,
		Kind:   m.Verdict.String(),
		Reason: a.skipReason(m),
		OfID:   m.Of.ID,
		Signal: string(m.Signal),
		At:     time.Now(),
	})
}

// recordSkippedReason is the same trace for something that never reached the
// mirror set: a container that opened into nothing, a handover the backend
// refused. Those fail after the request that started them has been answered, so
// without a record they fail into silence.
func (a *App) recordSkippedReason(u, kind, reason string) {
	a.pushSkipped(SkippedLink{URL: u, Kind: kind, Reason: reason, At: time.Now()})
}

func (a *App) pushSkipped(s SkippedLink) {
	a.mu.Lock()
	a.skipped = append(a.skipped, s)
	if len(a.skipped) > maxSkipped {
		a.skipped = append(a.skipped[:0], a.skipped[len(a.skipped)-maxSkipped:]...)
	}
	a.mu.Unlock()
	a.Hub.Broadcast("skipped", s)
}

// skipReason is the sentence shown next to a folded link. It names what the
// match rests on, because "already have it" is not something a user can check
// and "the same file name and byte count" is.
func (a *App) skipReason(m dedupe.Match) string {
	if m.Verdict == dedupe.Duplicate {
		// "Already in the list" is a sentence the user will go and check, and when
		// the copy is one the filter is holding they will not find it — the holding
		// area is deliberately not the collector. Saying where it actually is turns
		// a paste that looks ignored into one that points at the button to press.
		if a.heldLink(m.Of.ID) {
			return "the link filter is already holding this link"
		}
		return "the same link is already in the list"
	}
	name := m.Of.Name
	if name == "" {
		name = m.Of.URL
	}
	return fmt.Sprintf("already in the list as %q, matched on %s", name, m.Signal)
}

// SkippedLinks reports the links that never became tasks, oldest first.
func (a *App) SkippedLinks() []SkippedLink {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]SkippedLink, len(a.skipped))
	copy(out, a.skipped)
	return out
}

// ClearSkipped empties the trace.
func (a *App) ClearSkipped() {
	a.mu.Lock()
	a.skipped = nil
	a.mu.Unlock()
}

// AddLinksCnL satisfies the Click'n'Load listener's Adder interface. A CnL
// submission can carry the archive passwords for what it is sending, which is
// exactly the moment we can learn them without asking the user.
func (a *App) AddLinksCnL(urls []string, pkg string, passwords []string) {
	a.AddLinksWithPasswords(urls, pkg, passwords, OriginCnL)
}

// AddLinksWithPasswords stages links that arrived together with the archive
// passwords for them. The first password rides on the tasks themselves, because
// it was supplied for exactly these files; the rest join the global list, where
// a later archive from the same source can still reach them.
//
// The entrance is a parameter rather than OriginCnL fixed in place. Click'n'Load
// is still the only thing that supplies passwords, but it does not always arrive
// here directly: a bridge relays one over the REST API, and an older bridge
// relays it without naming the entrance at all. Pinning the origin here would
// file those links under an entrance the caller had already contradicted.
func (a *App) AddLinksWithPasswords(urls []string, pkg string, passwords []string, origin core.Origin) []*core.Task {
	created := a.addLinksFrom(urls, pkg, origin, LinkBatchOptions{})
	var first string
	for _, pw := range passwords {
		if pw = strings.TrimSpace(pw); pw != "" {
			first = pw
			break
		}
	}
	if first == "" || len(created) == 0 {
		return a.detached(created)
	}
	ids := make([]string, 0, len(created))
	for _, t := range created {
		ids = append(ids, t.ID)
	}
	if err := a.SetTaskOptions(ids, TaskOptions{Password: &first}); err != nil {
		log.Printf("could not apply the supplied archive password: %v", err)
	}
	if len(passwords) > 1 {
		a.rememberPasswords(passwords)
	}
	return a.detached(created)
}

// rememberPasswords folds passwords a submission brought along into the global
// list, so a later archive from the same source can still be opened.
func (a *App) rememberPasswords(passwords []string) {
	cfg := a.Settings.Get()
	known := map[string]bool{}
	for _, p := range cfg.ArchivePasswords {
		known[p] = true
	}
	added := false
	for _, p := range passwords {
		if p = strings.TrimSpace(p); p != "" && !known[p] {
			cfg.ArchivePasswords = append(cfg.ArchivePasswords, p)
			known[p] = true
			added = true
		}
	}
	if added {
		if _, err := a.Settings.Set(cfg); err != nil {
			log.Printf("could not store the passwords a submission brought along: %v", err)
		}
	}
}
