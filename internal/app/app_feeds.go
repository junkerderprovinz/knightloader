package app

// The RSS and Atom subscriptions, and where a subscription's memory is kept.
//
// This is the second intake nobody is sitting in front of, and it is built as
// the sibling of app_watch.go on purpose: a publisher posts something and the
// link is staged minutes later, so every question the collector would normally
// put to a person has to be answered from the subscription itself. The two
// halves are the same two the drop folders have. applyFeeds keeps the live
// runner matching the configuration without tearing down a subscription that did
// not change, and stageFeedJob carries out what the subscription asked for.
//
// There is deliberately no second way into the link list here. A feed entry goes
// down exactly the funnel a dropped file goes down, AddLinksFrom, so the filter,
// the crawler, the Packagizer and the mirror check all apply to it without any
// of them having to learn that feeds exist.

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// feedStateBucket is where each subscription's record of what it has already
// staged is kept.
//
// The interface-state store is reused rather than a table being added, the same
// reasoning routes_features.go's park bucket uses: this is not configuration,
// nothing but the runner reads it, and a settings field would put it in a
// document the settings form replaces wholesale on every save. It gets a bucket
// of its own so the browser, which writes its layout bucket whole, cannot
// overwrite it.
//
// The budget is worth writing down, because exceeding it is not a visible
// failure: store.MaxUIStateBytes is 256 KiB, a remembered entry is sixteen hex
// characters plus JSON quoting, and internal/feed remembers at most five hundred
// of them per subscription. That is roughly ten kilobytes per feed, so a couple
// of dozen subscriptions fit. Past that the store refuses the write and the
// runner logs it, which is the point at which somebody finds out rather than
// discovering it after a restart re-adds a feed's whole window.
const feedStateBucket = "feeds"

// feedState is the runner's State, backed by the interface-state store.
//
// One document holding every subscription rather than one row each, because the
// store is a key/value table and a key per subscription would be a key per
// address the user has ever typed, with nothing ever cleaning up the ones they
// removed. The whole document is rewritten on every poll, which is why the mutex
// is here: subscriptions poll on independent goroutines, and two of them
// read-modify-writing one document without it would lose whichever finished
// first.
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
	// Subscriptions that are not configured any more are dropped here rather
	// than in a sweep of their own: this is the only writer, it already holds the
	// whole document, and a record for an address nobody follows is bytes spent
	// against the budget above for a feed that will never be polled.
	//
	// A subscription that is removed and put back therefore starts fresh and
	// seeds again, which is the correct reading of the act: removing a
	// subscription is saying you no longer follow that feed.
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
		// Refused rather than started afresh, which is the opposite of what the
		// park bucket does with an unreadable document and deliberately so. A
		// forgotten park costs somebody one remembered folder; a forgotten feed
		// memory stages a publisher's whole current window, and the poller's own
		// load() is written to skip the poll when this fails for exactly that
		// reason.
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

// applyFeeds makes the running feed runner match the configuration. It runs on
// every settings change, so adding a subscription does not need a restart.
//
// The live runner is reconciled rather than rebuilt, for a reason that is
// sharper here than it is for the drop folders: a poller carries the memory of
// what it has already staged and, on a subscription that has not completed its
// first poll yet, the fact that it has not seeded. Rebuilding on every save
// would mean saving the speed limit could make a feed hand over its whole
// current window.
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
		// Every configured row failed. The runner is closed rather than kept, so
		// that fixing the address and saving again builds a fresh one instead of
		// reviving whatever this one was left holding.
		_ = a.feeds.Close()
		a.feeds = nil
		log.Print("no feed could be polled; the subscriptions are off")
		return
	}
	log.Printf("following %s", strings.Join(urls, ", "))
}

// FeedHealth reports how each subscription being polled is doing.
//
// A nil answer is a real one and not an empty instance: it means no runner, so
// no address is being polled at all, which a caller has to be able to tell apart
// from a subscription that is being polled and has nothing to say yet. The two
// look identical in a table and only one of them is a problem.
func (a *App) FeedHealth() []feed.Health {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	if a.feeds == nil {
		return nil
	}
	return a.feeds.Health()
}

// feedTestClient is the client the test button's fetch goes through: one, built
// once, rather than one per press. Somebody tuning a title filter presses that
// button a dozen times in a minute, and a client per press is a connection pool
// per press, each holding its idle connections open for a minute and a half
// after the answer is on screen.
//
// It carries the app's shared outbound policy with nothing added, because that
// is exactly what a poller's own client is (internal/feed builds
// httpx.New(httpx.Options{}) for a runner it was handed no client for). The
// whole worth of the button is that it answers about the request the
// subscription will really make, so a different user agent or a different
// ceiling here would make it answer about something else. Shared deliberately
// and written down here, which is what internal/httpx asks of a caller that
// shares one: nothing else in the process uses it.
var feedTestClient = httpx.New(httpx.Options{})

// InspectFeed fetches one feed once and reports what it carries, staging
// nothing and marking nothing as seen. It is the settings page's test button.
//
// It goes nowhere near the live runner, deliberately. Testing an address the
// runner is already polling must not disturb that subscription, and testing one
// it has never seen must not create anything: this is a person asking what is at
// an address, not a subscription being switched on.
func (a *App) InspectFeed(ctx context.Context, s feed.Subscription) (feed.Preview, error) {
	return feed.Inspect(ctx, feedTestClient, s)
}

// onFeedEntry receives one new entry. It runs on that subscription's polling
// goroutine, so the work goes onto a goroutine of its own: a poll that waits for
// the collector to resolve a link is a poll that is not looking at the feed, and
// everything that publisher posts next waits behind it.
func (a *App) onFeedEntry(j feed.Job) {
	a.spawn(func() { a.stageFeedJob(j) })
}

// stageFeedJob stages one entry.
//
// The order is the same one stageWatchJob uses and for the same reason: the link
// is staged, then what the subscription said about it is written on, and only
// then is anything started. Starting first would race the destination folder
// onto a download that had already chosen where to put its bytes.
func (a *App) stageFeedJob(j feed.Job) {
	created := a.AddLinksFrom([]string{j.URL}, j.Package, OriginFeed)
	if len(created) == 0 {
		// Nothing was staged, which is the normal outcome for an entry the link
		// filter held or one that duplicates a link already in the list. It is not
		// logged per entry: a feed whose every entry is filtered would otherwise
		// write a line per entry per poll forever.
		return
	}
	ids := make([]string, 0, len(created))
	for _, t := range created {
		ids = append(ids, t.ID)
	}
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
		// Copied rather than passed on: the Job's pointer belongs to the poller
		// that built it, and handing it to a setter that may keep it is how two
		// tasks end up sharing one priority.
		priority := *j.Priority
		opts.Priority, set = &priority, true
	}
	if set {
		if err := a.SetTaskOptions(ids, opts); err != nil {
			log.Printf("feed %s: %v", j.Source, err)
		}
	}

	// Only the instance's own AutoConfirm, with no per-entry override beside it.
	// A dropped crawljob can carry autoStart because a person wrote that file for
	// those links; a feed entry is written by a stranger, and letting a publisher
	// decide that this box starts downloading is not a switch worth building.
	//
	// TriggerAutoConfirm rather than a trigger of this intake's own, because the
	// setting that fired it is literally AutoConfirm and the trigger's only
	// effect is what confirm.Ask resolves to for a batch with nobody watching.
	// A sixth trigger would be a name for a distinction that changes nothing.
	if a.Settings.Get().AutoConfirm {
		a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerAutoConfirm)
	}
}
