package app

// Feed subscriptions: RSS and Atom feeds, channels yt-dlp lists, and
// Mediathek searches. Like the drop folders in app_watch.go, nobody is
// watching when an entry arrives, and every entry goes through addLinksFrom
// so the filter, crawler, Packagizer and mirror check apply.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// feedState is the runner's State, kept in the store's table of its own (see
// store.FeedSeen).
type feedState struct {
	app *App
}

func (f *feedState) Seen(url string) ([]string, bool, error) {
	return f.app.Store.FeedSeen(url)
}

// SetSeen also drops the records of subscriptions that are not configured,
// so a feed that is removed and added back seeds again.
func (f *feedState) SetSeen(url string, keys []string) error {
	if err := f.app.Store.SetFeedSeen(url, keys); err != nil {
		return err
	}
	keep := []string{url}
	for _, s := range f.app.Settings.Get().Feeds {
		keep = append(keep, strings.TrimSpace(s.URL))
	}
	return f.app.Store.KeepFeedSeen(keep)
}

// feedListingTimeout bounds one listing of a channel, and a backfill of all of
// a channel gets feedBackfillTimeout: yt-dlp reads about thirty videos per
// page request.
const (
	feedListingTimeout  = 2 * time.Minute
	feedBackfillTimeout = 30 * time.Minute
)

// listing is the part of the yt-dlp backend a channel subscription needs.
type listing interface {
	List(ctx context.Context, url string, limit int, each func(ytdlp.ListingEntry) bool) error
}

// feedLister lists channels for internal/feed with the yt-dlp backend wired
// at the time of the poll.
type feedLister struct{ a *App }

