package api

// What the feed subscriptions are doing, and what is at an address before
// anybody subscribes to it. The rows are written through /api/settings, like
// connections and rules, or through /api/feeds/subscriptions, which a member
// of the group reaches without the rest of the settings.
//
// The status table is what keeps a feed that has been answering 403 for weeks
// from looking like one with nothing new. The test route lets a filter be
// tried before saving; after a save the first poll writes the whole feed down
// as known, and the entries the filter would have matched are gone.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// feedRow is one configured subscription and what the runner can say about
// it. Rows come from the configured list, so a saved subscription that is not
// being polled still shows up.
type feedRow struct {
	URL   string    `json:"url"`
	Kind  feed.Kind `json:"kind"`
	Title string    `json:"title"`
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
		"every configured subscription with its kind and title, whether it is being polled, when it last was, the last error, whether it has seeded and how many entries it remembers",
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
				row := feedRow{URL: strings.TrimSpace(s.URL), Kind: s.Kind, Title: s.Title}
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
		"fetch one subscription row once and report its name and first entries with their length, marking which ones it takes and why not; stages nothing and remembers nothing",
		func(w http.ResponseWriter, r *http.Request) {
			var row feed.Subscription
			if !decodeJSON(w, r, &row) {
				return
			}
			subs := feed.Sanitize([]feed.Subscription{row})
			if len(subs) == 0 {
				http.Error(w, "there is no address to test; type the feed address into the row first", http.StatusBadRequest)
				return
			}
			sub := subs[0]
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

	reg.Add(http.MethodPost, "/api/feeds/resolve",
		"turn a pasted address into a subscription row: a YouTube channel or playlist into its feed plus its channel listing, a MediathekViewWeb search into a search row, anything else into a feed once a test fetch finds one",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				URL string `json:"url"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if strings.TrimSpace(body.URL) == "" {
				http.Error(w, "there is no address to look up; paste one first", http.StatusBadRequest)
				return
			}
			res, err := a.ResolveFeed(r.Context(), body.URL)
			if err != nil {
				var bad *feed.AddressError
				if errors.As(err, &bad) {
					http.Error(w, err.Error()+"; paste an http or https address", http.StatusBadRequest)
					return
				}
				// As in the test route: the request was fine and nothing usable
				// was found there.
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, res)
		})

	reg.Add(http.MethodGet, "/api/feeds/subscriptions",
		"the configured subscription rows, as the settings hold them",
		func(w http.ResponseWriter, r *http.Request) {
			subs := a.Settings.Get().Feeds
			if subs == nil {
				subs = []feed.Subscription{}
			}
			writeJSON(w, subs)
		})

	reg.Add(http.MethodPut, "/api/feeds/subscriptions",
		"replace every subscription row, checked like a settings save of feeds; answers with the rows as stored",
		func(w http.ResponseWriter, r *http.Request) {
			var rows []feed.Subscription
			if !decodeJSON(w, r, &rows) {
				return
			}
			if _, ok := memberOf(r); ok {
				// Where files land is configuration, so a member picks only among
				// the folders the owner set up, as for POST /api/links. Checked
				// here rather than in groupLimits so the refusal names the row.
				for i, s := range rows {
					if !a.InsideDownloadFolders(s.Dir) {
						writeJSONStatus(w, http.StatusForbidden, map[string]any{
							"error": fmt.Sprintf("feed row %d saves into a folder outside the download folders; "+
								"sign in at this instance's web interface to pick another one", i+1),
							"code":  "ownLoginNeeded",
							"field": fmt.Sprintf("feeds.%d", i),
						})
						return
					}
				}
			}
			raw, err := json.Marshal(rows)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			patch := map[string]json.RawMessage{"feeds": raw}
			preview, err := settings.ApplyPatch(a.Settings.Get(), patch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := settings.CheckFolders(a.DataDir, preview, patched(patch)); err != nil {
				writeValidationError(w, err)
				return
			}
			if err := validateRows(a.DataDir, preview, patched(patch)); err != nil {
				writeValidationError(w, err)
				return
			}
			applied, err := a.PatchSettings(patch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			subs := applied.Feeds
			if subs == nil {
				subs = []feed.Subscription{}
			}
			writeJSON(w, subs)
		})
}
