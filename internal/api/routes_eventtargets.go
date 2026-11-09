package api

// What the event targets are doing, and what a channel answers before anybody
// commits to it.
//
// The channels are written by PUT and PATCH /api/settings only, since their
// redact-and-merge round trip is what keeps a token from being sent to another
// address, and a second writer would be a second way past it. What is left is
// what a save cannot do: the health kept in memory beside the dispatcher, the
// test button, and the placeholder list the webhook template offers.

import (
	"net/http"
	"time"

	kitnotify "github.com/junkerderprovinz/parleyport/kit/notify"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// eventTargetRow is one configured channel joined onto its live health, so a
// channel that is switched on and has not sent yet has a row too.
type eventTargetRow struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Kind    kitnotify.Kind `json:"kind"`
	Enabled bool           `json:"enabled"`
	notify.Health
}

// testResult is how a channel took the sample event. Error is English and
// already free of the channel's secrets.
type testResult struct {
	OK         bool   `json:"ok"`
	Attempts   int    `json:"attempts"`
	Status     int    `json:"status"`
	Code       string `json:"code,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

// enabledEventTargets is how many channels can send, which is not how many
// exist: one being built, or one switched off for the week, is configuration.
func enabledEventTargets(s settings.Settings) int {
	n := 0
	for _, c := range s.NotifyChannels {
		if c.Enabled && c.On != kitnotify.OnNever {
			n++
		}
	}
	return n
}

func registerEventTargets(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/eventtargets",
		"every configured channel with when it last tried, when one last arrived, the last refusal and how many were dropped",
		func(w http.ResponseWriter, r *http.Request) {
			live := map[string]notify.Health{}
			for _, h := range a.EventTargetHealth() {
				live[h.ChannelID] = h
			}
			// The configured order, which is the order of the rows above the table.
			channels := a.Settings.Get().NotifyChannels
			rows := make([]eventTargetRow, 0, len(channels))
			for _, c := range channels {
				h := live[c.ID]
				h.ChannelID = c.ID
				rows = append(rows, eventTargetRow{ID: c.ID, Name: c.Name, Kind: c.Kind, Enabled: c.Enabled, Health: h})
			}
			writeJSON(w, rows)
		})

	reg.Add(http.MethodGet, "/api/eventtargets/placeholders",
		"the placeholder names a webhook template may use, with the scope and the events that carry each one",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, notify.Placeholders())
		})

	reg.Add(http.MethodPost, "/api/eventtargets/test",
		"send one made-up event to one channel right now and report how it went; stores nothing and really does send",
		func(w http.ResponseWriter, r *http.Request) {
			var draft kitnotify.Channel
			if !decodeJSON(w, r, &draft) {
				return
			}
			start := time.Now()
			res := a.TestEventTarget(r.Context(), draft)
			// 200 even when the far end refused: the request was good, and the
			// page shows the answer beside the channel.
			out := testResult{OK: res.OK(), Attempts: res.Attempts, Status: res.Status, Code: res.Code, DurationMS: time.Since(start).Milliseconds()}
			if res.Err != nil {
				out.Error = res.Err.Error()
			}
			writeJSON(w, out)
		})
}
