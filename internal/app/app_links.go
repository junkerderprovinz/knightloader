package app

// Everything between a pasted string and a staged task: the entrance, the
// filter, the crawl, the Packagizer, the package, the mirror set, and the
// links that never made it.

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The six entrances a link can arrive by. They live here, beside the funnels
// that set them, rather than in core, which only owns the type.
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
	// OriginFeed is an entry an RSS or Atom subscription published. It is kept
	// apart from OriginWatch so the collector can say where a link came from.
	OriginFeed core.Origin = "feed"
	// OriginContainer is a .dlc/.ccf/.rsdf/.txt container, whether it was read
	// here or opened by the JD backend on our behalf.
	OriginContainer core.Origin = "container"
)

// KnownOrigin parses an entrance a caller names and refuses anything else. A
// Click'n'Load bridge on the user's desktop relays submissions over the
// ordinary link route and uses this to say where they came from.
func KnownOrigin(s string) (core.Origin, bool) {
	switch o := core.Origin(strings.ToLower(strings.TrimSpace(s))); o {
	case OriginPaste, OriginCrawl, OriginCnL, OriginWatch, OriginFeed, OriginContainer:
		return o, true
	}
	return "", false
}

// intake is what an entrance knows about the links it hands over.
type intake struct {
	pkg    string
	origin core.Origin
	// source is the page a crawl found the link on; empty for everything else.
	source string
	// waived is the reason the filter held this link, passed back by
	// RestoreFiltered. Non-empty means the user overruled it, so the filter is
	// not asked again here or at the queue (see filterWaived).
	waived string

	// priority, autoExtract, comment and category are the batch options,
	// carried onto every task the batch creates, crawled ones included. stage
	// sets them before the Packagizer runs, so a matching rule wins: by default
	// for the first three (see LinkBatchOptions.Overrule), and always for the
	// category, which is an id.
	priority    *int
	autoExtract *bool
	comment     string
	category    string

	// playlistEntry marks a link from a --flat-playlist listing. Such links do
	// not start their own title probe, since one playlist can yield hundreds;
	// probePlaylistEntries probes them one at a time instead.
	playlistEntry bool
}

// AddLinks stages links pasted into the collector. Every other entrance calls
// AddLinksFrom with its own origin.
func (a *App) AddLinks(urls []string, pkg string) []*core.Task {
	return a.AddLinksFrom(urls, pkg, OriginPaste)
}

// AddLinksFrom resolves each URL and stages it in the link collector as a
// collected task; StartTasks moves tasks into the download queue. origin is
// recorded on every task, so "why is this here" can be answered later and
// rules can match on it.
func (a *App) AddLinksFrom(urls []string, pkg string, origin core.Origin) []*core.Task {
	created := a.addLinksFrom(urls, pkg, origin, LinkBatchOptions{})
	a.autoConfirm(idsOf(created))
	return a.detached(created)
}

// AddResolvedLinksFrom stages links whose name and possibly size are already
// known, such as those from a container JD opened. They skip the crawl and the
// playlist listing, which could only mistake a resolved file for a page; the
// filter, Packagizer, duplicate check, naming and auto-confirm run as usual.
func (a *App) AddResolvedLinksFrom(links []resolver.Result, pkg string, origin core.Origin) []*core.Task {
	return a.detached(a.addResolvedLinksFrom(links, intake{pkg: pkg, origin: origin}))
}

// verdict is one link's known availability, written through the locked path
// after the staging loop.
type verdict struct {
	id    string
	avail core.Availability
}

func (a *App) addResolvedLinksFrom(links []resolver.Result, in intake) []*core.Task {
	created := a.stageResolvedLinks(links, in)
	a.autoConfirm(idsOf(created))
	return created
}

