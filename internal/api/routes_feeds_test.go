package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// feedStateBucket is the interface-state key internal/app keeps each
// subscription's staged-entry record under. It is spelled out so the test
// reads the stored bytes independently of the code that writes them.
const feedStateBucket = "feeds"

// feedServer serves one small RSS document with three entries, two of which
// share a title prefix so a filter has something to take and something to leave.
func feedServer(t *testing.T) *httptest.Server {
	t.Helper()
	var items string
	for _, e := range []struct {
		n     int
		title string
	}{{3, "Folge 3"}, {2, "Werbung"}, {1, "Folge 1"}} {
		items += fmt.Sprintf(
			`<item><title>%s</title><link>https://example.invalid/e/%d</link><guid>kf-%04d</guid></item>`,
			e.title, e.n, e.n)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel>` +
			`<title>Kellerfunk</title>` + items + `</channel></rss>`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// postFeedTest presses the test button and hands back the status and the body.
func postFeedTest(t *testing.T, base, url, filter string) (int, feedTest, string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"url": url, "titleFilter": filter})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(base+"/api/feeds/test", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp) // closes resp.Body (federation_test.go)
	if err != nil {
		t.Fatal(err)
	}
	var out feedTest
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decoding the test answer: %v (%s)", err, raw)
		}
	}
	return resp.StatusCode, out, string(raw)
}

// TestATestFetchStagesNothingAndRemembersNothing guards against answering the
// test route with the subscription's own poll. That would stage the feed's
// whole window, and it would record every entry as seen, so the subscription
// saved afterwards would add nothing.
//
// The store starts with an empty record for this address, meaning the
// subscription has seeded and remembers nothing, so a poll would both stage
// and record.
func TestATestFetchStagesNothingAndRemembersNothing(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	feeds := feedServer(t)

	before := `{"` + feeds.URL + `":[]}`
	if err := a.SetUIState(feedStateBucket, before); err != nil {
		t.Fatal(err)
	}

	code, out, raw := postFeedTest(t, srv.URL, feeds.URL, "^Folge ")
	if code != http.StatusOK {
		t.Fatalf("POST /api/feeds/test answered %d: %s", code, raw)
	}
	// Checked first, since a route that fetched nothing would pass everything
	// below.
	if out.Title != "Kellerfunk" || out.Total != 3 || out.Matched != 2 || len(out.Entries) != 3 {
		t.Fatalf("the test fetch did not actually read the feed: %s", raw)
	}

	if got := stagedWithin(a, time.Second); len(got) != 0 {
		t.Errorf("a test fetch staged %d link(s); pressing test must never add anything to the collector", len(got))
	}
	after, err := a.UIState(feedStateBucket)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("a test fetch wrote the seen-memory:\n before %s\n  after %s\n"+
			"the subscription would now be seeded and would add nothing when it is saved", before, after)
	}
}

// stagedWithin waits up to d for links to appear and returns whatever it
// found. app.onFeedEntry stages on its own goroutine, so a single early check
// would pass against a broken version.
func stagedWithin(a *app.App, d time.Duration) []string {
	deadline := time.Now().Add(d)
	for {
		var names []string
		for _, t := range a.Tasks() {
			names = append(names, t.Name)
		}
		if len(names) > 0 || time.Now().After(deadline) {
			return names
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestATestFetchShowsTheTitlesAFilterIsWrittenAgainst checks that each title
// comes back with whether the filter takes it, not only a count.
func TestATestFetchShowsTheTitlesAFilterIsWrittenAgainst(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()
	feeds := feedServer(t)

	_, out, raw := postFeedTest(t, srv.URL, feeds.URL, "^Folge ")
	want := map[string]bool{"Folge 3": true, "Werbung": false, "Folge 1": true}
	for _, e := range out.Entries {
		match, known := want[e.Title]
		if !known {
			t.Errorf("the answer carried an entry the feed does not have: %q", e.Title)
			continue
		}
		if e.Matches != match {
			t.Errorf("%q: matches = %v, want %v", e.Title, e.Matches, match)
		}
		if e.Link == "" {
			t.Errorf("%q carries no link, so nobody can tell what would actually be staged", e.Title)
		}
	}
	if len(out.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %s", len(out.Entries), len(want), raw)
	}

	// No filter takes everything.
	_, all, raw := postFeedTest(t, srv.URL, feeds.URL, "")
	if all.Matched != all.Total || all.Total != 3 {
		t.Errorf("with no filter %d of %d entries matched, want all three: %s", all.Matched, all.Total, raw)
	}
}

// TestATestFetchRefusesAnAddressThisProcessMustNotFetch checks the same
// validation a subscription gets, so a file:// address cannot read files on
// the box.
func TestATestFetchRefusesAnAddressThisProcessMustNotFetch(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	for _, c := range []struct{ url, filter, why string }{
		{"file:///etc/passwd", "", "a scheme this process must never fetch"},
		{"", "", "an empty address"},
		{"https://example.invalid/rss.xml", "([", "a pattern that will not compile"},
	} {
		code, _, raw := postFeedTest(t, srv.URL, c.url, c.filter)
		if code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400: %s", c.why, code, raw)
		}
		if !strings.Contains(raw, ";") {
			t.Errorf("%s was refused without saying what to do about it: %s", c.why, raw)
		}
	}
}

// TestAFeedThatCannotBeReadIsAnAnswerNotAnError checks that a failing
// publisher yields a 200 with the reason, since the request itself was fine.
func TestAFeedThatCannotBeReadIsAnAnswerNotAnError(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer dead.Close()

	code, out, raw := postFeedTest(t, srv.URL, dead.URL, "")
	if code != http.StatusOK {
		t.Fatalf("a feed answering 503 came back as HTTP %d: %s", code, raw)
	}
	if out.Error == "" {
		t.Fatalf("a feed answering 503 was reported as a successful test: %s", raw)
	}
	if !strings.Contains(out.Error, "503") {
		t.Errorf("the reason does not name what the server said: %q", out.Error)
	}
	if out.Entries == nil {
		t.Error("entries came back as null rather than an empty list, so the panel has two shapes to draw")
	}
}

func TestTheFeedTableReportsASubscriptionThatIsBeingPolled(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	feeds := feedServer(t)

	saved := settingsWith(func(s *settings.Settings) {
		s.Feeds = []feed.Subscription{{URL: feeds.URL}}
	})
	if code, _, msg := putSettings(t, srv.URL, saved); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	// The runner polls on its own goroutine, so the row is waited for.
	var row feedRow
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows := getFeeds(t, srv.URL)
		if len(rows) != 1 {
			t.Fatalf("got %d rows, want the one configured subscription", len(rows))
		}
		row = rows[0]
		if !row.LastPolledAt.IsZero() || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !row.Polling {
		t.Fatalf("a saved, valid subscription is not being polled: %+v", row)
	}
	if row.LastPolledAt.IsZero() {
		t.Fatalf("the subscription was never polled: %+v", row)
	}
	if row.Error != "" {
		t.Errorf("a feed that answered fine reports an error: %q", row.Error)
	}
	if !row.Seeded {
		t.Error("the first poll did not report itself as the seeding pass, so nobody can tell why the collector is empty")
	}
	if row.Remembered != 3 {
		t.Errorf("remembered = %d, want the three entries the document carried", row.Remembered)
	}
	// The seeding pass stages nothing.
	if got := stagedWithin(a, 200*time.Millisecond); len(got) != 0 {
		t.Errorf("the first poll staged %v", got)
	}
}

// TestTheFeedTableNamesARowThatIsNotBeingPolled covers a saved row the runner
// does not poll, which a listing of the runner alone would leave out.
func TestTheFeedTableNamesARowThatIsNotBeingPolled(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	// Written to the store directly, as a hand-edited settings.json would be;
	// the API refuses this row.
	if _, err := a.Settings.Set(settingsWith(func(s *settings.Settings) {
		s.Feeds = []feed.Subscription{{URL: "ftp://example.invalid/rss.xml"}}
	})); err != nil {
		t.Fatal(err)
	}

	rows := getFeeds(t, srv.URL)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want the one configured subscription", len(rows))
	}
	if rows[0].Polling {
		t.Fatal("an address that cannot be fetched is reported as being polled")
	}
	if rows[0].Error == "" {
		t.Fatal("a row that is not being polled says nothing about why")
	}
	if !strings.Contains(rows[0].Error, "http") {
		t.Errorf("the reason does not name what is wrong with the address: %q", rows[0].Error)
	}
}

// TestTheFeedTableIsAListEvenWithNoSubscriptions: a nil slice encodes as JSON
// null, and a client that walks the answer would then have to check for it.
func TestTheFeedTableIsAListEvenWithNoSubscriptions(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/feeds")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "[]" {
		t.Errorf("GET /api/feeds on an instance following nothing answered %s, want []", raw)
	}
}

func getFeeds(t *testing.T, base string) []feedRow {
	t.Helper()
	resp, err := http.Get(base + "/api/feeds")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/feeds answered %d: %s", resp.StatusCode, raw)
	}
	var rows []feedRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decoding the feed table: %v (%s)", err, raw)
	}
	return rows
}
