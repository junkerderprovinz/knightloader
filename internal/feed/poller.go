package feed

// One subscription being polled: the fetch, the memory of what has already been
// handed over, and the rule that decides an entry is new. Everything here is
// about a single feed; which feeds exist at all is runner.go's problem.

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// maxSeen is how many entry keys one subscription remembers.
//
// The memory has to be at least as large as one document (maxItems) or an entry
// still sitting in the feed could age out of it and be staged a second time
// while it is still there. It is larger than that so an entry that briefly falls
// out of a feed and comes back, which publishers do when they fix a post, is
// still recognised.
//
// It is not unbounded, and that is the trade: the memory is persisted, and a
// list that only ever grows would eventually be refused by the store it is
// written to. Being refused is the bad outcome, because a memory that cannot be
// saved comes back empty after a restart and stages the whole feed again. Five
// hundred keys is about ten kilobytes per subscription.
const maxSeen = 500

// poller polls one feed and hands each new entry to a sink.
type poller struct {
	// url is the fetch address and the key the memory is stored under. It never
	// changes: a subscription that names a different address is a different
	// subscription, and runner.Apply builds a new poller for it.
	url   string
	http  *http.Client
	state State
	onJob func(Job)

	// ctx bounds this poller's fetches and is cancelled by close as well as by
	// the app shutting down. Cancelling it on close is what keeps a settings save
	// short: Apply closes the pollers that are going and waits for them, and
	// without this that wait would be a fetch sitting on a publisher's server for
	// the client's whole timeout, with the person who pressed save watching a
	// spinner the whole time.
	ctx    context.Context
	cancel context.CancelFunc

	// cmu guards sub and filt, which Apply replaces while the loop is asleep or
	// in the middle of a poll. The interval, the filter, the folder and the
	// priority can all be edited without the subscription losing what it knows,
	// which is the same promise watch.Apply makes for a drop folder that did not
	// change: rebuilding here would throw away the memory of what has already
	// been handed over and, on a subscription polled for the first time, would
	// re-run the seeding pass.
	cmu  sync.Mutex
	sub  Subscription
	filt *regexp.Regexp

	// seen, order and primed are touched only by the polling goroutine, so they
	// need no lock. primed is the difference between "this subscription has been
	// polled before and remembered nothing" and "this subscription has never been
	// polled", which is the whole of the first-run problem. See seed.
	seen   map[string]bool
	order  []string
	primed bool
	loaded bool

	// started says whether loop is running, and therefore whether close has a
	// goroutine to wait for. It is written and read under the Runner's lock and
	// nowhere else, which is why it carries no lock of its own.
	started   bool
	closeOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
	// wake is how a changed interval reaches a loop that is already asleep.
	// Buffered by one and sent to without blocking, the same arrangement
	// schedule.Runner uses: a settings page saved twice in a row must not stall
	// the saver, and one pending wake-up is as good as two.
	wake chan struct{}
}

func newPoller(r *Runner, s Subscription) *poller {
	ctx, cancel := context.WithCancel(r.ctx)
	return &poller{
		url:    s.URL,
		http:   r.http,
		state:  r.state,
		onJob:  r.onJob,
		ctx:    ctx,
		cancel: cancel,
		sub:    s,
		filt:   s.filter(),
		seen:   make(map[string]bool),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		wake:   make(chan struct{}, 1),
	}
}

// configure installs an edited row on a running poller and cuts the current
// sleep short, so a shortened interval takes effect now rather than at the end
// of the wait it was shortened from.
func (p *poller) configure(s Subscription) {
	p.cmu.Lock()
	p.sub, p.filt = s, s.filter()
	p.cmu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *poller) config() (Subscription, *regexp.Regexp) {
	p.cmu.Lock()
	defer p.cmu.Unlock()
	return p.sub, p.filt
}

func (p *poller) start() {
	if p.started {
		return
	}
	p.started = true
	go p.loop()
}

