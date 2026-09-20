package feed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every feed in these tests is served by an httptest server; nothing reaches
// the network.

// server is a feed whose document the test can swap between polls.
type server struct {
	*httptest.Server

	mu     sync.Mutex
	items  []string
	status int
	hits   int
	// enter and release let a test hold a poll open.
	enter   chan struct{}
	release chan struct{}
}

func newServer(t *testing.T, items ...string) *server {
	t.Helper()
	s := &server{items: items, status: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		body, status, enter, release := s.document(), s.status, s.enter, s.release
		s.hits++
		s.mu.Unlock()
		if enter != nil {
			select {
			case enter <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				// The client gave up, as a cancelled app context makes it.
				return
			}
		}
		if status != http.StatusOK {
			http.Error(w, "no", status)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

// item builds one RSS entry with a stable guid.
func item(n int, title string) string {
	return fmt.Sprintf(`<item><title>%s</title><link>https://example.invalid/e/%d</link><guid>kf-%04d</guid></item>`, title, n, n)
}

func (s *server) document() string {
	return `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Kellerfunk</title>` +
		strings.Join(s.items, "") + `</channel></rss>`
}

func (s *server) publish(entries ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Newest first, as real feeds serve them.
	s.items = append(entries, s.items...)
}

func (s *server) answer(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *server) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// memState is an in-memory State. Handing it to a second Runner stands in
// for a restart.
type memState struct {
	mu  sync.Mutex
	doc map[string][]string
}

func newState() *memState { return &memState{doc: map[string][]string{}} }

func (m *memState) Seen(url string) ([]string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys, known := m.doc[url]
	return append([]string(nil), keys...), known, nil
}

func (m *memState) SetSeen(url string, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.doc[url] = append([]string(nil), keys...)
	return nil
}

func (m *memState) count(url string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.doc[url])
}

// sink collects handed-over jobs; OnJob runs on a polling goroutine.
type sink struct {
	mu   sync.Mutex
	jobs []Job
}

func (s *sink) add(j Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
}

// take returns what has arrived and empties the sink.
func (s *sink) take() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.jobs
	s.jobs = nil
	return out
}

// pollAll polls every subscription once on the test goroutine. Pollers only
// run on their own after Start, so tests that skip Start control every poll.
func pollAll(r *Runner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.live {
		p.poll()
	}
}

func build(t *testing.T, srv *server, st State, sub Subscription) (*Runner, *sink) {
	t.Helper()
	rec := &sink{}
	if sub.URL == "" {
		sub.URL = srv.URL
	}
	r, err := New(Options{
		Subscriptions: []Subscription{sub},
		OnJob:         rec.add,
		State:         st,
		HTTP:          srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, rec
}

func titles(jobs []Job) []string {
	out := make([]string, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.Package)
	}
	return out
}

func TestTheFirstPollStagesNothing(t *testing.T) {
	srv := newServer(t, item(3, "Folge 3"), item(2, "Folge 2"), item(1, "Folge 1"))
	st := newState()
	r, rec := build(t, srv, st, Subscription{})

	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("a brand new subscription staged %v", titles(got))
	}
	// Nothing staged, but everything remembered.
	if n := st.count(srv.URL); n != 3 {
		t.Fatalf("the first poll remembered %d entries, want 3", n)
	}
}

func TestTheSameEntryIsStagedOnceAndOnlyOnce(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	st := newState()
	r, rec := build(t, srv, st, Subscription{})

	pollAll(r)
	rec.take() // the seeding pass

	srv.publish(item(2, "Folge 2"))
	pollAll(r)
	got := rec.take()
	if len(got) != 1 || got[0].Package != "Folge 2" {
		t.Fatalf("the new entry was not staged: %v", titles(got))
	}

	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("the same entry was staged a second time: %v", titles(got))
	}
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("the same entry was staged again on the third poll: %v", titles(got))
	}
}

