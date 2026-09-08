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
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/startupcheck"
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

	// MediaTools is which yt-dlp and ffmpeg this instance actually runs, where
	// each came from and what version it reports. It answers the first question
	// asked of every "this video link stopped working" report, which is "which
	// yt-dlp was that" - and it answers it without a second round trip to
	// somebody who has already closed the tab.
	//
	// A PATH, A VERSION, A TAG AND A DIGEST, AND NOTHING ELSE. The same
	// discipline the fields below keep about archive passwords applies here:
	// this file is attached to PUBLIC bug reports. The release JSON, the
	// SHA2-256SUMS body and anything resembling a credential stay out of it -
	// and internal/mediatools stores no credential at all, precisely so there
	// is nothing here to leak. The one path this does carry is the yt-dlp
	// binary's, which is the fact being reported; it is /usr/bin/yt-dlp or
	// <data>/tools/yt-dlp, not a home directory.
	MediaTools mediatools.Status `json:"mediaTools"`

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

	// StoreBytes is the database file plus any journal beside it, and
	// StoreReclaimableBytes is how much of that is space deleted rows left
	// inside it (a FLOOR - compacting usually gives back more, see
	// store.Sizes). SettingsBytes is settings.json, and SettingsPresent is
	// false on an install that has never saved a settings page, because
	// settings.Load reads that file and never writes it: "0 bytes" for a file
	// that is not there would be a different claim, and the wrong one.
	//
	// NO PATHS, and that is the same argument the Settings comment above
	// makes. A report that says "the database is 6 GB and 4 of them are dead
	// space" is exactly what a bug report about a slow list needs; a desktop
	// data directory is C:\Users\<a person's real name>\AppData\..., which is
	// not. The paths go out on the session-guarded maintenance route
	// (routes_dbmaint.go) and nowhere near this file.
	StoreBytes            int64 `json:"storeBytes"`
	StoreReclaimableBytes int64 `json:"storeReclaimableBytes"`
	SettingsBytes         int64 `json:"settingsBytes"`
	SettingsPresent       bool  `json:"settingsPresent"`

	// LogLines is the ring's own recent tail, oldest first - see
	// internal/logring for what feeds it and why capturing it costs no
	// changes anywhere else in the tree.
	LogLines []string `json:"logLines"`
	// LogCapacity is how many lines the ring keeps at most, so the page can
	// say "showing the last N" without a second copy of that number.
	LogCapacity int `json:"logCapacity"`
	// LogFile is whether any of those lines are also being kept on disk, and
	// how much of them - WITHOUT the path, which Redacted strips. This bundle
	// is a file people attach to PUBLIC bug reports and a desktop data
	// directory is C:\Users\<a person's real name>\AppData\..., which is the
	// same argument the Settings and store-size fields above already make and
	// what TestDiagnosticsShipsNoPaths pins. The unredacted path goes out on
	// the session-guarded /api/diagnostics/logfile route and nowhere else.
	LogFile logring.FileState `json:"logFile"`

	// Startup is the reading the start checks took once, just after this
	// instance came up: the programs it shells out to, every folder a download
	// can land in, and which clock a schedule window is read against
	// (internal/startupcheck).
	//
	// NULL IS A REAL ANSWER AND NOT AN EMPTY ONE. A nil pointer encodes as JSON
	// null and means nothing ever started a pass - a test, or a build that does
	// not run one - which is a different claim from "everything passed". An
	// empty check list shown as a clean bill of health is the worst version of
	// that mistake, so the state travels with it (see startupcheck.Report).
	//
	// It is the BOOT reading and stays the boot reading: POST
	// /api/diagnostics/startup answers with a fresh one and deliberately does
	// not replace this, because what was true at start is what a bug report
	// needs and pressing the button is exactly when somebody would destroy it.
	//
	// NO DATA-DIRECTORY PATH TRAVELS IN IT. app.StartupReport masks it to
	// "<data>" first, because the default download folder lives INSIDE the data
	// directory and TestDiagnosticsShipsNoPaths pins this bundle byte by byte.
	Startup *startupcheck.Report `json:"startup"`

	// The speed record (internal/app/app_speedhistory.go), counted rather than
	// copied: the bundle has no use for 480 readings, and these three fields
	// answer the only question anybody asks about it.
	//
	// That question is "why is my speed curve flat", and it has two completely
	// different answers that look identical on the Overview page. "0 of 120
	// recorded, nothing recording since" means the sampler is not running at
	// all and the curve will stay flat until that is fixed; "120 of 120 since
	// 02:14" means the sampler is fine and the box was simply quiet. Without
	// this row the two are indistinguishable from a screenshot, and the person
	// filing the report has no way to tell which one they are looking at.
	SpeedSamplesRecent int `json:"speedSamplesRecent"`
	SpeedSamplesHour   int `json:"speedSamplesHour"`
	// Absent while nothing has been recorded, which is the "0 of 120" case
	// above and also the first second after every restart.
	SpeedRecordingSince *time.Time `json:"speedRecordingSince,omitempty"`

	// Who this instance writes files as, and who owns each folder it writes
	// into. A report that does not carry the uid makes whoever reads it go and
	// ask for it, and the person who filed it usually has no idea how to find
	// out - while "the process is 1000:1000 and the download folder is 99:100"
	// is the whole diagnosis for the commonest NAS complaint there is.
	//
	// THE CHEAP HALF ONLY: one stat per folder, nothing created, nothing
	// written. The probe that measures what a NEW file comes out as lives
	// behind POST /api/fileowner/check, because this bundle is fetched by the
	// page on mount, and a bundle that littered every configured folder on
	// every page open is the mistake app_diskreport.go's header refuses by name.
	//
	// OwnedFolders carries a ROLE and no path, deliberately: this file is
	// attached to public bug reports, and with nothing configured the desktop
	// build's download folder sits inside the data directory, which is a
	// person's own home folder by name. Same rule as StoreBytes above.
	Ownership    OwnerIdentity     `json:"ownership"`
	OwnedFolders []FolderOwnership `json:"ownedFolders"`
}

