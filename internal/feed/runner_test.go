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

// Nothing in this file reaches the network. Every feed is served by an
// httptest server the test owns, so a failure here is a failure of this code
// and never of somebody's connection.

// server is a feed whose document the test can swap between polls.
type server struct {
	*httptest.Server

	mu     sync.Mutex
	items  []string
	status int
	hits   int
	// enter and release let a test hold a poll open, which is how "Close waits
	// for a poll in flight" is checked rather than assumed.
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
				// The client gave up, which is exactly what a cancelled app context
				// does to a fetch. Answering nothing is the honest thing here.
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

// item builds one RSS entry with a stable guid, so the identity under test is
// the feed's own identifier and not an accident of the address.
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
	// Newest first, the order every real feed serves.
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

// memState is a State with no disk behind it. It is what makes a restart
// testable: the same value is handed to a second Runner, which is exactly what
// the store does across a process boundary.
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

// sink collects the jobs a runner hands over. OnJob runs on a polling
// goroutine, so the slice needs a lock.
type sink struct {
	mu   sync.Mutex
	jobs []Job
}

func (s *sink) add(j Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
}

// take returns what has arrived and empties the sink, so each step of a test
// asserts on that step alone.
func (s *sink) take() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.jobs
	s.jobs = nil
	return out
}

// pollAll drives every live subscription once, with no goroutine running. Apply
// does not start a poller unless the runner has been started, so a test that
// never calls Start owns the polling and can assert on exact counts without
// waiting on a clock.
func pollAll(r *Runner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.live {
		p.poll()
	}
}

// build wires a runner over one subscription against one server. The interval
// is left at the default because nothing here waits for it.
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

// TestTheFirstPollStagesNothing is the first-run rule. A subscription pointed at
// an active feed is handed that publisher's whole current window in its very
// first response, and staging it would fill the collector with a back catalogue
// the moment somebody pastes an address.
func TestTheFirstPollStagesNothing(t *testing.T) {
	srv := newServer(t, item(3, "Folge 3"), item(2, "Folge 2"), item(1, "Folge 1"))
	st := newState()
	r, rec := build(t, srv, st, Subscription{})

	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("a brand new subscription staged %v", titles(got))
	}
	// Nothing was staged, but everything was written down, or the next poll would
	// stage the same window after all.
	if n := st.count(srv.URL); n != 3 {
		t.Fatalf("the first poll remembered %d entries, want 3", n)
	}
}

// TestTheSameEntryIsStagedOnceAndOnlyOnce is the one this whole package is
// built around. An entry stays in a feed's document for weeks, so every poll
// sees it again, and a second staging is a second download of a file the user
// already has.
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

	// The publisher has not changed anything: the same document, with Folge 2
	// still in it, is served again.
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("the same entry was staged a second time: %v", titles(got))
	}
	// And a third time, because a bug here is the kind that only shows up after
	// the loop has been round more than twice.
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("the same entry was staged again on the third poll: %v", titles(got))
	}
}

// TestARestartDoesNotStageTheSameEntryAgain is the same rule across a process
// boundary. The memory is the only thing that survives, so this is what proves
// it is actually being written and read rather than living in the poller.
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

	// A new process, the same stored memory, the same unchanged feed.
	second, rec2 := build(t, srv, st, Subscription{})
	pollAll(second)
	if got := rec2.take(); len(got) != 0 {
		t.Fatalf("after a restart the feed staged %v again", titles(got))
	}
	// And it is not simply seeding a second time: an entry published after the
	// restart still has to arrive.
	srv.publish(item(3, "Folge 3"))
	pollAll(second)
	got := rec2.take()
	if len(got) != 1 || got[0].Package != "Folge 3" {
		t.Fatalf("after a restart a genuinely new entry did not arrive: %v", titles(got))
	}
}

// TestTheFilterDecidesWhatIsStagedNotWhatIsRemembered is the reason remember
// writes down every entry rather than only the matches. Remembering only the
// matches would mean that relaxing a filter next month dumps the publisher's
// whole current window into the collector at once.
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

	// The filter is taken off. The entry it held back is old news, not new news.
	if errs := r.Apply([]Subscription{{URL: srv.URL}}); len(errs) != 0 {
		t.Fatal(errs)
	}
	pollAll(r)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("relaxing the filter staged %v out of the feed's history", titles(got))
	}
}

// TestEditingARowKeepsWhatItKnows is the reconcile promise. Rebuilding a poller
// on every settings save would throw away its memory, so saving the speed limit
// would make a feed hand over its whole current window.
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

// TestWhatTheSubscriptionAskedForRidesAlong: the destination folder and the
// priority reach the job, because nobody is there to type them in afterwards.
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

// TestAServerErrorIsNotAnEmptyFeed is the failure that would otherwise be
// silent in the worst possible way: a 404 page or a login form parses to zero
// entries, and treating that as a successful poll would mark the subscription
// as seeded with nothing in it, so the feed's whole window arrives the moment
// the server comes back.
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

	// The server comes back. This is still the subscription's first real poll, so
	// it seeds and stages nothing.
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

// TestAnUnreadableMemoryStopsThePollRatherThanEmptyingIt: polling with a memory
// that could not be read is polling with an empty memory, and an empty memory
// means everything in the feed is new.
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

// TestAMemoryLargerThanTheCapKeepsWhatIsStillInTheFeed. The memory is bounded,
// so something has to be forgotten; what must never be forgotten is an entry
// the publisher is still serving, or it is staged again while it is still
// sitting there.
func TestAMemoryLargerThanTheCapKeepsWhatIsStillInTheFeed(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	st := newState()
	// A memory already at its limit, none of it belonging to entries the feed
	// still carries.
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

// TestCloseWaitsForTheHandover is the promise everything downstream relies on:
// once Close returns, nothing is still on its way into the link list, so the
// store the sink writes to can be torn down under it.
func TestCloseWaitsForTheHandover(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	// A subscription that has already run, so the first poll of this runner hands
	// something over rather than seeding.
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

// TestCloseEndsAFetchInFlight is the other half of that promise, and the reason
// close cancels before it waits. Apply closes the pollers that are going and
// waits for them, so without the cancellation a settings save would sit on a
// publisher's server for the client's whole timeout with somebody watching a
// spinner.
func TestCloseEndsAFetchInFlight(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	srv.mu.Lock()
	// release is never closed: the only thing that lets this handler go is the
	// client giving up on the request.
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

// TestCancellingTheContextEndsAFetchInFlight: a shutdown must not wait out a
// publisher's server. Without the context on the request, Close would block for
// the client's whole timeout on a host that accepts the connection and then
// says nothing.
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

// TestARowThatCannotBePolledIsReportedAndTheRestStillRun. One address with a
// typo in it must not turn the whole intake off.
func TestARowThatCannotBePolledIsReportedAndTheRestStillRun(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	rec := &sink{}
	r, err := New(Options{
		Subscriptions: []Subscription{
			{URL: "file:///etc/passwd"},
			{URL: srv.URL},
			// The same feed twice, collapsed rather than reported.
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

	// And a list where nothing at all can be polled is an error, not a runner
	// that reports feeds it is not looking at.
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
