package app

// Nothing in this file reaches the network: the credential check and the
// yt-dlp probe are driven through selfTestCheckCredential and
// ytdlpVersionOutput.

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

// newSelfTestApp is an App on a throwaway data directory with both outside
// calls stubbed, restored when the test ends.
func newSelfTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	// A path that cannot exist, so a real yt-dlp on PATH does not matter.
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

// sweep runs one self-test to completion, waiting on the FinishedAt the browser
// polls, and returns the finished run.
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

// healthFileState is account_health.json as it is on disk, and whether it
// exists. A fresh install has none, and comparing only bytes would miss the
// file being created.
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

// A sweep in which every login fails with a network error must not bench any
// account: TestAccount's reportAccountFailure would take every debrid account
// out of routing for fifteen minutes. Asserted on the file, since that is what
// survives a restart.
func TestASweepWhoseLoginsAllFailNeverBenchesAnAccount(t *testing.T) {
	a := newSelfTestApp(t)

	// Seeded through the store rather than SetAccountCredential, so
	// rewireBackends never sees a routed credential.
	for _, svc := range []string{"alldebrid", "realdebrid"} {
		if err := a.Accounts.SetCredential(svc, "", accounts.Credential{APIKey: "fake-key"}); err != nil {
			t.Fatal(err)
		}
	}

	// A transport error is what ClassifyReason turns into a bench.
	selfTestCheckCredential = func(context.Context, string, accounts.Credential) (bool, int, error) {
		return false, 0, errors.New("dial tcp: lookup api.alldebrid.com: no such host")
	}

	beforePresent, beforeData := healthFileState(t, a)

	run := sweep(t, a)
	got := resultFor(t, run, selftest.CheckAccounts)
	if got.Status != selftest.StatusFail {
		t.Fatalf("the accounts check came back %q with every login refused, want %q; the logins did not run",
			got.Status, selftest.StatusFail)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("the accounts check reported %d rows for two configured logins", len(got.Rows))
	}

	afterPresent, afterData := healthFileState(t, a)
	if beforePresent != afterPresent {
		t.Fatalf("account_health.json went from present=%v to present=%v across a self-test; the sweep wrote to "+
			"the account-health store", beforePresent, afterPresent)
	}
	if string(beforeData) != string(afterData) {
		t.Fatalf("account_health.json changed across a self-test.\nbefore: %s\nafter:  %s", beforeData, afterData)
	}

	// The reading the dispatcher makes.
	for _, svc := range []string{"alldebrid", "realdebrid"} {
		if !a.acctHealthTracker().Usable(svc, "") {
			t.Errorf("%s is no longer routable after a self-test", svc)
		}
	}
}

// A failing row without the provider's words tells the operator nothing new.
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
		t.Fatalf("params = %v, want n=2 and bad=1 for the summary sentence", got.Params)
	}
	for _, row := range got.Rows {
		switch row.Code {
		case "accounts.rowOk":
			if row.Params["hosts"] != "91" {
				t.Errorf("the accepted row reports %q hosters, want 91", row.Params["hosts"])
			}
			if row.Params["label"] == "" {
				t.Error("the accepted row has no label for the sentence to name")
			}
		case "accounts.rowFailed":
			if row.Detail != "bad token" {
				t.Errorf("detail = %q, want the provider's own words verbatim", row.Detail)
			}
		default:
			t.Errorf("a row came back with code %q, which is neither of the two an account row can have", row.Code)
		}
	}
}

// "All logins work" about zero logins would be a meaningless green row.
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

// Missing, undated and old need different things from the reader, and only old
// means update.
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
				t.Error("no version in the params for the sentence to name")
			}
		})
	}
}

// There is no external port checker, so a configured port must not be reported
// as reachable.
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
		t.Fatalf("with a port configured, status = %q, want %q; a configured port is not a reachable one",
			got.Status, selftest.StatusUnknown)
	}
	if got.Code != "torrentPort.notChecked" || got.Params["port"] != "51413" {
		t.Fatalf("got (%s, %v), want torrentPort.notChecked naming port 51413", got.Code, got.Params)
	}
}

// The write probe has to run and leave nothing behind. The folders already
// exist, since a fresh install's default folder does not and would make the
// cleanup assertion vacuous.
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
			t.Errorf("%s reported as not there, though the test created it", row.ID)
			continue
		}
		probed++
		if row.Status == selftest.StatusFail {
			t.Errorf("%s reported as unwritable on a folder this process can write to: %s", row.ID, row.Detail)
		}
		if _, err := os.Stat(filepath.Join(row.Params["dir"], settings.WriteProbeName)); err == nil {
			t.Errorf("the write probe is still sitting in %s", row.Params["dir"])
		}
	}
	if probed != 2 {
		t.Fatalf("only %d of 2 folders were actually probed", probed)
	}
}

// A folder that is not there yet is reported, and the self-test never makes it.
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
		t.Errorf("status = %q, want %q", row.Status, selftest.StatusWarn)
	}
	if row.Params["measured"] == "" || row.Params["measured"] == dir {
		t.Errorf("measured = %q; the figures describe a folder above the one asked about and the row "+
			"has to say which", row.Params["measured"])
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("%s was created by a self-test", dir)
	}
}

// The relay mode defaults to "project", but nothing is dialled until a
// connection secret is stored, so a fresh install is not a fault.
func TestAFreshInstallsRelayRowIsNotAFault(t *testing.T) {
	a := newSelfTestApp(t)
	got := resultFor(t, sweep(t, a), selftest.CheckRelay)
	if got.Status != selftest.StatusSkipped {
		t.Fatalf("status = %q on an install that has never set up remote access, want %q", got.Status, selftest.StatusSkipped)
	}
	if got.Code != "relay.notSetUp" {
		t.Fatalf("code = %q, want relay.notSetUp", got.Code)
	}
}

// "My own relay" without an address is not the project relay and dials nothing.
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

// Overlapping sweeps would multiply provider logins, and a rate-limit refusal
// looks like a dead key.
func TestASecondPressJoinsTheSweepAlreadyRunning(t *testing.T) {
	a := newSelfTestApp(t)

	// The yt-dlp probe blocks, so the sweep is in flight during the second
	// press.
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
		t.Fatal("a second press started a second sweep while one was in flight")
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

	// Once finished, a press starts a new sweep.
	next, startedNext := a.SelfTestStart()
	if !startedNext || next.ID == first.ID {
		t.Fatalf("after the sweep finished, a press answered (started=%v, id=%q); it must begin a new sweep",
			startedNext, next.ID)
	}
	for a.SelfTestLatest().FinishedAt.IsZero() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// Before the first press the route answers with an empty run, and its lists
// must encode as [] rather than null.
func TestAnUnpressedInstanceAnswersAnEmptyRunRatherThanNothing(t *testing.T) {
	a := newSelfTestApp(t)
	run := a.SelfTestLatest()
	if run.ID != "" {
		t.Fatalf("id = %q before anything was ever run", run.ID)
	}
	if run.Results == nil || run.Planned == nil {
		t.Fatal("the empty run carries nil slices, which encode as JSON null")
	}
}

// The page draws one row per planned check and fills it in, so a check that
// never reports would wait forever.
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
			t.Errorf("%s reported with no code for the page to look up", id)
		}
		if got.At.IsZero() {
			t.Errorf("%s reported with no timestamp", id)
		}
	}
	// In Order, so two polls cannot hand the page a different row order.
	for i, r := range run.Results {
		if r.ID != selftest.Order[i] {
			t.Fatalf("result %d is %q, want %q; the list is kept in selftest.Order", i, r.ID, selftest.Order[i])
		}
	}
}