func TestARestartDoesNotStageTheSameEntryAgain(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	st := newState()

	first, rec := build(t, srv, st, Subscription{})
	pollAll(first)
	rec.take()
	srv.publish(item(2, "Folge 2"))
	pollAll(first)
	if got := rec.take(); len(got) != 1 {
		t.Fatalf("the first run staged %v, want one entry", titles(got))
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, rec2 := build(t, srv, st, Subscription{})
	pollAll(second)
	if got := rec2.take(); len(got) != 0 {
		t.Fatalf("after a restart the feed staged %v again", titles(got))
	}
	// Not just seeding again: a new entry still arrives.
	srv.publish(item(3, "Folge 3"))
	pollAll(second)
	got := rec2.take()
	if len(got) != 1 || got[0].Package != "Folge 3" {
		t.Fatalf("after a restart a genuinely new entry did not arrive: %v", titles(got))
	}
}

// TestTheFilterDecidesWhatIsStagedNotWhatIsRemembered checks that relaxing a
// filter does not stage the entries it held back.
func TestTheFilterDecidesWhatIsStagedNotWhatIsRemembered(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	st := newState()
	r, rec := build(t, srv, st, Subscription{TitleFilter: `^Folge `})

	pollAll(r)
	rec.take()

	srv.publish(item(3, "Werbung"), item(2, "Folge 2"))
	pollAll(r)
	got := rec.take()
	if len(got) != 1 || got[0].Package != "Folge 2" {
		t.Fatalf("the filter staged %v, want only Folge 2", titles(got))
	}

	if errs := r.Apply([]Subscription{{URL: srv.URL}}); len(errs) != 0 {
		t.Fatal(errs)
	}
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("relaxing the filter staged %v out of the feed's history", titles(got))
	}
}

func TestEditingARowKeepsWhatItKnows(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"), item(2, "Folge 2"))
	st := newState()
	r, rec := build(t, srv, st, Subscription{IntervalMinutes: 30})
	pollAll(r)
	rec.take()

	r.mu.Lock()
	before := r.live[srv.URL]
	r.mu.Unlock()

	if errs := r.Apply([]Subscription{{URL: srv.URL, IntervalMinutes: 5, Dir: "/downloads/funk"}}); len(errs) != 0 {
		t.Fatal(errs)
	}
	r.mu.Lock()
	after := r.live[srv.URL]
	r.mu.Unlock()
	if before != after {
		t.Fatal("an edit to the interval built a new poller, which loses everything the old one knew")
	}
	if sub, _ := after.config(); sub.IntervalMinutes != 5 || sub.Dir != "/downloads/funk" {
		t.Errorf("the edited row did not reach the poller: %+v", sub)
	}

	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("after an edit the feed staged %v again", titles(got))
	}
}

func TestWhatTheSubscriptionAskedForRidesAlong(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	two := 2
	r, rec := build(t, srv, newState(), Subscription{Dir: "/downloads/funk", Priority: &two})
	pollAll(r)
	rec.take()

	srv.publish(item(2, "Folge 2"))
	pollAll(r)
	got := rec.take()
	if len(got) != 1 {
		t.Fatalf("got %d jobs, want 1", len(got))
	}
	j := got[0]
	if j.Dir != "/downloads/funk" {
		t.Errorf("dir is %q", j.Dir)
	}
	if j.Priority == nil || *j.Priority != 2 {
		t.Errorf("priority is %v", j.Priority)
	}
	if j.Priority == &two {
		t.Error("the subscription's own pointer was handed over, so every entry would share one priority")
	}
	if j.Source != srv.URL {
		t.Errorf("source is %q, want the feed address so a staged link can be traced back", j.Source)
	}
	if j.URL != "https://example.invalid/e/2" {
		t.Errorf("url is %q", j.URL)
	}
}

// TestAServerErrorIsNotAnEmptyFeed: treating an error page as an empty feed
// would seed the subscription with nothing, and the whole window would arrive
// once the server recovered.
func TestAServerErrorIsNotAnEmptyFeed(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"), item(2, "Folge 2"))
	st := newState()
	srv.answer(http.StatusInternalServerError)
	r, rec := build(t, srv, st, Subscription{})

	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("a failed fetch staged %v", titles(got))
	}
	if _, known, _ := st.Seen(srv.URL); known {
		t.Fatal("a failed fetch was written down as a completed poll")
	}

	// The first successful poll still seeds.
	srv.answer(http.StatusOK)
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("the first successful poll staged %v", titles(got))
	}
	srv.publish(item(3, "Folge 3"))
	pollAll(r)
	if got := rec.take(); len(got) != 1 {
		t.Fatalf("got %v, want the one entry published after the seeding poll", titles(got))
	}
}

func TestAnUnreadableMemoryStopsThePollRatherThanEmptyingIt(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	r, rec := build(t, srv, brokenState{}, Subscription{})
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("staged %v", titles(got))
	}
	if n := srv.requests(); n != 0 {
		t.Fatalf("the feed was fetched %d times although the memory could not be read", n)
	}
}

type brokenState struct{}

