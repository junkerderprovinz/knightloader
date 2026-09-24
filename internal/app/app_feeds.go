package app

// RSS and Atom subscriptions. Like the drop folders in app_watch.go, nobody is
// watching when a feed entry arrives, and every entry goes through
// addLinksFrom so the filter, crawler, Packagizer and mirror check apply.

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// feedStateBucket is the interface-state bucket that records what each
// subscription has already staged. It is not configuration, and a bucket of
// its own keeps the browser's layout writes from overwriting it.
//
// store.MaxUIStateBytes is 256 KiB and internal/feed remembers at most 500
// keys per subscription, about 10 KB, so a couple of dozen feeds fit. Past
// that the store refuses the write and the runner logs it.
const feedStateBucket = "feeds"

// feedState is the runner's State, kept as one document for all subscriptions
// so removed addresses can be cleaned up. Subscriptions poll on separate
// goroutines and each rewrites the whole document, hence the mutex.
type feedState struct {
	app *App
	mu  sync.Mutex
}

func (f *feedState) Seen(url string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, err := f.load()
	if err != nil {
		return nil, false, err
	}
	keys, known := doc[url]
	return keys, known, nil
}

func (f *feedState) SetSeen(url string, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, err := f.load()
	if err != nil {
		return err
	}
	doc[url] = keys
	// Records of subscriptions that are not configured are dropped here,
	// so a feed that is removed and added back seeds again.
	for u := range doc {
		if u != url && !f.configured(u) {
			delete(doc, u)
		}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return f.app.SetUIState(feedStateBucket, string(b))
}

func (f *feedState) load() (map[string][]string, error) {
	value, err := f.app.UIState(feedStateBucket)
	if err != nil {
		return nil, err
	}
	doc := map[string][]string{}
	if value == "" {
		return doc, nil
	}
	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		// Starting afresh would stage every feed's whole current window, so the
		// poller skips the poll instead.
		return nil, err
	}
	return doc, nil
}

// configured reports whether an address is still one of the subscriptions.
func (f *feedState) configured(url string) bool {
	for _, s := range f.app.Settings.Get().Feeds {
		if strings.TrimSpace(s.URL) == url {
			return true
		}
	}
	return false
}

// applyFeeds makes the feed runner match the configuration on every settings
// change. The runner is reconciled rather than rebuilt, because a rebuilt
// poller that has not seeded yet would stage its feed's whole current window.
func (a *App) applyFeeds(s settings.Settings) {
	subs := s.Feeds
	a.fmu.Lock()
	defer a.fmu.Unlock()

	if len(subs) == 0 {
		if a.feeds != nil {
			_ = a.feeds.Close()
			a.feeds = nil
		}
		return
	}
	if a.feeds == nil {
		r, err := feed.New(feed.Options{
			Subscriptions: subs,
			OnJob:         a.onFeedEntry,
			State:         &feedState{app: a},
			Context:       a.ctx,
		})
		if err != nil {
			log.Printf("no feed could be polled (%v); the subscriptions are off", err)
			return
		}
		r.Start()
		a.feeds = r
		log.Printf("following %s", strings.Join(r.URLs(), ", "))
		return
	}
	for _, err := range a.feeds.Apply(subs) {
		log.Printf("feed subscription is not being polled: %v", err)
	}
	urls := a.feeds.URLs()
	if len(urls) == 0 {
		// Every row failed. Closing the runner means the next save builds a
		// fresh one.
		_ = a.feeds.Close()
		a.feeds = nil
		log.Print("no feed could be polled; the subscriptions are off")
		return
	}
	log.Printf("following %s", strings.Join(urls, ", "))
}

// FeedHealth reports how each polled subscription is doing. Nil means no runner
// at all, which callers must tell apart from subscriptions with nothing to
// report yet.
func (a *App) FeedHealth() []feed.Health {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	if a.feeds == nil {
		return nil
	}
	return a.feeds.Health()
}

// feedTestClient serves the settings page's test button. It is built once so
// repeated presses share one connection pool, and it has the same options as
// a poller's client so the test answers about the real request.
var feedTestClient = httpx.New(httpx.Options{})

// InspectFeed fetches one feed once and reports what it carries, staging
// nothing and marking nothing as seen. It never touches the live runner.
func (a *App) InspectFeed(ctx context.Context, s feed.Subscription) (feed.Preview, error) {
	return feed.Inspect(ctx, feedTestClient, s)
}

// onFeedEntry stages one new entry on its own goroutine, so the subscription's
// poll does not wait for the collector.
func (a *App) onFeedEntry(j feed.Job) {
	a.spawn(func() { a.stageFeedJob(j) })
}

// stageFeedJob stages one entry, applies the subscription's options and only
// then starts anything, so the destination is set before a download picks
// where to write.
func (a *App) stageFeedJob(j feed.Job) {
	ids := idsOf(a.addLinksFrom([]string{j.URL}, j.Package, OriginFeed, LinkBatchOptions{}))
	if len(ids) == 0 {
		// Filtered or duplicate; not logged, or a filtered feed would log on
		// every poll.
		return
	}
	// A video feed's entry is a yt-dlp link, and the options below are for
	// every row it became.
	ids = a.withVariantFamilies(ids)
	log.Printf("feed %s added %s", j.Source, j.Package)

	var (
		opts TaskOptions
		set  bool
	)
	if j.Dir != "" {
		dir := j.Dir
		opts.Dir, set = &dir, true
	}
	if j.Priority != nil {
		// Copied, since the pointer belongs to the poller that built the job.
		priority := *j.Priority
		opts.Priority, set = &priority, true
	}
	if set {
		if err := a.SetTaskOptions(ids, opts); err != nil {
			log.Printf("feed %s: %v", j.Source, err)
		}
	}

	// Only the instance's own AutoConfirm decides: a feed entry is written by a
	// stranger, who should not get to start downloads on this machine.
	a.autoConfirm(ids)
}
