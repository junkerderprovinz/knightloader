package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/feed"
)

func TestAFeedRowSaysItsKindAndTitle(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	feeds := feedServer(t)
	body := `[{"url":"` + feeds.URL + `","title":"Kellerfunk"}]`
	if status, out := owner(http.MethodPut, "/api/feeds/subscriptions", body); status != http.StatusOK {
		t.Fatalf("saving the subscriptions answered %d: %s", status, out)
	}
	status, out := owner(http.MethodGet, "/api/feeds", "")
	var rows []feedRow
	if status != http.StatusOK || json.Unmarshal(out, &rows) != nil || len(rows) != 1 {
		t.Fatalf("GET /api/feeds answered %d: %s", status, out)
	}
	if rows[0].Kind != feed.KindRSS || rows[0].Title != "Kellerfunk" {
		t.Errorf("row %+v", rows[0])
	}
}

func TestATestTakesAWholeRowAndSaysWhyAnEntryIsLeftOut(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	feeds := feedServer(t)
	row, _ := json.Marshal(feed.Subscription{URL: feeds.URL, TitleFilter: "^Folge ", ExcludeFilter: "1$"})
	status, out := owner(http.MethodPost, "/api/feeds/test", string(row))
	if status != http.StatusOK {
		t.Fatalf("answered %d: %s", status, out)
	}
	var got feedTest
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]feed.Reason{"Folge 3": "", "Werbung": feed.ReasonInclude, "Folge 1": feed.ReasonExclude}
	for _, e := range got.Entries {
		if e.Reason != want[e.Title] || e.Matches != (e.Reason == "") {
			t.Errorf("%q: reason %q matches %v, want %q", e.Title, e.Reason, e.Matches, want[e.Title])
		}
	}
	if !strings.Contains(string(out), `"duration":0`) || strings.Contains(string(out), `"reason":""`) {
		t.Errorf("the entries do not carry their length, or a taken entry carries an empty reason: %s", out)
	}
	if got.Matched != 1 {
		t.Errorf("matched %d", got.Matched)
	}

	// A row the save would refuse is refused here as well.
	if status, out := owner(http.MethodPost, "/api/feeds/test", `{"url":"`+feeds.URL+`","minMinutes":9,"maxMinutes":3}`); status != http.StatusBadRequest {
		t.Errorf("an impossible length range answered %d: %s", status, out)
	}
}

func TestResolvingAPastedAddress(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	feeds := feedServer(t)

	status, out := owner(http.MethodPost, "/api/feeds/resolve", `{"url":"https://mediathekviewweb.de/#query=%21ard%20%23tatort%20%3E80"}`)
	var mt feed.Resolved
	if status != http.StatusOK || json.Unmarshal(out, &mt) != nil {
		t.Fatalf("answered %d: %s", status, out)
	}
	if mt.Kind != feed.KindMediathek || mt.Mediathek == nil || mt.Mediathek.Channel != "ard" || mt.MinMinutes != 80 || mt.URL == "" {
		t.Errorf("a search resolved to %s", out)
	}

	status, out = owner(http.MethodPost, "/api/feeds/resolve", `{"url":"`+feeds.URL+`"}`)
	var rss feed.Resolved
	if status != http.StatusOK || json.Unmarshal(out, &rss) != nil || rss.Kind != feed.KindRSS || rss.Title != "Kellerfunk" || rss.URL != feeds.URL {
		t.Errorf("a feed resolved to %d %s", status, out)
	}
	if strings.Contains(string(out), `"channel"`) {
		t.Errorf("a plain feed offers a channel listing: %s", out)
	}

	dead := httptest.NewServer(http.NotFoundHandler())
	defer dead.Close()
	status, out = owner(http.MethodPost, "/api/feeds/resolve", `{"url":"`+dead.URL+`"}`)
	var failed struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if status != http.StatusBadRequest || json.Unmarshal(out, &failed) != nil || failed.Error == "" || failed.Code != "feedNotFound" {
		t.Errorf("an address with nothing there answered %d: %s, want 400 with the code feedNotFound and the reason", status, out)
	}

	if status, out := owner(http.MethodPost, "/api/feeds/resolve", `{"url":"file:///etc/passwd"}`); status != http.StatusBadRequest {
		t.Errorf("a local file answered %d: %s", status, out)
	}
}