// stageResolvedLinks is addResolvedLinksFrom without the auto-confirm, for a
// caller that still writes to the tasks before anything may start.
func (a *App) stageResolvedLinks(links []resolver.Result, in intake) []*core.Task {
	var created []*core.Task
	var verdicts []verdict
	seen := map[string]bool{}
	b := &bucket{}
	for _, l := range links {
		u := strings.TrimSpace(l.DirectURL)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		cand := rules.Candidate{URL: u, Package: in.pkg, Added: a.stamps.next()}
		if v := a.filter(cand); v.Rejected {
			if t := a.hold(cand, v, in.origin, cand.Added, nil); t != nil {
				created = append(created, t)
			}
			continue
		}
		if t := a.stage(u, l.Name, l.Size, in); t != nil {
			// Collected and written later through setAvailability, since the
			// task is shared and only that path takes a.mu and broadcasts.
			if l.Available != "" {
				verdicts = append(verdicts, verdict{id: t.ID, avail: l.Available})
			}
			b.tasks = append(b.tasks, t)
			created = append(created, t)
		}
	}
	if strings.TrimSpace(in.pkg) == "" {
		a.nameBucket(b)
	}
	// Before catchAll and auto-confirm, so the verdict is on the rows first.
	for _, v := range verdicts {
		a.setAvailability(v.id, v.avail, "", core.ReasonUnknown)
	}
	a.catchAll(created)
	return created
}

