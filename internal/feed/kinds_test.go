package feed

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeChannel is a Lister over a fixed listing, newest first, that records the
// queries it was asked.
type fakeChannel struct {
	mu      sync.Mutex
	title   string
	items   []Item
	queries []ListQuery
}

func (c *fakeChannel) List(_ context.Context, _ string, q ListQuery) (Feed, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queries = append(c.queries, q)
	var out []Item
	for _, it := range c.items {
		if q.Limit > 0 && len(out) >= q.Limit && (q.Since.IsZero() || it.Published.Before(q.Since)) {
			break
		}
		out = append(out, it)
	}
	return Feed{Title: c.title, Items: out}, nil
}

func (c *fakeChannel) upload(it Item) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append([]Item{it}, c.items...)
}

// video is one listed video, n days old.
func video(n int, title string) Item {
	return Item{
		Title: title,
		Link:  fmt.Sprintf("https://www.youtube.com/watch?v=v%04d", n),
		GUID:  fmt.Sprintf("yt:video:v%04d", n),
		// An hour short of n days, so a cutoff of n days is not a race.
		Published: time.Now().AddDate(0, 0, -n).Add(time.Hour),
		Duration:  10 * time.Minute,
	}
}

func newChannel(n int) *fakeChannel {
	c := &fakeChannel{title: "Kellerfunk"}
	for i := 1; i <= n; i++ {
		c.items = append(c.items, video(i, fmt.Sprintf("Folge %d", n-i+1)))
	}
	return c
}

func buildChannel(t *testing.T, c Lister, st State, sub Subscription) (*Runner, *sink) {
	t.Helper()
	rec := &sink{}
	sub.Kind = KindChannel
	if sub.URL == "" {
		sub.URL = "https://www.youtube.com/@kellerfunk/videos"
	}
	r, err := New(Options{
		Subscriptions: Sanitize([]Subscription{sub}),
		OnJob:         rec.add,
		State:         st,
		Lister:        c,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, rec
}

func TestAChannelSeedsLikeAFeedWithoutABackfill(t *testing.T) {
	c := newChannel(30)
	r, rec := buildChannel(t, c, newState(), Subscription{})
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("a channel without a backfill handed over %d videos on its first poll", len(got))
	}
	c.upload(video(0, "Neu"))
	pollAll(r)
	if got := rec.take(); len(got) != 1 || got[0].Title != "Neu" {
		t.Fatalf("the new upload was not handed over: %v", titles(got))
	}
	if q := c.queries[len(c.queries)-1]; q.Limit != channelWindow {
		t.Errorf("an ordinary poll listed %+v, want the newest %d", q, channelWindow)
	}
}

func TestABackfillHandsOverTheChannelsPastOnTheFirstPoll(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    Backfill
		want int
	}{
		{"all", Backfill{Mode: BackfillAll}, 600},
		{"count", Backfill{Mode: BackfillCount, N: 20}, 20},
		{"days", Backfill{Mode: BackfillDays, N: 10}, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChannel(600)
			st := newState()
			b := tc.b
			r, rec := buildChannel(t, c, st, Subscription{Backfill: &b})
			pollAll(r)
			got := rec.take()
			if len(got) != tc.want {
				t.Fatalf("the backfill handed over %d videos, want %d", len(got), tc.want)
			}
			// More than an RSS subscription's memory, and none forgotten.
			if n := st.count(r.URLs()[0]); n < len(got) {
				t.Fatalf("the memory holds %d keys after handing over %d", n, len(got))
			}
			pollAll(r)
			if again := rec.take(); len(again) != 0 {
				t.Fatalf("the second poll handed over %d videos again", len(again))
			}
		})
	}
}

func TestAFullBackfillIsRememberedWhole(t *testing.T) {
	c := newChannel(2000)
	st := newState()
	r, rec := buildChannel(t, c, st, Subscription{Backfill: &Backfill{Mode: BackfillAll}})
	pollAll(r)
	if got := rec.take(); len(got) != 2000 {
		t.Fatalf("handed over %d", len(got))
	}
	if n := st.count(r.URLs()[0]); n != 2000 {
		t.Fatalf("the memory kept %d of the 2000 videos handed over", n)
	}
}

func TestABackfillIsForTheFirstPollOnly(t *testing.T) {
	c := newChannel(50)
	st := newState()
	// A subscription that already ran, edited to backfill afterwards.
	if err := st.SetSeen("https://www.youtube.com/@kellerfunk/videos", []string{}); err != nil {
		t.Fatal(err)
	}
	r, rec := buildChannel(t, c, st, Subscription{Backfill: &Backfill{Mode: BackfillAll}})
	pollAll(r)
	got := rec.take()
	// The window an ordinary poll lists holds all 50, which are new to an
	// empty memory, but nothing past the window is fetched.
	if len(got) != 50 || c.queries[0].Limit != channelWindow {
		t.Fatalf("got %d with %+v", len(got), c.queries[0])
	}
}

