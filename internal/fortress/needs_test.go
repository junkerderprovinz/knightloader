package fortress

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func ids(ns []need) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.id
	}
	return out
}

func failedTask(id, pkg string) *core.Task {
	return &core.Task{ID: id, Name: id + ".mkv", Package: pkg, Status: core.StatusError, Enabled: true}
}

func TestNeedsListCaptchasFirstThenFailuresThenWarnings(t *testing.T) {
	in := Inputs{
		Now:      now,
		Tasks:    []*core.Task{failedTask("t1", "")},
		Captchas: []captcha.Challenge{{ID: "c1", Host: "rapidgator.net"}},
		Accounts: []Account{{ID: "a1", Name: "Real-Debrid", Enabled: true, Configured: true, Expiry: now.Add(48 * time.Hour).Format(time.RFC3339)}},
	}
	got := ids(needs(in))
	want := []string{"captcha:c1", "failed:t1", "expiry:a1:" + in.Accounts[0].Expiry}
	if len(got) != len(want) {
		t.Fatalf("needs = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("needs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNeedsSkipATestCaptcha(t *testing.T) {
	if got := needs(Inputs{Now: now, Captchas: []captcha.Challenge{{ID: "c1", Test: true}}}); len(got) != 0 {
		t.Errorf("a test captcha became %q", ids(got))
	}
}

func TestFailedDownloadsOfOnePackageAreOneNeed(t *testing.T) {
	got := needs(Inputs{Now: now, Tasks: []*core.Task{failedTask("t1", "Film"), failedTask("t2", "Film"), failedTask("t3", "")}})
	if len(got) != 2 {
		t.Fatalf("needs = %q, want one for the package and one for the loose link", ids(got))
	}
	if got[0].key != "fortress.need.failedMany" || got[0].args["n"] != "2" || got[0].args["name"] != "Film" {
		t.Errorf("package need = %+v", got[0])
	}
	if got[1].key != "fortress.need.failed" || got[1].args["name"] != "t3.mkv" {
		t.Errorf("loose need = %+v", got[1])
	}
}

func TestAFailureWithARetryComingIsNoNeed(t *testing.T) {
	waiting := failedTask("t1", "")
	waiting.NextTry = now.Add(time.Minute)
	due := failedTask("t2", "")
	due.NextTry = now.Add(-30 * time.Second)
	off := failedTask("t3", "")
	off.Enabled = false
	gaveUp := failedTask("t4", "")
	gaveUp.NextTry, gaveUp.GaveUp = now.Add(time.Minute), true

	got := ids(needs(Inputs{Now: now, Tasks: []*core.Task{waiting, due, off, gaveUp}}))
	if len(got) != 1 || got[0] != "failed:t4" {
		t.Errorf("needs = %q, want only the download that gave up", got)
	}
}

func TestDoneSetsAFailureAside(t *testing.T) {
	in := Inputs{Now: now, Tasks: []*core.Task{failedTask("t1", "P"), failedTask("t2", "P")}, Done: map[string]bool{"task:t1": true}}
	got := needs(in)
	if len(got) != 1 || got[0].key != "fortress.need.failed" || got[0].args["name"] != "P" {
		t.Errorf("needs = %+v, want the one download still open", got)
	}
}

func TestUnpackFailures(t *testing.T) {
	broken := &core.Task{ID: "t1", Name: "a.rar", Status: core.StatusDone, Enabled: true, Unpack: core.UnpackFailed}
	locked := &core.Task{ID: "t2", Name: "b.rar", Package: "B", Status: core.StatusDone, Enabled: true, Unpack: core.UnpackPassword}
	got := needs(Inputs{Now: now, Tasks: []*core.Task{broken, locked}})
	if len(got) != 2 || got[0].code != "unpack.failed" || got[1].code != "unpack.password" {
		t.Fatalf("needs = %+v", got)
	}
	if got[1].args["name"] != "B" {
		t.Errorf("a package is named by its package, got %q", got[1].args["name"])
	}
}

func TestAccountsAndLogins(t *testing.T) {
	in := Inputs{
		Now: now,
		Accounts: []Account{
			{ID: "a1", Name: "AllDebrid", Enabled: true, Configured: true, Detail: "401"},
			{ID: "a2", Name: "Off", Enabled: false, Configured: true, Detail: "401"},
			{ID: "a3", Name: "Later", Enabled: true, Configured: true, Expiry: now.Add(30 * 24 * time.Hour).Format(time.RFC3339)},
		},
		Logins: []hosterauth.LoginState{
			{Host: "rapidgator.net", Enabled: true, Status: "rejected"},
			{Host: "ddownload.com", Enabled: true, Expiry: now.Add(time.Hour).Format(time.RFC3339)},
		},
	}
	got := ids(needs(in))
	want := []string{"account:a1", "login:rapidgator.net", "expiry:login:ddownload.com:" + in.Logins[1].Expiry}
	if len(got) != len(want) {
		t.Fatalf("needs = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("needs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDiskFloors(t *testing.T) {
	in := Inputs{
		Now:      now,
		DiskLow:  100,
		DiskStop: 10,
		Volumes: []Volume{
			{Dir: "/a", Free: 5, Known: true},
			{Dir: "/a", Free: 5, Known: true},
			{Dir: "/b", Free: 50, Known: true},
			{Dir: "/c", Free: 500, Known: true},
			{Dir: "/d", Known: false},
		},
	}
	got := needs(in)
	if len(got) != 2 || got[0].id != "disk:stop:/a" || got[0].severity != status.SeverityError || got[1].id != "disk:low:/b" {
		t.Errorf("needs = %+v", got)
	}
}

func TestVolumeCap(t *testing.T) {
	start := now.Add(-24 * time.Hour)
	got := needs(Inputs{Now: now, Cap: Cap{Reached: true, PeriodStart: start}})
	if len(got) != 1 || got[0].id != "volumeCap:"+start.Format(time.RFC3339Nano) {
		t.Errorf("needs = %+v", got)
	}
}

func TestHealthPartsLeaveOutWhatOtherNeedsSay(t *testing.T) {
	in := Inputs{
		Now: now,
		Parts: []Part{
			{ID: "relay", State: "degraded", Remedy: "relay.down"},
			{ID: "captcha", State: "degraded", Remedy: "captcha.waiting"},
			{ID: "disk", State: "failed", Remedy: "disk.low"},
			{ID: "jd", State: "failed", Remedy: "jd.unreachable"},
			{ID: "ytdlp", State: "unused", Remedy: "ytdlp.missing"},
			{ID: "accounts", State: "failed", Remedy: "accounts.invalid"},
		},
		Accounts: []Account{{ID: "a1", Name: "X", Enabled: true, Configured: true, Detail: "bad"}},
	}
	got := ids(needs(in))
	want := []string{"account:a1", "health:jd:jd.unreachable", "health:relay:relay.down"}
	if len(got) != len(want) {
		t.Fatalf("needs = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("needs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHealthCodesAreSpelledAsTheContractWants(t *testing.T) {
	ns := needs(Inputs{Now: now, Parts: []Part{{ID: "jd", State: "degraded", Remedy: "jd.notWired"}}})
	if ns[0].code != "jd.not_wired" {
		t.Errorf("code = %q, want jd.not_wired", ns[0].code)
	}
}

func TestNeedTextsFollowTheLanguage(t *testing.T) {
	ns := needs(Inputs{Now: now, Parts: []Part{{ID: "relay", State: "degraded", Remedy: "relay.down"}}})
	en, de := ns[0].render("en"), ns[0].render("de")
	if en.Text != "Relay · Working, with a fault" {
		t.Errorf("en text = %q", en.Text)
	}
	if de.Text == en.Text {
		t.Errorf("de text is the English one: %q", de.Text)
	}
	if en.Code != "relay.down" || en.Severity != status.SeverityWarn || en.Link != "/settings/health" {
		t.Errorf("need = %+v", en)
	}
}
