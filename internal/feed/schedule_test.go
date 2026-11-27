package feed

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// These tests run the polling loops in a synctest bubble, whose clock only
// moves when every goroutine in it is blocked. The feed is served from memory,
// and its address is an IP literal, so no fetch waits on a socket or a DNS
// lookup the bubble cannot see.

const (
	bubbleFeed = "http://192.0.2.1/feed.xml"
	otherFeed  = "http://192.0.2.2/feed.xml"
)

// memFeed serves the same feed document at every address and counts the
// fetches of each.
type memFeed struct {
	mu   sync.Mutex
	hits map[string]int
}

func (m *memFeed) RoundTrip(r *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.hits[r.URL.String()]++
	m.mu.Unlock()
	body := `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Kellerfunk</title>` +
		item(1, "Folge 1") + `</channel></rss>`
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": {"application/rss+xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

func (m *memFeed) fetches(url string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits[url]
}

// running starts a Runner over one subscription and returns once its first
// poll, which runs at once, is done.
func running(t *testing.T, sub Subscription) (*Runner, *memFeed) {
	t.Helper()
	srv := &memFeed{hits: map[string]int{}}
	sub.URL = bubbleFeed
	r, err := New(Options{
		Subscriptions: []Subscription{sub},
		OnJob:         func(Job) {},
		HTTP:          &http.Client{Transport: srv},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	r.Start()
	synctest.Wait()
	if n := srv.fetches(bubbleFeed); n != 1 {
		t.Fatalf("the first poll fetched %d times, want once", n)
	}
	return r, srv
}

// apply hands the Runner a saved list and waits for its loops to settle.
func apply(t *testing.T, r *Runner, subs ...Subscription) {
	t.Helper()
	if errs := r.Apply(subs); len(errs) != 0 {
		t.Fatal(errs)
	}
	synctest.Wait()
}

// after moves the bubble's clock on and waits for whatever came due.
func after(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestSavingTheSettingsKeepsTheRemainingWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, srv := running(t, Subscription{IntervalMinutes: 30})

		after(10 * time.Minute)
		apply(t, r, Subscription{URL: bubbleFeed, IntervalMinutes: 30})
		after(10 * time.Minute)
		apply(t, r, Subscription{URL: bubbleFeed, IntervalMinutes: 30, Dir: "/downloads/funk"})

		after(10*time.Minute + time.Second)
		if n := srv.fetches(bubbleFeed); n != 2 {
			t.Fatalf("30 minutes after the first poll, with two saves in between, the feed was fetched %d times, want 2", n)
		}
	})
}

func TestAShorterIntervalTakesEffectAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, srv := running(t, Subscription{IntervalMinutes: 30})

		after(20 * time.Minute)
		apply(t, r, Subscription{URL: bubbleFeed, IntervalMinutes: 10})
		if n := srv.fetches(bubbleFeed); n != 2 {
			t.Fatalf("shortening the interval below the time already waited fetched %d times, want a poll at once", n)
		}

		after(10*time.Minute + time.Second)
		if n := srv.fetches(bubbleFeed); n != 3 {
			t.Fatalf("10 minutes after that poll the feed was fetched %d times, want 3", n)
		}
	})
}

func TestALongerIntervalCountsFromTheLastPoll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, srv := running(t, Subscription{IntervalMinutes: 10})

		after(5 * time.Minute)
		apply(t, r, Subscription{URL: bubbleFeed, IntervalMinutes: 60})

		after(55*time.Minute - time.Second)
		if n := srv.fetches(bubbleFeed); n != 1 {
			t.Fatalf("before the hour was up the feed was fetched %d times, want once", n)
		}
		after(2 * time.Second)
		if n := srv.fetches(bubbleFeed); n != 2 {
			t.Fatalf("an hour after the first poll the feed was fetched %d times, want 2", n)
		}
	})
}

func TestASubscriptionAddedBySavingIsPolledAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, srv := running(t, Subscription{IntervalMinutes: 30})

		after(10 * time.Minute)
		apply(t, r, Subscription{URL: bubbleFeed, IntervalMinutes: 30}, Subscription{URL: otherFeed, IntervalMinutes: 30})
		if n := srv.fetches(otherFeed); n != 1 {
			t.Fatalf("a subscription added by a save was fetched %d times, want once at once", n)
		}
	})
}
