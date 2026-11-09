package fortress

import (
	"strconv"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

func done(name string, at time.Time) script.Firing {
	return script.Firing{Trigger: script.TriggerTaskDone, At: at, Task: &script.TaskView{Name: name}}
}

func TestLogPagesOldestFirstWithACursor(t *testing.T) {
	l := NewLog(10)
	for i := range 3 {
		l.On(done("f"+strconv.Itoa(i), now.Add(time.Duration(i)*time.Second)))
	}
	first := l.Page(status.LogQuery{Limit: 2, Lang: "en"})
	if len(first.Entries) != 2 || first.Entries[0].Text != "f0 is downloaded" || first.Entries[1].Text != "f1 is downloaded" {
		t.Fatalf("first page = %+v", first.Entries)
	}
	if first.Next != first.Entries[1].ID {
		t.Errorf("next = %q, want the last id %q", first.Next, first.Entries[1].ID)
	}
	second := l.Page(status.LogQuery{After: first.Next, Limit: 2, Lang: "en"})
	if len(second.Entries) != 1 || second.Entries[0].Text != "f2 is downloaded" {
		t.Fatalf("second page = %+v", second.Entries)
	}
	end := l.Page(status.LogQuery{After: second.Next, Limit: 2})
	if len(end.Entries) != 0 || end.Next != second.Next {
		t.Errorf("end = %+v, want nothing and the same cursor", end)
	}
}

func TestLogKeepsTheNewestEntries(t *testing.T) {
	l := NewLog(2)
	for i := range 5 {
		l.On(done("f"+strconv.Itoa(i), now))
	}
	page := l.Page(status.LogQuery{Limit: 10, Lang: "en"})
	if len(page.Entries) != 2 || page.Entries[0].Text != "f3 is downloaded" || page.Entries[1].Text != "f4 is downloaded" {
		t.Errorf("page = %+v", page.Entries)
	}
}

func TestACursorFromAnotherRunStartsAtTheOldestEntry(t *testing.T) {
	before := NewLog(10)
	before.On(done("old", now))
	cursor := before.Page(status.LogQuery{Limit: 10}).Next

	l := NewLog(10)
	l.On(done("new", now))
	page := l.Page(status.LogQuery{After: cursor, Limit: 10, Lang: "en"})
	if len(page.Entries) != 1 || page.Entries[0].Text != "new is downloaded" {
		t.Errorf("page = %+v, want the entry of this run", page.Entries)
	}
	if got := l.Page(status.LogQuery{After: "rubbish", Limit: 10}); len(got.Entries) != 1 {
		t.Errorf("an unreadable cursor gave %d entries, want 1", len(got.Entries))
	}
}

func TestLogTextsFollowTheLanguage(t *testing.T) {
	l := NewLog(10)
	l.On(done("film.mkv", now))
	en := l.Page(status.LogQuery{Limit: 1, Lang: "en"}).Entries[0]
	de := l.Page(status.LogQuery{Limit: 1, Lang: "de"}).Entries[0]
	if en.ID != de.ID || en.Text == de.Text {
		t.Errorf("en %+v, de %+v", en, de)
	}
}

func TestLogDescribesEveryEvent(t *testing.T) {
	cases := []struct {
		f     script.Firing
		level status.Severity
		code  string
		text  string
	}{
		{script.Firing{Trigger: script.TriggerTaskFailed, Task: &script.TaskView{Name: "a"}}, status.SeverityError, "download.failed", "a failed"},
		{script.Firing{Trigger: script.TriggerQueueIdle}, status.SeverityInfo, "queue.idle", "Nothing left to download"},
		{script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "P", Files: 3, Done: 3}}, status.SeverityInfo, "package.done", "Package P is complete"},
		{script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "P", Files: 3, Done: 1, Failed: 2}}, status.SeverityWarn, "package.failed", "Package P is through, 2 of its files failed"},
		{script.Firing{Trigger: script.TriggerExtractDone, Extract: &script.ExtractView{Name: "a.rar", OK: true}}, status.SeverityInfo, "unpack.done", "a.rar is unpacked"},
		{script.Firing{Trigger: script.TriggerExtractDone, Extract: &script.ExtractView{Name: "a.rar", Package: "P"}}, status.SeverityError, "unpack.failed", "P could not be unpacked"},
		{script.Firing{Trigger: script.TriggerChecksumFailed, Task: &script.TaskView{Name: "a"}}, status.SeverityError, "checksum.failed", "The checksum of a does not match"},
		{script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &script.ReconnectView{OK: true, Changed: true}}, status.SeverityInfo, "reconnect.done", "Reconnected with a new IP address"},
		{script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &script.ReconnectView{OK: true}}, status.SeverityInfo, "reconnect.done", "Reconnected, the IP address stayed the same"},
		{script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &script.ReconnectView{}}, status.SeverityWarn, "reconnect.failed", "The reconnect failed"},
		{script.Firing{Trigger: script.TriggerAccountExpired, Account: &script.AccountView{Service: "realdebrid", Label: "Home"}}, status.SeverityWarn, "account.expired", "The Home account has expired"},
		{script.Firing{Trigger: script.TriggerCaptchaPending, Captcha: &script.CaptchaView{Host: "rapidgator.net"}}, status.SeverityWarn, "captcha.waiting", "A captcha for rapidgator.net is waiting"},
	}
	for _, c := range cases {
		l := NewLog(10)
		l.On(c.f)
		page := l.Page(status.LogQuery{Limit: 10, Lang: "en"})
		if len(page.Entries) != 1 {
			t.Errorf("%s: %d entries", c.f.Trigger, len(page.Entries))
			continue
		}
		e := page.Entries[0]
		if e.Level != c.level || e.Code != c.code || e.Text != c.text {
			t.Errorf("%s: got %s %s %q, want %s %s %q", c.f.Trigger, e.Level, e.Code, e.Text, c.level, c.code, c.text)
		}
	}
}

func TestLogLeavesOutAddedLinks(t *testing.T) {
	l := NewLog(10)
	l.On(script.Firing{Trigger: script.TriggerLinkAdded, Task: &script.TaskView{Name: "a"}})
	if got := l.Page(status.LogQuery{Limit: 10}); len(got.Entries) != 0 {
		t.Errorf("an added link was logged: %+v", got.Entries)
	}
}
