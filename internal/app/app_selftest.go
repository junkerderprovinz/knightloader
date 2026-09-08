package app

// One button that asks this instance every question it can answer about
// itself: the JDownloader sidecar, yt-dlp and how old it is, the target
// folders, the debrid logins, the relay, the clock and the torrent port.
//
// EVERY ONE OF THESE WAS ALREADY REACHABLE AND NONE OF THEM WERE IN ONE PLACE.
// JDStatus answers three routes down (routes_resolvers.go), DiskReport another
// (routes_diskspace.go), the relay's live socket a third (routes_relay.go),
// and the account credentials a fourth - so an operator whose downloads have
// stopped had four pages to visit and no reason to think any of them was the
// one. What this file adds is not a new measurement, it is the sweep: one
// press, one list, and a sentence per finding that names what to do next.
//
// WHAT IT DELIBERATELY DOES NOT DO, said here so nobody adds it later thinking
// it was an oversight:
//
//   - It never presses the UPnP button. internal/api/routes_portmap.go is a
//     POST because AddPortMapping writes a rule into somebody's router, and a
//     diagnostic that silently reconfigures network equipment is not a
//     diagnostic.
//   - It never runs a reconnect. That drops the WAN link, which would take
//     every other check in the same sweep down with it.
//   - It never MkdirAll's a folder. settings.Validate does, which is exactly
//     why it is not used here - see app_diskreport.go's own note on the same
//     temptation: a readout that creates directories turns opening a page into
//     a change on disk.
//   - It never contacts a machine the operator did not configure. The one set
//     of outbound calls it makes are the debrid logins, and those go to the
//     providers whose keys the operator entered themselves. In particular
//     there is no external "is my torrent port open" probe: that question can
//     only be answered by a third party, this repo has twice ruled it will not
//     reach one on its own initiative (internal/proxycfg/probe.go,
//     internal/reconnect/config.go), and the check says so in plain words
//     rather than pretending the question does not exist.
//   - It never reports anything to the account-health tracker. See
//     selfTestAccountsRO, which is where the sharpest edge in this whole
//     feature lives.
//
// SINGLE-FLIGHT, AND THAT IS NOT TIDINESS. Seven checks, one of which logs
// into up to seven providers. Two browser tabs and an impatient operator is
// dozens of provider calls a minute, and core.ReasonLimit is a real answer
// these APIs give - one that would then be indistinguishable from a key that
// has actually stopped working. A second start while a sweep is in flight
// joins that sweep and gets its id back.
//
// THE STATE IS PACKAGE LEVEL AND KEYED BY *App, the same arrangement
// diskReportState (app_diskreport.go), maintenance state (app_dbmaint.go),
// activityReg and hosterAuth already document: app.go's struct is not this
// file's to grow, and production runs exactly one App for the life of the
// process.

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/relay"
	"github.com/junkerderprovinz/knightloader/internal/seedphrase"
	"github.com/junkerderprovinz/knightloader/internal/selftest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The ceilings. Each is per CHECK rather than for the sweep, because the sweep
// runs its checks in parallel and one slow provider must not decide how long
// the folder readout takes to appear.
const (
	// ytdlpVersionTimeout bounds `yt-dlp --version`. Ten seconds is generous
	// for a process that prints one line and exits - the number is here for
	// the pathological case, a binary on a network mount that has gone away,
	// where the alternative is a sweep that never finishes.
	ytdlpVersionTimeout = 10 * time.Second
	// accountCheckTimeout bounds one provider login. The same fifteen seconds
	// VerifyCredential and TestAccount already use, deliberately: three
	// different callers asking a provider the same question should not
	// disagree about how long it is allowed to take.
	accountCheckTimeout = 15 * time.Second
	// accountCheckConcurrency is how many providers are asked at once.
	//
	// Four, and both directions of that were considered. Serial would put six
	// configured accounts on a slow line at ninety seconds, which is long
	// enough that an operator concludes the button is broken and presses it
	// again. Unbounded would fire every login at once from one address, which
	// is what a rate limiter is for and would produce exactly the answer this
	// check must never produce by its own doing: a temporary refusal that
	// reads like a dead key.
	accountCheckConcurrency = 4
)