// close stops the polling loop and waits for the running poll to finish, so no
// OnJob call is still in flight once it returns. That wait is what lets a
// subscription be removed and added back without two goroutines ever fetching
// one feed, which would hand the same entry over twice.
//
// The fetch is cancelled first and the wait comes after, so what is waited on is
// the handover and never a publisher's server.
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
	// Poll straight away. A process that restarts every night must still notice
	// what a feed published overnight, and holding the first poll back by a full
	// interval would mean a subscription set to a week is never polled at all on a
	// box that reboots more often than that.
	if !p.sleeping() {
		p.poll()
	}
	for {
		sub, _ := p.config()
		// A fresh timer each round rather than one Ticker, because the period is
		// re-read every round: a Ticker built once would keep the interval the
		// subscription had when it started, and an edit to it would appear to have
		// been saved and do nothing.
		t := time.NewTimer(sub.Every())
		select {
		case <-p.stop:
			t.Stop()
			return
		case <-p.ctx.Done():
			// The app is shutting down, or close cancelled this poller's own
			// context. Both are the same exit as stop above, and both are listed
			// because a shutdown cancels only the app's context and never touches
			// the channel.
			t.Stop()
			return
		case <-p.wake:
			// The row was edited. Round again so the new interval is read, without
			// fetching: a settings save is not a reason to make a request.
			t.Stop()
		case <-t.C:
			p.poll()
		}
	}
}

// sleeping reports whether the loop has already been told to stop, so the first
// poll is not made by a poller that was closed between start and the goroutine
// actually running.
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
	if !p.load() {
		// The memory could not be read. Polling anyway would mean polling with an
		// empty memory, and an empty memory is indistinguishable from a
		// subscription that has never run: everything currently in the feed would
		// be staged. Skipping the poll costs one interval; the alternative costs
		// the user a collector full of links they already have.
		return
	}
	sub, filt := p.config()
	f, err := p.fetch()
	if err != nil {
		// A publisher's server is allowed to be down, and the next tick tries
		// again. It is logged rather than swallowed because a subscription that
		// quietly produces nothing for a month looks exactly like one that is not
		// running at all.
		log.Printf("feed %s was not read: %v", p.url, err)
		return
	}

	fresh := p.pick(f, sub, filt)
	// Remembered and written down BEFORE anything is handed over, which is the
	// same order watch.consume retires a drop file in and for the same reason: if
	// the sink panics or the process dies in the middle of the handover we would
	// rather lose one entry than re-stage the whole batch on every poll from here
	// to eternity.
	p.remember(f)

	for _, j := range fresh {
		p.onJob(j)
	}
}

// load reads the stored memory, once, on the first poll rather than when the
// poller is built. Building happens under the Runner's lock, and a store read is
// not something a settings save should wait on; doing it here also means a store
// that was briefly unreadable is retried at the next tick instead of leaving the
// subscription permanently blind.
func (p *poller) load() bool {
	if p.loaded {
		return true
	}
	if p.state == nil {
		// No persistence configured. The memory then lives for as long as this
		// process does, which is the right behaviour for a test and for anybody
		// embedding the runner without a store: the first poll still seeds, so
		// nothing is dumped into the collector either way.
		p.loaded = true
		return true
	}
	keys, known, err := p.state.Seen(p.url)
	if err != nil {
		log.Printf("feed %s was not polled: its record of what it has already added could not be read: %v", p.url, err)
		return false
	}
	for _, k := range keys {
		p.seen[k] = true
	}
	p.order = keys
	p.primed = known
	p.loaded = true
	return true
}

// pick decides which of a document's entries are handed over.
//
// A subscription polled for the first time hands over nothing at all. See seed
// for why that is not a compromise but the only answer that works.
func (p *poller) pick(f Feed, sub Subscription, filt *regexp.Regexp) []Job {
	if !p.primed {
		p.seed(f)
		return nil
	}
	var out []Job
	for _, it := range f.Items {
		if p.seen[it.Key()] {
			continue
		}
		if !sub.wants(filt, it.Title) {
			continue
		}
		out = append(out, Job{
			URL:      it.Link,
			Package:  packageName(it, f, p.url),
			Dir:      sub.Dir,
			Priority: copyPriority(sub.Priority),
			Source:   p.url,
		})
	}
	return out
}

