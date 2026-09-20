package api

// The diagnostics bundle: enough about this build, this configuration and this
// process's recent log output to debug a report. The bundle is meant to be
// attached to public bug reports, so it carries no credentials and no paths.

import (
	"net/http"
	"runtime"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/startupcheck"
)

// Diagnostics is everything GET /api/diagnostics answers. The page's preview
// and its download button read the same document.
type Diagnostics struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Version     string    `json:"version"`
	// Deployment is "container" or "desktop"; a report attached from a browser
	// has no other way to tell which binary produced it.
	Deployment string `json:"deployment"`
	GoVersion  string `json:"goVersion"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Goroutines int    `json:"goroutines"`

	// MediaTools is which yt-dlp and ffmpeg this instance runs, where each came
	// from and its version. It holds a path, a version, a tag and a digest,
	// and mediatools stores no credential.
	MediaTools mediatools.Status `json:"mediaTools"`

	// Settings is what GET /api/settings sends, with the archive passwords also
	// cleared. Settings.Redacted keeps them because the Archives page shows
	// them to their owner; ArchivePasswordCount keeps the useful fact.
	Settings             settings.Settings `json:"settings"`
	ArchivePasswordCount int               `json:"archivePasswordCount"`

	// StoreBytes is the database file plus any journal, and
	// StoreReclaimableBytes a lower bound on the space deleted rows left in it
	// (see store.Sizes). SettingsPresent is false on an install that has never
	// saved settings, since settings.Load never writes the file. The paths stay
	// on the session-guarded maintenance route, because a desktop data
	// directory contains the user's name.
	StoreBytes            int64 `json:"storeBytes"`
	StoreReclaimableBytes int64 `json:"storeReclaimableBytes"`
	SettingsBytes         int64 `json:"settingsBytes"`
	SettingsPresent       bool  `json:"settingsPresent"`

	// LogLines is the ring's recent tail, oldest first (see internal/logring).
	LogLines []string `json:"logLines"`
	// LogCapacity is how many lines the ring keeps at most.
	LogCapacity int `json:"logCapacity"`
	// LogFile is whether and how much of the log is kept on disk, without the
	// path; /api/diagnostics/logfile has that.
	LogFile logring.FileState `json:"logFile"`

	// Startup is the reading the start checks took once at boot. Nil means no
	// pass ever ran, which differs from "everything passed". POST
	// /api/diagnostics/startup does not replace it, because the boot reading is
	// what a report needs. app.StartupReport masks the data directory to
	// "<data>".
	Startup *startupcheck.Report `json:"startup"`

	// The speed record, counted rather than copied. The counts tell a sampler
	// that is not running apart from a box that was simply quiet, which look
	// the same on the Overview page.
	SpeedSamplesRecent int `json:"speedSamplesRecent"`
	SpeedSamplesHour   int `json:"speedSamplesHour"`
	// SpeedRecordingSince is absent while nothing has been recorded.
	SpeedRecordingSince *time.Time `json:"speedRecordingSince,omitempty"`

	// Ownership is who this instance writes files as, and OwnedFolders who owns
	// each folder it writes into, by role and without a path. Only one stat per
	// folder: the probe that writes a test file lives behind POST
	// /api/fileowner/check, since the page fetches this bundle on every mount.
	Ownership    OwnerIdentity     `json:"ownership"`
	OwnedFolders []FolderOwnership `json:"ownedFolders"`
}

func registerDiagnostics(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/diagnostics",
		"version, build info, redacted settings, recent log lines and the goroutine count, for a bug report",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, buildDiagnostics(a))
		})
	// A POST because the checks write a test file into every configured
	// folder, and a GET would fire on any prefetch or link scanner. It is on
	// neither the relay allowlist nor the federation forwarder, because a
	// peer's answer describes the wrong machine
	// (TestStartupRecheckIsNeitherRelayedNorForwarded).
	reg.Add(http.MethodPost, "/api/diagnostics/startup",
		"run the start checks again now and answer with a fresh report; the bundle keeps the reading taken at start",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.RunStartupCheckNow())
		})
}

func buildDiagnostics(a *app.App) Diagnostics {
	redacted := a.Settings.Get().Redacted()
	count := len(redacted.ArchivePasswords)
	redacted.ArchivePasswords = nil
	// The same snapshots GET /api/stats/speed and the maintenance page use, so
	// the bundle cannot contradict them. StorageInfo never blocks on the
	// database, so a bundle pulled during a compaction still answers.
	speed := a.SpeedHistory()
	storage := a.StorageInfo()
	ownership, ownedFolders := ownershipDiagnostics(a)
	return Diagnostics{
		GeneratedAt:          time.Now().UTC(),
		Version:              buildinfo.Version,
		Deployment:           buildinfo.Deployment,
		GoVersion:            runtime.Version(),
		OS:                   runtime.GOOS,
		Arch:                 runtime.GOARCH,
		Goroutines:           runtime.NumGoroutine(),
		MediaTools:           a.MediaTools(),
		Settings:             redacted,
		ArchivePasswordCount: count,
		LogLines:             logring.Lines(),
		LogCapacity:          logring.Capacity,
		LogFile:              logring.FileStatus().Redacted(),

		Startup: a.StartupReport(),

		StoreBytes:            storage.StoreBytes,
		StoreReclaimableBytes: storage.StoreReclaimableBytes,
		SettingsBytes:         storage.SettingsBytes,
		SettingsPresent:       storage.SettingsPresent,

		SpeedSamplesRecent:  len(speed.Recent),
		SpeedSamplesHour:    len(speed.Hour),
		SpeedRecordingSince: speed.RecordingSince,

		Ownership:    ownership,
		OwnedFolders: ownedFolders,
	}
}