// selfTestState is one App's current or last sweep.
type selfTestState struct {
	mu sync.Mutex
	// run is the sweep. Its zero value - an empty ID - is what "nothing has
	// ever been run here" looks like, and the page draws that as its own
	// sentence rather than as an empty result list.
	run selftest.Run
	// running is true from the moment a sweep is admitted until the moment it
	// finishes, and it is what makes this single-flight. Deliberately not
	// derived from run.FinishedAt.IsZero(): a sweep that has just been
	// admitted and has not yet set StartedAt would read as finished for the
	// width of that gap, which is precisely the gap two tabs land in.
	running bool
}

var (
	selfTestMu  sync.Mutex
	selfTestReg = map[*App]*selfTestState{}
)

func (a *App) selfTestStateFor() *selfTestState {
	selfTestMu.Lock()
	defer selfTestMu.Unlock()
	st, ok := selfTestReg[a]
	if !ok {
		// Results initialised to an empty slice rather than left nil, for the
		// reason DiskReport's own Volumes field carries: a nil slice encodes
		// as JSON null and the page that walks it throws instead of drawing
		// nothing.
		st = &selfTestState{run: selftest.Run{Planned: []string{}, Results: []selftest.Result{}}}
		selfTestReg[a] = st
	}
	return st
}

// SelfTestStart begins one sweep, or joins the one already in flight.
//
// started is false when no NEW sweep was begun - either one was already
// running, in which case the Run returned describes that one and its id is
// the id the caller should poll, or the app is shutting down, in which case
// the Run comes back already finished. Both are answered as 202 by the route:
// from the caller's side "your sweep is under way" and "somebody else's sweep
// is under way and you are welcome to watch it" are the same thing to do next.
func (a *App) SelfTestStart() (selftest.Run, bool) {
	st := a.selfTestStateFor()

	st.mu.Lock()
	if st.running {
		run := st.snapshotLocked()
		st.mu.Unlock()
		return run, false
	}
	st.running = true
	st.run = selftest.Run{
		ID:        newID(),
		StartedAt: time.Now(),
		Planned:   append([]string(nil), selftest.Order...),
		Results:   []selftest.Result{},
	}
	run := st.snapshotLocked()
	st.mu.Unlock()

	// Through track() rather than a bare `go`, so Close cancels this sweep and
	// then waits for it. Without that, a sweep in the middle of a fifteen
	// second provider call outlives the App it is reading, and the store it
	// would touch on the way out is already shut - the exact tail track()'s own
	// doc comment exists to prevent.
	if !a.track() {
		st.mu.Lock()
		st.running = false
		st.run.FinishedAt = time.Now()
		run = st.snapshotLocked()
		st.mu.Unlock()
		return run, false
	}
	go func() {
		defer a.wg.Done()
		a.runSelfTest(st)
	}()
	return run, true
}

// SelfTestLatest is the current sweep, or the last one, or the zero Run when
// nothing has ever been swept here.
//
// The zero Run rather than a (Run, bool) pair or a 404, because the route this
// feeds is polled once a second by a page that has to render something on its
// very first load: an empty id is a perfectly readable "not run yet", and a
// 404 would make the browser's own json() decoder throw on the ordinary case.
func (a *App) SelfTestLatest() selftest.Run {
	st := a.selfTestStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.snapshotLocked()
}

// snapshotLocked copies the run so that nothing outside this file can hold a
// slice the sweep is still appending to. Caller holds st.mu.
func (st *selfTestState) snapshotLocked() selftest.Run {
	out := st.run
	out.Planned = append([]string(nil), st.run.Planned...)
	out.Results = append([]selftest.Result(nil), st.run.Results...)
	if out.Planned == nil {
		out.Planned = []string{}
	}
	if out.Results == nil {
		out.Results = []selftest.Result{}
	}
	return out
}