func (l feedLister) List(ctx context.Context, url string, q feed.ListQuery) (feed.Feed, error) {
	l.a.bmu.RLock()
	b, ok := l.a.ytdlp.(listing)
	l.a.bmu.RUnlock()
	if !ok {
		return feed.Feed{}, errors.New(ytdlpMissing)
	}
	limit := q.Limit
	if !q.Since.IsZero() {
		// The listing goes past the limit until it reaches the date, which
		// only the entries can tell.
		limit = 0
	}
	timeout := feedListingTimeout
	if limit == 0 {
		timeout = feedBackfillTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var f feed.Feed
	err := b.List(ctx, url, limit, func(e ytdlp.ListingEntry) bool {
		return addListed(&f, q, e)
	})
	if err != nil {
		return feed.Feed{}, err
	}
	return f, nil
}

// addListed adds one listed video to f and reports whether the listing should
// go on.
func addListed(f *feed.Feed, q feed.ListQuery, e ytdlp.ListingEntry) bool {
	var published time.Time
	if e.Timestamp > 0 {
		published = time.Unix(e.Timestamp, 0)
	}
	if q.Limit > 0 && len(f.Items) >= q.Limit &&
		(q.Since.IsZero() || (!published.IsZero() && published.Before(q.Since))) {
		return false
	}
	if f.Title == "" {
		// A channel's tabs are titled "<channel> - Videos"; a playlist keeps
		// its own title.
		f.Title = e.Playlist
		if e.Channel != "" && (e.Playlist == "" || strings.HasPrefix(e.Playlist, e.Channel+" - ")) {
			f.Title = e.Channel
		}
		if e.ChannelID != "" {
			f.Link = "https://www.youtube.com/channel/" + e.ChannelID
		}
	}
	// The same id YouTube's own feed gives the video, so a key does not
	// depend on which kind found it.
	guid := strings.ToLower(e.Extractor) + ":" + e.ID
	if e.Extractor == "Youtube" {
		guid = "yt:video:" + e.ID
	}
	if e.ID == "" {
		guid = ""
	}
	f.Items = append(f.Items, feed.Item{
		Title:     e.Title,
		Link:      e.URL,
		GUID:      guid,
		Published: published,
		Duration:  time.Duration(e.Duration * float64(time.Second)),
	})
	return true
}

// feedProber finds an entry's length with the same yt-dlp probe staging
// runs, so a video that passes is not probed a second time.
type feedProber struct{ a *App }

func (p feedProber) Duration(ctx context.Context, link string) (time.Duration, error) {
	if res := p.a.stagingResolverFor(link, ""); res == nil || res.Info().ID != "ytdlp" {
		return 0, fmt.Errorf("%s is not a link yt-dlp can measure", link)
	}
	tp, ok := p.a.ytdlpTitleProber()
	if !ok {
		return 0, errors.New(ytdlpMissing)
	}
	r, err := p.a.linkProbe(ctx, tp, link, nil, false)
	if err != nil {
		return 0, err
	}
	return time.Duration(r.Duration * float64(time.Second)), nil
}

// feedSources is what the runner and the test button read subscriptions with.
func (a *App) feedSources(hc *http.Client) feed.Sources {
	return feed.Sources{HTTP: hc, Lister: feedLister{a}, Prober: feedProber{a}}
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
		src := a.feedSources(nil)
		r, err := feed.New(feed.Options{
			Subscriptions: subs,
			OnJob:         a.onFeedEntry,
			State:         &feedState{app: a},
			Lister:        src.Lister,
			Prober:        src.Prober,
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

// feedTestClient serves the settings page's test and resolve buttons. It is
// built once so repeated presses share one connection pool, and it has the
// same options as a poller's client so the test answers about the real
// request.
var feedTestClient = httpx.New(httpx.Options{})

// InspectFeed fetches one subscription once and reports what it carries,
// staging nothing and marking nothing as seen. It never touches the live
// runner.
func (a *App) InspectFeed(ctx context.Context, s feed.Subscription) (feed.Preview, error) {
	p, err := feed.Inspect(ctx, a.feedSources(feedTestClient), s)
	if err != nil {
		return p, err
	}
	return a.markDownloaded(p, s), nil
}

// markDownloaded turns away the entries of a test that the subscription would
// reject as downloaded before, the same check staging makes.
func (a *App) markDownloaded(p feed.Preview, s feed.Subscription) feed.Preview {
	if s.AllowDownloaded {
		return p
	}
	for i, e := range p.Entries {
		if !e.Matches {
			continue
		}
		if v := a.downloadedVerdict(rules.Candidate{URL: e.Link}, dedupe.MediaKeyOf(e.Link), core.DownloadedCheckOn); v.Rejected {
			p.Entries[i].Matches, p.Entries[i].Reason = false, feed.ReasonDownloaded
			p.Matched--
		}
	}
	return p
}

// ResolveFeed works out the subscription for a pasted address.
func (a *App) ResolveFeed(ctx context.Context, raw string) (feed.Resolved, error) {
	return feed.Resolve(ctx, a.feedSources(feedTestClient), raw)
}

// onFeedEntry stages one new entry on its own goroutine, so the subscription's
// poll does not wait for the collector.
func (a *App) onFeedEntry(j feed.Job) {
	a.spawn(func() { a.stageFeedJob(j) })
}

// feedScope is how far inside the network a crawl of a feed's entries may
// reach: no further than the feed's own address. A feed URL that does not
// parse keeps its entries on the open internet.
func feedScope(source string) *httpx.Scope {
	s := httpx.ScopePublic
	if u, err := url.Parse(source); err == nil && u.Hostname() != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s = httpx.ScopeOfHost(ctx, u.Hostname())
	}
	return &s
}

// stageFeedJob stages one entry, applies the subscription's options and only
// then starts anything, so the destination is set before a download picks
// where to write.
func (a *App) stageFeedJob(j feed.Job) {
	sub := j.Sub
	batch := LinkBatchOptions{Within: feedScope(j.Source), Source: j.Source, Preset: sub.Media, downloaded: core.DownloadedCheckOn}
	if sub.AllowDownloaded {
		batch.downloaded = core.DownloadedCheckOff
	}
	if j.FileName != "" {
		// A plain file under the entry's name: the CDN's own name says nothing,
		// and a backend that names files itself would undo this one.
		batch.File, batch.FileName = true, sanitizeSegment(j.FileName)
	}
	ids := idsOf(a.addLinksFrom([]string{j.URL}, j.Package, OriginFeed, batch))
	if len(ids) == 0 {
		// Filtered or duplicate; not logged, or a filtered feed would log on
		// every poll.
		return
	}
	if j.Subtitle != "" {
		sb := LinkBatchOptions{Within: batch.Within, Source: j.Source, File: true, FileName: sanitizeSegment(j.SubtitleName), downloaded: batch.downloaded}
		ids = append(ids, idsOf(a.addLinksFrom([]string{j.Subtitle}, j.Package, OriginFeed, sb))...)
	}
	// A video feed's entry is a yt-dlp link, and the options below are for
	// every row it became.
	ids = a.withVariantFamilies(ids)
	log.Printf("feed %s added %s", core.RedactURL(j.Source), j.Package)

	var (
		opts TaskOptions
		set  bool
	)
	dir := j.Dir
	// The series layout makes the show's folder itself.
	if sub.Naming == feed.NamingChannel && j.Show != "" && sub.Library != feed.LibrarySeries {
		dir = a.showDir(dir, j.Show)
	}
	if dir != "" {
		opts.Dir, set = &dir, true
	}
	if j.Priority != nil {
		// Copied, since the pointer belongs to the poller that built the job.
		priority := *j.Priority
		opts.Priority, set = &priority, true
	}
	if sub.SponsorBlock != nil {
		opts.SponsorBlock, set = sub.SponsorBlock, true
	}
	if sub.Library == feed.LibrarySeries {
		opts.Series, set = &ytdlp.Series{Show: j.Show}, true
	}
	if set {
		if err := a.SetTaskOptions(ids, opts); err != nil {
			log.Printf("feed %s: %v", core.RedactURL(j.Source), err)
		}
	}

	// A feed entry is written by a stranger, so only the instance's own
	// AutoConfirm or the subscription's own switch starts it. With AutoConfirm
	// on, the entry waits out the countdown like any other link.
	switch {
	case a.Settings.Get().AutoConfirm:
		a.autoConfirm(ids)
	case sub.AutoStart:
		a.ConfirmTasks(ids, confirm.Config{}, confirm.TriggerAutoConfirm)
	}
}

// showDir is the folder a show's entries go to: a folder named after it in
// the subscription's own folder, or in the download folder. Where the
// download folder already gets a folder per package, which is the show,
// that one is used.
func (a *App) showDir(base, show string) string {
	if base == "" {
		if a.Settings.Get().SubfolderByPackage {
			return ""
		}
		base = a.defaultDir()
	}
	return filepath.Join(base, sanitizeSegment(show))
}

// feedPresets maps each subscription that picks its own rows to that choice,
// by the address its entries carry as their source.
func feedPresets(s settings.Settings) map[string]ytdlp.HosterPreset {
	out := map[string]ytdlp.HosterPreset{}
	for _, sub := range s.Feeds {
		if sub.Media != nil {
			out[strings.TrimSpace(sub.URL)] = *sub.Media
		}
	}
	return out
}
