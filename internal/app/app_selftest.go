package app

// The self-test: one sweep over the JDownloader sidecar, yt-dlp, the target
// folders, the debrid logins, the relay, the clock and the torrent port, with a
// sentence per finding that says what to do next. Each of these was already
// answered by some route; this puts them on one page.
//
// It never touches what it measures:
//
//   - no UPnP mapping (routes_portmap.go writes a rule into the router);
//   - no reconnect, which would drop the WAN link under the other checks;
//   - no folder made, which is why settings.Validate is not used here: it
//     answers for a missing folder by making and removing a throwaway one;
//   - no machine the operator did not configure, so there is no external "is my
//     torrent port open" probe (see internal/proxycfg/probe.go);
//   - no report to the account-health tracker (see selfTestAccountsRO).
//
// It is single-flight because the account check logs into up to seven
// providers, and repeated presses would earn rate limits that look like dead
// keys. A second start joins the sweep in flight.
//
// The state is package-level and keyed by *App, like diskReportState.

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

// The limits are per check, since the checks run in parallel and one slow
// provider must not hold up the rest.
const (
	// ytdlpVersionTimeout covers a binary on a network mount that has gone away.
	ytdlpVersionTimeout = 10 * time.Second
	// accountCheckTimeout matches VerifyCredential and TestAccount.
	accountCheckTimeout = 15 * time.Second
	// accountCheckConcurrency keeps a sweep over many accounts short without
	// firing every login at once from one address and tripping rate limits.
	accountCheckConcurrency = 4
)

// selfTestState is one App's current or last sweep.
type selfTestState struct {
	mu sync.Mutex
	// run is the sweep. An empty ID means nothing has run here yet.
	run selftest.Run
	// running is true from admission until the sweep finishes. It is not
	// derived from run.FinishedAt, which is still zero in the gap before a new
	// sweep sets StartedAt.
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
		// Empty slices rather than nil so they encode as [] and not null.
		st = &selfTestState{run: selftest.Run{Planned: []string{}, Results: []selftest.Result{}}}
		selfTestReg[a] = st
	}
	return st
}

// SelfTestStart begins one sweep, or joins the one already in flight.
//
// started is false when no new sweep began: either one was running and the
// returned Run describes it, or the app is shutting down and the Run is already
// finished. The route answers 202 either way.
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

	// track so Close cancels the sweep and waits for it instead of letting a
	// provider call outlive the store.
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
// nothing has run here. The page polls it from its first load, and an empty id
// reads as "not run yet".
func (a *App) SelfTestLatest() selftest.Run {
	st := a.selfTestStateFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.snapshotLocked()
}

// snapshotLocked copies the run so nothing outside holds a slice the sweep is
// still appending to. Caller holds st.mu.
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

// add files one result, keeping the list in selftest.Order so the row order
// does not change between two polls.
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

// runSelfTest is the sweep itself, one at a time per App.
//
// The checks run concurrently and results are polled, so the fast rows appear
// while the account check is still waiting on providers. All checks share one
// settings snapshot so a save mid-sweep cannot split the report.
func (a *App) runSelfTest(st *selfTestState) {
	// Deferred so running cannot stay stuck true.
	defer st.finish()

	ctx := a.ctx
	if ctx == nil {
		// A hand-built App in a test that never called New.
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

// selfTestJD reports the headless JDownloader sidecar.
//
// This build provisions its own JD and fills in KL_JD itself. With
// KL_PROVISION_JD=0 an empty address is the operator's choice and the row is
// skipped; without it, an empty address means provisioning failed.
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
		// It answered /help but gave no revision. Links still open, but a JD
		// that answers half its API usually has a proxy in front of it.
		r.Status, r.Code = selftest.StatusWarn, "jd.noVersion"
		r.Detail = st.Detail
		return r
	default:
		r.Status, r.Code = selftest.StatusPass, "jd.ok"
		r.Params = map[string]string{"version": strconv.FormatInt(st.Version, 10)}
		return r
	}
}