// addLinksFrom is AddLinksFrom without the auto-confirm and the final copy.
// Callers that still write to the tasks afterwards, like AddLinksWithPasswords,
// need the live ones, and they call autoConfirm once they are done so that a
// confirm cannot overtake what they write. The copy happens once, at the
// outermost exported call.
//
// batch holds the add-links form's options and is zero for every other caller.
func (a *App) addLinksFrom(urls []string, pkg string, origin core.Origin, batch LinkBatchOptions) []*core.Task {
	var created []*core.Task
	// seen only stops the same text in one paste from being fetched twice.
	// Whether a link is already in the list is the mirror set's decision alone.
	seen := map[string]bool{}
	// One bucket per crawled page, plus one for plain links, so each page's
	// links are named after that page.
	var buckets []*bucket
	loose := &bucket{}
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		// Filtered here as well as in stage, because the crawl below would
		// otherwise contact a host a rule told us to avoid.
		cand := rules.Candidate{URL: u, Package: pkg, Added: a.stamps.next()}
		if v := a.filter(cand); v.Rejected {
			if t := a.hold(cand, v, origin, cand.Added, nil); t != nil {
				created = append(created, t)
			}
			continue
		}
		// A playlist becomes its videos rather than one task. This is asked
		// before the crawl, which only claims yt-dlp links by exclusion.
		if pl, ok := a.ytdlpPlaylist(u); ok {
			b := &bucket{title: pl.Title}
			b.tasks = a.stagePlaylistEntries(u, pl, pkg, batch)
			created = append(created, b.tasks...)
			buckets = append(buckets, b)
			continue
		}
		// A page that points at files becomes those files.
		if crawled := a.crawl(u); len(crawled) > 0 {
			b := &bucket{title: crawlTitle(crawled)}
			for _, c := range crawled {
				if c.URL == "" {
					continue
				}
				// OriginCrawl whatever brought the page in, with the page on
				// Source. c.Size is 0 for a page crawl, which stage reads as no
				// hint; a remote directory listing supplies a real one.
				if t := a.stage(c.URL, c.Name, c.Size, intake{
					pkg: pkg, origin: OriginCrawl, source: u,
					priority: batch.Priority, autoExtract: batch.AutoExtract, comment: batch.Comment, category: batch.Category,
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
			priority: batch.Priority, autoExtract: batch.AutoExtract, comment: batch.Comment, category: batch.Category,
		}); t != nil {
			loose.tasks = append(loose.tasks, t)
			created = append(created, t)
		}
	}

	buckets = append(buckets, loose)

	// Each bucket gets the best name its links agree on; whatever is still
	// nameless goes into the catch-all package.
	if strings.TrimSpace(pkg) == "" {
		for _, b := range buckets {
			a.nameBucket(b)
		}
	}
	a.catchAll(created)
	return created
}

// detached copies tasks out of the live map before they leave this package.
// stage returns pointers into a.tasks and has already started goroutines that
// write to them, so encoding those pointers would race. The copy is taken
// under a.mu, so callers must not hold it.
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

// snapshotTasks is detached without the nil check, for callers inside this
// package that read the copies themselves.
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

// bucket is one group of links named together: the yield of one crawl, or the
// plain links of a paste.
type bucket struct {
	// title is what the crawled page called itself, empty for the loose bucket.
	title string
	tasks []*core.Task
}

// crawlTitle returns the first non-empty page title among the results; a
// site-specific crawler may fill it on some results only.
func crawlTitle(found []crawler.Result) string {
	for _, c := range found {
		if s := strings.TrimSpace(c.Title); s != "" {
			return s
		}
	}
	return ""
}

// nameBucket gives a batch without a user-given package a name derived from
// its links. Tasks a Packagizer rule already named are left alone. It reads a
// snapshot, because title probes may be writing the live tasks.
func (a *App) nameBucket(b *bucket) {
	if b == nil || len(b.tasks) == 0 {
		return
	}
	snap := awaitingMediaProbe.exclude(a.snapshotTasks(b.tasks))
	if len(snap) == 0 {
		return
	}
	derived := derivePackage(snap, b.title)
	if derived == "" {
		return
	}
	ids := unpackagedIDs(snap)
	if len(ids) > 0 {
		a.SetPackage(ids, derived)
		// A probe that answered between the snapshot and SetPackage found no
		// package to replace, and SetPackage then filed the task under the
		// URL guess. Re-checking here covers that order; setTaskName covers a
		// probe answering later.
		a.regressGuessedPackages(ids)
	}
}

// regressGuessedPackages replaces a URL-guessed package for tasks that already
// have a real name (see nameBucket).
func (a *App) regressGuessedPackages(ids []string) {
	var changed []taskCopy
	a.mu.Lock()
	for _, id := range ids {
		t := a.tasks[id]
		// Name == URL means no probe has answered; setTaskName handles it.
		if t == nil || t.Name == "" || t.Name == t.URL {
			continue
		}
		// The whole variant family comes back, since the siblings are in no id
		// list of their own.
		changed = append(changed, a.copiesLocked(reguessPackageLocked(a.tasks, t, t.Name))...)
	}
	a.mu.Unlock()
	a.publishTasks(changed)
}

// catchAllPackage is where links without a name of their own are filed, so
// they can be collapsed and handled like any package. It is not translated: a
// package name is stored, becomes a folder name and is matched by rules.
const catchAllPackage = "Various"

// catchAll files whatever is still nameless. It reads a snapshot for the same
// reason as nameBucket.
func (a *App) catchAll(created []*core.Task) {
	if ids := unpackagedIDs(awaitingMediaProbe.exclude(a.snapshotTasks(created))); len(ids) > 0 {
		a.SetPackage(ids, catchAllPackage)
	}
}

type mediaProbePending struct{}

// awaitingMediaProbe is the rule both naming passes use to skip a media link
// whose title probe has not answered yet. Before the probe, the only guess is
// the URL path, which for YouTube is "watch". Such links stay ungrouped until
// the probe files them under the video title (see packageIsStillAGuess), or
// under the URL guess if the probe fails.
var awaitingMediaProbe mediaProbePending

// has reports whether t is a media link still waiting for its probe. Name ==
// URL is the placeholder every stage path leaves until a name is known.
func (mediaProbePending) has(t *core.Task) bool {
	return t != nil && t.Resolver == "ytdlp" && t.Name == t.URL
}

func (p mediaProbePending) exclude(tasks []*core.Task) []*core.Task {
	out := make([]*core.Task, 0, len(tasks))
	for _, t := range tasks {
		if !p.has(t) {
			out = append(out, t)
		}
	}
	return out
}

// unpackagedIDs returns the tasks nothing has filed yet. A package the user
// chose by hand, even the empty one, is never overwritten.
func unpackagedIDs(tasks []*core.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t != nil && !t.ManualPackage && strings.TrimSpace(t.Package) == "" {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// derivePackage guesses a name for a batch: the shared stem of the file names,
// else the crawled page's title, else the single host. It returns "" when none
// is worth using, and those links go to the catch-all. The title comes before
// the host because a host name would group everything ever fetched from it.
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
	// Tested before sanitizing, since sanitizeSegment turns "" into "package".
	if title = strings.TrimSpace(title); title != "" {
		return sanitizeSegment(title)
	}
	if len(hosts) == 1 {
		for h := range hosts {
			if h != "" && !strings.HasPrefix(h, "http") {
				return sanitizeSegment(h)
			}
		}
	}
	return ""
}

// fileStem returns the part of a task's file name that identifies the release:
// "film.part03.rar" and "film.r02" both reduce to "film".
func fileStem(t *core.Task) string {
	name := t.Name
	if name == "" || strings.Contains(name, "://") {
		// Nothing resolved yet; the URL's last segment is all there is.
		if u, err := url.Parse(t.URL); err == nil {
			name = path.Base(u.Path)
		}
	}
	if name == "" || name == "." || name == "/" {
		return ""
	}
	if key, ok := extract.SetKey(name); ok {
		// SetKey lower-cases its base, so only its length is used, keeping the
		// original capitalisation.
		if base, _, cut := strings.Cut(key, "|"); cut && len(base) <= len(name) {
			return name[:len(base)]
		}
	}
	return strings.TrimSuffix(name, path.Ext(name))
}

// commonStem returns the longest prefix all names share. When that cuts a
// name short it is trimmed back to a separator, since half a word is a worse
// label.
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
// crawling is off, the link is already a file, or the page yielded nothing;
// the link is then staged as itself.
func (a *App) crawl(u string) []crawler.Result {
	// A folder on the user's own server is expanded regardless of the Crawl
	// setting, which is about fetching arbitrary web pages. A folder link
	// cannot be downloaded as one file.
	if res := a.Registry.For(u); res != nil && res.Info().ID == remotefs.ResolverID {
		return a.listRemoteDir(res, u)
	}
	if !a.Settings.Get().Crawl {
		return nil
	}
	// Only the HTTP fallback and yt-dlp may be pages; yt-dlp claims every link
	// no hoster knows. Direct files, debrid and JD links are not crawled,
	// whichever the priority card puts first.
	if res := a.stagingResolverFor(u); res != nil {
		switch res.Info().ID {
		case "http", "ytdlp":
		default:
			return nil
		}
	}
	opt := crawlOptions(a.Settings.Get())
	// Activity starts only here, after the cheap early returns.
	ctx, cancel := context.WithTimeout(context.Background(), crawlBudget(opt))
	defer cancel()
	// Registered with a stop handle, since a deep crawl can take minutes.
	done := a.startActivityRun(ActivityCrawl, cancel)
	defer done()

	found, err := a.crawlWith(ctx, u, opt)
	if err != nil {
		log.Printf("crawl %s: %v", u, err)
		return nil
	}
	// A single result that is the page itself is not a crawl.
	if len(found) == 1 && found[0].URL == u {
		return nil
	}
	return found
}

// crawlWith uses CrawlDeep when the configured crawler supports it.
// Site-specific crawlers and test fakes implement only crawler.Crawler.
func (a *App) crawlWith(ctx context.Context, u string, opt crawler.Options) ([]crawler.Result, error) {
	if deep, ok := a.Crawler.(crawler.DeepCrawler); ok {
		return deep.CrawlDeep(ctx, u, opt)
	}
	return a.Crawler.Crawl(ctx, u)
}

// crawlOptions converts the crawl settings for the crawler. The values are
// already clamped by settings.sanitizeIntake, so they pass through unchanged.
func crawlOptions(cfg settings.Settings) crawler.Options {
	return crawler.Options{
		Depth:    cfg.CrawlDepth,
		MaxPages: cfg.CrawlMaxPages,
		SameHost: cfg.CrawlSameHost,
		Include:  cfg.CrawlInclude,
		Exclude:  cfg.CrawlExclude,
	}
}

const (
	// pageCrawlTimeout is the budget for a one-page crawl; the user is
	// waiting at the paste box.
	pageCrawlTimeout = 30 * time.Second

	// deepCrawlTimeout is the budget for a multi-page walk. A healthy walk
	// never comes near it, and the status strip offers a stop button for the
	// rest.
	deepCrawlTimeout = 5 * time.Minute
)

// crawlBudget returns how long the whole crawl may take. Only a walk that was
// asked for gets the longer budget.
func crawlBudget(opt crawler.Options) time.Duration {
	if opt.Depth > 1 {
		return deepCrawlTimeout
	}
	return pageCrawlTimeout
}

// remoteListTimeout bounds one directory expansion. It is longer than a page
// crawl because an FTP server pays a round trip per nested folder.
const remoteListTimeout = 60 * time.Second

// listRemoteDir expands a link to a folder on the user's own server into one
// entry per file. A link to a single file yields nothing and is staged as
// itself. A failure is logged and the link is staged as itself, where it fails
// with a message naming it as a folder.
func (a *App) listRemoteDir(res resolver.Resolver, u string) []crawler.Result {
	r, ok := res.(remotefs.Resolver)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteListTimeout)
	defer cancel()
	// Stoppable like a page crawl; an unresponsive server costs a round trip
	// per level.
	done := a.startActivityRun(ActivityCrawl, cancel)
	defer done()
	found, err := r.List(ctx, u)
	if err != nil {
		log.Printf("list %s: %v", u, err)
		return nil
	}
	if len(found) == 0 {
		return nil
	}
	// The folder's name as the bucket title (see remotefs.PackageName).
	title := remotefs.PackageName(u)
	out := make([]crawler.Result, 0, len(found))
	for _, f := range found {
		out = append(out, crawler.Result{URL: f.URL, Name: f.Name, Size: f.Size, Title: title})
	}
	return out
}

// stagingResolverFor picks the backend a link is collected with, using the
// same per-URL ranking dispatch uses, host rule included. Registry.For only
// knows the static priorities, and the collected choice sticks:
// resolverForTaskLocked keeps t.Resolver while it is routable, so a hoster link
// JD can reach would otherwise go out as a plain "direct" GET.
func (a *App) stagingResolverFor(u string) resolver.Resolver {
	chain := hostChain(a.Registry.All(u), u, a.Settings.Get())
	if len(chain) == 0 {
		return nil
	}
	return chain[0]
}

// stagedAt hands out the moments links enter the list, each one later than
// the last. Dispatch starts links in CreatedAt order, and on Windows time.Now
// moves in steps of about half a millisecond, so a batch staged within one
// step would otherwise start in any order. When the clock has not moved on
// since the last link, the next one goes a nanosecond after it.
type stagedAt struct {
	mu   sync.Mutex
	last time.Time
}

func (s *stagedAt) next() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if !now.After(s.last) {
		now = s.last.Add(time.Nanosecond)
	}
	s.last = now
	return now
}

