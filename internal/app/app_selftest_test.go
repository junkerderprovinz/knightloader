package app

// The self-test sweep.
//
// THE FIRST TEST IN THIS FILE IS THE REASON THE FILE EXISTS. Everything else
// here checks that a row says the right words; that one checks that pressing a
// diagnostic button cannot stop somebody's download queue. See
// selfTestAccountsRO's own doc comment for the mechanism it guards against -
// TestAccount's reportAccountFailure benching every debrid account for fifteen
// minutes on a network error, which is exactly the condition an operator would
// be pressing this button in.
//
// Nothing in this file reaches the network. The credential check is driven
// through selfTestCheckCredential and the yt-dlp probe through
// ytdlpVersionOutput, both package variables that exist for this - see their
// own comments, and app_diskreport.go's diskUsage for the pattern.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/selftest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newSelfTestApp is an App on a throwaway data directory with the two outside
// calls stubbed out, so a sweep in these tests launches no process and opens no
// socket. Both stubs are restored when the test ends.
func newSelfTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// A binary path that cannot exist, so the yt-dlp row is deterministic even
	// on a developer machine that happens to have a real yt-dlp on PATH.
	t.Setenv("KL_YTDLP", filepath.Join(t.TempDir(), "no-such-yt-dlp"))
	t.Setenv("KL_JD", "")

	prevVersion := ytdlpVersionOutput
	ytdlpVersionOutput = func(context.Context, string) (string, error) {
		return "", errors.New("exec: \"yt-dlp\": executable file not found in $PATH")
	}
	prevCheck := selfTestCheckCredential
	t.Cleanup(func() {
		ytdlpVersionOutput = prevVersion
		selfTestCheckCredential = prevCheck
	})
	return a
}

