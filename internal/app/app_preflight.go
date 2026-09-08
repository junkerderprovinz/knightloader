package app

// The start report: the one place that knows both what internal/startupcheck
// can look at and where THIS instance keeps its things.
//
// WHY IT IS A SEAM AND NOT PART OF THE PACKAGE NEXT DOOR. internal/startupcheck
// deliberately imports neither this package nor internal/settings, so it has no
// idea that a category folder can be a template, that the download folder falls
// back to one inside the data directory, or that KL_JD pointing somewhere else
// means this box starts no Java at all. All of that lives here, and the check
// package stays something a test can drive with two strings.
//
// WHY IT IS NOT IN app.New. app.New is called by several hundred tests. Running
// this there would add three or four process spawns to every construction, and -
// once somebody presses the button - probe files written into every t.TempDir,
// racing the harness's own cleanup: app_health.go already documents that exact
// failure ("a write that lands while a test's t.TempDir() cleanup is mid-
// RemoveAll fails as 'directory not empty'"). The precedent is
// cmd/knightloader/main.go's StartHosterAuth and StartAccountHealthNow, whose
// own comment says it in as many words - an optional bit of startup that belongs
// to the server binary, not to app.New.
//
// WHY IT RUNS AFTER THE LISTENER IS UP. app_diskreport.go says it plainly: a
// folder on a mount that has gone away takes as long to stat as that mount takes
// to time out. Run synchronously before net.Listen and one dead NFS server turns
// a start report into a start hang, and the Dockerfile's HEALTHCHECK - ten
// second start period - then restarts the container into a loop. So: after the
// listener, on a.spawn so Close waits for it, and with a deadline on every row.
//
// WHAT IT NEVER DOES: create a folder, gate anything (not the boot, not the
// queue, not a download), reach the network, or write a probe file at boot. The
// last of those is the owner's decision of 2026-09-08 and is explained in
// internal/startupcheck's package comment.

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

// dataDirMask is what stands in for the data directory anywhere in the report.
//
// THE BUNDLE SHIPS NO PATHS INTO THE DATA DIRECTORY, and that is a rule this
// feature had to be built around rather than one it may bend.
// routes_diagnostics.go's own header spells out why: the bundle is a file people
// attach to PUBLIC bug reports, and a desktop data directory is
// C:\Users\<a person's real name>\AppData\... . api's TestDiagnosticsShipsNoPaths
// pins it byte by byte.
//
// It matters here because the default download folder IS inside the data
// directory (app.go's dlDir), so an install that has never set one would ship
// that person's name in a folder row. Masked rather than dropped: "<data>/
// downloads is not there" still tells the reader everything the row is for,
// while "" would make the one row nobody can act on.
//
// Configured folders travel unmasked and that is not an inconsistency - the
// settings document in the same bundle already carries DownloadDir, WorkDir and
// every category folder verbatim, because those are configuration somebody
// typed. The data directory is the one path the bundle has always refused.
const dataDirMask = "<data>"

// startupState is one App's start report.
//
// Package level keyed by *App rather than a field on App, the same trade
// app_diskreport.go's diskReportState and app_health.go already document:
// app.go's struct is not this file's to grow.
//
// The entry is created by the first WRITE and never by a read, which is what
// keeps the several hundred tests that build an App and fetch diagnostics from
// each leaving one behind: none of them starts a check, StartupReport finds no
// entry, and the bundle says null.
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

// StartStartupCheck takes the boot reading, on its own goroutine, and returns at
// once. Call it from the server binary once the listener is up - never from
// app.New, and never anywhere a test can reach by accident.
func (a *App) StartStartupCheck() {
	if a.ctx != nil && a.ctx.Err() != nil {
		// Close has already committed to shutting down, so a.spawn would drop
		// the goroutine and the report would sit at "running" for ever - a
		// state that reads as "still working on it" and never resolves.
		return
	}
	st := a.startupStateFor()
	// Published as "running" BEFORE the work starts, so a browser that reaches
	// the diagnostics page in the first second of a slow pass is told what is
	// happening rather than shown "never run", which is the one sentence that
	// would be false.
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
// (KL_STARTUP_CHECK=0).
//
// It exists because "switched off" and "never ran" are different sentences and
// an empty check list is neither. A page shown a report with no rows and no
// state would draw a clean bill of health over an instance that looked at
// nothing at all, which is the "no opinion labelled as ok" failure in its worst
// form.
func (a *App) MarkStartupCheckOff() {
	st := a.startupStateFor()
	st.mu.Lock()
	st.report = &startupcheck.Report{State: startupcheck.StateOff, StartedAt: time.Now(), Checks: []startupcheck.Check{}}
	st.mu.Unlock()
}

// StartupReport is the reading taken at boot, or nil when nothing ever started
// one - a test, or a build that does not run it. Nil and "everything passed" are
// different answers and the wire keeps them apart (a nil *Report encodes as JSON
// null).
//
// The result is a copy. The stored report is handed to an HTTP encoder on one
// goroutine while the pass that produced it may still be writing the next one,
// and a shared slice header between those two is a data race that only shows up
// under load.
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
	// make/copy and not append-to-nil, which is the same trap one layer down:
	// appending zero elements to a nil slice answers nil, so a report with an
	// empty check list ("switched off", "still running") came back out of here
	// encoding as JSON null and the page walking it threw. Found by the test
	// that asserts exactly that.
	out.Checks = make([]startupcheck.Check, len(st.report.Checks))
	copy(out.Checks, st.report.Checks)
	return &out
}