// stage creates one collected task for a URL. It is the only way a link enters
// the list, so crawled and pasted links pass the same filter, and everything
// that can refuse a link runs before put. It returns nil when no task was
// created and the held task when the filter refused the link.
//
// sizeHint is a byte count the caller already knows, or 0. Like a known name,
// it is not overwritten by a resolver's placeholder answer.
func (a *App) stage(u, name string, sizeHint int64, in intake) *core.Task {
	// One local clock reading for CreatedAt and <jd:date>; UTC would shift
	// dated folders by a day east of Greenwich.
	now := a.stamps.next()
	cand := rules.Candidate{URL: u, Source: in.source, Package: in.pkg, Added: now}
	if n := strings.TrimSpace(name); n != "" {
		cand.Filename = n
	}
	// First pass, before anything is fetched: rules on URL, hoster or source
	// save the network round trip.
	if in.waived == "" {
		if v := a.filter(cand); v.Rejected {
			return a.hold(cand, v, in.origin, now, nil)
		}
	}
	// An advisory duplicate check; the binding one is in put. A mirror the user
	// keeps is not short-circuited here, because it needs a resolved name and
	// size first.
	if m := a.mirror(dedupe.Entry{URL: u, Name: cand.Filename}); m.Seen() && !a.keepsAsSibling(m) {
		a.recordSkipped(u, m)
		return nil
	}

	t := &core.Task{
		URL:     u,
		Name:    u,
		Package: in.pkg,
		Status:  core.StatusCollected,
		Enabled: true,
		Source:  in.source,
		Origin:  in.origin,
		// The overruled filter reason, empty unless the link was restored (see
		// filterWaived).
		SkipReason: in.waived,
		// torrentHost, since a magnet names no single host.
		Host:      torrentHost(u),
		CreatedAt: now,
	}
	// Batch options go on before finishStaging runs the Packagizer, so a
	// matching rule overwrites them.
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
	if in.category != "" {
		t.Category = settings.CategoryID(in.category)
		// Given once, at creation, as packagize gives it to a link a rule files.
		if p, ok := a.Settings.Get().PriorityFor(t.Category); ok {
			t.Priority = p
		}
	}
	if cand.Filename != "" {
		t.Name = cand.Filename
	}
	if sizeHint > 0 {
		t.Size = sizeHint
	}
	res := a.stagingResolverFor(u)
	if res == nil {
		// Staged anyway, with the reason, so links never silently vanish.
		t.Error = a.unhandledError(u, "no backend handles this link")
		t.Reason = core.ReasonUnsupported
		t.Online = core.AvailOffline
		return a.finishStaging(t, cand)
	}
	t.Resolver = res.Info().ID
	t.Mode = a.modeForLocked(t, t.Resolver)
	result, err := res.Resolve(context.Background(), resolver.Request{URL: u})
	if err != nil {
		t.Error = err.Error()
		t.Reason = classify(failure{err: err})
		return a.finishStaging(t, cand)
	}
	// Resolvers that do not know the name yet answer with the URL itself; that
	// placeholder must not replace a name the link arrived with.
	if result.Name != "" && result.Name != u {
		t.Name = result.Name
	}
	if result.Size > 0 {
		t.Size = result.Size
	}
	if t.Resolver == "torrent" {
		// resolver.Result has no room for the info hash and trackers. For a
		// magnet, Describe only parses the URI locally.
		if md, err := (torrent.Resolver{}).Describe(u); err == nil {
			t.InfoHash = md.InfoHash
			t.Trackers = md.Trackers
		}
	}

	// Second pass, with name, size and a torrent's trackers known, still
	// before staging.
	cand.Filename, cand.Filesize = filename(t), t.Size
	if in.waived == "" {
		v := a.filter(cand)
		if !v.Rejected {
			v = trackerBan(t, a.Settings.Get().Torrent)
		}
		if v.Rejected {
			return a.hold(cand, v, in.origin, now, nil)
		}
	}
	staged := a.finishStaging(t, cand)
	// A HEAD probe for plain file links fills in size and availability while
	// the task waits in the collector.
	if staged != nil && res.Info().ID == "direct" {
		a.spawn(func() { a.analyze(t.ID, result.DirectURL) })
	} else if staged != nil && res.Info().ID == "ytdlp" {
		// Local map writes and a save, so it runs inline and the variant rows
		// exist as soon as the link appears.
		a.expandYtdlpVariants(staged)
		// A title probe, except for playlist entries, whose listing already
		// named them (see probePlaylistEntries).
		if !in.playlistEntry {
			a.spawn(func() { a.probeYtdlpTitle(t.ID, result.DirectURL) })
		}
	}
	return staged
}

