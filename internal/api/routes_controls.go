package api

// The few settings the shell's quick controls reach. /api/settings is the
// wrong shape for them: its GET returns the whole configuration, and its PUT
// replaces the document, so a widget writing back an old snapshot would undo
// whatever the settings page changed since. POST here merges only the fields
// that were sent.
//
// The queue's master switch is not here; POST /api/queue already halts and
// releases.

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

	// Chunks is how many connections one download opens, as configured; zero
	// leaves it to the dispatcher. It is not a count of open sockets, which no
	// backend reports.
	Chunks int `json:"chunks"`

	// MaxChunks is the engine's ceiling, served so the interface cannot offer
	// more than connsFor honours. The two concurrency limits have no bound here
	// because settings.sanitizeQueue owns it; the POST answers with what was
	// stored.
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
			// Pointers, because zero is a real value for the speed limit and
			// the chunk count and has to differ from "not sent".
			var body struct {
				MaxConcurrent *int   `json:"maxConcurrent"`
				MaxPerHost    *int   `json:"maxPerHost"`
				Chunks        *int   `json:"chunks"`
				SpeedLimit    *int64 `json:"speedLimit"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}

			// Refused rather than clamped: connsFor would cut an over-large
			// count at dispatch, so a stored value would not be honoured.
			if body.Chunks != nil && (*body.Chunks < 0 || *body.Chunks > rules.MaxChunks) {
				http.Error(w, fmt.Sprintf("chunk count %d is outside 0..%d", *body.Chunks, rules.MaxChunks), http.StatusBadRequest)
				return
			}
			if body.SpeedLimit != nil && *body.SpeedLimit < 0 {
				http.Error(w, "a speed limit cannot be negative", http.StatusBadRequest)
				return
			}

			// Two clients patching at the same instant can still lose a field,
			// since Get and ApplySettings are two calls, but that window is far
			// shorter than a widget's lifetime on screen.
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
			// No download-folder check, unlike the settings PUT: the folder came
			// from the store, and a disk that went away must not make a speed
			// limit unsettable.
			applied, err := a.ApplySettings(s)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, controlsOf(applied))
		})
}
