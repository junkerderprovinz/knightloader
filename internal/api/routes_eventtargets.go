package api

// What the event targets are actually doing, and what an address answers
// before anybody commits to it.
//
// The rows get no write route here, the same refusal routes_feeds.go,
// routes_connections.go and routes_rules.go make for their own fields of the
// settings document. EventTargets is written by PUT and PATCH /api/settings,
// and for this field the redact-and-merge round trip is the security of the
// feature (see notify.Merge), so a second writer would be a second way past
// it.
//
// What is left is the three things a save cannot do. The status table, because
// when a target last tried, when one last arrived and how many were dropped
// live in memory beside the dispatcher, so a target answering 401 for a
// fortnight otherwise looks like one whose events have not happened yet. The
// test button, because a push server's topic, token and body shape are typed
// blind. And the placeholder list, taken from the expander so the picker
// cannot offer names this build does not fill in.

import (
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// eventTargetRow is one configured target as the interface sees it. Built from
// the configured list joined onto the live health rather than from the live
// list alone: a target that is saved, switched on and not sending has no
// worker, and that is the row worth seeing.
type eventTargetRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Host is the URL's host and never the whole address: ntfy and Gotify take
	// their credential in the query, and every browser on the settings page
	// polls this table. See notify.Target.Host and notify's redact.
	Host string `json:"host"`
	// LastAttempt is when a request was last made, absent when none has been
	// made since this process started. The health table lives in memory, so
	// after a restart that is briefly true of a target that has been
	// delivering for months. omitzero rather than omitempty, because a zero
	// time is not empty to encoding/json and would ship as the year one.
	LastAttempt time.Time `json:"lastAttempt,omitzero"`
	// LastOK is when one last arrived. Both times recent is a working target;
	// this one old is a target that has been failing since then.
	LastOK     time.Time `json:"lastOk,omitzero"`
	LastStatus int       `json:"lastStatus"`
	// LastError is the transport failure, already redacted by internal/notify.
	// Empty when the far end answered at all; what it answered is in
	// LastStatus and LastCode.
	LastError string `json:"lastError,omitempty"`
	// LastCode is a notify.Problem code, so the page can say what to try in the
	// reader's own language rather than showing an English sentence.
	LastCode string `json:"lastCode,omitempty"`
	Attempts int    `json:"attempts"`
	Sent     int    `json:"sent"`
	Dropped  int    `json:"dropped"`
}

// enabledEventTargets is how many targets are sending, which is not how many
// exist: a row being built, or one switched off for the week, is
// configuration. It lives here rather than beside its caller in
// routes_features.go because what counts as on belongs with the subsystem.
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
			// sits under the rows on the settings card, and a list that does
			// not line up with them is read off the wrong row.
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
			// hand-copied list offers names the server never fills in and
			// omits ones it does, the argument script.AllTriggers makes for
			// the trigger vocabulary this page also reuses.
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
				// 400, unlike the send below: this is the row being wrong, it
				// is fixed by a keystroke in the form, and the typed code lets
				// the page name the field in the reader's own language.
				writeValidationError(w, p)
				return
			}
			// 200 with the whole attempt in the body even when the far end
			// refused, the same as /api/feeds/test and /api/reconnect/import.
			// The request was good and the far end was not, and a 4xx would
			// have the browser log the one answer somebody is meant to read.
			//
			// The request's own context, so a browser that navigated away does
			// not leave this waiting on a push server for the whole time limit.
			writeJSON(w, a.TestEventTarget(r.Context(), draft))
		})
}