// RunStartupCheckNow is the human press: the same checks, now, and this time
// with the write test.
//
// IT DOES NOT REPLACE THE BOOT READING. If it did, the evidence of what was true
// when the instance started would be destroyed the first time anybody pressed
// the button - which is precisely the moment a support thread needs it. The
// answer goes back to whoever asked; the bundle keeps the boot reading.
//
// The parent is a.ctx and NOT the HTTP request's context, deliberately. This
// pass writes a file into every folder that exists and removes it again; a
// browser that navigates away mid-request would otherwise abort the pass between
// those two steps and leave a dotfile behind in somebody's download folder.
func (a *App) RunStartupCheckNow() startupcheck.Report {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rep := startupcheck.Run(ctx, a.startupInput(true))
	maskReport(&rep, a.DataDir)
	return rep
}

// startupInput assembles what this instance actually has to look at.
func (a *App) startupInput(probe bool) startupcheck.Input {
	return startupcheck.Input{
		Data:    startupcheck.FolderTarget{ID: startupcheck.IDData, Dir: filepath.Clean(a.DataDir)},
		Tools:   a.startupTools(),
		Folders: a.startupFolders(),
		Probe:   probe,
	}
}

// startupFolders is every folder a download can end up in, worked out exactly
// the way app_diskreport.go:282-302 works it out and then extended with the
// three destinations that readout has no use for.
//
// TEMPLATES GO THROUGH settings.FixedPrefix FIRST, and this is the difference
// between a report people read and one they learn to ignore. A category folder
// is routinely "/mnt/user/media/<jd:packagename>"; stat-ed as written it asks
// about a directory that never exists, so every install using the packagizer
// would get a "folder missing" row on every single boot, for ever. FixedPrefix
// is the app's own rule for where a template stops being a path, exported for
// exactly this kind of caller.
func (a *App) startupFolders() []startupcheck.FolderTarget {
	cfg := a.Settings.Get()

	var out []startupcheck.FolderTarget
	seen := map[string]bool{}
	add := func(dir, role string) {
		dir = filepath.Clean(settings.FixedPrefix(strings.TrimSpace(dir)))
		if dir == "." || !filepath.IsAbs(dir) {
			// Nothing here can say where a relative folder is: it resolves
			// against whatever the process's working directory happens to be,
			// which is why sanitizePaths refuses to store one in the first
			// place. Dropped rather than reported, because a row about a
			// setting the app has already refused is a row about nothing.
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
		// Two dashes. `java -version` prints to stderr and `java --version`
		// (JDK 9+) to stdout; the run reads both anyway, but the one-dash
		// spelling is also the one that predates a machine-readable answer.
		Args: []string{"--version"},
		// provision.FindJava rather than a bare PATH lookup, because that is
		// the rule the app itself follows: JAVA_HOME/bin/java first, PATH
		// second. A second, simpler lookup here would report "no Java" on a box
		// where the app is about to start a JVM perfectly happily.
		Resolve: provision.FindJava,
	}
	if reason := javaNotNeeded(); reason != "" {
		java.SkipCode = reason
	}

	ytbin := os.Getenv("KL_YTDLP")
	if ytbin == "" {
		ytbin = "yt-dlp"
	}
	// Read off the live routing table rather than from a stored flag, the same
	// signal routes_features.go's resolverRegistered uses: rewireBackends only
	// registers the yt-dlp resolver once the binary has actually run, so a
	// binary that runs NOW while the id is absent means it appeared after this
	// instance started and nothing is being routed to it.
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
		// One dash for both of these: it is what ffmpeg and ffprobe accept, and
		// `--version` makes them exit non-zero with a usage message, which would
		// report a perfectly good ffmpeg as broken.
		{ID: startupcheck.IDFfmpeg, Bin: "ffmpeg", Args: []string{"-version"}},
		// Optional, so a missing one is amber and not red: ffprobe is read for
		// the length check on a finished video and for nothing else, and
		// backend.go already decides that a failed ffprobe says nothing at all.
		{ID: startupcheck.IDFfprobe, Bin: "ffprobe", Args: []string{"-version"}, Optional: true},
	}
}

// javaNotNeeded is why this install starts no Java of its own, or "" when it
// does.
//
// Skipped and never failed. cmd/knightloader/main.go skips provisioning entirely
// when KL_JD is already set or KL_PROVISION_JD is off, and a red "Java missing"
// row on a box that deliberately points at a JD sidecar is a false alarm about a
// choice somebody made on purpose. One false alarm is enough for an operator to
// stop reading the whole report.
//
// KL_PROVISION_JD is read the way main.go reads it - anything that does not
// parse to 1 is off - so the two cannot disagree about what the same variable
// means. The desktop build ignores that variable and always provisions, so an
// install that sets it there gets a skipped row it did not need; that is the
// cheap direction of a wrong guess, and the alternative is this file having an
// opinion about which binary is running it.
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

// maskDataDir replaces the data directory wherever it appears in one string.
//
// Both spellings, because the two halves of one path can reach this from
// different places: Go's own error strings carry the separator the platform
// uses, and anything that has been through filepath.ToSlash carries the other.
// A mask that caught only one of them would be a redaction that works until the
// day it does not, which is worse than none.
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

// logStartupReport writes the whole report as ONE multi-line log.Printf.
//
// One call and not eight. cmd/knightloader/main.go logs "KnightLoader listening
// on…" from inside the serving goroutine and the schedule runner logs on its
// first pass, so eight separate calls would arrive shuffled through both and the
// report would be unreadable in exactly the log somebody pastes into a bug
// report. One call is atomic in the container log and still becomes N separate
// ring entries, because logring splits on '\n' before it stores anything - so
// the diagnostics bundle gets the lines individually either way.
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

// startupLogLine is one row, in the order somebody reads it: what it is, how bad
// it is, which failure, then the evidence.
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