// finishStaging applies the Packagizer and stages the task, or records the
// link as already covered. The Packagizer runs before put, so a rule's folder
// is in place before anything asks dirFor.
func (a *App) finishStaging(t *core.Task, cand rules.Candidate) *core.Task {
	cand.Filename, cand.Filesize, cand.Package = filename(t), t.Size, t.Package
	a.packagize(t, cand)
	if m, ok := a.put(t); !ok {
		// A kept mirror is staged here, where it finally has a name and size
		// of its own.
		if a.stageSibling(t, m) {
			return t
		}
		a.recordSkipped(t.URL, m)
		return nil
	}
	return t
}

// filename returns a task's file name, or "" while the name is still the URL
// placeholder, so rules do not match the URL as a file name.
func filename(t *core.Task) string {
	if t.Name == t.URL {
		return ""
	}
	return t.Name
}

// candidateOf describes an existing task to the rule engine. Source is left
// out so that source rules keep deciding at staging time only.
func candidateOf(t *core.Task) rules.Candidate {
	return rules.Candidate{
		URL:      t.URL,
		Filename: filename(t),
		Filesize: t.Size,
		Package:  t.Package,
		Added:    t.CreatedAt,
	}
}

// filter asks the link filter about a candidate. An empty rule set is never
// consulted.
func (a *App) filter(cand rules.Candidate) rules.Verdict {
	_, f := a.matchers()
	if f == nil || f.Empty() {
		return rules.Verdict{}
	}
	return f.Check(cand)
}

