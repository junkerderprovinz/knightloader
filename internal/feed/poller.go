package feed

// One subscription being polled: the fetch, the memory of what was already
// handed over, and the rule for what counts as new.

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

// maxSeen is how many entry keys one subscription remembers. It must be at
// least maxItems, or an entry still in the feed could age out and be staged
// again, and it is larger so an entry that briefly drops out and returns is
// still recognised. It is bounded because the memory is persisted, and a
// memory the store refuses comes back empty after a restart and restages the
// feed. 500 keys is about 10 KB per subscription.
const maxSeen = 500

// poller polls one feed and hands each new entry to a sink.
type poller struct {
	// url is the fetch address and the key the memory is stored under. A
	// different address is a different subscription with its own poller.
	url   string
	http  *http.Client
	state State
	onJob func(Job)

	// ctx bounds this poller's fetches and is cancelled by close as well as
	// by shutdown, so a settings save that removes a subscription does not
	// wait out a slow publisher.
	ctx    context.Context
	cancel context.CancelFunc

	// cmu guards sub and filt, which Apply replaces in place so an edited
	// subscription keeps its memory rather than seeding again.
	cmu  sync.Mutex
	sub  Subscription
	filt *regexp.Regexp

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
	// wake tells a sleeping loop that the interval changed. It is buffered
	// by one and sent to without blocking, so saving twice never stalls.
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

// configure installs an edited subscription on a running poller and cuts the
// current sleep short, so a shorter interval applies at once.
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
	for {
		sub, _ := p.config()
		// A new timer each round, since the interval can be edited.
		t := time.NewTimer(sub.Every())
		select {
		case <-p.stop:
			t.Stop()
			return
		case <-p.ctx.Done():
			// Shutdown cancels only the context, never the channel.
			t.Stop()
			return
		case <-p.wake:
			// Edited: re-read the interval without fetching.
			t.Stop()
		case <-t.C:
			p.poll()
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
		log.Printf("feed %s was not polled: %v", p.url, err)
		p.notePoll(err)
		return
	}
	sub, filt := p.config()
	f, err := p.fetch()
	if err != nil {
		log.Printf("feed %s was not read: %v", p.url, err)
		p.notePoll(err)
		return
	}

	fresh := p.pick(f, sub, filt)
	// Remember before handing over, as watch.consume does: a crash mid-handover
	// loses one entry rather than restaging the batch on every poll.
	p.remember(f)
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

// pick decides which of a document's entries are handed over. A first poll
// hands over nothing; see seed.
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
	log.Printf("feed %s: %d entries already there are marked as seen, so only what appears from now on is added", p.url, len(f.Items))
}

// remember records every entry in the document, filtered or not, and drops
// the oldest keys once the memory is full. The document's keys go first on
// every poll, so an entry still in the feed never ages out.
func (p *poller) remember(f Feed) {
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
		// The memory still works in this process, but after a restart the feed
		// would be seeded again.
		log.Printf("feed %s: what it has already added could not be written down, so a restart would treat this feed as new again: %v", p.url, err)
	}
}

func (p *poller) fetch() (Feed, error) {
	return fetch(p.ctx, p.http, p.url)
}

// fetch is one GET of one feed document. The preview (preview.go) uses it too,
// so a test fetch reports exactly what a poll would get.
func fetch(ctx context.Context, hc *http.Client, rawurl string) (Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return Feed{}, err
	}
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
