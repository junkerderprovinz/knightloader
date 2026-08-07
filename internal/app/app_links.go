package app

// Everything between a pasted string and a staged task: the filter, the crawl,
// the Packagizer, the mirror set, and the trace of links that never made it.

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

// AddLinks resolves each URL and stages it in the link collector (JD-style):
// tasks are created "collected" (analysed but not started). StartTasks moves
// them into the download queue.
func (a *App) AddLinks(urls []string, pkg string) []*core.Task {
	var created []*core.Task
	// seen is about the pasted text and nothing else: it stops one page being
	// fetched twice when it appears twice in the same box. Whether a *link* is
	// already in the list is the mirror set's answer alone. Two ideas of "we
	// already have this" that normalise URLs differently disagree sooner or later,
	// and then a link a raw string comparison let through comes back reported as a
	// duplicate of itself.
	seen := map[string]bool{}
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
			if t := a.stageRejected(cand, v, cand.Added); t != nil {
				created = append(created, t)
			}
			continue
		}
		// A page that points at files becomes those files, not one task for the
		// page. Without this a gallery or an index listing can only ever be a
		// single unusable download.
		if crawled := a.crawl(u); len(crawled) > 0 {
			for _, c := range crawled {
				if c.URL == "" {
					continue
				}
				// The page is passed as the source, which is the only place a rule
				// keyed on "where did this link come from" can get it.
				if t := a.stage(c.URL, c.Name, pkg, u); t != nil {
					created = append(created, t)
				}
			}
			continue
		}
		if t := a.stage(u, "", pkg, ""); t != nil {
			created = append(created, t)
		}
	}
	// A batch pasted without a package name gets one derived from the links
	// themselves. Without this everything lands in "ungrouped", which is where
	// a collector stops being useful the moment there is more than one batch in
	// it.
	//
	// A task a Packagizer rule already named is left out. The rule is the more
	// specific answer and it ran first, so overwriting it here would make a rule
	// that works look like one that does nothing.
	if strings.TrimSpace(pkg) == "" {
		if derived := derivePackage(created); derived != "" {
			ids := make([]string, 0, len(created))
			for _, t := range created {
				if strings.TrimSpace(t.Package) == "" {
					ids = append(ids, t.ID)
				}
			}
			if len(ids) > 0 {
				a.SetPackage(ids, derived)
			}
		}
	}

	// Auto-start hands everything straight to the queue for users who don't
	// want the staging step.
	if len(created) > 0 && a.Settings.Get().AutoStart {
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		a.StartTasks(ids)
	}
	return created
}