// add files one landed result, keeping the list in selftest.Order.
//
// Sorted on the way in rather than on the way out, and by an insertion rather
// than a sort call, because the list is seven long and the alternative is a
// JSON document whose row order changes between two polls one second apart -
// which a page that keys on it would redraw for no reason, and which makes two
// captured answers impossible to diff.
func (st *selfTestState) add(res selftest.Result) {
	rank := func(id string) int {
		for i, want := range selftest.Order {
			if want == id {
				return i
			}
		}
		return len(selftest.Order)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	at := len(st.run.Results)
	for i, have := range st.run.Results {
		if rank(have.ID) > rank(res.ID) {
			at = i
			break
		}
	}
	st.run.Results = append(st.run.Results, selftest.Result{})
	copy(st.run.Results[at+1:], st.run.Results[at:])
	st.run.Results[at] = res
}

func (st *selfTestState) finish() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.run.FinishedAt = time.Now()
	st.running = false
}

// runSelfTest is the sweep itself. Only SelfTestStart calls it, and only ever
// one at a time per App.
//
// The seven run CONCURRENTLY, which is the whole reason the result list is
// delivered by polling rather than in the POST's own response: the folder
// readout and the clock land in milliseconds, yt-dlp takes as long as a
// process launch, and the account sweep can take fifteen seconds. Run in
// sequence the operator would watch a blank card for half a minute; run in
// parallel the list fills in front of them, which is also the only thing that
// makes an individual check's slowness visible as such.
//
// ONE SETTINGS SNAPSHOT for the whole sweep. Six of the seven read the
// configuration, and a save landing in the middle would otherwise have half
// the report describing the old document and half the new one - a report that
// cannot be reasoned about is worse than one taken a second earlier.
func (a *App) runSelfTest(st *selfTestState) {
	// Deferred, so a panic in the coordination below cannot leave `running`
	// stuck true and the button disabled for the life of the process. (A panic
	// inside one of the checks takes the process with it, as any panic in a
	// goroutine does; this covers the part that is recoverable at all.)
	defer st.finish()

	ctx := a.ctx
	if ctx == nil {
		// Only reachable from a hand-built App in a test that never called
		// New. Background rather than a nil dereference: a sweep with no
		// cancellation is worse than one with, and far better than a crash.
		ctx = context.Background()
	}
	cfg := a.Settings.Get()

	checks := []func(context.Context, settings.Settings) selftest.Result{
		a.selfTestJD,
		a.selfTestYtdlp,
		a.selfTestFolders,
		a.selfTestAccountsRO,
		a.selfTestRelay,
		a.selfTestClock,
		a.selfTestTorrentPort,
	}
	var wg sync.WaitGroup
	for _, check := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st.add(check(ctx, cfg))
		}()
	}
	wg.Wait()
}

// ---- the seven checks ------------------------------------------------------

// selfTestJD reports the headless JDownloader sidecar.
//
// It reads KL_PROVISION_JD as well as KL_JD, and that second variable is the
// whole difference between a useful row and a nagging one. This build starts
// its own headless JD while booting and fills KL_JD in itself
// (cmd/knightloader/main.go); an operator who set KL_PROVISION_JD=0 has said
// they do not want one, so an empty address is their decision rather than a
// failure - skipped, not a warning. An empty address WITHOUT that opt-out
// means the provisioning attempt failed, which is worth saying out loud
// because the only trace it otherwise leaves is a line in the log.
func (a *App) selfTestJD(context.Context, settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckJD, At: time.Now()}
	st := a.JDStatus()
	switch {
	case !st.Configured:
		if strings.TrimSpace(os.Getenv("KL_PROVISION_JD")) == "0" {
			r.Status, r.Code = selftest.StatusSkipped, "jd.provisioningOff"
			return r
		}
		r.Status, r.Code = selftest.StatusWarn, "jd.missing"
		return r
	case !st.Reachable:
		r.Status, r.Code = selftest.StatusFail, "jd.unreachable"
		r.Params = map[string]string{"address": strings.TrimSpace(os.Getenv("KL_JD"))}
		r.Detail = st.Detail
		return r
	case st.Version == 0:
		// It answered /help and would not give a revision. Not a failure -
		// every container link this instance hands it will still be opened -
		// but worth a line, because a JD that answers half its own API is
		// usually a JD somebody put a proxy in front of.
		r.Status, r.Code = selftest.StatusWarn, "jd.noVersion"
		r.Detail = st.Detail
		return r
	default:
		r.Status, r.Code = selftest.StatusPass, "jd.ok"
		r.Params = map[string]string{"version": strconv.FormatInt(st.Version, 10)}
		return r
	}
}

