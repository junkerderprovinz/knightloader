package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

func TestAMemberReadsAndClearsTheLinksThatWereNotAdded(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	link := "https://user:SKIPPEDPW@host.example/film.mkv?passkey=SKIPPEDLEAK"
	for range 2 {
		if _, err := a.AddLinksWithOptions([]string{link}, "", app.OriginPaste, app.LinkBatchOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.SkippedLinks()) == 0 {
		t.Fatal("the second paste was not folded, so this test proves nothing")
	}

	status, body := member(http.MethodGet, "/api/collector/skipped", "")
	if status != http.StatusOK {
		t.Fatalf("a member reading the links not added = %d (%s), want 200", status, body)
	}
	var skipped []app.SkippedLink
	if err := json.Unmarshal(body, &skipped); err != nil || len(skipped) == 0 {
		t.Fatalf("the answer %s holds no folded link (%v)", body, err)
	}
	for _, leak := range []string{"SKIPPEDPW", "SKIPPEDLEAK"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("a member was shown %q: %s", leak, body)
		}
	}
	if !strings.Contains(skipped[0].URL, "host.example") {
		t.Errorf("the folded link lost its host: %q", skipped[0].URL)
	}

	if status, body := member(http.MethodDelete, "/api/collector/skipped", ""); status != http.StatusNoContent {
		t.Fatalf("a member clearing them = %d (%s), want 204", status, body)
	}
	if n := len(a.SkippedLinks()); n != 0 {
		t.Errorf("%d links still noted after a member cleared them", n)
	}
}

// heldBy puts a link filter rule on the instance and stages links it rejects.
func heldBy(t *testing.T, a *app.App, urls ...string) []*core.Task {
	t.Helper()
	s := a.Settings.Get()
	s.LinkFilter = rules.Set{Rules: []rules.Rule{{
		Name:       "no samples",
		Conditions: []rules.Condition{{Field: rules.FieldURL, Op: rules.OpContains, Value: "sample"}},
		Action:     rules.Action{Reject: true, Reason: "sample files are not wanted here"},
	}}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	held := a.AddLinks(urls, "Batch")
	if len(held) != len(urls) || !held[0].Skipped {
		t.Fatalf("held %d of %d links", len(held), len(urls))
	}
	return held
}

func TestAMemberRestoresAndClearsTheRejectedLinks(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	held := heldBy(t, a, "https://host.example/sample-one.mkv", "https://host.example/sample-two.mkv")

	// The phone reads them off the task list it already polls.
	status, body := member(http.MethodGet, "/api/tasks", "")
	if status != http.StatusOK {
		t.Fatalf("a member reading the tasks = %d (%s)", status, body)
	}
	var tasks []core.Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Fatal(err)
	}
	listed := 0
	for _, task := range tasks {
		if task.Skipped && task.SkipReason != "" {
			listed++
		}
	}
	if listed != 2 {
		t.Errorf("the task list shows a member %d rejected links with their reason, want 2", listed)
	}

	status, body = member(http.MethodPost, "/api/collector/filtered/restore", `{"ids":["`+held[0].ID+`"]}`)
	if status != http.StatusOK {
		t.Fatalf("a member restoring a rejected link = %d (%s), want 200", status, body)
	}
	if status, body := member(http.MethodDelete, "/api/collector/filtered?ids="+held[1].ID, ""); status != http.StatusOK {
		t.Fatalf("a member clearing a rejected link = %d (%s), want 200", status, body)
	}
	if n := len(a.FilteredLinks()); n != 0 {
		t.Errorf("%d links still held after a member restored one and cleared the other", n)
	}
	restored := a.TasksOf([]string{held[0].ID})
	if len(restored) != 1 || restored[0].Skipped {
		t.Errorf("the restored link is %+v, want it back in the list", restored)
	}
}

// The phone reads the rejected links off the task list, so the route that
// lists them for other clients stays closed.
func TestAMemberIsNotServedTheHoldingAreaOnItsOwn(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	heldBy(t, a, "https://host.example/sample.mkv")
	if status, body := member(http.MethodGet, "/api/collector/filtered", ""); !refusedForGroup(status, body) {
		t.Errorf("a member reading the holding area = %d (%s), want ownLoginNeeded", status, body)
	}
}