// filterWaived reports a link the user restored against the filter: not held,
// but still carrying the reason it was held for. The queue skips its
// final filter check for such links.
func filterWaived(t *core.Task) bool { return t != nil && !t.Skipped && t.SkipReason != "" }

// packagize applies the Packagizer's answer to a task before it is staged.
// Only fields a rule set are applied. The rename action is not, since backends
// choose the file name themselves and the list would disagree with the disk.
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
		// Already expanded by the rules package; dirFor uses it verbatim.
		t.Dir = e.Dir
	}
	if e.ExtractDir != "" {
		// Already expanded, like e.Dir.
		t.ExtractDir = e.ExtractDir
	}
	if e.Category != "" {
		// Kept even if the category was deleted; CategoryFor then answers with
		// the empty category.
		t.Category = e.Category
	}
	if e.Comment != "" {
		t.Comment = e.Comment
	}
	if e.Priority != nil {
		// Already clamped to SetPriority's range.
		t.Priority = *e.Priority
	} else if p, ok := a.Settings.Get().PriorityFor(t.Category); ok {
		// A category's priority is applied once, at creation. The dispatcher
		// cannot do it, because it would undo manual reordering, and 0 is a
		// real priority, so ok decides rather than p != 0. A rule's priority
		// wins over the category's.
		t.Priority = p
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

// hold parks a link the filter refused in the holding area. It is a real task,
// so it survives a restart and can be restored, but Skipped keeps it out of the
// collector, the queue and the counters. Nothing is resolved, so a refused host
// is never contacted.
//
// A torrent is read from its own link, which asks nobody. It keeps its
// trackers, so a tracker banned after a restore still stops it, and files, the
// selection ticked in its file tree, which a restore must not hand back to the
// file rules. files is nil for anything else.
func (a *App) hold(cand rules.Candidate, v rules.Verdict, origin core.Origin, now time.Time, files []core.TorrentFile) *core.Task {
	t := &core.Task{
		URL:     cand.URL,
		Name:    cand.URL,
		Package: cand.Package,
		Size:    cand.Filesize,
		Status:  core.StatusCollected,
		Skipped: true,
		// rejection() has already added the rule's name where needed.
		SkipReason: rejection(v),
		// Still enabled: once the rule is fixed, the link can be started.
		Enabled:   true,
		Source:    cand.Source,
		Origin:    origin,
		Host:      torrentHost(cand.URL),
		CreatedAt: now,
		// Online stays unset; nobody checked whether the link is alive.
	}
	if cand.Filename != "" {
		t.Name = cand.Filename
	}
	if torrent.IsURI(cand.URL) {
		if md, err := (torrent.Resolver{}).Describe(cand.URL); err == nil {
			t.InfoHash, t.Trackers = md.InfoHash, md.Trackers
		}
		t.TorrentFiles = files
	}
	if v.Rule != "" {
		// The rule as data, so clients need not parse it out of a sentence.
		t.MatchedRules = []string{v.Rule}
	}
	if m, ok := a.put(t); !ok {
		a.recordSkipped(t.URL, m)
		return nil
	}
	return t
}

// rejection returns the reason shown on a refused link, adding the rule's name
// unless the reason already quotes it.
func rejection(v rules.Verdict) string {
	if v.Rule == "" || strings.Contains(v.Reason, strconv.Quote(v.Rule)) {
		return v.Reason
	}
	return fmt.Sprintf("%s (link filter rule %q)", v.Reason, v.Rule)
}

// FilteredLinks returns the holding area, oldest first. It is derived from the
// task list, for clients that do not follow the task stream.
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

// RestoreFiltered moves held links back into the collector with the filter
// waived for them; an empty id list restores all. Without the waiver the
// queue's final filter check would refuse the link again.
func (a *App) RestoreFiltered(ids []string) []*core.Task {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0

	a.mu.Lock()
	var freed []taskCopy
	for id, t := range a.tasks {
		if !t.Skipped || !(all || want[id]) {
			continue
		}
		t.Skipped = false
		// SkipReason stays as the record of the waiver.
		freed = append(freed, a.copyLocked(t))
	}
	a.mu.Unlock()

	sort.Slice(freed, func(i, j int) bool { return freed[i].CreatedAt.Before(freed[j].CreatedAt) })
	out := make([]*core.Task, 0, len(freed))
	restored := make([]string, 0, len(freed))
	for i := range freed {
		c := &freed[i]
		a.publish(c)
		out = append(out, &c.Task)
		restored = append(restored, c.ID)
	}
	// Held links were never resolved, so recheck them in the background.
	if len(restored) > 0 {
		a.spawn(func() { a.RecheckTasks(restored) })
	}
	return out
}

// ClearFiltered deletes held links; an empty id list empties the holding area.
// Held links never downloaded anything, so no files are touched.
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
	// RemoveTasks also updates the mirror set and open screens.
	return a.RemoveTasks(doomed, false)
}

