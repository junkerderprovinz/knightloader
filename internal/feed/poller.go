package feed

// One subscription being polled: the fetch, the memory of what was already
// handed over, and the rule for what counts as new.

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// maxSeen is how many entry keys an RSS subscription remembers. It must be
// at least maxItems, or an entry still in the feed could age out and be
// staged again, and it is larger so an entry that briefly drops out and
// returns is still recognised. 500 keys is about 10 KB per subscription.
const maxSeen = 500

// maxSeenListing is the same for channels and Mediathek searches. A channel's
// backfill can hand over its whole history, and forgetting part of it would
// stage those videos again should they reappear in a listing.
const maxSeenListing = 20000

// channelWindow is how many of a channel's newest videos an ordinary poll
// lists: far more than a channel publishes between polls, and few enough
// that a poll is a handful of page requests.
const channelWindow = 100

func seenCap(k Kind) int {
	if k == KindChannel || k == KindMediathek {
		return maxSeenListing
	}
	return maxSeen
}

// poller polls one feed and hands each new entry to a sink.
type poller struct {
	// url is the fetch address and the key the memory is stored under. A
	// different address is a different subscription with its own poller.
	url   string
	src   Sources
	state State
	onJob func(Job)

	// ctx bounds this poller's fetches and is cancelled by close as well as
	// by shutdown, so a settings save that removes a subscription does not
	// wait out a slow publisher.
	ctx    context.Context
	cancel context.CancelFunc

	// cmu guards sub and match, which Apply replaces in place so an edited
	// subscription keeps its memory rather than seeding again.
	cmu   sync.Mutex
	sub   Subscription
	match matcher

	// seen, order, primed and loaded belong to the polling goroutine alone.
	// primed tells "polled before, remembered nothing" from "never polled";
	// see seed.
	seen   map[string]bool
	order  []string
	primed bool
	loaded bool

	// hmu guards copies of the polling goroutine's facts for Health, so the
	// polling path needs no lock on seen and order.
	hmu        sync.Mutex
	lastPolled time.Time
	lastErr    string
	seeded     bool
	remembered int

	// started says whether loop runs, and so whether close has a goroutine
	// to wait for. It is only touched under the Runner's lock.
	started   bool
	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
	// wake tells a sleeping loop that the subscription was edited. It is
	// buffered by one and sent to without blocking, so saving twice never
	// stalls.
	wake chan struct{}
}

func newPoller(r *Runner, s Subscription) *poller {
	ctx, cancel := context.WithCancel(r.ctx)
	return &poller{
		url:    s.URL,
		src:    r.src,
		state:  r.state,
		onJob:  r.onJob,
		ctx:    ctx,
		cancel: cancel,
		sub:    s,
		match:  s.matcher(),
		seen:   make(map[string]bool),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		wake:   make(chan struct{}, 1),
	}
}

// configure installs an edited subscription on a running poller and wakes it,
// so the next poll is measured against the new interval at once.
func (p *poller) configure(s Subscription) {
	p.cmu.Lock()
	p.sub, p.match = s, s.matcher()
	p.cmu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *poller) config() (Subscription, matcher) {
	p.cmu.Lock()
	defer p.cmu.Unlock()
	return p.sub, p.match
}

func (p *poller) start() {
	if p.started {
		return
	}
	p.started = true
	go p.loop()
}

// close stops the loop and waits for a running poll to finish, so no OnJob
// call is in flight afterwards and a removed and re-added subscription never
// has two pollers. The fetch is cancelled first, so the wait is for the
// handover, not for a remote server.
func (p *poller) close() {
	p.closeOnce.Do(func() {
		close(p.stop)
		p.cancel()
	})
	if p.started {
		<-p.done
	}
}

func (p *poller) loop() {
	defer close(p.done)
	// Poll at once, so a box that reboots more often than the interval still
	// polls.
	if !p.sleeping() {
		p.poll()
	}
	// The next poll is due an interval after the last one, whatever the
	// interval is by then, so a save that leaves it alone moves nothing and
	// a shorter one already overdue polls at once.
	last := time.Now()
	for {
		sub, _ := p.config()
		t := time.NewTimer(time.Until(last.Add(sub.Every())))
		select {
		case <-p.stop:
			t.Stop()
			return
		case <-p.ctx.Done():
			// Shutdown cancels only the context, never the channel.
			t.Stop()
			return
		case <-p.wake:
			t.Stop()
		case <-t.C:
			p.poll()
			last = time.Now()
		}
	}
}

// sleeping reports whether the poller was closed before the loop started.
func (p *poller) sleeping() bool {
	select {
	case <-p.stop:
		return true
	case <-p.ctx.Done():
		return true
	default:
		return false
	}
}

