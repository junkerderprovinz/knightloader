// Package startupcheck answers, once and out loud, the handful of questions
// whose wrong answer otherwise only surfaces days later and in the wrong place:
// is there a Java to open an encrypted container link with, does yt-dlp run, is
// ffmpeg there to join the separate picture and sound streams most media sites
// hand out, is every folder a download can land in actually there, and is this
// process reading the clock a schedule window is written against or silently
// reading UTC.
//
// IT IS A READING AND NOT A GATE. Nothing here holds a download back, refuses a
// boot, or changes a setting. Every one of these facts is already discoverable
// by somebody who knows where to look; the point is that nobody does, so the
// answer arrives as "the container is encrypted and none is configured" three
// weeks later, or as a nightly window that quietly ran an hour off all summer.
//
// IT KNOWS NOTHING ABOUT THE APP. It imports neither internal/app nor
// internal/settings, so the caller stays the one authority on where downloads
// go: a folder arrives here already cut back to its fixed prefix
// (settings.FixedPrefix) and already absolute, and this package refuses
// anything else rather than guessing what it meant.
//
// AND IT CREATES NOTHING. Not a folder, not a settings file. settings.Validate
// is the obvious-looking way to find out whether a folder is usable and it
// MkdirAll's the path first; called at boot on a box whose /mnt/user/media did
// not mount, it creates that path inside the container's own writable layer,
// the write test then passes, the report says "fine", and every download lands
// inside the container and is destroyed by the next image pull. That is the
// exact failure this package exists to make visible, so it would have laundered
// its own reason for existing. app_diskreport.go's header refuses Validate for
// the weaker version of the same trap; this is the strong one.
//
// WHEN IT WRITES ITS PROBE FILE, which is the one thing the owner ruled on
// (2026-09-08). A pass started by the boot only LOOKS: it stats, and it writes
// nothing anywhere. On Unraid a write into /mnt/user/... spins up whichever
// array disk holds that share, and probing every configured folder at every
// start would wake a sleeping array on every container restart, for ever, as a
// behaviour change on upgrade. So the write test happens only when somebody
// presses the button for it (Input.Probe), and even then only into a folder
// that is already there. Whatever draws this has to say which of the two it is
// looking at, because "the folder exists" and "this instance can write in it"
// are different claims and only one of them was checked.
package startupcheck

import (
	"context"
	"strings"
	"time"
)

// Verdict is how bad a row is, in four words the interface translates. It
// travels as a stable id and never as prose: the server has no idea which of
// the forty-two locales is reading, the same reason routes_features.go sends a
// Feature.ID.
type Verdict string

const (
	// VerdictOK is "this was looked at and there is nothing to do".
	VerdictOK Verdict = "ok"
	// VerdictWarn is "worth a look", and it is deliberately where the two
	// normal-but-interesting states live: a download folder that does not
	// exist yet (which is every fresh install, because the folder is created
	// by whoever writes the first file into it) and a folder that did not
	// answer in time (which is every sleeping array disk). Painting either of
	// those red would teach an operator to ignore the report inside a week,
	// and one ignored report is worth less than none.
	VerdictWarn Verdict = "warn"
	// VerdictFail is "this will not work as configured".
	VerdictFail Verdict = "fail"
	// VerdictSkipped is "not needed on this install" and never "switched off":
	// a box pointing KL_JD at a JDownloader somewhere else starts no Java of
	// its own, and a red "Java missing" row there is a false alarm about a
	// deliberate configuration.
	VerdictSkipped Verdict = "skipped"
)

// The kinds of thing a row can be about. Stable ids again, and open: an id this
// build has never heard of still has to draw as a row rather than vanish.
const (
	IDData    = "data"
	IDJava    = "java"
	IDYtdlp   = "ytdlp"
	IDFfmpeg  = "ffmpeg"
	IDFfprobe = "ffprobe"
	IDFolder  = "folder"
	IDClock   = "clock"
)