// sweep runs one self-test to completion and hands back the finished run.
//
// It waits rather than sleeping a fixed amount, and it waits on the same
// FinishedAt the browser polls on - so a check that never reports would fail
// this test by timing out rather than by being silently missed.
func sweep(t *testing.T, a *App) selftest.Run {
	t.Helper()
	run, started := a.SelfTestStart()
	if !started {
		t.Fatalf("SelfTestStart did not start a sweep on a fresh app; it answered with run %q", run.ID)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		got := a.SelfTestLatest()
		if !got.FinishedAt.IsZero() {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sweep did not finish inside 30s; it has %d of %d results", len(got.Results), len(got.Planned))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// resultFor is one check out of a finished run.
func resultFor(t *testing.T, run selftest.Run, id string) selftest.Result {
	t.Helper()
	for _, r := range run.Results {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("the run reported no result at all for %q; it has %d results", id, len(run.Results))
	return selftest.Result{}
}

// healthFileState is account_health.json exactly as it is on disk, and whether
// it is there at all. "Not there" is a real state and the one a fresh install
// is in - a test that only compared bytes would pass trivially by comparing
// nothing to nothing without noticing the file had been created.
func healthFileState(t *testing.T, a *App) (present bool, data []byte) {
	t.Helper()
	path := filepath.Join(filepath.Dir(a.dlDir), "account_health.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return true, b
}

// TestASweepWhoseLoginsAllFailNeverBenchesAnAccount is the guard this whole
// feature is fenced by.
//
// The failure it prevents, in full: an operator whose line is flapping at three
// in the morning presses "Selbsttest". The obvious implementation loops over
// the accounts and calls TestAccount, whose own doc comment claims it "never
// persists anything". It does - reportAccountFailure runs the error through
// accounts.ClassifyReason, a network error becomes HealthTempDisabled, and the
// account is benched for fifteen minutes, doubling per episode up to six hours.
// A benched account is not Usable() and dispatch skips it. So the diagnostic
// tool takes every debrid account out of the routing table at exactly the
// moment somebody is trying to work out why nothing is downloading.
//
// Asserted on the FILE rather than on the tracker's in-memory map, because the
// file is what survives a restart and therefore what the queue reads tomorrow.
func TestASweepWhoseLoginsAllFailNeverBenchesAnAccount(t *testing.T) {
	a := newSelfTestApp(t)

	// Seeded straight through the store rather than through
	// SetAccountCredential, the pattern accounts_test.go documents: it keeps
	// rewireBackends from ever seeing a routed credential, so nothing here
	// depends on a third party.
	for _, svc := range []string{"alldebrid", "realdebrid"} {
		if err := a.Accounts.SetCredential(svc, "", accounts.Credential{APIKey: "fake-key"}); err != nil {
			t.Fatal(err)
		}
	}

	// The failure shape that does the damage: a transport error, which is what
	// a flapping line produces and what ClassifyReason turns into a bench.
	selfTestCheckCredential = func(context.Context, string, accounts.Credential) (bool, int, error) {
		return false, 0, errors.New("dial tcp: lookup api.alldebrid.com: no such host")
	}

	beforePresent, beforeData := healthFileState(t, a)

	run := sweep(t, a)
	got := resultFor(t, run, selftest.CheckAccounts)
	if got.Status != selftest.StatusFail {
		t.Fatalf("the accounts check came back %q with every login refused, want %q - if it did not actually "+
			"run the logins, this test proves nothing at all", got.Status, selftest.StatusFail)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("the accounts check reported %d rows for two configured logins", len(got.Rows))
	}

	afterPresent, afterData := healthFileState(t, a)
	if beforePresent != afterPresent {
		t.Fatalf("account_health.json went from present=%v to present=%v across a self-test; the sweep wrote to "+
			"the account-health store, which benches accounts and stops the queue", beforePresent, afterPresent)
	}
	if string(beforeData) != string(afterData) {
		t.Fatalf("account_health.json changed across a self-test.\nbefore: %s\nafter:  %s\n"+
			"The sweep must report what it finds and record nothing: a self-test run while the line is down "+
			"may not bench every account for the next quarter of an hour.", beforeData, afterData)
	}

	// And the reading the dispatcher actually makes, which is the thing the
	// file only stands in for.
	for _, svc := range []string{"alldebrid", "realdebrid"} {
		if !a.acctHealthTracker().Usable(svc, "") {
			t.Errorf("%s is no longer routable after a self-test; dispatch now skips it and every link that "+
				"could only have gone there waits", svc)
		}
	}
}

// TestTheAccountRowsCarryTheLabelTheHostCountAndTheProvidersOwnWords pins what
// a row is FOR. A failing row with no detail is a red line that tells the
// operator nothing they did not already know.
func TestTheAccountRowsCarryTheLabelTheHostCountAndTheProvidersOwnWords(t *testing.T) {
	a := newSelfTestApp(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "good"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.SetCredential("realdebrid", "", accounts.Credential{APIKey: "bad"}); err != nil {
		t.Fatal(err)
	}
	selfTestCheckCredential = func(_ context.Context, service string, _ accounts.Credential) (bool, int, error) {
		if service == "alldebrid" {
			return true, 91, nil
		}
		return false, 0, errors.New("bad token")
	}

	got := resultFor(t, sweep(t, a), selftest.CheckAccounts)
	if got.Code != "accounts.someFailed" {
		t.Fatalf("code = %q with one of two logins refused, want accounts.someFailed", got.Code)
	}
	if got.Params["n"] != "2" || got.Params["bad"] != "1" {
		t.Fatalf("params = %v, want n=2 and bad=1 - the summary sentence is built from those two", got.Params)
	}
	for _, row := range got.Rows {
		switch row.Code {
		case "accounts.rowOk":
			if row.Params["hosts"] != "91" {
				t.Errorf("the accepted row reports %q hosters, want 91", row.Params["hosts"])
			}
			if row.Params["label"] == "" {
				t.Error("the accepted row has no label; the sentence names the account and would name nothing")
			}
		case "accounts.rowFailed":
			if row.Detail != "bad token" {
				t.Errorf("detail = %q, want the provider's own words verbatim - paraphrasing them throws away "+
					"the only part of the answer that names the actual problem", row.Detail)
			}
		default:
			t.Errorf("a row came back with code %q, which is neither of the two an account row can have", row.Code)
		}
	}
}

// TestAnInstanceWithNoDebridAccountReportsSkippedAndNotPass keeps the two
// apart. "All your logins work" said about zero logins is a green row that
// means nothing, and it is the reading somebody would take away from it.
func TestAnInstanceWithNoDebridAccountReportsSkippedAndNotPass(t *testing.T) {
	a := newSelfTestApp(t)
	got := resultFor(t, sweep(t, a), selftest.CheckAccounts)
	if got.Status != selftest.StatusSkipped {
		t.Fatalf("status = %q with nothing configured, want %q", got.Status, selftest.StatusSkipped)
	}
	if got.Code != "accounts.none" {
		t.Fatalf("code = %q, want accounts.none", got.Code)
	}
	if len(got.Rows) != 0 {
		t.Fatalf("%d rows for an install with no accounts", len(got.Rows))
	}
}

// TestTheYtdlpCheckTellsMissingUndatedAndOldApart covers the three answers that
// are easy to collapse into one. "There is no yt-dlp", "there is one and its
// age cannot be judged" and "there is one and it is nine months old" need three
// different things from the reader, and only the last one means go and update.
func TestTheYtdlpCheckTellsMissingUndatedAndOldApart(t *testing.T) {
	a := newSelfTestApp(t)

	cases := []struct {
		name       string
		out        string
		err        error
		wantStatus selftest.Status
		wantCode   string
	}{
		{"no binary", "", errors.New("executable file not found in $PATH"), selftest.StatusFail, "ytdlp.missing"},
		{"a version that is not a date", "custom-build\n", nil, selftest.StatusUnknown, "ytdlp.undated"},
		{"a current release", time.Now().UTC().Format("2006.01.02") + "\n", nil, selftest.StatusPass, "ytdlp.ok"},
		{"a release two months old", time.Now().UTC().AddDate(0, 0, -60).Format("2006.01.02") + "\n", nil, selftest.StatusWarn, "ytdlp.aging"},
		{"a release nine months old", time.Now().UTC().AddDate(0, 0, -270).Format("2006.01.02") + "\n", nil, selftest.StatusFail, "ytdlp.old"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ytdlpVersionOutput = func(context.Context, string) (string, error) { return c.out, c.err }
			got := resultFor(t, sweep(t, a), selftest.CheckYtdlp)
			if got.Status != c.wantStatus || got.Code != c.wantCode {
				t.Fatalf("got (%s, %s), want (%s, %s)", got.Status, got.Code, c.wantStatus, c.wantCode)
			}
			if c.err != nil {
				if !strings.Contains(got.Detail, "executable file not found") {
					t.Errorf("detail = %q, want the exec error verbatim", got.Detail)
				}
				return
			}
			if got.Params["version"] == "" {
				t.Error("no version in the params; the sentence names the version and would name nothing")
			}
		})
	}
}

// TestTheTorrentPortRowNeverClaimsToKnowWhetherThePortIsOpen is the owner's
// decision written down as a test. There is no external port checker in this
// build, deliberately, and the row must say so rather than report a configured
// port as though somebody had confirmed it was reachable.
func TestTheTorrentPortRowNeverClaimsToKnowWhetherThePortIsOpen(t *testing.T) {
	a := newSelfTestApp(t)

	got := resultFor(t, sweep(t, a), selftest.CheckTorrentPort)
	if got.Status != selftest.StatusUnknown || got.Code != "torrentPort.noPort" {
		t.Fatalf("with no port set, got (%s, %s), want (%s, torrentPort.noPort)", got.Status, got.Code, selftest.StatusUnknown)
	}

	cfg := a.Settings.Get()
	cfg.Torrent.Port = 51413
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	got = resultFor(t, sweep(t, a), selftest.CheckTorrentPort)
	if got.Status != selftest.StatusUnknown {
		t.Fatalf("with a port configured, status = %q, want %q - a configured port is not a reachable one, and "+
			"only a machine outside this network could say which it is", got.Status, selftest.StatusUnknown)
	}
	if got.Code != "torrentPort.notChecked" || got.Params["port"] != "51413" {
		t.Fatalf("got (%s, %v), want torrentPort.notChecked naming port 51413", got.Code, got.Params)
	}
}

// TestTheWriteProbeRunsInAFolderThatExistsAndLeavesNothingBehind pins both
// halves of the one thing this feature writes to disk anywhere. The probe has
// to actually happen - a row that says "can be written to" without having tried
// is a guess - and it has to leave nothing behind, because the folder it runs
// in is somebody's download folder.
//
// The download folder is pointed at a directory that ALREADY EXISTS on purpose.
// A fresh install's default folder has not been created yet, so a sweep over it
// probes nothing at all, and a cleanup assertion made there would pass by
// looking for a file that was never written - a test that cannot fail.
func TestTheWriteProbeRunsInAFolderThatExistsAndLeavesNothingBehind(t *testing.T) {
	a := newSelfTestApp(t)
	dir := t.TempDir()
	work := t.TempDir()
	cfg := a.Settings.Get()
	cfg.DownloadDir, cfg.WorkDir = dir, work
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	got := resultFor(t, sweep(t, a), selftest.CheckFolders)
	if len(got.Rows) != 2 {
		t.Fatalf("%d folder rows for a configured download folder and working folder", len(got.Rows))
	}
	probed := 0
	for _, row := range got.Rows {
		if row.Params["dir"] == "" || row.Params["role"] == "" {
			t.Errorf("a folder row arrived without the two fields that say what it describes: %v", row.Params)
		}
		if row.Code == "folders.missing" {
			t.Errorf("%s reported as not there, though the test created it - no probe can have run", row.ID)
			continue
		}
		probed++
		if row.Status == selftest.StatusFail {
			t.Errorf("%s reported as unwritable on a folder this process can plainly write to: %s", row.ID, row.Detail)
		}
		if _, err := os.Stat(filepath.Join(row.Params["dir"], settings.WriteProbeName)); err == nil {
			t.Errorf("the write probe is still sitting in %s; a diagnostic must not litter somebody's "+
				"download folder, and every scanner in the tree would have to learn to ignore it", row.Params["dir"])
		}
	}
	if probed != 2 {
		t.Fatalf("only %d of 2 folders were actually probed; the cleanup assertion above proves nothing "+
			"about a probe that never ran", probed)
	}
}

// TestAFolderThatIsNotThereYetIsReportedAndNeverCreated is the "this writes
// nothing" promise in the one place it is most tempting to break.
// settings.Validate does exactly what this check wants AND os.MkdirAll's the
// path on the way - wiring it in here would mean that pressing a diagnostic
// button creates folders on somebody's disk. app_diskreport.go's own comment
// makes the same argument about the same function.
func TestAFolderThatIsNotThereYetIsReportedAndNeverCreated(t *testing.T) {
	a := newSelfTestApp(t)
	dir := filepath.Join(t.TempDir(), "not-created-yet")
	cfg := a.Settings.Get()
	cfg.DownloadDir = dir
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}

	got := resultFor(t, sweep(t, a), selftest.CheckFolders)
	var row selftest.Result
	for _, r := range got.Rows {
		if r.Params["role"] == roleDownloads {
			row = r
		}
	}
	if row.Code != "folders.missing" {
		t.Fatalf("code = %q for a folder that is not there, want folders.missing", row.Code)
	}
	if row.Status != selftest.StatusWarn {
		t.Errorf("status = %q, want %q - it is the normal state of a folder nothing has written to yet, and "+
			"also what a mount that did not come up looks like", row.Status, selftest.StatusWarn)
	}
	if row.Params["measured"] == "" || row.Params["measured"] == dir {
		t.Errorf("measured = %q; the figures describe a folder ABOVE the one that was asked about and the row "+
			"has to say which, or a missing mount is reported as the download disk", row.Params["measured"])
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("%s was created by a self-test. Pressing a diagnostic button must not change anything on "+
			"disk; settings.Validate is the tempting call here and it MkdirAll's the path", dir)
	}
}

// TestAFreshInstallsRelayRowIsNotAFault is trap 14 as a test.
// settings.RelayModeOf resolves an untouched install to "project", while
// nothing is dialled at all until a connection secret is stored - so a check
// that read the mode as "a relay is configured" would put a red row on every
// install that has never used remote access.
func TestAFreshInstallsRelayRowIsNotAFault(t *testing.T) {
	a := newSelfTestApp(t)
	got := resultFor(t, sweep(t, a), selftest.CheckRelay)
	if got.Status != selftest.StatusSkipped {
		t.Fatalf("status = %q on an install that has never set up remote access, want %q - the relay mode "+
			"defaults to \"project\" and nothing is dialled until a phrase is entered", got.Status, selftest.StatusSkipped)
	}
	if got.Code != "relay.notSetUp" {
		t.Fatalf("code = %q, want relay.notSetUp", got.Code)
	}
}

// TestAnOwnRelayWithNoAddressIsSkippedRatherThanReportedDisconnected is the
// other half of the same trap, and the one that had a real bug behind it:
// "my own relay" with no address is not the project relay and dials nothing.
func TestAnOwnRelayWithNoAddressIsSkippedRatherThanReportedDisconnected(t *testing.T) {
	a := newSelfTestApp(t)
	cfg := a.Settings.Get()
	cfg.RelayMode = settings.RelayModeOwn
	cfg.RelayURL = ""
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	got := resultFor(t, sweep(t, a), selftest.CheckRelay)
	if got.Status != selftest.StatusSkipped || got.Code != "relay.ownNoAddress" {
		t.Fatalf("got (%s, %s), want (%s, relay.ownNoAddress)", got.Status, got.Code, selftest.StatusSkipped)
	}
}

// TestASecondPressJoinsTheSweepAlreadyRunning is the rate limit guard. Seven
// checks, one of which logs into up to seven providers: two tabs and an
// impatient operator is dozens of provider calls a minute, and a temporary
// refusal from one of them is then indistinguishable from a key that has
// actually stopped working.
func TestASecondPressJoinsTheSweepAlreadyRunning(t *testing.T) {
	a := newSelfTestApp(t)

	// Held open by blocking the yt-dlp probe, so the sweep is genuinely in
	// flight while the second press happens - a sleep would be a race dressed
	// up as a test.
	release := make(chan struct{})
	entered := make(chan struct{})
	var once bool
	ytdlpVersionOutput = func(context.Context, string) (string, error) {
		if !once {
			once = true
			close(entered)
			<-release
		}
		return "", errors.New("not found")
	}

	first, started := a.SelfTestStart()
	if !started {
		t.Fatal("the first press did not start a sweep")
	}
	<-entered

	second, startedAgain := a.SelfTestStart()
	if startedAgain {
		close(release)
		t.Fatal("a second press started a SECOND sweep while one was in flight; that is dozens of provider " +
			"calls a minute from one impatient operator, and a rate-limit refusal reads like a dead key")
	}
	if second.ID != first.ID {
		close(release)
		t.Fatalf("the second press answered with run %q while %q is running; the caller would poll a run that "+
			"does not exist", second.ID, first.ID)
	}
	close(release)

	deadline := time.Now().Add(30 * time.Second)
	for a.SelfTestLatest().FinishedAt.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("the sweep never finished after the probe was released")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// And once it HAS finished, a press starts a new one - single-flight is
	// about overlap, not about running the thing once per process.
	next, startedNext := a.SelfTestStart()
	if !startedNext || next.ID == first.ID {
		t.Fatalf("after the sweep finished, a press answered (started=%v, id=%q); it must begin a new sweep",
			startedNext, next.ID)
	}
	for a.SelfTestLatest().FinishedAt.IsZero() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// TestAnUnpressedInstanceAnswersAnEmptyRunRatherThanNothing pins the state
// every install is in before the button is pressed for the first time. The
// route answers 200 with this, so the page's own decoder must not be handed a
// null list to walk.
func TestAnUnpressedInstanceAnswersAnEmptyRunRatherThanNothing(t *testing.T) {
	a := newSelfTestApp(t)
	run := a.SelfTestLatest()
	if run.ID != "" {
		t.Fatalf("id = %q before anything was ever run", run.ID)
	}
	if run.Results == nil || run.Planned == nil {
		t.Fatal("the empty run carries nil slices; those encode as JSON null and the page that walks them " +
			"throws instead of drawing nothing")
	}
}

// TestEveryPlannedCheckActuallyReports is the shape guarantee the page's
// pending rows rest on: it draws one row per entry in Planned and fills each in
// as its result lands, so a planned check that never reports is a row that says
// "waiting" for ever.
func TestEveryPlannedCheckActuallyReports(t *testing.T) {
	a := newSelfTestApp(t)
	run := sweep(t, a)
	if len(run.Planned) != len(selftest.Order) {
		t.Fatalf("Planned has %d entries, want %d", len(run.Planned), len(selftest.Order))
	}
	for _, id := range run.Planned {
		got := resultFor(t, run, id)
		if got.Status == "" {
			t.Errorf("%s reported with no status at all", id)
		}
		if got.Code == "" {
			t.Errorf("%s reported with no code; the page has no sentence to look up and would draw a blank row", id)
		}
		if got.At.IsZero() {
			t.Errorf("%s reported with no timestamp", id)
		}
	}
	// In Order, so two polls a second apart cannot hand the page a different
	// row order and make it redraw for nothing.
	for i, r := range run.Results {
		if r.ID != selftest.Order[i] {
			t.Fatalf("result %d is %q, want %q - the list is kept in selftest.Order", i, r.ID, selftest.Order[i])
		}
	}
}