func (brokenState) Seen(string) ([]string, bool, error) {
	return nil, false, fmt.Errorf("the store is unreadable")
}
func (brokenState) SetSeen(string, []string) error { return nil }

// TestAMemoryLargerThanTheCapKeepsWhatIsStillInTheFeed checks that trimming
// the memory never forgets an entry the feed still serves.
func TestAMemoryLargerThanTheCapKeepsWhatIsStillInTheFeed(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	st := newState()
	// A full memory of entries the feed no longer carries.
	old := make([]string, maxSeen)
	for i := range old {
		old[i] = fmt.Sprintf("%016x", i)
	}
	if err := st.SetSeen(srv.URL, old); err != nil {
		t.Fatal(err)
	}
	r, rec := build(t, srv, st, Subscription{})

	pollAll(r)
	if got := rec.take(); len(got) != 1 {
		t.Fatalf("got %v, want the one entry, since this subscription had already run", titles(got))
	}
	if n := st.count(srv.URL); n != maxSeen {
		t.Fatalf("the memory holds %d keys, want it cut to %d", n, maxSeen)
	}
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("the entry still sitting in the feed aged out of the memory and was staged again: %v", titles(got))
	}
}

// TestCloseWaitsForTheHandover: once Close returns, nothing is still on its
// way into the link list.
func TestCloseWaitsForTheHandover(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	// Already run once, so the first poll hands something over.
	st := newState()
	if err := st.SetSeen(srv.URL, nil); err != nil {
		t.Fatal(err)
	}

	entered, release := make(chan struct{}, 1), make(chan struct{})
	r, err := New(Options{
		Subscriptions: []Subscription{{URL: srv.URL}},
		State:         st,
		HTTP:          srv.Client(),
		OnJob: func(Job) {
			entered <- struct{}{}
			<-release
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was ever handed over")
	}

	done := make(chan struct{})
	go func() {
		_ = r.Close()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Close returned while an entry was still on its way into the link list")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return once the handover finished")
	}
}

// TestCloseEndsAFetchInFlight: close cancels before waiting, so a settings
// save does not wait out a slow publisher.
func TestCloseEndsAFetchInFlight(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	srv.mu.Lock()
	// release is never closed; only the client giving up ends the request.
	srv.enter, srv.release = make(chan struct{}, 1), make(chan struct{})
	enter := srv.enter
	srv.mu.Unlock()

	r, rec := build(t, srv, newState(), Subscription{})
	r.Start()
	select {
	case <-enter:
	case <-time.After(5 * time.Second):
		t.Fatal("the first poll never reached the server")
	}

	done := make(chan struct{})
	go func() {
		_ = r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not end the fetch; it is waiting out the client's own timeout")
	}
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("an abandoned fetch staged %v", titles(got))
	}
}

func TestCancellingTheContextEndsAFetchInFlight(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	srv.mu.Lock()
	srv.enter, srv.release = make(chan struct{}, 1), make(chan struct{})
	enter := srv.enter
	srv.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	rec := &sink{}
	r, err := New(Options{
		Subscriptions: []Subscription{{URL: srv.URL}},
		OnJob:         rec.add,
		HTTP:          srv.Client(),
		Context:       ctx,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	select {
	case <-enter:
	case <-time.After(5 * time.Second):
		t.Fatal("the first poll never reached the server")
	}

	cancel()
	done := make(chan struct{})
	go func() {
		_ = r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the context did not release the fetch")
	}
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("an aborted fetch staged %v", titles(got))
	}
}

func TestARowThatCannotBePolledIsReportedAndTheRestStillRun(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	rec := &sink{}
	r, err := New(Options{
		Subscriptions: []Subscription{
			{URL: "file:///etc/passwd"},
			{URL: srv.URL},
			// The same feed twice collapses into one.
			{URL: srv.URL, IntervalMinutes: 60},
		},
		OnJob: rec.add,
		HTTP:  srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if urls := r.URLs(); len(urls) != 1 || urls[0] != srv.URL {
		t.Fatalf("polling %v, want only the usable address once", urls)
	}

	if _, err := New(Options{
		Subscriptions: []Subscription{{URL: "file:///etc/passwd"}},
		OnJob:         rec.add,
	}); err == nil {
		t.Fatal("a runner was built over a subscription that can never be polled")
	}
}

func TestARunnerWithoutASinkIsRefused(t *testing.T) {
	if _, err := New(Options{Subscriptions: []Subscription{{URL: "https://example.invalid/rss.xml"}}}); err == nil {
		t.Fatal("a runner with nowhere to put its entries was built")
	}
}