func TestTheSubscriptionsAreReadAndSavedOnTheirOwn(t *testing.T) {
	t.Parallel()
	a, downloads, _, _, owner := groupServer(t)
	feeds := feedServer(t)

	if status, out := owner(http.MethodGet, "/api/feeds/subscriptions", ""); status != http.StatusOK || strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("an instance following nothing answered %d: %s", status, out)
	}
	body := `[{"url":" ` + feeds.URL + ` ","excludeFilter":"Werbung","dir":"` + jsonPath(filepath.Join(downloads, "funk")) + `"},` +
		`{"kind":"mediathek","mediathek":{"topic":"Tatort"}}]`
	status, out := owner(http.MethodPut, "/api/feeds/subscriptions", body)
	if status != http.StatusOK {
		t.Fatalf("answered %d: %s", status, out)
	}
	var saved []feed.Subscription
	if err := json.Unmarshal(out, &saved); err != nil || len(saved) != 2 {
		t.Fatalf("the answer is not the stored rows: %s", out)
	}
	if saved[0].URL != feeds.URL || saved[1].URL == "" || saved[1].Mediathek.Quality != feed.QualityHD {
		t.Errorf("the rows were not cleaned as a settings save cleans them: %s", out)
	}
	if got := a.Settings.Get().Feeds; len(got) != 2 || got[0].ExcludeFilter != "Werbung" {
		t.Errorf("the settings hold %+v", got)
	}

	status, out = owner(http.MethodPut, "/api/feeds/subscriptions", `[{"url":"`+feeds.URL+`"},{"url":"`+feeds.URL+`/b","excludeFilter":"(["}]`)
	var refusal struct {
		Error string `json:"error"`
		Field string `json:"field"`
	}
	if status != http.StatusBadRequest || json.Unmarshal(out, &refusal) != nil || refusal.Field != "feeds.1" || refusal.Error == "" {
		t.Errorf("a broken pattern in the second row answered %d: %s", status, out)
	}
	if got := a.Settings.Get().Feeds; len(got) != 2 {
		t.Errorf("a refused save changed the settings to %+v", got)
	}
}

func TestAMemberSavesSubscriptionsOnlyIntoTheDownloadFolders(t *testing.T) {
	t.Parallel()
	a, downloads, _, member, owner := groupServer(t)
	feeds := feedServer(t)
	elsewhere := t.TempDir()
	rows := func(dir string) string {
		return `[{"url":"` + feeds.URL + `"},{"url":"` + feeds.URL + `/b","dir":"` + jsonPath(dir) + `"}]`
	}

	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/feeds", ""},
		{http.MethodGet, "/api/feeds/subscriptions", ""},
		{http.MethodPost, "/api/feeds/test", `{"url":"` + feeds.URL + `"}`},
		{http.MethodPost, "/api/feeds/resolve", `{"url":"` + feeds.URL + `"}`},
	} {
		if status, out := member(route.method, route.path, route.body); status != http.StatusOK {
			t.Errorf("a member's %s %s answered %d: %s", route.method, route.path, status, out)
		}
	}

	if status, out := member(http.MethodPut, "/api/feeds/subscriptions", rows(filepath.Join(downloads, "funk"))); status != http.StatusOK {
		t.Fatalf("a member saving into the download folder answered %d: %s", status, out)
	}
	status, out := member(http.MethodPut, "/api/feeds/subscriptions", rows(elsewhere))
	var refusal struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	}
	if !refusedForGroup(status, out) || json.Unmarshal(out, &refusal) != nil || refusal.Field != "feeds.1" {
		t.Errorf("a member saving into %s answered %d: %s", elsewhere, status, out)
	}
	if got := a.Settings.Get().Feeds; len(got) != 2 || got[1].Dir != filepath.Join(downloads, "funk") {
		t.Errorf("the refused save reached the settings: %+v", got)
	}
	if status, out := owner(http.MethodPut, "/api/feeds/subscriptions", rows(elsewhere)); status != http.StatusOK {
		t.Errorf("the owner saving into %s answered %d: %s", elsewhere, status, out)
	}
}

// jsonPath is a folder as it sits inside a JSON string.
func jsonPath(p string) string {
	b, _ := json.Marshal(p)
	return strings.Trim(string(b), `"`)
}