// derivePackage guesses a name for a batch that arrived without one: the shared
// stem of the file names if the links look like parts of one thing, else the
// host they came from. It returns "" when neither is worth using, because a bad
// guess is worse than no group at all.
func derivePackage(tasks []*core.Task) string {
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
// source is the page a crawl found the link on, and is empty for a pasted link.
// It returns nil when the link never became a task.
func (a *App) stage(u, name, pkg, source string) *core.Task {
	// One clock reading for the whole link, in local time: it is what CreatedAt
	// gets and what pathvars formats for <jd:date>, and a UTC reading here would
	// flip a dated folder name a day early for everyone east of Greenwich.
	now := time.Now()
	cand := rules.Candidate{URL: u, Source: source, Package: pkg, Added: now}
	if n := strings.TrimSpace(name); n != "" {
		cand.Filename = n
	}
	// First pass, before anything is fetched. The byte count and the file type
	// are still unknown, so a rule keyed on those cannot fire yet — but a reject
	// on the URL, the hoster or the source page saves the whole network round
	// trip, which on a paste of several thousand links is the entire cost.
	if v := a.filter(cand); v.Rejected {
		return a.stageRejected(cand, v, now)
	}
	// An advisory look before the expensive part, so an obvious duplicate costs
	// nothing. The binding check is in put, under the lock that inserts the task.
	if m := a.mirror(dedupe.Entry{URL: u, Name: cand.Filename}); m.Seen() {
		a.recordSkipped(u, m)
		return nil
	}

	t := &core.Task{
		URL:     u,
		Name:    u,
		Package: pkg,
		Status:  core.StatusCollected,
		// Set here and not left to the zero value: a link nobody has switched off
		// is on, and a Task built without this would be staged already disabled.
		Enabled:   true,
		Source:    source,
		Host:      hostOf(u),
		CreatedAt: now,
	}
	if cand.Filename != "" {
		t.Name = cand.Filename
	}
	res := a.Registry.For(u)
	if res == nil {
		// A link is never dropped on the floor. If nothing can handle it, or
		// resolving fails, it is still staged — with the reason on it — so the
		// user can see what happened instead of watching links vanish.
		t.Error = "no backend handles this link"
		t.Online = core.AvailOffline
		return a.finishStaging(t, cand)
	}
	t.Resolver = res.Info().ID
	result, err := res.Resolve(context.Background(), resolver.Request{URL: u})
	if err != nil {
		t.Error = err.Error()
		return a.finishStaging(t, cand)
	}
	if result.Name != "" {
		t.Name = result.Name
	}
	t.Size = result.Size

	// Second pass, now that the name and the byte count exist. This is the one
	// that can act on a size or a file-type condition, and it still runs before
	// the task is staged.
	cand.Filename, cand.Filesize = filename(t), t.Size
	if v := a.filter(cand); v.Rejected {
		return a.stageRejected(cand, v, now)
	}
	staged := a.finishStaging(t, cand)
	// Lightweight analysis for plain file links: a HEAD gives size + an online
	// check while the task waits in the collector.
	if staged != nil && res.Info().ID == "direct" {
		go a.analyze(t.ID, result.DirectURL)
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

// stageRejected records a link the filter refused.
//
// It is staged rather than dropped, through the same mechanism a link no backend
// handles goes through: the reason on the task and an offline availability.
// Eating links in silence is JDownloader's single most complained-about
// behaviour, and a link that disappears without a trace is indistinguishable
// from a bug in the paste box.
//
// It stays collected rather than being settled as an error, so it reads as what
// it is — a link in the collector that will not be taken — and so a user who
// fixes the rule can simply start it. dispatchLocked asks the filter again
// before any bytes move, which is what stops "start everything" from undoing the
// filter in the meantime.
func (a *App) stageRejected(cand rules.Candidate, v rules.Verdict, now time.Time) *core.Task {
	t := &core.Task{
		URL:     cand.URL,
		Name:    cand.URL,
		Package: cand.Package,
		Status:  core.StatusCollected,
		Online:  core.AvailOffline,
		Error:   rejection(v),
		// A refused link is still an enabled one: what stopped it was the filter,
		// and a user who fixes the rule expects to be able to start it.
		Enabled:   true,
		Source:    cand.Source,
		Host:      hostOf(cand.URL),
		CreatedAt: now,
	}
	if cand.Filename != "" {
		t.Name = cand.Filename
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

// SkippedLink is a link that never became a task. It is kept so the interface
// can say what happened to it: a link folded away with nothing to show for it
// looks exactly like a bug in the paste box, and gets reported as one.
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
		Reason: skipReason(m),
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
func skipReason(m dedupe.Match) string {
	if m.Verdict == dedupe.Duplicate {
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
	a.AddLinksWithPasswords(urls, pkg, passwords)
}

// AddLinksWithPasswords stages links that arrived together with the archive
// passwords for them. The first password rides on the tasks themselves, because
// it was supplied for exactly these files; the rest join the global list, where
// a later archive from the same source can still reach them.
func (a *App) AddLinksWithPasswords(urls []string, pkg string, passwords []string) []*core.Task {
	created := a.AddLinks(urls, pkg)
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
	return created
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

// onWatchJob stages what a dropped file asked for. It runs on the watcher's
// goroutine, so it hands the slow part off and returns quickly.
func (a *App) onWatchJob(j watch.Job) {
	go func() {
		created := a.AddLinks(j.URLs, j.Package)
		if len(created) == 0 {
			return
		}
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		if j.Dir != "" || j.Password != "" {
			opts := TaskOptions{}
			if j.Dir != "" {
				opts.Dir = &j.Dir
			}
			if j.Password != "" {
				opts.Password = &j.Password
			}
			if err := a.SetTaskOptions(ids, opts); err != nil {
				log.Printf("dropped job: %v", err)
			}
		}
		// AutoStart on the job wins over the global setting, which has already
		// started them if it was on.
		if j.AutoStart && !a.Settings.Get().AutoStart {
			a.StartTasks(ids)
		}
	}()
}
