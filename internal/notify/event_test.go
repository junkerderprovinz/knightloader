package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	kit "github.com/junkerderprovinz/parleyport/kit/notify"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

func TestEventTellsWhatHappened(t *testing.T) {
	f := script.Firing{Trigger: script.TriggerTaskFailed, Task: &script.TaskView{Name: "a.mkv", Host: "1fichier.com", Error: "gone"}}
	e := Event(f, "Tower")
	if e.Kind != "task.failed" || e.Severity != kit.SeverityFailure || e.Title != "Download failed" || e.Body != "a.mkv" {
		t.Errorf("event = %+v", e)
	}
	if e.Fields["task.host"] != "1fichier.com" || e.Fields["instance"] != "Tower" || e.Fields["package.name"] != "" {
		t.Errorf("fields = %v", e.Fields)
	}
}

func TestSeverityFollowsTheOutcome(t *testing.T) {
	cases := []struct {
		f    script.Firing
		want kit.Severity
	}{
		{script.Firing{Trigger: script.TriggerTaskDone}, kit.SeveritySuccess},
		{script.Firing{Trigger: script.TriggerLinkAdded}, kit.SeverityInfo},
		{script.Firing{Trigger: script.TriggerQueueIdle}, kit.SeverityInfo},
		{script.Firing{Trigger: script.TriggerChecksumFailed}, kit.SeverityFailure},
		{script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Failed: 1}}, kit.SeverityFailure},
		{script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{}}, kit.SeveritySuccess},
		{script.Firing{Trigger: script.TriggerExtractDone, Extract: &script.ExtractView{}}, kit.SeverityFailure},
		{script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &script.ReconnectView{OK: true}}, kit.SeveritySuccess},
		{script.Firing{Trigger: script.TriggerCaptchaPending}, kit.SeverityWarning},
		{script.Firing{Trigger: script.TriggerAccountExpired}, kit.SeverityWarning},
	}
	for _, c := range cases {
		if got := severity(c.f); got != c.want {
			t.Errorf("%s: %s, want %s", c.f.Trigger, got, c.want)
		}
	}
}

func TestTestSendsToADisabledDraft(t *testing.T) {
	hit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit <- struct{}{} }))
	defer srv.Close()
	r := Test(context.Background(), kit.Channel{ID: "1", Kind: kit.KindNtfy, Ntfy: &kit.Ntfy{URL: srv.URL + "/topic"}}, "Tower", nil)
	if !r.OK() {
		t.Fatalf("result = %+v", r)
	}
	select {
	case <-hit:
	default:
		t.Error("nothing was sent")
	}
}
