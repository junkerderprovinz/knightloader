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
// subscription's record of what it has already staged under. Spelled out here
// rather than exported from that package: this test has to look at the stored
// bytes the way anything else would, and a helper that read them through the
// same code that writes them could agree with a bug.
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

// TestATestFetchStagesNothingAndRemembersNothing is the guard the test route was
// written around, and it is aimed squarely at the shortcut somebody will
// eventually take: answering this route by running the subscription's own poll
// and reporting what came back.
//
// Both halves of that shortcut are one-way from the user's side.
//
// Staging puts a publisher's whole current window into the collector because
// somebody pressed a button labelled "test", and on an instance with
// auto-confirm on it starts downloading all of it.
//
// Writing the memory is worse, because it is silent. A poll marks every entry in
// the document as seen, so an address that has been TESTED and not yet saved
// would arrive already seeded: the subscription is then created, adds nothing at
// all, and the entries it was created for are gone for good. Nothing anywhere
// says why.
//
// The store is pre-loaded with an empty record for this address, which is the
// state that makes both halves reachable at once: an empty record that EXISTS
// means the subscription has run before and remembers nothing, so the seeding
// pass is behind it and the polling path would stage every entry it matched
// rather than quietly writing them down.
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
	// Checked first, and this matters more than it looks: a route that fetched
	// nothing at all would pass every assertion below. The guard only means
	// something once the fetch it guards has demonstrably happened.
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

// stagedWithin waits up to d for links to appear and returns whatever it found.
//
// Waited for rather than sampled once, because the path this guards against
// hands entries over on a goroutine of its own (app.onFeedEntry spawns), so a
// check made the instant the response lands would pass against a broken version
// purely by being early. That is the shape of blind guard this repository has
// been bitten by before, so the wait is the test.
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

// TestATestFetchShowsTheTitlesAFilterIsWrittenAgainst is what the route is for.
// A title filter is a regular expression typed against titles nobody has seen,
// so an answer that reported only a count would leave somebody guessing at the
// spelling of the very thing they are matching.
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

	// No filter is "take everything" and never "take nothing", which is the one
	// reading of an empty pattern that a person types blind can afford.
	_, all, raw := postFeedTest(t, srv.URL, feeds.URL, "")
	if all.Matched != all.Total || all.Total != 3 {
		t.Errorf("with no filter %d of %d entries matched, want all three: %s", all.Matched, all.Total, raw)
	}
}

// TestATestFetchRefusesAnAddressThisProcessMustNotFetch is the same narrowing
// the subscription itself is validated with. This route reaches an address a
// person just typed, so a file:// one would be a way to read any file on the box
// through a settings field.
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

// TestAFeedThatCannotBeReadIsAnAnswerNotAnError: the request was perfectly good,
// it is the publisher's server that was not, and a 4xx would have the browser
// log the one answer somebody is meant to read.
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

// TestTheFeedTableReportsASubscriptionThatIsBeingPolled is the whole point of
// GET /api/feeds. Everything it answers used to exist only as log lines, so a
// subscription that had been answering 403 for a fortnight looked exactly like
// one whose publisher had posted nothing.
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

	// The runner polls the moment it starts, on its own goroutine, so the row is
	// waited for rather than read once. A restart-shaped blank (nothing polled
	// yet) is a legitimate answer this route has to be able to give, which is
	// exactly why it cannot be asserted on immediately.
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
	// The seeding pass stages nothing, and the table saying so is the whole
	// answer to "I added a feed and nothing happened".
	if got := stagedWithin(a, 200*time.Millisecond); len(got) != 0 {
		t.Errorf("the first poll staged %v", got)
	}
}

// TestTheFeedTableNamesARowThatIsNotBeingPolled covers the row that matters
// most: one that is saved and dead. Listing only what the runner accepted would
// leave it out of the table altogether, which is the same silence this route
// exists to end.
func TestTheFeedTableNamesARowThatIsNotBeingPolled(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	// Written straight into the store rather than saved through the API, because
	// the API refuses this row on purpose (validateRows). A hand-edited
	// settings.json is how it gets there, and it is precisely then that somebody
	// needs the table to say something.
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