// Why a folder row is in the list. The app owns these strings; they are here
// only so the two halves cannot drift.
const (
	RoleDownloads   = "downloads"
	RoleWork        = "work"
	RoleCategory    = "category"
	RoleExtract     = "extract"
	RoleExtractMove = "extractMove"
	RoleWatch       = "watch"
)

// Codes say WHICH failure, so the interface can offer the one remedy that
// helps. "cannot write here" is four different problems with four different
// answers, and a single code for all of them sends somebody to check
// permissions on a filesystem that is mounted read-only.
const (
	// Folder codes.
	CodeMissing    = "missing"    // not there; Measured names the deepest folder above it that is
	CodeDenied     = "denied"     // there, and this process may not write in it
	CodeReadOnly   = "readonly"   // the filesystem under it is mounted read-only
	CodeFull       = "full"       // no room left on the volume
	CodeNotRemoved = "notRemoved" // the probe was written and could not be deleted again
	CodeNotADir    = "notADir"    // a file sits at this path
	CodeRelative   = "relative"   // not an absolute path, so nobody can say where it is
	CodeTimeout    = "timeout"    // did not answer inside the deadline and was left alone
	CodeError      = "error"      // something else; Err carries the system's own words

	// Tool codes. notFound and notRunnable are deliberately two answers and
	// not one: a yt-dlp whose Python environment broke prints a traceback and
	// exits non-zero, and telling that person to install a thing they already
	// have is how a report loses its reader.
	CodeNotFound     = "notFound"
	CodeNotRunnable  = "notRunnable"
	CodeAppearedLate = "appearedLate" // it runs now and was not there at boot, so nothing routes to it yet

	// Clock codes. See clock.go for why each one is only ever raised on a
	// fact that can be proven from inside the process.
	CodeUTCFallback    = "utcFallback"
	CodeNoZoneDatabase = "noZoneDatabase"
	CodeTZUnset        = "tzUnset"
)

// Report states.
const (
	// StateRunning is a pass that has started and not finished. Checks is
	// already a slice, never nil, so whatever draws it can walk it.
	StateRunning = "running"
	StateDone    = "done"
	// StateOff is KL_STARTUP_CHECK=0: nothing was looked at, which is a
	// different claim from "nothing was wrong" and has to read as one.
	StateOff = "off"
)

// Check is one thing the pass looked at.
type Check struct {
	// ID is what kind of thing this is - see the ID constants. Open on
	// purpose: a row this build has never heard of still draws.
	ID string `json:"id"`
	// Role is why a folder row is in the list at all. Empty on every other row.
	Role    string  `json:"role,omitempty"`
	Verdict Verdict `json:"verdict"`
	// Subject is the concrete thing that was checked: the absolute folder, the
	// resolved binary path, the raw TZ value.
	Subject string `json:"subject,omitempty"`
	// Detail is the fact worth reading - a version line, the zone abbreviation
	// and the offset. Already clamped, because `ffmpeg -version` prints several
	// hundred bytes of configure line and this document goes into the container
	// log, into all 500 ring lines and into every bundle somebody downloads.
	Detail string `json:"detail,omitempty"`
	// Measured is, for a folder row, the deepest folder above Subject that does
	// exist. When it differs from Subject the folder is not there, and on a box
	// that mounts a share that usually means the share is not mounted - the
	// same distinction DiskReport.Measured draws, and for the same reason: the
	// substitution has to be visible instead of trusted.
	Measured string `json:"measured,omitempty"`
	// Probed says a probe file was actually written into this folder and
	// removed again. FALSE IS THE NORMAL CASE at boot and does not mean the
	// write failed - it means nothing was written, so "this instance can write
	// here" was never established. Reporting a bare "ok" without this would be
	// the report claiming a test it did not run.
	Probed bool `json:"probed,omitempty"`
	// Code is WHICH failure. Empty on an ok row.
	Code string `json:"code,omitempty"`
	// Err is the system's own message, verbatim and clamped. Shown raw and
	// never translated: it is evidence, and a translated errno is neither
	// searchable nor quotable in a bug report.
	Err string `json:"err,omitempty"`
}

