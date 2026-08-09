package api

// The handful of knobs the shell's quick controls reach, and nothing else.
//
// It is not a second settings page. /api/settings is simply the wrong shape for
// a widget that lives above every route:
//
//   - GET answers with the WHOLE configuration — every rule set, every outbound
//     connection, the timetable, the reconnect block — and the shell would ask
//     for all of it on every load to fill four spinners;
//   - PUT REPLACES the document. A widget holding a snapshot taken when it
//     mounted writes that snapshot back on save, so anything the settings page
//     changed in between is silently put back. POST here takes only the fields
//     that were actually sent and merges them onto what is stored at that
//     moment, which is the difference between editing a number and restoring a
//     backup nobody asked for.
//
// The queue's master switch is deliberately NOT here, even though the quick
// panel shows it: POST /api/queue already halts and releases, and two doors to
// one room is how a client picks the worse one without ever finding out.

import (
	"fmt"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// controls is what the shell needs and cannot read off the task stream.
type controls struct {
	MaxConcurrent int `json:"maxConcurrent"`
	MaxPerHost    int `json:"maxPerHost"`

	// Chunks is how many connections ONE download opens, as configured. Zero is
	// "no opinion", and the dispatcher's own fallback applies.
	//
	// It is NOT a count of sockets that are open right now, and no field here is:
	// a backend reports a status, a size, a byte count and a speed (core.Update)
	// and never how many connections it holds. A figure labelled "open
	// connections" would be this number wearing a name it cannot honour, and
	// somebody would tune against it — so the interface says "chunks", which is
	// what it is.
	Chunks int `json:"chunks"`

	// MaxChunks is the engine's own ceiling, served rather than compiled into the
	// interface for the same reason the cleanup classes are: a spinner that goes
	// higher than connsFor will honour is a control that lies about what saving
	// it did.
	//
	// The two concurrency numbers have no bound here on purpose. Theirs lives in
	// settings.sanitizeQueue and is not exported, and a limit typed into this
	// struct would be a second copy of it, drifting from the first the day it
	// moves. The POST answers with what was actually stored instead, so a field
	// that asked for 999 settles on the truth rather than on a guess the client
	// was carrying.
	MaxChunks int `json:"maxChunks"`

	SpeedLimit int64 `json:"speedLimit"`
}

func controlsOf(s settings.Settings) controls {
	return controls{
		MaxConcurrent: s.MaxConcurrent,
		MaxPerHost:    s.MaxPerHost,
		Chunks:        s.Chunks,
		MaxChunks:     rules.MaxChunks,
		SpeedLimit:    s.SpeedLimit,
	}
}

func registerControls(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/controls", "the few settings the shell's quick controls edit, plus the engine's connection ceiling",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, controlsOf(a.Settings.Get()))
		})

	reg.Add(http.MethodPost, "/api/controls", "change only the quick controls that were sent, leaving the rest of the configuration alone",
		func(w http.ResponseWriter, r *http.Request) {
			// Pointers, so "not sent" and "sent as zero" are different requests.
			// Zero is a real answer for two of these — no speed limit, and no
			// opinion about chunks — and a plain struct would make every save wipe
			// whatever the panel does not show.
			var body struct {
				MaxConcurrent *int   `json:"maxConcurrent"`
				MaxPerHost    *int   `json:"maxPerHost"`
				Chunks        *int   `json:"chunks"`
				SpeedLimit    *int64 `json:"speedLimit"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}

			// Refused rather than clamped, in the same words SetTaskOptions refuses
			// by. connsFor cuts an over-large count at dispatch, so a value quietly
			// accepted here would be stored, shown back to the user, and then not
			// honoured by the thing it was set for.
			if body.Chunks != nil && (*body.Chunks < 0 || *body.Chunks > rules.MaxChunks) {
				http.Error(w, fmt.Sprintf("chunk count %d is outside 0..%d", *body.Chunks, rules.MaxChunks), http.StatusBadRequest)
				return
			}
			if body.SpeedLimit != nil && *body.SpeedLimit < 0 {
				http.Error(w, "a speed limit cannot be negative", http.StatusBadRequest)
				return
			}

			// Merged onto what is stored right now. Two clients patching in the same
			// instant can still lose one field to the other — Get and ApplySettings
			// are two calls — but that window is microseconds wide, where the one
			// this route exists to close is however long the widget has been on
			// screen.
			s := a.Settings.Get()
			if body.MaxConcurrent != nil {
				s.MaxConcurrent = *body.MaxConcurrent
			}
			if body.MaxPerHost != nil {
				s.MaxPerHost = *body.MaxPerHost
			}
			if body.Chunks != nil {
				s.Chunks = *body.Chunks
			}
			if body.SpeedLimit != nil {
				s.SpeedLimit = *body.SpeedLimit
			}
			// No download-folder check here, unlike the settings PUT: the folder
			// came out of the store rather than off the wire, so it has already
			// passed one, and re-running it would let a disk that went away make a
			// speed limit unsettable.
			applied, err := a.ApplySettings(s)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, controlsOf(applied))
		})
}
