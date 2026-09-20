package app

// The start report: what internal/startupcheck should look at on this instance.
// startupcheck imports neither this package nor internal/settings, so the
// knowledge of category templates, the default download folder and whether this
// box starts Java at all lives here.
//
// It is not run from app.New, which hundreds of tests call: it would add process
// spawns to every construction and race t.TempDir cleanup with probe files. The
// server binary starts it once the listener is up, since a stat on a dead mount
// can take as long as the mount's timeout and would otherwise trip the
// container's HEALTHCHECK. It runs on a.spawn so Close waits for it.
//
// It never creates a folder, gates anything, reaches the network, or writes a
// probe file at boot (see internal/startupcheck's package comment).

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/provision"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/startupcheck"
)

// dataDirMask stands in for the data directory anywhere in the report.
//
// The diagnostics bundle is attached to public bug reports and never carries
// paths into the data directory, which on a desktop contains the user's name
// (api's TestDiagnosticsShipsNoPaths). The default download folder lives inside
// it, so it is masked rather than dropped to keep the row useful. Configured
// folders stay unmasked; the settings in the same bundle carry them anyway.
const dataDirMask = "<data>"

// startupState is one App's start report. It lives in a package-level map keyed
// by *App, like diskReportState, and an entry is created only by a write so
// tests that never start a check leave nothing behind.
type startupState struct {
	mu     sync.Mutex
	report *startupcheck.Report
}

var (
	startupMu  sync.Mutex
	startupReg = map[*App]*startupState{}
)

func (a *App) startupStateFor() *startupState {
	startupMu.Lock()
	defer startupMu.Unlock()
	st, ok := startupReg[a]
	if !ok {
		st = &startupState{}
		startupReg[a] = st
	}
	return st
}

// StartStartupCheck takes the boot reading on its own goroutine and returns at
// once. The server binary calls it once the listener is up.
func (a *App) StartStartupCheck() {
	if a.ctx != nil && a.ctx.Err() != nil {
		// a.spawn would drop the goroutine and the report would stay "running".
		return
	}
	st := a.startupStateFor()
	// Published as running before the work starts, so an early visitor is not
	// told the check never ran.
	st.mu.Lock()
	st.report = &startupcheck.Report{State: startupcheck.StateRunning, StartedAt: time.Now(), Checks: []startupcheck.Check{}}
	st.mu.Unlock()

	a.spawn(func() {
		rep := startupcheck.Run(a.ctx, a.startupInput(false))
		maskReport(&rep, a.DataDir)
		st.mu.Lock()
		st.report = &rep
		st.mu.Unlock()
		logStartupReport(rep)
	})
}

// MarkStartupCheckOff records that this instance was told not to look
// (KL_STARTUP_CHECK=0). An empty report would otherwise read as a clean bill of
// health.
func (a *App) MarkStartupCheckOff() {
	st := a.startupStateFor()
	st.mu.Lock()
	st.report = &startupcheck.Report{State: startupcheck.StateOff, StartedAt: time.Now(), Checks: []startupcheck.Check{}}
	st.mu.Unlock()
}

// StartupReport is the reading taken at boot, or nil when nothing started one.
// The result is a copy, since the encoder may read it while a pass writes the
// next one.
func (a *App) StartupReport() *startupcheck.Report {
	startupMu.Lock()
	st := startupReg[a]
	startupMu.Unlock()
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.report == nil {
		return nil
	}
	out := *st.report
	// make and copy rather than append to nil, which would turn an empty check
	// list into JSON null.
	out.Checks = make([]startupcheck.Check, len(st.report.Checks))
	copy(out.Checks, st.report.Checks)
	return &out
}

// RunStartupCheckNow runs the same checks on request, this time with the write
// test. The answer goes to the caller and the boot reading is kept, since that
// is what a support thread needs.
//
// It uses a.ctx rather than the request's context: the pass writes a probe file
// and removes it, and an aborted request must not leave the file behind.
func (a *App) RunStartupCheckNow() startupcheck.Report {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rep := startupcheck.Run(ctx, a.startupInput(true))
	maskReport(&rep, a.DataDir)
	return rep
}

func (a *App) startupInput(probe bool) startupcheck.Input {
	return startupcheck.Input{
		Data:    startupcheck.FolderTarget{ID: startupcheck.IDData, Dir: filepath.Clean(a.DataDir)},
		Tools:   a.startupTools(),
		Folders: a.startupFolders(),
		Probe:   probe,
	}
}

// startupFolders is every folder a download can end up in, as the disk report
// works it out, plus the destinations that readout does not need.
//
// Templates go through settings.FixedPrefix first. A category folder such as
// "/mnt/user/media/<jd:packagename>" never exists as written and would report a
// missing folder on every boot.
func (a *App) startupFolders() []startupcheck.FolderTarget {
	cfg := a.Settings.Get()

	var out []startupcheck.FolderTarget
	seen := map[string]bool{}
	add := func(dir, role string) {
		dir = filepath.Clean(settings.FixedPrefix(strings.TrimSpace(dir)))
		if dir == "." || !filepath.IsAbs(dir) {
			// sanitizePaths already refuses relative folders, so there is
			// nothing meaningful to report.
			return
		}
		if seen[dir] {
			return // one folder, one row: the first role that named it wins
		}
		seen[dir] = true
		out = append(out, startupcheck.FolderTarget{Role: role, Dir: dir})
	}

	add(a.defaultDir(), startupcheck.RoleDownloads)
	add(cfg.WorkDir, startupcheck.RoleWork)
	for _, c := range cfg.Categories {
		add(c.Dir, startupcheck.RoleCategory)
	}
	add(cfg.ExtractTo, startupcheck.RoleExtract)
	add(cfg.ExtractMoveTo, startupcheck.RoleExtractMove)
	add(cfg.WatchDir, startupcheck.RoleWatch)
	return out
}