// ytdlpVersionOutput runs `yt-dlp --version` and hands back what it printed.
//
// Behind a package variable for the reason app_diskreport.go's diskUsage is:
// a reading nothing can replace is a reading no test can control, and the four
// answers that matter here - no binary at all, a date, a nightly, and a string
// that is not a date - cannot all be produced by whatever yt-dlp the machine
// running the tests happens to have. Written by tests only.
var ytdlpVersionOutput = func(ctx context.Context, bin string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		// A binary that failed AND printed to stderr has said something worth
		// carrying - "python: can't open file", a missing shared library - and
		// exec's own error is only ever "exit status 1" or "executable file
		// not found". Both, in that order, so the reader sees the diagnosis
		// before the exit code.
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", errors.New(strings.TrimSpace(string(ee.Stderr)) + ": " + err.Error())
		}
		return "", err
	}
	return string(out), nil
}

// selfTestYtdlp reports which yt-dlp this is and how far behind it has fallen.
//
// The binary is resolved exactly as rewireBackends resolves it
// (app_accounts.go: KL_YTDLP, else "yt-dlp" on PATH), because a self-test that
// looked at a different binary from the one the downloads use would be a
// perfect green row on a broken install.
func (a *App) selfTestYtdlp(ctx context.Context, _ settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckYtdlp, At: time.Now()}
	bin := strings.TrimSpace(os.Getenv("KL_YTDLP"))
	if bin == "" {
		bin = "yt-dlp"
	}
	ctx, cancel := context.WithTimeout(ctx, ytdlpVersionTimeout)
	defer cancel()

	out, err := ytdlpVersionOutput(ctx, bin)
	if err != nil {
		r.Status, r.Code = selftest.StatusFail, "ytdlp.missing"
		r.Detail = err.Error()
		return r
	}
	shown := strings.TrimSpace(out)
	if i := strings.IndexAny(shown, "\r\n"); i >= 0 {
		shown = strings.TrimSpace(shown[:i])
	}
	released, ok := selftest.ParseVersion(out)
	if !ok {
		// A yt-dlp whose age cannot be judged is a different answer from an
		// old one and from a missing one, and it is StatusUnknown for exactly
		// the reason that word exists: it is configured, it runs, and this
		// build cannot find out the one thing being asked.
		r.Status, r.Code = selftest.StatusUnknown, "ytdlp.undated"
		r.Params = map[string]string{"version": shown}
		return r
	}
	status, days := selftest.AgeVerdict(released, time.Now())
	code := "ytdlp.ok"
	switch status {
	case selftest.StatusWarn:
		code = "ytdlp.aging"
	case selftest.StatusFail:
		code = "ytdlp.old"
	}
	r.Status, r.Code = status, code
	r.Params = map[string]string{"version": shown, "days": strconv.Itoa(days)}
	return r
}

// selfTestFolders reports the download folder and the working folder: whether
// they are there, whether this process can write into them, and how much room
// is left.
//
// THE FIGURES COME FROM DiskReport AND NOTHING ELSE. That call is already
// cached and single-flighted, it already does the walk that says which folder
// the numbers actually describe, and it already knows that "this platform
// cannot be asked" is a third answer. A second measurement here would be a
// second opinion that can disagree with the one the disk guard acts on, which
// is the one thing a diagnostic must never produce.
//
// THE WRITE PROBE IS THE ONE THING THIS FILE ADDS, and it is fenced. Only the
// download and working folders, never a category folder; only where the folder
// already exists, so nothing is created; and the probe file is
// settings.WriteProbeName, the same name settings.Validate already uses, so no
// scanner in the tree has to learn to ignore a second one (internal/watch's
// poller skips dotfiles, which is what makes the existing name safe).
func (a *App) selfTestFolders(_ context.Context, cfg settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckFolders, At: time.Now()}
	rep := a.DiskReport()
	var statuses []selftest.Status
	for _, v := range rep.Volumes {
		if v.Role != roleDownloads && v.Role != roleWork {
			continue
		}
		row := folderRow(v, cfg.DiskLowSpace)
		statuses = append(statuses, row.Status)
		r.Rows = append(r.Rows, row)
	}
	r.Status = selftest.Worst(statuses...)
	switch {
	case len(r.Rows) == 0:
		// Not reachable on any install that has a download folder, which is
		// every install - but a role list that grows or a configuration that
		// resolves to nothing relative must not produce a row saying "fine"
		// about nothing measured.
		r.Code = "folders.none"
	case r.Status == selftest.StatusPass:
		// "Every folder checked", not "both": an install with no working folder
		// configured has exactly one row here, and the summary saying "both"
		// over a single row is the kind of small wrongness that makes a reader
		// stop trusting the rest of the page. Found on a live run rather than
		// reasoned about, which is why the sentence is worded the way it is.
		r.Code = "folders.allOk"
	default:
		r.Code = "folders.someBad"
	}
	return r
}

