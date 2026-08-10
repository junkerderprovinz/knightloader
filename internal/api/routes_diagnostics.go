package api

// The diagnostics bundle: enough about this build, this configuration and
// this process's own recent log output to debug a report without asking
// whoever filed it to go copy their settings file by hand.

import (
	"net/http"
	"runtime"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// Diagnostics is everything GET /api/diagnostics answers. The frontend's live
// preview and its "download bundle" button read the same document, so there
// is nothing the page shows that the saved file does not also carry.
type Diagnostics struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Version     string    `json:"version"`
	// Deployment is "container" or "desktop" (internal/buildinfo) - the same
	// fact 10D's quit/restart route reports, worth repeating here because a
	// bug report attached from a browser gives no other way to tell which
	// binary produced it.
	Deployment string `json:"deployment"`
	GoVersion  string `json:"goVersion"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Goroutines int    `json:"goroutines"`

	// Settings is what GET /api/settings sends, with one further redaction
	// on top: Settings.Redacted() covers the router and proxy passwords (see
	// its own doc comment, internal/settings/settings_network.go) but was
	// never meant to cover ArchivePasswords - that field is ordinary,
	// visible config on the Archives settings page, where a user is editing
	// their own passwords and needs to see them. This bundle is a different
	// exposure: a file meant to be attached to a PUBLIC bug report, where
	// the same values have no business appearing. Cleared here rather than
	// by changing Settings.Redacted() itself, which would incorrectly blank
	// the Archives page too. ArchivePasswordCount below keeps the one
	// diagnostically useful fact (is anything configured at all) without
	// the values themselves.
	Settings             settings.Settings `json:"settings"`
	ArchivePasswordCount int               `json:"archivePasswordCount"`

	// LogLines is the ring's own recent tail, oldest first - see
	// internal/logring for what feeds it and why capturing it costs no
	// changes anywhere else in the tree.
	LogLines []string `json:"logLines"`
	// LogCapacity is how many lines the ring keeps at most, so the page can
	// say "showing the last N" without a second copy of that number.
	LogCapacity int `json:"logCapacity"`
}

func registerDiagnostics(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/diagnostics",
		"version, build info, redacted settings, recent log lines and the goroutine count, for a bug report",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, buildDiagnostics(a))
		})
}

func buildDiagnostics(a *app.App) Diagnostics {
	redacted := a.Settings.Get().Redacted()
	count := len(redacted.ArchivePasswords)
	redacted.ArchivePasswords = nil
	return Diagnostics{
		GeneratedAt:          time.Now().UTC(),
		Version:              buildinfo.Version,
		Deployment:           buildinfo.Deployment,
		GoVersion:            runtime.Version(),
		OS:                   runtime.GOOS,
		Arch:                 runtime.GOARCH,
		Goroutines:           runtime.NumGoroutine(),
		Settings:             redacted,
		ArchivePasswordCount: count,
		LogLines:             logring.Lines(),
		LogCapacity:          logring.Capacity,
	}
}