// heldLink reports whether a task id belongs to a held link. Caller must not
// hold mu.
func (a *App) heldLink(id string) bool {
	if id == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tasks[id]
	return t != nil && t.Skipped
}

// SkippedLink is a link that never became a task because it was folded into
// one already in the list, kept so the interface can say what happened to it.
// Filtered links are tasks in the holding area instead.
type SkippedLink struct {
	URL string `json:"url"`
	// Kind is what the mirror set decided: "duplicate" or "mirror".
	Kind   string    `json:"kind"`
	Reason string    `json:"reason"`
	OfID   string    `json:"ofId,omitempty"`
	Signal string    `json:"signal,omitempty"`
	At     time.Time `json:"at"`
}

// maxSkipped caps the trace; a watch folder rereading one list would otherwise
// grow it for ever.
const maxSkipped = 500

// mirror asks the mirror set whether a link is already covered. It takes a.mu
// on its own because the caller resolves the link before the binding check.
func (a *App) mirror(e dedupe.Entry) dedupe.Match {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dupes.Check(e)
}

// recordSkipped keeps and broadcasts a link folded into one already in the
// list, with the task and signal it matched so the user can check it.
func (a *App) recordSkipped(u string, m dedupe.Match) {
	a.pushSkipped(SkippedLink{
		URL:    u,
		Kind:   m.Verdict.String(),
		Reason: a.skipReason(m),
		OfID:   m.Of.ID,
		Signal: string(m.Signal),
		At:     time.Now(),
	})
}