// poll fetches the feed once and hands over whatever is new.
func (p *poller) poll() {
	if err := p.load(); err != nil {
		// With an unreadable memory the poll would look like a first run and
		// stage the whole feed, so it is skipped.
		log.Print(core.RedactText(fmt.Sprintf("feed %s was not polled: %v", p.url, err)))
		p.notePoll(err)
		return
	}
	sub, m := p.config()
	w := p.window(sub)
	f, err := p.src.read(p.ctx, sub, w.q)
	if err != nil {
		log.Print(core.RedactText(fmt.Sprintf("feed %s was not read: %v", p.url, err)))
		p.notePoll(err)
		return
	}

	fresh := p.pick(f, sub, m, w)
	// Remember before handing over, as watch.consume does: a crash mid-handover
	// loses one entry rather than restaging the batch on every poll.
	p.remember(f, seenCap(sub.Kind))
	p.notePoll(nil)

	for _, j := range fresh {
		p.onJob(j)
	}
}

// notePoll records how the poll ended and what the subscription knows now.
// It runs on the polling goroutine, which is what makes reading primed and
// order safe. A failure says the next poll retries, so nobody deletes a
// subscription over a temporary 503.
func (p *poller) notePoll(err error) {
	p.hmu.Lock()
	defer p.hmu.Unlock()
	// The time of the poll that ran, successful or not; together with the
	// error it shows a subscription that keeps trying and failing.
	p.lastPolled = time.Now()
	p.seeded, p.remembered = p.primed, len(p.order)
	if err == nil {
		p.lastErr = ""
		return
	}
	p.lastErr = err.Error() + "; the next poll tries again"
}

func (p *poller) health() Health {
	p.hmu.Lock()
	defer p.hmu.Unlock()
	return Health{
		URL:        p.url,
		LastPolled: p.lastPolled,
		LastError:  p.lastErr,
		Seeded:     p.seeded,
		Remembered: p.remembered,
	}
}

// load reads the stored memory on the first poll rather than in newPoller,
// which runs under the Runner's lock; a failed read is retried next time.
func (p *poller) load() error {
	if p.loaded {
		return nil
	}
	if p.state == nil {
		// No persistence: the memory lasts as long as the process, and the
		// first poll still seeds.
		p.loaded = true
		return nil
	}
	keys, known, err := p.state.Seen(p.url)
	if err != nil {
		return fmt.Errorf("its record of what it has already added could not be read: %w", err)
	}
	for _, k := range keys {
		p.seen[k] = true
	}
	p.order = keys
	p.primed = known
	p.loaded = true
	return nil
}

// backfills reports whether a subscription's first poll hands over what is
// already there rather than seeding.
func backfills(sub Subscription) bool {
	return sub.Kind == KindChannel && sub.Backfill != nil
}

// window is what one poll lists of a channel and how much of that a
// backfill hands over: the first upto entries, or those since a date. A
// backfill lists at least an ordinary poll's window, so the second poll finds
// nothing it would take for new.
type window struct {
	q     ListQuery
	upto  int
	since time.Time
}

func (p *poller) window(sub Subscription) window {
	w := window{q: ListQuery{Limit: channelWindow}}
	if p.primed || !backfills(sub) {
		return w
	}
	switch b := sub.Backfill; b.Mode {
	case BackfillCount:
		w.q.Limit, w.upto = max(b.N, channelWindow), b.N
	case BackfillDays:
		w.since = time.Now().AddDate(0, 0, -b.N)
		w.q.Since = w.since
	default:
		w.q = ListQuery{}
	}
	return w
}

// pick decides which of a document's entries are handed over. A first poll
// hands over nothing unless the subscription backfills; see seed.
func (p *poller) pick(f Feed, sub Subscription, m matcher, w window) []Job {
	first := !p.primed
	if first && !backfills(sub) {
		p.seed(f)
		return nil
	}
	var out []Job
	for i, it := range f.Items {
		if p.seen[it.Key()] || (w.upto > 0 && i >= w.upto) {
			continue
		}
		if !w.since.IsZero() && !it.Published.IsZero() && it.Published.Before(w.since) {
			continue
		}
		it, why := m.judge(p.ctx, p.src.Prober, it)
		if why != "" {
			continue
		}
		out = append(out, newJob(it, f, sub))
	}
	if first {
		log.Print(core.RedactText(fmt.Sprintf("feed %s: the first poll hands over %d of the %d entries already there, as its backfill asks", p.url, len(out), len(f.Items))))
	}
	return out
}