// folderRow is one folder's verdict. mark is settings.DiskLowSpace - the floor
// below which the dispatcher stops starting anything - or 0 when none is set.
func folderRow(v VolumeReport, mark int64) selftest.Result {
	row := selftest.Result{ID: v.Dir, At: time.Now()}
	params := map[string]string{"dir": v.Dir, "role": v.Role, "measured": v.Measured}
	if v.Known {
		// Bytes as a decimal string, formatted by the browser's own fmtBytes.
		// A server that wrote "4,2 GB" would have decided the reader's
		// language and their decimal separator on their behalf.
		params["free"] = strconv.FormatUint(v.Free, 10)
	}
	row.Params = params

	// The probe first, because "this process cannot write here" outranks
	// everything else the row could say: a folder with three terabytes free
	// that this uid may not write into is a queue that fails every task.
	// Skipped when the folder is not there yet - creating it to find out is
	// exactly what this file refuses to do.
	if v.Exists {
		if err := probeWritable(v.Dir); err != nil {
			row.Status, row.Code = selftest.StatusFail, "folders.notWritable"
			row.Detail = err.Error()
			params["uid"] = processOwner()
			return row
		}
	}

	switch {
	case !v.Exists:
		// The normal case for a folder nothing has written into yet, AND the
		// dangerous one: a mount that did not come up walks all the way to the
		// volume root, and the figures then describe the container's own
		// filesystem under the name of somebody's NAS share. Both paths travel
		// in the params so the reader can see which they are looking at -
		// app_diskreport.go's own comment makes the same argument.
		row.Status, row.Code = selftest.StatusWarn, "folders.missing"
	case !v.Known:
		row.Status, row.Code = selftest.StatusUnknown, "folders.unknown"
	case mark > 0 && v.Free < uint64(mark):
		row.Status, row.Code = selftest.StatusWarn, "folders.low"
		params["mark"] = strconv.FormatInt(mark, 10)
	case mark <= 0:
		// Stated rather than warned about. The absolute thresholds ship off,
		// so warning here would put an amber row on every fresh install - and
		// it would not even be true that nothing holds the queue back, because
		// DiskReserve is on by default and refuses a download that does not
		// fit as it is. The sentence says exactly that much and no more.
		row.Status, row.Code = selftest.StatusPass, "folders.noMark"
	default:
		row.Status, row.Code = selftest.StatusPass, "folders.ok"
		params["mark"] = strconv.FormatInt(mark, 10)
	}
	return row
}

// probeWritable writes and removes settings.WriteProbeName in dir.
//
// Deliberately NOT settings.Validate, which does the same thing plus an
// os.MkdirAll - see this file's own header and app_diskreport.go:24-30. The
// caller guarantees dir already exists.
func probeWritable(dir string) error {
	probe := filepath.Join(dir, settings.WriteProbeName)
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	return os.Remove(probe)
}

// processOwner names the account this process runs as, for the permission
// advice.
//
// READ AT RUNTIME AND NEVER HARDCODED, and that is not caution for its own
// sake: this tree contains two different claims about it - Dockerfile:27-28
// creates and runs as uid 1000, while internal/resolver/jd/client.go's doc
// comment says the container runs as uid 99 - so an advice string built from
// whichever one the author happened to read would be wrong on at least one
// deployment, and would be wrong for anybody overriding PUID on Unraid too.
//
// Windows has no uid and os.Getuid answers -1 there, which is a number that
// means nothing to the person reading it. The account name is what that
// platform can actually be asked about, so that is what it says instead.
func processOwner() string {
	if uid := os.Getuid(); uid >= 0 {
		return strconv.Itoa(uid)
	}
	if u := strings.TrimSpace(os.Getenv("USERNAME")); u != "" {
		return u
	}
	return "?"
}