func registerDiagnostics(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/diagnostics",
		"version, build info, redacted settings, recent log lines and the goroutine count, for a bug report",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, buildDiagnostics(a))
		})
	// POST AND NEVER GET, and that is not a REST preference. Running the checks
	// again writes a test file into every configured folder that exists; a GET
	// that writes is a route any browser prefetch, any link scanner and any
	// speculative navigation fires on its own, so somebody hovering a bookmark
	// would be dropping files into their download folder.
	//
	// Registered here rather than in routes.go so that adding it costs no edit
	// to the shared table at all.
	//
	// It is deliberately NOT on the relay allowlist (routes_relay.go) and NOT
	// forwardable by /api/instances/{name}/{rest...} (routes_federation.go), for
	// the reason routes_diskspace.go already spells out for disk figures: "java
	// is missing" about a PEER's box, shown while a peer's list is on screen,
	// names the wrong machine with total confidence. Both are explicit
	// allowlists, so this route stays out of them by construction;
	// TestStartupRecheckIsNeitherRelayedNorForwarded pins that.
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
	// Taken from the same snapshot GET /api/stats/speed serves rather than from
	// a second accessor built for this page: two ways of counting the same ring
	// are two things that can disagree, and a diagnostics row that contradicts
	// the route it is meant to explain is worse than no row.
	speed := a.SpeedHistory()
	// The same measurement the maintenance page renders, taken through the same
	// call rather than by stat-ing the files a second time here - two ways of
	// measuring one file are two answers that can disagree, and a bundle that
	// contradicts the page it was pulled from is worse than no row. Only the
	// four sizes are copied across; StorageInfo's paths and temp volume stay on
	// the session-guarded route, for the reason spelled out on the fields above.
	// It never blocks on the database, so a bundle pulled in the middle of a
	// compaction still answers - see app.StorageInfo.
	storage := a.StorageInfo()
	// One stat per configured folder, taken here rather than by the page,
	// because a browser cannot stat anything and the uid is the one fact that
	// turns "my library is empty" into a command somebody can run. See the
	// fields' own comment for why the writing half of the check is deliberately
	// not in this bundle.
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

		// nil until something starts a pass, which in a test is never - see the
		// field's own comment for why that null matters.
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
