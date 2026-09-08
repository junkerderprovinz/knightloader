package api

// What the feed subscriptions are actually doing, and what is at an address
// before anybody commits to it.
//
// The rows themselves deliberately get no write route here, the same refusal
// routes_connections.go and routes_rules.go write down for their own field of
// the settings document: Feeds is served by GET /api/settings, written by PUT
// and PATCH /api/settings, and a second writer is a second place for that round
// trip to be gotten wrong. What is left is the two things a save cannot do.
//
// The first is the reason this file exists at all. Everything a person wants to
// know about a subscription - when it was last looked at, whether the last look
// failed, whether it has done the first pass that writes the feed's current
// window down as known, and how much it remembers - lived only in the log until
// now. The settings card could show an address and an interval and nothing else,
// so a feed that had been answering 403 for a fortnight looked exactly like a
// feed whose publisher had posted nothing.
//
// The second is the title filter. It is a regular expression typed blind against
// titles nobody can see, and the only way to learn what it matched used to be to
// save it and wait, by which time the subscription has already written the whole
// document down as known and the entries it would have matched are gone for
// good.

import (
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/feed"
)

// feedRow is one configured subscription as the interface sees it: the address
// it is filed under, and what the runner can say about it.
//
// It is built from the CONFIGURED list joined onto the live one rather than from
// the live one alone, because the row that matters most is the one that is
// missing from the runner: a subscription that is saved and not being polled.
// Listing only what is polled would leave that row out of the table entirely,
// which is the same silence this route exists to end.
type feedRow struct {
	URL string `json:"url"`
	// Polling says whether this address is actually being polled right now.
	// False is the answer to "why has this feed never added anything", and it is
	// the value everything below it has to be read against.
	Polling bool `json:"polling"`
	// LastPolledAt is when the last poll ran, successful or not. Absent when none
	// has run since this process started, which after a restart is briefly true
	// of a subscription that has been followed for months: the memory of what it
	// has staged survives a restart and this table does not. omitzero rather than
	// omitempty, because a struct is never empty to encoding/json and a zero time
	// would otherwise be shipped as the year one.
	LastPolledAt time.Time `json:"lastPolledAt,omitzero"`
	// Error is why the last poll produced nothing, or why this row is not being
	// polled at all. Empty means the last poll was fine.
	Error string `json:"error,omitempty"`
	// Seeded says whether the first poll, which writes down everything already in
	// the feed and stages none of it, is behind this subscription. Until it is,
	// nothing this feed publishes will be added, and somebody watching an empty
	// collector deserves to be told that rather than left to wonder.
	//
	// False while lastPolledAt is absent means "not known yet", not "it has not
	// seeded". Same for remembered.
	Seeded bool `json:"seeded"`
	// Remembered is how many entries the subscription currently recognises, which
	// is what stops the feed's current window being staged a second time.
	Remembered int `json:"remembered"`
}

// feedTest is a preview plus the one sentence a preview cannot carry, the same
// shape and the same reason as reconnectImport: the body always describes the
// attempt, whether or not the far end played along.
type feedTest struct {
	feed.Preview
	Error string `json:"error,omitempty"`
}

// feedTestFailure is the body a fetch that did not work answers with. The entry
// list is still a list and not null, so whatever draws the panel has one shape
// to render however the attempt ended - the same promise Preview.Entries makes
// for a feed that turned out to be empty.
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
			// The configured order, not the runner's sorted one: this table sits
			// under the rows on the settings card, and a status list that does not
			// line up with the rows it describes is one somebody reads the wrong
			// row out of.
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
					// Not being polled, and Validate is asked why: it is the same
					// validator that refuses the save and the same one the runner
					// refuses a single row with, so the three cannot end up saying
					// different things about one address. A row that is valid and
					// still absent leaves this empty and says so through polling
					// alone, because that is the runner not having taken the list
					// yet: a moment at startup, not a fault worth inventing a
					// sentence for.
					row.Error = err.Error()
				}
				rows = append(rows, row)
			}
			// Never nil, so an instance following no feeds answers with an empty
			// list rather than JSON null.
			writeJSON(w, rows)
		})

	reg.Add(http.MethodPost, "/api/feeds/test",
		"fetch one feed once and report its name and first entries, marking which ones a title filter takes; stages nothing and remembers nothing",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				URL string `json:"url"`
				// The filter is optional and empty means no filter, which is "take
				// everything" and never "take nothing" - the same reading the
				// subscription itself gives an empty pattern.
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
				// 400, unlike the fetch below, and the difference is worth being
				// deliberate about: these two failures are the row itself being
				// wrong, they are fixed by a keystroke in the form, and Validate's
				// sentence already names which of the two fields it was. What it
				// does not carry is what to do next, so that is added here.
				http.Error(w, err.Error()+"; fix that field and test again", http.StatusBadRequest)
				return
			}
			// The request's own context, so a browser that navigated away does not
			// leave this waiting on a publisher's server for the client's whole
			// ceiling.
			p, err := a.InspectFeed(r.Context(), sub)
			if err != nil {
				// 200 with the reason in the body, the same call /api/reconnect/import
				// makes: the request was perfectly good, it was the far end that was
				// not, and a 4xx would have the browser log the one answer somebody
				// is meant to read. It also keeps the panel one shape, so a client
				// renders a failure without a second code path.
				writeJSON(w, feedTestFailure(err))
				return
			}
			writeJSON(w, feedTest{Preview: p})
		})
}