// selfTestCheckCredential is checkCredential behind a package variable, so the
// account sweep can be driven to fail in a test without a network and without
// a real provider key. Written by tests only - see selfTestAccountsRO, whose
// guarantee is the reason this indirection is worth having at all.
var selfTestCheckCredential = checkCredential

// selfTestAccountsRO asks every configured debrid login whether it still
// works, AND REPORTS NOTHING IT LEARNS TO THE ACCOUNT-HEALTH TRACKER.
//
// THIS IS THE SHARPEST EDGE IN THE WHOLE FEATURE, so it is written out in
// full. The obvious implementation is to loop over the accounts and call the
// existing TestAccount (app_accounts.go), whose own doc comment says it "never
// persists anything - it is a read". That comment is wrong. Its body calls
// reportAccountFailure, which runs the failure through accounts.ClassifyReason;
// a network error classifies as core.ReasonNetwork, which becomes
// HealthTempDisabled, which benches the account for benchDelay(1) - fifteen
// minutes, doubling per episode up to six hours. A benched account is not
// Usable() and dispatch skips it.
//
// So an operator whose line is flapping at three in the morning presses
// "Selbsttest" and the diagnostic tool takes every debrid account they own out
// of the routing table for the next quarter of an hour. A tool somebody
// reaches for BECAUSE something is already wrong must not be able to make it
// worse; a diagnostic that changes what it measures is not a diagnostic.
//
// Hence checkCredential directly - the shared network call underneath both
// VerifyCredential and TestAccount - and neither reportAccountFailure nor
// reportAccountSuccess anywhere in this function. app_selftest_test.go asserts
// account_health.json is byte-identical after a sweep in which every login
// fails; that test is the thing standing between this feature and a queue that
// stops because somebody pressed a button.
//
// The CACHED health state does ride along, on a failing row, as a params
// entry. That is a pure local read (the tracker's own Get), it costs nothing,
// and it is what lets the interface tell "the provider says this key is wrong"
// apart from "something timed out" without this function guessing.
func (a *App) selfTestAccountsRO(ctx context.Context, _ settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckAccounts, At: time.Now()}

	// Membership from the catalogue's own Group rather than a list of service
	// ids repeated here, so a debrid service added later is swept without
	// anybody remembering this file - the same discipline
	// accountForResolverLocked (app_health.go) holds itself to.
	type target struct {
		service string
		account string
		label   string
	}
	var targets []target
	for _, svc := range accounts.Catalogue {
		if svc.Group != accounts.GroupDebrid {
			continue
		}
		for _, account := range append([]string{""}, a.Accounts.AccountIDs(svc.ID)...) {
			row, ok := a.accountRow(svc, account)
			if !ok {
				continue // nothing configured in this slot
			}
			label := row.Label
			if strings.TrimSpace(label) == "" {
				label = svc.Label
			}
			targets = append(targets, target{service: svc.ID, account: account, label: label})
		}
	}
	if len(targets) == 0 {
		r.Status, r.Code = selftest.StatusSkipped, "accounts.none"
		return r
	}

	rows := make([]selftest.Result, len(targets))
	sem := make(chan struct{}, accountCheckConcurrency)
	var wg sync.WaitGroup
	for i, tg := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rows[i] = a.selfTestOneAccount(ctx, tg.service, tg.account, tg.label)
		}()
	}
	wg.Wait()

	bad := 0
	statuses := make([]selftest.Status, 0, len(rows))
	for _, row := range rows {
		statuses = append(statuses, row.Status)
		if row.Status == selftest.StatusFail {
			bad++
		}
	}
	r.Rows = rows
	r.Status = selftest.Worst(statuses...)
	r.Params = map[string]string{"n": strconv.Itoa(len(rows)), "bad": strconv.Itoa(bad)}
	if bad == 0 {
		r.Code = "accounts.allOk"
	} else {
		r.Code = "accounts.someFailed"
	}
	return r
}