// ytdlpVersionOutput runs `yt-dlp --version` and returns what it printed. It is
// a variable so tests can produce every answer without depending on the local
// yt-dlp.
var ytdlpVersionOutput = func(ctx context.Context, bin string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		// exec's error is only an exit status; stderr carries the diagnosis.
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", errors.New(strings.TrimSpace(string(ee.Stderr)) + ": " + err.Error())
		}
		return "", err
	}
	return string(out), nil
}

// selfTestYtdlp reports which yt-dlp this is and how far behind it has fallen.
// The binary is resolved as rewireBackends resolves it (KL_YTDLP, else yt-dlp on
// PATH), so the check looks at the one downloads use.
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
		// It runs, but its age cannot be judged.
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

// selfTestFolders reports the download and working folders: whether they exist,
// whether this process can write into them, and how much room is left.
//
// The figures come from DiskReport so they cannot disagree with what the disk
// guard acts on. The write probe is the only addition: download and working
// folders only, only where they already exist, using settings.WriteProbeName,
// a dotfile the watch-folder poller already skips.
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
		// Every install has a download folder, but a row must never say "fine"
		// about nothing measured.
		r.Code = "folders.none"
	case r.Status == selftest.StatusPass:
		// "Every folder", since an install without a working folder has one row.
		r.Code = "folders.allOk"
	default:
		r.Code = "folders.someBad"
	}
	return r
}

// folderRow is one folder's verdict. mark is settings.DiskLowSpace, the floor
// below which the dispatcher starts nothing, or 0 when none is set.
func folderRow(v VolumeReport, mark int64) selftest.Result {
	row := selftest.Result{ID: v.Dir, At: time.Now()}
	params := map[string]string{"dir": v.Dir, "role": v.Role, "measured": v.Measured}
	if v.Known {
		// Raw bytes; the browser formats them in the reader's locale.
		params["free"] = strconv.FormatUint(v.Free, 10)
	}
	row.Params = params

	// Not being able to write outranks everything else the row could say. A
	// folder that does not exist yet is not created to find out.
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
		// Normal before the first download, but also what a mount that did not
		// come up looks like: the figures then describe the volume root. Both
		// paths are in the params so the reader can tell.
		row.Status, row.Code = selftest.StatusWarn, "folders.missing"
	case !v.Known:
		row.Status, row.Code = selftest.StatusUnknown, "folders.unknown"
	case mark > 0 && v.Free < uint64(mark):
		row.Status, row.Code = selftest.StatusWarn, "folders.low"
		params["mark"] = strconv.FormatInt(mark, 10)
	case mark <= 0:
		// The floor ships off, and DiskReserve still refuses a download that
		// does not fit, so this is stated rather than warned about.
		row.Status, row.Code = selftest.StatusPass, "folders.noMark"
	default:
		row.Status, row.Code = selftest.StatusPass, "folders.ok"
		params["mark"] = strconv.FormatInt(mark, 10)
	}
	return row
}

// probeWritable writes and removes settings.WriteProbeName in dir, which must
// already exist. settings.Validate would pass a folder that is not there yet.
func probeWritable(dir string) error {
	probe := filepath.Join(dir, settings.WriteProbeName)
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	return os.Remove(probe)
}

// processOwner names the account this process runs as, for the permission
// advice. It is read at runtime because the uid differs between deployments
// (PUID on Unraid). Windows has no uid, so the account name is used there.
func processOwner() string {
	if uid := os.Getuid(); uid >= 0 {
		return strconv.Itoa(uid)
	}
	if u := strings.TrimSpace(os.Getenv("USERNAME")); u != "" {
		return u
	}
	return "?"
}

// selfTestCheckCredential lets tests make the account sweep fail without a
// network or a real key.
var selfTestCheckCredential = checkCredential

