package api

// What the feed subscriptions are doing, and what is at an address before
// anybody subscribes to it. The rows themselves are written through
// /api/settings only, like connections and rules.
//
// The status table is what keeps a feed that has been answering 403 for weeks
// from looking like one with nothing new. The test route lets a title filter
// be tried before saving; after a save the first poll writes the whole feed
// down as known, and the entries the filter would have matched are gone.

import (
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/feed"
)

// feedRow is one configured subscription and what the runner can say about
// it. Rows come from the configured list, so a saved subscription that is not
// being polled still shows up.
type feedRow struct {
	URL string `json:"url"`
	// Polling says whether this address is being polled right now; the fields
	// below are read against it.
	Polling bool `json:"polling"`
	// LastPolledAt is when the last poll ran, successful or not, and absent
	// until one has run since this process started. omitzero keeps a zero
	// time from shipping as the year one.
	LastPolledAt time.Time `json:"lastPolledAt,omitzero"`
	// Error is why the last poll produced nothing, or why this row is not
	// being polled. Empty means the last poll was fine.
	Error string `json:"error,omitempty"`
	// Seeded says whether the first poll, which records what is already in
	// the feed and stages none of it, has happened. False while LastPolledAt
	// is absent means not known yet, and the same goes for Remembered.
	Seeded bool `json:"seeded"`
	// Remembered is how many entries the subscription recognises, which keeps
	// the feed's current window from being staged twice.
	Remembered int `json:"remembered"`
}

// feedTest is a preview plus the reason a fetch failed, like reconnectImport.
type feedTest struct {
	feed.Preview
	Error string `json:"error,omitempty"`
}

// feedTestFailure is the body of a fetch that did not work, with an empty
// entry list rather than null so the panel has one shape to render.
func feedTestFailure(err error) feedTest {
	return feedTest{Preview: feed.Preview{Entries: []feed.PreviewEntry{}}, Error: err.Error()}
}

func registerFeeds(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/feeds",
		"every configured subscription with whether it is being polled, when it last was, the last error, whether it has seeded and how many entries it remembers",
		func(w http.ResponseWriter, r *http.Request) {
			live := map[string]feed.Health{}
			for _, h := range a.FeedHealth() {
				live[h.URL] = h
			}
			// The configured order, so the table lines up with the rows on the
			// settings card.
			subs := a.Settings.Get().Feeds
			rows := make([]feedRow, 0, len(subs))
			for _, s := range subs {
				row := feedRow{URL: strings.TrimSpace(s.URL)}
				h, polling := live[row.URL]
				row.Polling = polling
				if polling {
					row.LastPolledAt, row.Error = h.LastPolled, h.LastError
					row.Seeded, row.Remembered = h.Seeded, h.Remembered
				} else if err := s.Validate(); err != nil {
					// The same validator the save and the runner use. A valid row
					// that is not polled yet is the runner catching up at startup,
					// not a fault.
					row.Error = err.Error()
				}
				rows = append(rows, row)
			}
			writeJSON(w, rows)
		})

	reg.Add(http.MethodPost, "/api/feeds/test",
		"fetch one feed once and report its name and first entries, marking which ones a title filter takes; stages nothing and remembers nothing",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				URL string `json:"url"`
				// TitleFilter is optional; empty takes everything, as in a
				// subscription.
				TitleFilter string `json:"titleFilter"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			sub := feed.Subscription{
				URL:         strings.TrimSpace(body.URL),
				TitleFilter: strings.TrimSpace(body.TitleFilter),
			}
			if sub.URL == "" {
				http.Error(w, "there is no address to test; type the feed address into the row first", http.StatusBadRequest)
				return
			}
			if err := sub.Validate(); err != nil {
				// A 400, unlike a failed fetch: the row itself is wrong.
				http.Error(w, err.Error()+"; fix that field and test again", http.StatusBadRequest)
				return
			}
			// The request context cancels the fetch when the browser goes away.
			p, err := a.InspectFeed(r.Context(), sub)
			if err != nil {
				// 200 with the reason in the body, as in /api/reconnect/import:
				// the request was fine and the far end was not.
				writeJSON(w, feedTestFailure(err))
				return
			}
			writeJSON(w, feedTest{Preview: p})
		})
}