// selfTestOneAccount is one login, asked once. It writes nothing anywhere.
func (a *App) selfTestOneAccount(ctx context.Context, service, account, label string) selftest.Result {
	row := selftest.Result{ID: metaKey(service, account), At: time.Now()}
	row.Params = map[string]string{"label": label, "service": service}

	svc, known := accounts.Lookup(service)
	if !known {
		row.Status, row.Code = selftest.StatusUnknown, "accounts.rowUnknownService"
		return row
	}
	cred := a.credentialFor(svc, account)
	if cred.IsZero() {
		// Configured a moment ago when the target list was built, gone by the
		// time this ran: a credential cleared from another tab mid-sweep.
		// Skipped rather than failed - nothing is broken, there is simply
		// nothing there any more.
		row.Status, row.Code = selftest.StatusSkipped, "accounts.rowGone"
		return row
	}

	ctx, cancel := context.WithTimeout(ctx, accountCheckTimeout)
	defer cancel()
	ok, hosts, err := selfTestCheckCredential(ctx, service, cred)
	if err != nil {
		row.Status, row.Code = selftest.StatusFail, "accounts.rowFailed"
		row.Detail = err.Error()
		// The cached verdict, read and never written. It is what turns "this
		// login was refused" into advice: a key the provider has revoked, a
		// subscription that lapsed and a rate limit need three different next
		// steps, and the interface picks between them on this value.
		if state := a.acctHealthTracker().Get(service, account).State; state != "" {
			row.Params["health"] = string(state)
		}
		return row
	}
	if !ok {
		// checkCredential's contract is (false, 0, err) on every refusal, so
		// this is unreachable today. Kept because "false with no error" is the
		// one combination that would otherwise render as a pass.
		row.Status, row.Code = selftest.StatusFail, "accounts.rowFailed"
		return row
	}
	row.Status, row.Code = selftest.StatusPass, "accounts.rowOk"
	row.Params["hosts"] = strconv.Itoa(hosts)
	return row
}

// selfTestRelay reports the relay connection AND DIALS NOTHING.
//
// a.Federation.RelayConnected() is the live socket the relay client is already
// maintaining and already retrying on its own. Opening a second connection to
// test it would be wrong twice over: it would report a green row on an
// instance whose real client is stuck, and it would hit the project relay once
// per press of the button from every install that has one.
//
// WHICH RELAY IS ACTUALLY DIALLED is resolved here rather than read off the
// mode, because the two are not the same question and getting that wrong makes
// this row lie about a fresh install. settings.RelayModeOf resolves an
// untouched install to "project", while nothing is dialled at all until a
// connection secret is stored - so a naive "mode is project, therefore a relay
// is configured, therefore not being connected is a fault" would put a red row
// on every install that has never used remote access.
//
// This mirrors internal/api's relayTarget deliberately rather than calling it:
// that function lives in package api, which imports this one, so calling it
// would be an import cycle. The mirroring is the price, and it is why both
// sides name each other.
func (a *App) selfTestRelay(_ context.Context, cfg settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckRelay, At: time.Now()}

	mode := cfg.RelayModeOf()
	if mode == settings.RelayModeOff {
		r.Status, r.Code = selftest.StatusSkipped, "relay.off"
		return r
	}
	override := ""
	if mode == settings.RelayModeOwn {
		override = strings.TrimRight(strings.TrimSpace(cfg.RelayURL), "/")
		if override == "" {
			// "My own relay" with no address is not the project relay, and
			// relayTarget makes exactly this call for exactly this reason.
			r.Status, r.Code = selftest.StatusSkipped, "relay.ownNoAddress"
			return r
		}
	}

	address := ""
	if secretHex, err := a.Accounts.Get(relay.SeedAccountService); err == nil && secretHex != "" {
		secret, decErr := hex.DecodeString(secretHex)
		if decErr != nil || len(secret) != seedphrase.SecretLen {
			// Sealed but unusable. Loud, because the instance sits there
			// looking configured while reaching nothing, and re-entering the
			// phrase is not a fix anybody guesses from silence.
			r.Status, r.Code = selftest.StatusFail, "relay.badSecret"
			return r
		}
		address = relay.DefaultRelayURL
		if override != "" {
			address = override
		}
	} else if override != "" {
		// The hand-entered key path, which only ever reaches a relay whose
		// address the operator gave: in project mode relayTarget returns an
		// empty URL for it and applyRelay clears the client, so a stored key
		// with no address of its own dials nothing.
		if manual, mErr := a.Accounts.Get(relay.AccountService); mErr == nil && manual != "" {
			address = override
		}
	}
	if address == "" {
		r.Status, r.Code = selftest.StatusSkipped, "relay.notSetUp"
		return r
	}

	r.Params = map[string]string{"address": address}
	if a.Federation.RelayConnected() {
		r.Status, r.Code = selftest.StatusPass, "relay.connected"
		return r
	}
	// Warn and not fail. The relay client retries on its own for ever, so a
	// relay that is merely down right now resolves itself; what the operator
	// needs is to be told the connection is not up, not to be told their
	// configuration is broken when it may well be fine.
	r.Status, r.Code = selftest.StatusWarn, "relay.notConnected"
	return r
}