// seed is what a subscription's very first poll does: every entry the feed is
// carrying is written down and none of them is handed over.
//
// The writing itself is remember's, which the poll does either way, so this
// function is only the decision and the log line. It exists as a function rather
// than as a comment inside pick because the reasoning below is the single
// largest thing a reader has to be told about this package, and an argument this
// long buried in the middle of a loop is an argument nobody finds.
//
// THE PROBLEM. A feed's document is a window, not a diary. A newly configured
// subscription pointed at an active feed is handed the last fifty, or two
// hundred, entries in its very first response, and every one of them is new as
// far as this app is concerned. Staging them would fill the collector with a
// publisher's back catalogue the instant somebody pastes an address, and on a
// box with auto-confirm on it would start downloading all of them.
//
// THE ANSWER, and why it is this one. The alternative is to stage only entries
// whose date is later than the moment the subscription was created, and it does
// not work, because the dates are not trustworthy enough to gate on. Plenty of
// feeds publish no date at all; plenty publish one in the publisher's local time
// with no zone, so an entry from an hour ago can read as being from the future
// or from yesterday depending on where the box is; and a publisher that
// backdates a post would have it silently skipped forever. Writing the keys down
// uses the same identity everything else in this package uses (Item.Key), needs
// no clock on either side, and gives one unambiguous rule: an entry is new when
// this subscription has not seen it before, and on the first poll it has seen
// everything.
//
// What it costs is stated plainly: an entry published in the seconds between
// somebody pasting the address and the first fetch is never staged. That is one
// entry, once, at setup, against a whole back catalogue on every new
// subscription.
func (p *poller) seed(f Feed) {
	log.Printf("feed %s: %d entries already there are marked as seen, so only what appears from now on is added", p.url, len(f.Items))
}

// remember writes down every entry in the document, matched by the filter or
// not, and drops the oldest keys once the memory is full.
//
// The new list is this document's keys in document order, followed by whatever
// was already known and is not in the document any more. That ordering is what
// keeps an entry that is still in the feed from ever ageing out of the memory
// while it is still being served: it is put back at the front on every single
// poll, and only entries the publisher has actually dropped drift towards the
// cut.
func (p *poller) remember(f Feed) {
	next := make([]string, 0, len(f.Items)+len(p.order))
	fresh := make(map[string]bool, len(f.Items))
	for _, it := range f.Items {
		k := it.Key()
		if fresh[k] {
			// One document listing the same entry twice, which happens when a
			// publisher's template repeats an item. Keeping both would waste a slot
			// in a bounded memory.
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
	if len(next) > maxSeen {
		next = next[:maxSeen]
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
		// Not fatal to this poll, because the entries have still been recognised
		// in memory and the process is still running. It is logged loudly because
		// the consequence lands at the next restart, a long way from the cause: a
		// memory that never reached disk means the whole feed is seeded again, and
		// the first entries after that are the ones nobody gets.
		log.Printf("feed %s: what it has already added could not be written down, so a restart would treat this feed as new again: %v", p.url, err)
	}
}

// fetch gets the feed document. The context is the app's own, so a shutdown
// cancels a request that is still waiting on somebody else's server rather than
// holding the process open for the client's whole timeout.
func (p *poller) fetch() (Feed, error) {
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return Feed{}, err
	}
	// Said explicitly because a fair number of sites serve HTML to a client that
	// asks for anything, and the resulting parse error names XML rather than the
	// content negotiation that caused it.
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.5")
	resp, err := p.http.Do(req)
	if err != nil {
		return Feed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Checked before the body is read, and reported with the code. A 404 page
		// or a login form is still a body that parses to zero entries, and a
		// subscription that reports "no new entries" forever is the version of this
		// failure nobody ever debugs.
		//
		// The body is drained first so the connection can go back into the pool
		// instead of being dropped and dialled again at the next tick.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return Feed{}, fmt.Errorf("the server answered %s", resp.Status)
	}
	return Parse(p.url, resp.Body)
}

// packageName is what the staged link is filed under. The entry's own title is
// what somebody recognises in the collector; the feed's name is the next best
// thing when an entry has none, and the host is the last resort so that a task
// is never filed under an empty package.
func packageName(it Item, f Feed, feedURL string) string {
	if it.Title != "" {
		return it.Title
	}
	if f.Title != "" {
		return f.Title
	}
	return feedURL
}

// copyPriority hands out a value of its own rather than the subscription's
// pointer. The subscription is shared by every entry this poller stages, and
// passing its pointer to a setter that may keep it is how two tasks end up
// sharing one priority.
func copyPriority(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
