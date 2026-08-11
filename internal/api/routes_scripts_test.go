package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

func postScript(t *testing.T, url string, in scriptInput) (int, script.Script) {
	t.Helper()
	body, _ := json.Marshal(in)
	resp, err := http.Post(url+"/api/scripts", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out script.Script
	if resp.StatusCode == http.StatusCreated {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

func getScripts(t *testing.T, url string) []script.Script {
	t.Helper()
	resp, err := http.Get(url + "/api/scripts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []script.Script
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestScriptsLifecycleOverHTTP is the whole CRUD-plus-run story the editor
// (Scripts.tsx) and lib/scripts.ts drive: create, see it listed, edit it,
// test-run it, delete it, see it gone. Exercises every route
// registerScripts adds.
func TestScriptsLifecycleOverHTTP(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if got := getScripts(t, srv.URL); len(got) != 0 {
		t.Fatalf("a fresh instance already has %d scripts", len(got))
	}

	code, created := postScript(t, srv.URL, scriptInput{
		Name: "my script", Trigger: script.TriggerOnDemand, Enabled: true, Code: "1",
	})
	if code != http.StatusCreated {
		t.Fatalf("POST /api/scripts answered %d, want 201", code)
	}
	if created.ID == "" || created.Name != "my script" {
		t.Fatalf("created script = %+v", created)
	}

	listed := getScripts(t, srv.URL)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("GET /api/scripts = %+v, want the one script just created", listed)
	}

	// PUT: edit the name and code in place, same id.
	body, _ := json.Marshal(scriptInput{Name: "renamed", Trigger: script.TriggerOnDemand, Enabled: true, Code: "2"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/scripts/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var updated script.Script
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/scripts/{id} answered %d, want 200", resp.StatusCode)
	}
	if updated.ID != created.ID || updated.Name != "renamed" {
		t.Fatalf("updated script = %+v, want the same id and the new name", updated)
	}

	// Run it: no task, the toolbar Test Run case.
	runResp, err := http.Post(srv.URL+"/api/scripts/"+created.ID+"/run", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var result script.Result
	if err := json.NewDecoder(runResp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	runResp.Body.Close()
	if runResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/scripts/{id}/run answered %d, want 200", runResp.StatusCode)
	}
	if !result.OK {
		t.Fatalf("run result = %+v, want ok", result)
	}

	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/scripts/"+created.ID, nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE /api/scripts/{id} answered %d, want 204", delResp.StatusCode)
	}
	if got := getScripts(t, srv.URL); len(got) != 0 {
		t.Errorf("the script is still listed after being deleted: %+v", got)
	}
}

// TestScriptTriggersListsKnownTriggers is fetchScriptTriggers' whole reason
// to ask the server rather than hard-code the list (lib/scripts.ts's own
// doc comment): the registry answers with exactly what script.AllTriggers
// reports, not a hand-copied guess.
func TestScriptTriggersListsKnownTriggers(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/scripts/triggers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"task.done": true, "task.failed": true, "queue.idle": true, "manual": true}
	if len(got) != len(want) {
		t.Fatalf("GET /api/scripts/triggers = %v, want exactly %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected trigger %q", g)
		}
	}
}

// TestCreateScriptRefusesBadCode mirrors internal/script/store.go's own
// validate: a script that does not compile must never reach disk, the same
// way an empty API token name never reaches apitoken's store.
func TestCreateScriptRefusesBadCode(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, _ := postScript(t, srv.URL, scriptInput{
		Name: "broken", Trigger: script.TriggerOnDemand, Enabled: true, Code: "this is not valid javascript {{{",
	})
	if code != http.StatusBadRequest {
		t.Errorf("POST /api/scripts with unparsable code answered %d, want 400", code)
	}
	if got := getScripts(t, srv.URL); len(got) != 0 {
		t.Errorf("a refused create still produced a script: %+v", got)
	}
}

// TestRunUnknownScriptIs404 and TestDeleteUnknownScriptIs404 stop a client
// from believing a typo'd id ran or removed something, matching
// routes_tokens_test.go's TestRevokeUnknownTokenIs404.
func TestRunUnknownScriptIs404(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/scripts/does-not-exist/run", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("running an unknown script answered %d, want 404", resp.StatusCode)
	}
}

func TestDeleteUnknownScriptIs404(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/scripts/does-not-exist", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("deleting an unknown script answered %d, want 404", resp.StatusCode)
	}
}

// TestRunScriptWithUnknownTaskIs404 is the run route's other lookup: a
// taskId the app has no task for must not silently run with task absent
// from the sandbox, which would look identical to a toolbar-placed Test Run
// that never claimed to have a task at all.
func TestRunScriptWithUnknownTaskIs404(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	_, created := postScript(t, srv.URL, scriptInput{
		Name: "needs a task", Trigger: script.TriggerOnDemand, Enabled: true, Code: "1",
	})

	body, _ := json.Marshal(map[string]string{"taskId": "does-not-exist"})
	resp, err := http.Post(srv.URL+"/api/scripts/"+created.ID+"/run", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("running with an unknown taskId answered %d, want 404", resp.StatusCode)
	}
}

// TestManagingScriptsNeedsASession mirrors
// TestManagingTokensNeedsASession: script routes are not accidentally left
// open the way the login routes deliberately are.
func TestManagingScriptsNeedsASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/scripts")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/scripts with no session answered %d, want 401", resp.StatusCode)
	}
}