// selfTestAccountsRO asks every configured debrid login whether it still
// works, and reports nothing it learns to the account-health tracker.
//
// TestAccount cannot be used here: it passes failures to reportAccountFailure,
// and a network error there benches the account for fifteen minutes or more.
// Pressing the self-test on a flapping line would take every debrid account out
// of routing. checkCredential is the network call underneath both, and
// app_selftest_test.go asserts account_health.json is unchanged after a sweep
// in which every login fails.
//
// A failing row carries the cached health state, read only, so the page can
// tell a revoked key from a timeout.
func (a *App) selfTestAccountsRO(ctx context.Context, _ settings.Settings) selftest.Result {
	r := selftest.Result{ID: selftest.CheckAccounts, At: time.Now()}

	// Membership comes from the catalogue's Group, so a new debrid service is
	// swept without touching this file.
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

// selfTestOneAccount checks one login once and writes nothing.
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
		// Cleared from another tab since the target list was built.
		row.Status, row.Code = selftest.StatusSkipped, "accounts.rowGone"
		return row
	}

	ctx, cancel := context.WithTimeout(ctx, accountCheckTimeout)
	defer cancel()
	ok, hosts, err := selfTestCheckCredential(ctx, service, cred)
	if err != nil {
		row.Status, row.Code = selftest.StatusFail, "accounts.rowFailed"
		row.Detail = err.Error()
		// The cached verdict, read and never written, picks the advice: a
		// revoked key, a lapsed subscription and a rate limit need different
		// next steps.
		if state := a.acctHealthTracker().Get(service, account).State; state != "" {
			row.Params["health"] = string(state)
		}
		return row
	}
	if !ok {
		// checkCredential returns an error with every refusal, but false with
		// no error must not render as a pass.
		row.Status, row.Code = selftest.StatusFail, "accounts.rowFailed"
		return row
	}
	row.Status, row.Code = selftest.StatusPass, "accounts.rowOk"
	row.Params["hosts"] = strconv.Itoa(hosts)
	return row
}

// selfTestRelay reports the relay connection without dialling anything. It
// reads the socket the relay client already maintains; a second connection
// could pass while the real client is stuck.
//
// The dialled relay is resolved here rather than read off the mode: an
// untouched install resolves to "project" but dials nothing until a connection
// secret is stored. This mirrors internal/api's relayTarget, which cannot be
// called from here without an import cycle.
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
			// "My own relay" without an address is not the project relay, as
			// in relayTarget.
			r.Status, r.Code = selftest.StatusSkipped, "relay.ownNoAddress"
			return r
		}
	}

	address := ""
	if secretHex, err := a.Accounts.Get(relay.SeedAccountService); err == nil && secretHex != "" {
		secret, decErr := hex.DecodeString(secretHex)
		if decErr != nil || len(secret) != seedphrase.SecretLen {
			// Stored but unusable: the instance looks configured and reaches
			// nothing.
			r.Status, r.Code = selftest.StatusFail, "relay.badSecret"
			return r
		}
		address = relay.DefaultRelayURL
		if override != "" {
			address = override
		}
	} else if override != "" {
		// A hand-entered key only reaches a relay whose address the operator
		// gave; in project mode relayTarget returns no URL for it.
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
	// A warning, since the client keeps retrying and a relay that is down now
	// may recover without the configuration being wrong.
	r.Status, r.Code = selftest.StatusWarn, "relay.notConnected"
	return r
}

// selfTestClock reports the zone this process runs in and its clock.
//
// internal/schedule works in time.Local and the image sets no TZ, so a
// container started without one runs every timetable in UTC. That is only
// raised when a timetable exists; a UTC clock alone changes nothing. The skew
// against the reader's clock needs the browser's own clock, so
// web/src/lib/selftest.ts computes it.
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
		// TZ names a zone the database lacks, and Go fell back to UTC silently.
		r.Status, r.Code = selftest.StatusWarn, "clock.noZoneDB"
	case z.UTC && len(cfg.Schedule) > 0:
		r.Status, r.Code = selftest.StatusWarn, "clock.utc"
	default:
		r.Status, r.Code = selftest.StatusPass, "clock.ok"
	}
	return r
}

// selfTestTorrentPort reports the torrent listen port as unknown, and says why.
//
// With port 0 gopeed picks one and there is no read-back of what was bound. A
// configured port may not be live either: gopeed's bt client is built on the
// first torrent, so a later change waits for a restart. Whether the port is
// reachable from outside needs a third party, which this app does not contact.
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
