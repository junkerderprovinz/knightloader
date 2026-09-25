package trackerlist

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const bestList = `udp://tracker.opentrackr.org:1337/announce

udp://open.stealth.si:80/announce
# mirrored from somewhere else
udp://tracker.opentrackr.org:1337/announce
https://tracker.gbitt.info:443/announce
not a tracker
`

var bestTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"https://tracker.gbitt.info:443/announce",
}

// server answers with whatever answer holds at the time of the request, and
// counts the requests.
type server struct {
	*httptest.Server
	hits   atomic.Int32
	status atomic.Int32
	body   atomic.Value
}

func newServer(t *testing.T) *server {
	s := &server{}
	s.status.Store(http.StatusOK)
	s.body.Store(bestList)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		w.WriteHeader(int(s.status.Load()))
		_, _ = w.Write([]byte(s.body.Load().(string)))
	}))
	t.Cleanup(s.Close)
	return s
}

// clock is a time that only moves when a test moves it.
type clock struct{ t time.Time }

func (c *clock) now() time.Time       { return c.t }
func (c *clock) pass(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock                { return &clock{t: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)} }
func newList(t *testing.T, c *clock) *List {
	return listAt(filepath.Join(t.TempDir(), "trackers.json"), c)
}

func listAt(path string, c *clock) *List {
	l := New(path, http.DefaultClient)
	l.now = c.now
	return l
}

func TestAGoodAnswerIsUsedAndOutlivesARestart(t *testing.T) {
	srv := newServer(t)
	c := newClock()
	path := filepath.Join(t.TempDir(), "trackers.json")
	l := listAt(path, c)

	if !l.Due(srv.URL) {
		t.Fatal("a list never fetched is not due")
	}
	if err := l.Refresh(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := l.Trackers(srv.URL); !slices.Equal(got, bestTrackers) {
		t.Fatalf("Trackers = %q, want %q", got, bestTrackers)
	}

	restarted := listAt(path, c)
	if got := restarted.Trackers(srv.URL); !slices.Equal(got, bestTrackers) {
		t.Fatalf("after a restart Trackers = %q, want the list from disk", got)
	}
	if restarted.Due(srv.URL) {
		t.Fatal("a list fetched a minute ago is due again after a restart")
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("the list was asked %d times, want once", n)
	}
}

func TestTheListIsFetchedAtMostOnceADay(t *testing.T) {
	srv := newServer(t)
	c := newClock()
	l := newList(t, c)
	ctx := context.Background()

	for range 3 {
		if err := l.Refresh(ctx, srv.URL); err != nil {
			t.Fatal(err)
		}
	}
	c.pass(RefreshAfter - time.Minute)
	if err := l.Refresh(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("the list was asked %d times within a day, want once", n)
	}
	c.pass(time.Minute)
	if !l.Due(srv.URL) {
		t.Fatal("a list a day old is not due")
	}
	if err := l.Refresh(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if n := srv.hits.Load(); n != 2 {
		t.Fatalf("the list was asked %d times after a day, want twice", n)
	}
}

func TestAFailedFetchKeepsTheLastGoodList(t *testing.T) {
	srv := newServer(t)
	c := newClock()
	l := newList(t, c)
	ctx := context.Background()
	if err := l.Refresh(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	fetched := c.t

	c.pass(RefreshAfter)
	srv.status.Store(http.StatusBadGateway)
	if err := l.Refresh(ctx, srv.URL); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("Refresh = %v, want the 502 reported", err)
	}
	if got := l.Trackers(srv.URL); !slices.Equal(got, bestTrackers) {
		t.Fatalf("after a failed fetch Trackers = %q, want the last good list", got)
	}
	st := l.Status(srv.URL)
	if st.Trackers != len(bestTrackers) || !st.FetchedAt.Equal(fetched) || st.Error == "" || !st.TriedAt.Equal(c.t) {
		t.Fatalf("Status = %+v, want the good list's count and time beside the failure", st)
	}

	if l.Due(srv.URL) {
		t.Fatal("a failed list is due again at once")
	}
	c.pass(RetryAfter)
	if !l.Due(srv.URL) {
		t.Fatal("a failed list is not retried after RetryAfter")
	}
	srv.status.Store(http.StatusOK)
	if err := l.Refresh(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if st := l.Status(srv.URL); st.Error != "" || !st.FetchedAt.Equal(c.t) {
		t.Fatalf("Status after the retry = %+v, want the failure gone", st)
	}
}

// A portal or an error page answered with 200 is not a list.
func TestAnAnswerWithoutTrackersIsAFailure(t *testing.T) {
	srv := newServer(t)
	srv.body.Store("<html><body>Please sign in</body></html>")
	l := newList(t, newClock())
	if err := l.Refresh(context.Background(), srv.URL); err == nil {
		t.Fatal("a page with no tracker on it was taken for a list")
	}
	if got := l.Trackers(srv.URL); got != nil {
		t.Fatalf("Trackers = %q, want none", got)
	}
}

func TestAnotherAddressDoesNotInheritTheOldList(t *testing.T) {
	srv := newServer(t)
	l := newList(t, newClock())
	if err := l.Refresh(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	other := srv.URL + "/other.txt"
	if got := l.Trackers(other); got != nil {
		t.Fatalf("Trackers(other) = %q, want nothing until it is fetched", got)
	}
	if !l.Due(other) {
		t.Fatal("a new address is not due at once")
	}
	if l.Due("") || l.Trackers("") != nil {
		t.Fatal("no address has a list")
	}
}

// A list kept behind a login carries the key in its address, in the query or
// the path as often as in the user part, and the error reaches the log and the
// settings page.
func TestAFailedFetchKeepsTheAddressOutOfItsError(t *testing.T) {
	l := newList(t, newClock())
	err := l.Refresh(context.Background(), "http://someone:hunter2@127.0.0.1:1/k/0123456789abcdef/trackers.txt?token=s3cr3t")
	if err == nil {
		t.Fatal("a closed port answered")
	}
	for _, secret := range []string{"hunter2", "0123456789abcdef", "s3cr3t"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error %q carries %q from the address", err, secret)
		}
	}
}

// Only the copy on disk failed, so the log must not say the fetch did.
func TestAListThatCannotBeSavedIsStillInUse(t *testing.T) {
	srv := newServer(t)
	l := listAt(filepath.Join(t.TempDir(), "gone", "trackers.json"), newClock())
	if err := l.Refresh(context.Background(), srv.URL); !errors.Is(err, ErrNotSaved) {
		t.Fatalf("Refresh = %v, want ErrNotSaved", err)
	}
	if got := l.Trackers(srv.URL); !slices.Equal(got, bestTrackers) {
		t.Fatalf("Trackers = %q, want the list just fetched", got)
	}
	if st := l.Status(srv.URL); st.Error != "" || st.Trackers != len(bestTrackers) {
		t.Fatalf("Status = %+v, want the fetch reported as good", st)
	}
}
