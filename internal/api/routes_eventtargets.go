package api

// What the event targets are actually doing, and what an address answers before
// anybody commits to it.
//
// The rows themselves deliberately get no write route here, the same refusal
// routes_feeds.go, routes_connections.go and routes_rules.go each write down
// for their own field of the settings document: EventTargets is served by GET
// /api/settings, written by PUT and PATCH /api/settings, and a second writer is
// a second place for the redact-and-merge round trip to be gotten wrong. For
// this field that round trip is the security of the feature (see notify.Merge),
// so a second way in would be a second way past it.
//
// What is left is the three things a save cannot do.
//
// The first is the status table. Everything a person wants to know about a
// target - when it last tried, when one last arrived, why the last one did not,
// how many were dropped because the queue was full - lives in memory beside the
// dispatcher and nowhere else. Without this route a target that has been
// answering 401 for a fortnight looks exactly like a target whose events have
// not happened yet.
//
// The second is the test button. A push server's configuration is a topic name,
// a token and a body shape typed blind, and the only other way to find out
// whether they are right is to save them and wait for something to fail.
//
// The third is the placeholder list, which comes from the expander itself so
// that the picker cannot offer names this build does not fill in.

import (
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// eventTargetRow is one configured target as the interface sees it.
//
// Built from the CONFIGURED list joined onto the live health rather than from
// the live one alone, because the row that matters most is the one missing from
// the dispatcher: a target that is saved, switched on and not sending. Listing
// only what has a worker would leave that row out of the table entirely, which
// is the same silence this route exists to end.
type eventTargetRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Host is the URL's HOST and never the whole address. ntfy and Gotify both
	// take their credential in the query, and this table is polled by every
	// browser that opens the settings page - see notify.Target.Host, and see
	// notify's own redact for the same rule applied to error strings.
	Host string `json:"host"`
	// LastAttempt is when a request was last made. Absent when none has been
	// made since this process started, which after a restart is briefly true of
	// a target that has been delivering for months: the health table is in
	// memory and does not survive a restart. omitzero rather than omitempty,
	// because a struct is never empty to encoding/json and a zero time would
	// otherwise ship as the year one.
	LastAttempt time.Time `json:"lastAttempt,omitzero"`
	// LastOK is when one last arrived. The gap between this and LastAttempt is
	// the whole story: both recent is a working target, this one old is a target
	// that has been failing since then.
	LastOK     time.Time `json:"lastOk,omitzero"`
	LastStatus int       `json:"lastStatus"`
	// LastError is the transport failure, already redacted by internal/notify.
	// Empty when the far end answered at all, whatever it answered - that is in
	// LastStatus and LastCode.
	LastError string `json:"lastError,omitempty"`
	// LastCode is a notify.Problem code, so the page can say what to try in the
	// reader's own language rather than showing an English sentence.
	LastCode string `json:"lastCode,omitempty"`
	Attempts int    `json:"attempts"`
	Sent     int    `json:"sent"`
	Dropped  int    `json:"dropped"`
}

// enabledEventTargets is how many targets are actually sending, which is not
// the same as how many exist: a row being built, or one switched off for the
// week, is configuration and not activity.
//
// It lives here rather than beside its caller in routes_features.go for the
// same reason enabledConnections lives beside the connections: the question
// "what counts as on for this subsystem" belongs with the subsystem, and the
// module table is only asking.
func enabledEventTargets(s settings.Settings) int {
	n := 0
	for _, t := range s.EventTargets {
		if t.Enabled && len(t.Triggers) > 0 {
			n++
		}
	}
	return n
}

func registerEventTargets(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/eventtargets",
		"every configured event target with when it last tried, when one last arrived, the last refusal and how many were dropped",
		func(w http.ResponseWriter, r *http.Request) {
			live := map[string]notify.Health{}
			for _, h := range a.EventTargetHealth() {
				live[h.TargetID] = h
			}
			// The configured order, not the dispatcher's map order: this table
			// sits under the rows on the settings card, and a status list that
			// does not line up with the rows it describes is one somebody reads
			// the wrong row out of.
			targets := a.Settings.Get().EventTargets
			rows := make([]eventTargetRow, 0, len(targets))
			for _, t := range targets {
				row := eventTargetRow{ID: t.ID, Name: t.Name, Enabled: t.Enabled, Host: t.Host()}
				if h, ok := live[t.ID]; ok {
					row.LastAttempt, row.LastOK = h.LastAttempt, h.LastOK
					row.LastStatus, row.LastError, row.LastCode = h.LastStatus, h.LastError, h.LastCode
					row.Attempts, row.Sent, row.Dropped = h.Attempts, h.Sent, h.Dropped
				}
				rows = append(rows, row)
			}
			// Never nil, so an instance with no targets answers with an empty
			// list rather than JSON null.
			writeJSON(w, rows)
		})

	reg.Add(http.MethodGet, "/api/eventtargets/placeholders",
		"the placeholder names this build fills in, with the scope and the triggers that carry each one",
		func(w http.ResponseWriter, r *http.Request) {
			// Straight from the expander's own table. A picker built from a
			// hand-copied list offers names the server never fills in and omits
			// ones it does - the identical argument script.AllTriggers makes for
			// the trigger vocabulary, which this page also reuses rather than
			// listing a second time.
			writeJSON(w, notify.Placeholders())
		})

	reg.Add(http.MethodPost, "/api/eventtargets/test",
		"send one made-up event to one target right now and report the whole answer; stores nothing and really does send",
		func(w http.ResponseWriter, r *http.Request) {
			var draft notify.Target
			if !decodeJSON(w, r, &draft) {
				return
			}
			if p := notify.Validate(draft); p != nil {
				// 400, unlike the send below, and the difference is worth being
				// deliberate about: this failure is the ROW being wrong, it is
				// fixed by a keystroke in the form, and the typed code lets the
				// page say which field in the reader's own language.
				writeValidationError(w, p)
				return
			}
			// 200 with the whole attempt in the body even when the far end
			// refused, the same call /api/feeds/test and /api/reconnect/import
			// both make: the request was perfectly good, it was the far end that
			// was not, and a 4xx would have the browser log the one answer
			// somebody is meant to read. It also keeps the panel one shape, so a
			// client renders a refusal without a second code path.
			//
			// The request's own context, so a browser that navigated away does
			// not leave this waiting on a push server for the whole time limit.
			writeJSON(w, a.TestEventTarget(r.Context(), draft))
		})
}