// recordSkippedReason records something that failed before reaching the mirror
// set, such as a container that opened into nothing, after its request was
// already answered.
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

// skipReason is the sentence shown next to a folded link, naming what the
// match rests on.
func (a *App) skipReason(m dedupe.Match) string {
	if m.Verdict == dedupe.Duplicate {
		// The holding area is not the collector, so say where the copy is.
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

// AddLinksCnL implements the Click'n'Load listener's Adder. A submission may
// carry archive passwords for the links it sends.
func (a *App) AddLinksCnL(urls []string, pkg string, passwords []string) {
	a.AddLinksWithPasswords(urls, pkg, passwords, OriginCnL)
}

// AddLinksWithPasswords stages links that arrived with archive passwords. The
// first password goes on the tasks; the rest join the global list for later
// archives from the same source. The origin is a parameter because a bridge
// may relay a Click'n'Load submission over the REST API.
func (a *App) AddLinksWithPasswords(urls []string, pkg string, passwords []string, origin core.Origin) []*core.Task {
	created := a.addLinksWithPasswords(urls, pkg, passwords, origin)
	a.autoConfirm(idsOf(created))
	return a.detached(created)
}

// addLinksWithPasswords is AddLinksWithPasswords without the auto-confirm and
// the final copy, for a caller with more to write first (see addLinksFrom).
func (a *App) addLinksWithPasswords(urls []string, pkg string, passwords []string, origin core.Origin) []*core.Task {
	created := a.addLinksFrom(urls, pkg, origin, LinkBatchOptions{})
	var first string
	for _, pw := range passwords {
		if pw = strings.TrimSpace(pw); pw != "" {
			first = pw
			break
		}
	}
	if first == "" || len(created) == 0 {
		return created
	}
	ids := a.withVariantFamilies(idsOf(created))
	if err := a.SetTaskOptions(ids, TaskOptions{Password: &first}); err != nil {
		log.Printf("could not apply the supplied archive password: %v", err)
	}
	if len(passwords) > 1 {
		a.rememberPasswords(passwords)
	}
	return created
}

// rememberPasswords adds a submission's passwords to the global list, so later
// archives from the same source can be opened.
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