func TestTheFiltersApplyToEveryKind(t *testing.T) {
	c := newChannel(0)
	p := &lengths{of: map[string]time.Duration{}}
	rec := &sink{}
	r, err := New(Options{
		Subscriptions: Sanitize([]Subscription{{
			URL: "https://www.youtube.com/@k/videos", Kind: KindChannel,
			ExcludeFilter: "(?i)trailer", MinMinutes: 5, MaxMinutes: 60,
			Backfill: &Backfill{Mode: BackfillAll},
		}}),
		OnJob:  rec.add,
		Lister: c,
		Prober: p,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	short := video(1, "Kurz")
	short.Duration = time.Minute
	long := video(2, "Lang")
	long.Duration = 3 * time.Hour
	unknown := video(3, "Unbekannt")
	unknown.Duration = 0
	p.of[unknown.Link] = 20 * time.Minute
	c.items = []Item{video(4, "Folge"), video(5, "Folge Trailer"), short, long, unknown}

	pollAll(r)
	got := titles(rec.take())
	slices.Sort(got)
	if !slices.Equal(got, []string{"Folge", "Unbekannt"}) {
		t.Fatalf("handed over %v", got)
	}
	if !slices.Equal(p.asked, []string{unknown.Link}) {
		t.Errorf("probed %v, want only the video whose length the listing lacked", p.asked)
	}
}

func TestTheJobCarriesTheWholeSubscription(t *testing.T) {
	c := newChannel(1)
	two := 2
	sub := Subscription{
		Title: "Der Kellerfunk", Naming: NamingChannel, AutoStart: true, Library: LibrarySeries,
		SponsorBlock: &SponsorBlock{Remove: []string{"sponsor"}, Mark: []string{}}, AllowDownloaded: true,
		Priority: &two, Dir: "/dl/kf",
	}
	r, rec := buildChannel(t, c, newState(), sub)
	pollAll(r)
	c.upload(video(0, "Folge 9"))
	pollAll(r)
	got := rec.take()
	if len(got) != 1 {
		t.Fatalf("got %d jobs", len(got))
	}
	j := got[0]
	if j.Package != "Der Kellerfunk" || j.Show != "Der Kellerfunk" || j.Title != "Folge 9" {
		t.Errorf("named %q in %q (show %q), want the package after the channel and the title after the video", j.Title, j.Package, j.Show)
	}
	if !j.Sub.AutoStart || j.Sub.Library != LibrarySeries || j.Sub.SponsorBlock == nil || !j.Sub.AllowDownloaded || j.Sub.Kind != KindChannel {
		t.Errorf("the job lost part of its subscription: %+v", j.Sub)
	}
	if j.Duration != 10*time.Minute {
		t.Errorf("duration %v", j.Duration)
	}
	if j.Sub.Priority == nil || j.Sub.Priority == &two || j.Priority == j.Sub.Priority {
		t.Error("the job shares a priority pointer")
	}
}

func TestWithoutChannelNamingTheEntryIsThePackage(t *testing.T) {
	c := newChannel(1)
	r, rec := buildChannel(t, c, newState(), Subscription{})
	pollAll(r)
	c.upload(video(0, "Folge 9"))
	pollAll(r)
	got := rec.take()
	if len(got) != 1 || got[0].Package != "Folge 9" || got[0].Show != "Kellerfunk" {
		t.Fatalf("got %+v", got)
	}
}

func TestAChannelWithoutYtdlpSaysSo(t *testing.T) {
	_, err := Inspect(context.Background(), Sources{}, Subscription{URL: "https://www.youtube.com/@k", Kind: KindChannel})
	if err == nil {
		t.Fatal("a channel was read with nothing to list it")
	}
}

func TestAMediathekSearchIsPolledAndStagesItsSubtitles(t *testing.T) {
	srv, _ := mediathekAPI(t)
	rec := &sink{}
	st := newState()
	sub := Sanitize([]Subscription{{Kind: KindMediathek, Mediathek: &Mediathek{Channel: "ARD", Topic: "tagesschau", Subtitles: true}}})[0]
	r, err := New(Options{Subscriptions: []Subscription{sub}, OnJob: rec.add, State: st, HTTP: srv.Client(), MediathekAPI: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// Seeded with an empty memory, so every result is new.
	if err := st.SetSeen(sub.URL, []string{}); err != nil {
		t.Fatal(err)
	}
	pollAll(r)
	got := rec.take()
	if len(got) != 3 {
		t.Fatalf("got %d jobs", len(got))
	}
	j := got[0]
	if j.FileName != "tagesschau 20:00 Uhr, 26.11.2026.mp4" || j.Subtitle == "" || j.SubtitleName != "tagesschau 20:00 Uhr, 26.11.2026.ttml" {
		t.Errorf("job %+v", j)
	}
	if j.Show != "tagesschau" {
		t.Errorf("show %q", j.Show)
	}
}

func TestATestReportsLengthsAndReasons(t *testing.T) {
	c := newChannel(0)
	short := video(1, "Kurz")
	short.Duration = time.Minute
	c.items = []Item{video(2, "Folge"), short, video(3, "Werbung")}
	p, err := Inspect(context.Background(), Sources{Lister: c, HTTP: http.DefaultClient},
		Subscription{URL: "https://www.youtube.com/@k", Kind: KindChannel, ExcludeFilter: "Werbung", MinMinutes: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []PreviewEntry{
		{Title: "Folge", Link: video(2, "").Link, Matches: true, Duration: 600},
		{Title: "Kurz", Link: short.Link, Matches: false, Duration: 60, Reason: ReasonShort},
		{Title: "Werbung", Link: video(3, "").Link, Matches: false, Duration: 600, Reason: ReasonExclude},
	}
	if !slices.Equal(p.Entries, want) || p.Matched != 1 || p.Total != 3 {
		t.Fatalf("got %+v", p)
	}
	if c.queries[0].Limit != previewWindow {
		t.Errorf("a test listed %+v", c.queries[0])
	}
}