// newJob is one picked entry with everything the subscription asked for.
func newJob(it Item, f Feed, sub Subscription) Job {
	show := showName(f, sub)
	j := Job{
		URL:      it.Link,
		Package:  packageName(it, f, sub.URL),
		Show:     show,
		Title:    it.Title,
		FileName: it.FileName,
		Dir:      sub.Dir,
		Priority: copyPriority(sub.Priority),
		Source:   sub.URL,
		Duration: it.Duration,
		Sub:      sub,
	}
	j.Sub.Priority = copyPriority(sub.Priority)
	if sub.Naming == NamingChannel {
		j.Package = show
	}
	if it.Subtitle != "" {
		j.Subtitle = it.Subtitle
		j.SubtitleName = subtitleName(it, it.Subtitle)
	}
	return j
}

// showName is the channel or show a subscription follows: what the user
// called it, else what the feed calls itself, else its address.
func showName(f Feed, sub Subscription) string {
	if sub.Title != "" {
		return sub.Title
	}
	if f.Title != "" {
		return f.Title
	}
	return sub.URL
}

// subtitleName names a subtitle file after its video, with the extension its
// address shows or TTML, which the broadcasters serve without one.
func subtitleName(it Item, link string) string {
	base := strings.TrimSuffix(it.FileName, path.Ext(it.FileName))
	if base == "" {
		base = it.Title
	}
	ext := ".ttml"
	if u, err := url.Parse(link); err == nil {
		switch e := strings.ToLower(path.Ext(u.Path)); e {
		case ".srt", ".vtt", ".ttml", ".xml", ".dfxp", ".ass", ".sub":
			ext = e
		}
	}
	return base + ext
}

// seed is a subscription's first poll: every entry in the feed is remembered
// (by remember) and none is handed over.
//
// A feed document is a window over the publisher's recent entries, so a new
// subscription would otherwise stage the whole back catalogue, and with
// auto-confirm download it. Gating on entry dates does not work: many feeds
// have none, some have no time zone, and backdated posts would be skipped
// forever. The cost of seeding is that an entry published between setup and
// the first fetch is never staged.
func (p *poller) seed(f Feed) {
	log.Print(core.RedactText(fmt.Sprintf("feed %s: %d entries already there are marked as seen, so only what appears from now on is added", p.url, len(f.Items))))
}

// remember records every entry in the document, filtered or not, and drops
// the oldest keys once the memory holds limit of them. The document's keys
// go first on every poll, so an entry still in the feed never ages out.
func (p *poller) remember(f Feed, limit int) {
	next := make([]string, 0, len(f.Items)+len(p.order))
	fresh := make(map[string]bool, len(f.Items))
	for _, it := range f.Items {
		k := it.Key()
		if fresh[k] {
			// A template that repeats an item would waste a slot.
			continue
		}
		fresh[k] = true
		next = append(next, k)
	}
	for _, k := range p.order {
		if !fresh[k] {
			next = append(next, k)
		}
	}
	if len(next) > limit {
		next = next[:limit]
	}

	p.order = next
	p.seen = make(map[string]bool, len(next))
	for _, k := range next {
		p.seen[k] = true
	}
	p.primed = true

	if p.state == nil {
		return
	}
	if err := p.state.SetSeen(p.url, next); err != nil {
		// The memory still works in this process, but after a restart the feed
		// would be seeded again.
		log.Print(core.RedactText(fmt.Sprintf("feed %s: what it has already added could not be written down, so a restart would treat this feed as new again: %v", p.url, err)))
	}
}

// fetch is one GET of one feed document. The preview (preview.go) uses it too,
// so a test fetch reports exactly what a poll would get.
func fetch(ctx context.Context, hc *http.Client, rawurl string) (Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return Feed{}, err
	}
	// A redirect is the publisher's choice, and must not take the request
	// further into the network than the subscribed address.
	req = req.WithContext(httpx.Confine(ctx, scopeOf(ctx, req.URL.Hostname())))
	// Some sites serve HTML to a client that accepts anything.
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.5")
	resp, err := hc.Do(req)
	if err != nil {
		return Feed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// A 404 page or a login form parses to zero entries and would read as
		// "nothing new" forever. Drain a little so the connection is reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return Feed{}, fmt.Errorf("the server answered %s", resp.Status)
	}
	return Parse(rawurl, resp.Body)
}

// scopeOf is how far into the network a fetch from host may go. The services
// the resolver and the Mediathek kind talk to are public, which needs no DNS
// lookup to know.
func scopeOf(ctx context.Context, host string) httpx.Scope {
	switch strings.ToLower(host) {
	case "www.youtube.com", "mediathekviewweb.de":
		return httpx.ScopePublic
	}
	return httpx.ScopeOfHost(ctx, host)
}

// packageName is what a staged link is filed under: the entry's title, else
// the feed's, else the feed URL, so a package is never empty.
func packageName(it Item, f Feed, feedURL string) string {
	if it.Title != "" {
		return it.Title
	}
	if f.Title != "" {
		return f.Title
	}
	return feedURL
}

// copyPriority returns a copy so tasks never share the subscription's
// pointer.
func copyPriority(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