// selfTestClock reports the zone this process runs in, and the clock it is
// reading.
//
// THE ZONE IS THE FINDING. internal/schedule works in time.Local, the
// Dockerfile installs tzdata and sets no TZ, and TZ appears in this project
// only as documentation - so a container started without it runs every
// timetable in UTC and nothing has ever said so. Somebody with a nightly
// window at 22:00 has been starting it at 22:00 UTC, possibly for a year.
//
// AND IT IS ONLY RAISED WHEN A TIMETABLE EXISTS. A UTC clock with no schedule
// is not a problem, it is a container. Raising it anyway would put an amber row
// on the majority of installs for something that changes nothing about them,
// which is how a diagnostic page teaches people to ignore it.
//
// The difference between this machine's clock and the reader's is NOT computed
// here and cannot be: it needs the browser's own clock and the round trip
// discounted, so the browser computes it (web/src/lib/selftest.ts) and may
// raise this row further on what it finds.
func (a *App) selfTestClock(_ context.Context, cfg settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckClock, At: time.Now()}
	z := selftest.ZoneReport()
	r.Params = map[string]string{
		"zone":   z.Name,
		"offset": strconv.Itoa(z.OffsetSeconds),
		"time":   time.Now().Format(time.RFC3339),
	}
	switch {
	case z.TZ != "" && !z.TZResolved:
		// The one that is invisible from every other angle: TZ names a zone,
		// the database does not have it, and Go fell back to UTC without a
		// word. Everything in the process then runs in UTC while the
		// configuration insists otherwise.
		r.Status, r.Code = selftest.StatusWarn, "clock.noZoneDB"
	case z.UTC && len(cfg.Schedule) > 0:
		r.Status, r.Code = selftest.StatusWarn, "clock.utc"
	default:
		r.Status, r.Code = selftest.StatusPass, "clock.ok"
	}
	return r
}

// selfTestTorrentPort reports the torrent listen port, and is honest about the
// three separate things it cannot tell you.
//
// FIRST, WITH Port 0 THIS BUILD CANNOT NAME THE PORT AT ALL. Zero means gopeed
// picks one; internal/engine only ever WRITES bt.ListenPort and there is no
// read-back of what anacrolix actually bound, so there is no number to report.
//
// SECOND, A CONFIGURED PORT IS NOT NECESSARILY THE LIVE ONE. gopeed's bt client
// is a lazy singleton built on the first torrent of the process, so a port
// saved after that point is stored correctly and will not be in force until a
// restart - settings_torrent.go's own Port doc says so at length.
//
// THIRD, AND THIS IS THE OWNER'S DECISION RATHER THAN A LIMITATION: whether the
// port is reachable from outside cannot be answered from inside the network,
// only by a third party, and this app contacts none the operator did not pick.
// So the row reports StatusUnknown and says which question it is declining -
// which is a better answer than a green row about a port nobody can reach.
func (a *App) selfTestTorrentPort(_ context.Context, cfg settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckTorrentPort, At: time.Now(), Status: selftest.StatusUnknown}
	if cfg.Torrent.Port == 0 {
		r.Code = "torrentPort.noPort"
		return r
	}
	r.Code = "torrentPort.notChecked"
	r.Params = map[string]string{"port": strconv.Itoa(cfg.Torrent.Port)}
	return r
}