// startupTools is the four binaries this app shells out to, each with the reason
// it might not be needed here.
func (a *App) startupTools() []startupcheck.ToolTarget {
	java := startupcheck.ToolTarget{
		ID:  startupcheck.IDJava,
		Bin: "java",
		// `java --version` (JDK 9+) prints to stdout; the run reads both streams.
		Args: []string{"--version"},
		// The lookup the app itself uses: JAVA_HOME/bin/java first, then PATH.
		Resolve: provision.FindJava,
	}
	if reason := javaNotNeeded(); reason != "" {
		java.SkipCode = reason
	}

	ytbin := os.Getenv("KL_YTDLP")
	if ytbin == "" {
		ytbin = "yt-dlp"
	}
	// rewireBackends registers the yt-dlp resolver only once the binary has run,
	// so a binary that runs now while the id is absent appeared after startup
	// and nothing is routed to it.
	registered := false
	for _, id := range a.Registry.IDs() {
		if id == "ytdlp" {
			registered = true
			break
		}
	}

	return []startupcheck.ToolTarget{
		java,
		{ID: startupcheck.IDYtdlp, Bin: ytbin, Args: []string{"--version"}, Registered: &registered},
		// ffmpeg and ffprobe take one dash; `--version` exits non-zero.
		{ID: startupcheck.IDFfmpeg, Bin: "ffmpeg", Args: []string{"-version"}},
		// ffprobe only feeds the length check on a finished video, and a failed
		// probe is already treated as no answer.
		{ID: startupcheck.IDFfprobe, Bin: "ffprobe", Args: []string{"-version"}, Optional: true},
	}
}

// javaNotNeeded is why this install starts no Java of its own, or "" when it
// does. main.go skips provisioning when KL_JD is set or KL_PROVISION_JD is off,
// and a red row on a box that points at a JD sidecar would be a false alarm.
//
// KL_PROVISION_JD is parsed as main.go parses it. The desktop build ignores the
// variable, so there it can yield a skipped row that was not needed, which is
// the harmless direction to be wrong in.
func javaNotNeeded() string {
	if strings.TrimSpace(os.Getenv("KL_JD")) != "" {
		return "javaNotNeeded"
	}
	if v := os.Getenv("KL_PROVISION_JD"); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n != 1 {
			return "javaNotNeeded"
		}
	}
	return ""
}

// maskReport takes the data directory out of every field of every row. See
// dataDirMask.
func maskReport(rep *startupcheck.Report, dataDir string) {
	dataDir = filepath.Clean(dataDir)
	if dataDir == "" || dataDir == "." {
		return
	}
	for i := range rep.Checks {
		c := &rep.Checks[i]
		c.Subject = maskDataDir(c.Subject, dataDir)
		c.Measured = maskDataDir(c.Measured, dataDir)
		c.Detail = maskDataDir(c.Detail, dataDir)
		c.Err = maskDataDir(c.Err, dataDir)
	}
}

// maskDataDir replaces the data directory wherever it appears in s, in both the
// platform's separator and the slashed form, since paths reach here both ways.
func maskDataDir(s, dataDir string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, dataDir, dataDirMask)
	if slashed := filepath.ToSlash(dataDir); slashed != dataDir {
		s = strings.ReplaceAll(s, slashed, dataDirMask)
	}
	return s
}

// logStartupReport writes the whole report in one log call so other startup
// lines cannot interleave with it. logring still stores each line separately.
func logStartupReport(rep startupcheck.Report) {
	var counts = map[startupcheck.Verdict]int{}
	for _, c := range rep.Checks {
		counts[c.Verdict]++
	}
	var b strings.Builder
	written := "looked only, nothing was written"
	if rep.Probed {
		written = "with the write test"
	}
	fmt.Fprintf(&b, "start report (%s): %d ok, %d worth a look, %d not usable, %d not needed here",
		written, counts[startupcheck.VerdictOK], counts[startupcheck.VerdictWarn],
		counts[startupcheck.VerdictFail], counts[startupcheck.VerdictSkipped])
	for _, c := range rep.Checks {
		b.WriteString("\n  " + startupLogLine(c))
	}
	log.Print(b.String())
}

// startupLogLine is one row, in reading order: what it is, how bad it is, which
// failure, then the evidence.
func startupLogLine(c startupcheck.Check) string {
	name := c.ID
	if c.Role != "" {
		name += "/" + c.Role
	}
	line := fmt.Sprintf("%-18s %-8s", name, c.Verdict)
	if c.Code != "" {
		line += " [" + c.Code + "]"
	}
	if c.Subject != "" {
		line += " " + c.Subject
	}
	if c.Measured != "" && c.Measured != c.Subject {
		line += " -> nearest existing: " + c.Measured
	}
	if c.Detail != "" {
		line += " (" + c.Detail + ")"
	}
	if c.Err != "" {
		line += " err: " + c.Err
	}
	return line
}