// Report is one whole pass.
type Report struct {
	State      string    `json:"state"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	// Probed says this pass was allowed to write its probe file at all. False
	// for every boot pass (see the package comment), true only for one somebody
	// pressed the button for. It is a property of the PASS and not of a row,
	// because "nothing was written anywhere" is one sentence at the top of a
	// card rather than a qualifier repeated on every line.
	Probed bool `json:"probed"`
	// Checks is never nil, even while the pass is still running: a nil slice
	// encodes as JSON null and the page that walks it throws instead of drawing
	// nothing. app_diskreport.go initialises its own slice for exactly this.
	Checks []Check `json:"checks"`
}

// Default deadlines.
//
// PER CHECK AND IN TOTAL, and both halves are load-bearing. A folder on a mount
// that has gone away takes as long to stat as that mount takes to time out, and
// there can be a dozen of those configured; without a total the pass would sit
// there for minutes holding a goroutine per folder. Without a per-check one the
// first dead mount would eat the whole budget and every folder after it would
// come back as a timeout row that says nothing about itself.
const (
	// DefaultToolTimeout is per binary. A version probe that has not answered
	// in five seconds is not going to.
	DefaultToolTimeout = 5 * time.Second
	// DefaultFolderTimeout is per folder, and it is deliberately generous: on
	// Unraid a stat against a share whose array disk is spun down waits for
	// that disk to spin up, which is routinely seven or eight seconds.
	DefaultFolderTimeout = 15 * time.Second
	// DefaultTotal is the whole pass.
	DefaultTotal = 90 * time.Second
)

// maxDetail is how many runes of anything a tool or the system said may travel.
//
// A twin of clampRunes in internal/resolver/ytdlp/backend.go, deliberately not
// imported from there: that one is unexported, and exporting it would put a
// media backend's helper in a diagnostics package's import list for four lines
// of code. Both exist because the same failure keeps happening - a subprocess
// that answers with a banner instead of a version, landing whole in a log ring
// with 500 slots and in every downloaded bundle.
const maxDetail = 200

// FolderTarget is one folder to look at.
type FolderTarget struct {
	// ID is what kind of row this becomes. Empty means IDFolder; the data
	// directory passes IDData, because it is the one folder whose failure has
	// a completely different consequence and therefore a different remedy.
	ID string
	// Role is why it is in the list - see the Role constants.
	Role string
	// Dir is the folder, ABSOLUTE and already cut back to the fixed prefix of
	// whatever template it came from. Doing that here would mean this package
	// knowing settings' template rules, which is exactly the dependency the
	// package comment refuses; doing it in the caller means there are still
	// only the three copies of that rule the tree already documents.
	Dir string
}

// ToolTarget is one binary to look for and run.
type ToolTarget struct {
	ID string
	// Bin is what to look for on PATH. Ignored when Resolve is set.
	Bin string
	// Args is how this binary is asked for its version, and it differs per
	// binary in ways that matter: java wants `--version` (two dashes, JDK 9+)
	// because the one-dash spelling prints to stderr, while ffmpeg and ffprobe
	// want `-version` with one. Getting it wrong reports "found, version
	// unknown" for every healthy install.
	Args []string
	// Resolve overrides the PATH lookup for a tool the app finds its own way.
	// Java is the one: provision.FindJava prefers JAVA_HOME over PATH, and a
	// second lookup here that did not would report "not found" on a box where
	// the app is about to start a JVM perfectly happily.
	Resolve func() (string, error)
	// SkipCode, when set, means this install does not need this tool, and this
	// is the reason. VerdictSkipped, never VerdictFail: one false alarm is
	// enough for somebody to stop reading the whole report.
	SkipCode string
	// Optional downgrades a missing or broken binary from fail to warn, for a
	// tool nothing important depends on. ffprobe is the only one: it is read
	// for the length check on a finished video and nothing else.
	Optional bool
	// Registered is whether this tool's resolver is in the live routing table
	// right now. nil for a tool that has no such concept. A binary that runs
	// while its resolver is NOT registered is the "installed after this
	// instance started" case, which looks perfect from the command line and
	// routes nothing until a restart.
	Registered *bool
}

// Input is one pass.
type Input struct {
	// Data is the instance's own data directory - the database, the settings
	// and the encrypted account store. It is checked FIRST and kept out of the
	// folder list on purpose: it is local, it answers instantly, and it is the
	// one folder whose failure means the next thing anybody saves is lost. An
	// empty Dir leaves the row out entirely.
	Data FolderTarget
	// Tools are checked second: each is bounded by its own deadline, so a
	// hanging wrapper script cannot eat the folder budget.
	Tools []ToolTarget
	// Folders are checked LAST, because they are the only rows that can wait on
	// a machine that is not answering. Anything the total deadline cuts off
	// still gets a row, marked CodeTimeout: an absent row is invisible, and a
	// timeout row is a finding.
	Folders []FolderTarget
	// Probe allows the write test. See the package comment: false for every
	// boot pass, true only for one a person asked for.
	Probe bool
	// SkipClock leaves the clock row out. Only a test wants this.
	SkipClock bool

	// Zero means the Default above.
	ToolTimeout   time.Duration
	FolderTimeout time.Duration
	Total         time.Duration
}

// Run takes one whole reading. It never returns a partial report: every target
// handed in comes back as a row, whatever happened to it.
func Run(ctx context.Context, in Input) Report {
	total := in.Total
	if total <= 0 {
		total = DefaultTotal
	}
	toolTimeout := in.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = DefaultToolTimeout
	}
	folderTimeout := in.FolderTimeout
	if folderTimeout <= 0 {
		folderTimeout = DefaultFolderTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, total)
	defer cancel()

	rep := Report{
		State:     StateDone,
		StartedAt: time.Now(),
		Probed:    in.Probe,
		Checks:    []Check{},
	}

	if strings.TrimSpace(in.Data.Dir) != "" {
		t := in.Data
		if t.ID == "" {
			t.ID = IDData
		}
		rep.Checks = append(rep.Checks, folderRow(ctx, t, in.Probe, folderTimeout))
	}
	for _, t := range in.Tools {
		rep.Checks = append(rep.Checks, Tool(ctx, t, toolTimeout))
	}
	if !in.SkipClock {
		rep.Checks = append(rep.Checks, Clock())
	}
	for _, t := range in.Folders {
		rep.Checks = append(rep.Checks, folderRow(ctx, t, in.Probe, folderTimeout))
	}

	rep.FinishedAt = time.Now()
	return rep
}

// folderRow is Folder with the target's own ID put back on the row: Folder
// answers about a folder and has no opinion about what kind of row it becomes.
func folderRow(ctx context.Context, t FolderTarget, probe bool, timeout time.Duration) Check {
	c := Folder(ctx, t.Dir, t.Role, probe, timeout)
	if t.ID != "" {
		c.ID = t.ID
	}
	return c
}

// clamp cuts s to maxDetail runes, by RUNES and not by bytes: a byte cut lands
// in the middle of a multi-byte character often enough to matter, and the
// result is a replacement glyph in a bug report where a version number should
// be. The ellipsis says the cut happened rather than leaving a sentence that
// merely looks like it ended.
func clamp(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxDetail {
		return s
	}
	return string(r[:maxDetail]) + "…"
}

// firstLine is the first line of output with anything in it.
//
// Blank-skipping rather than "line zero", because several of these binaries put
// a warning, or nothing at all, on their first line and the version on the
// second. Taking line zero blind reports an empty version for a tool that
// answered perfectly well.
func firstLine(s string) string {
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
