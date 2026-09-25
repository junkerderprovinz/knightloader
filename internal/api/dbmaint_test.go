package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// maintServer attaches this file's routes and nothing else, like
// routes_ytdlpcookies_test.go's cookieServer. testServer would build the whole
// table through registerAll; whether this subsystem is on that list is
// TestEverySubsystemIsRegistered's question, and these tests are about the
// handlers.
func maintServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerDBMaintenance(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, a
}

// getMaintenance reads the state and hands back the raw body too, since a test
// looking for something that must not be in the document at all needs the
// bytes rather than the struct.
func getMaintenance(t *testing.T, base string) (int, app.MaintenanceState, []byte) {
	t.Helper()
	resp, err := http.Get(base + "/api/system/maintenance")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp) // closes resp.Body (federation_test.go)
	if err != nil {
		t.Fatal(err)
	}
	var st app.MaintenanceState
	if len(raw) > 0 && raw[0] == '{' {
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatalf("decoding the maintenance state: %v (%s)", err, raw)
		}
	}
	return resp.StatusCode, st, raw
}

// postMaintenance starts a pass and hands back what the route said.
func postMaintenance(t *testing.T, base, action string) (int, app.MaintenanceState, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"action": action})
	resp, err := http.Post(base+"/api/system/maintenance", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp)
	if err != nil {
		t.Fatal(err)
	}
	var st app.MaintenanceState
	if len(raw) > 0 && raw[0] == '{' {
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatalf("decoding the answer to %q: %v (%s)", action, err, raw)
		}
	}
	return resp.StatusCode, st, raw
}

// waitIdle polls the route the way the page does, and gives up rather than
// hanging a test run.
func waitIdle(t *testing.T, base string) app.MaintenanceState {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		code, st, raw := getMaintenance(t, base)
		if code != http.StatusOK {
			t.Fatalf("GET answered %d: %s", code, raw)
		}
		if st.Running == "" {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("a %s pass was still running after a minute", st.Running)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// seedBigRows puts a real amount of content in the database in few writes: a
// handful of rows with a large comment each rather than thousands of small
// ones. It makes a compaction take long enough to still be running when the
// next request arrives (TestASecondStartIsAConflict) without spending ten
// seconds of the run on inserts.
func seedBigRows(t *testing.T, a *app.App, rows, sizeEach int) {
	t.Helper()
	padding := strings.Repeat("z", sizeEach)
	for i := 0; i < rows; i++ {
		if err := a.Store.Save(&core.Task{
			ID:        fmt.Sprintf("big-%03d", i),
			URL:       "https://host.example/big.bin",
			Name:      "big.bin",
			Comment:   padding,
			Status:    core.StatusPaused,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMaintenanceReportsRealSizes: every figure here is a measurement rather
// than a placeholder, and a zero renders as a confident claim about somebody's
// disk.
func TestMaintenanceReportsRealSizes(t *testing.T) {
	t.Parallel()
	srv, a := maintServer(t)
	seedBigRows(t, a, 4, 64<<10)

	code, st, raw := getMaintenance(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("GET /api/system/maintenance answered %d: %s", code, raw)
	}
	if st.Storage.StoreBytes <= 0 {
		t.Errorf("storeBytes = %d for a database with content in it", st.Storage.StoreBytes)
	}
	if st.Storage.StorePath == "" {
		t.Error("storePath is empty; the message that tells somebody which file to copy aside would name nothing")
	}
	if st.Storage.TempDir == "" {
		t.Error("tempDir is empty; the compaction's scratch copy would be reported as going nowhere")
	}
	if st.Running != "" {
		t.Errorf("running = %q on an instance where nothing was started", st.Running)
	}
	if st.Last != nil {
		t.Errorf("a fresh instance reports a last run: %+v", *st.Last)
	}
	if st.NextRunAt != nil {
		t.Errorf("a fresh instance reports a next run while the interval is 0: %v", *st.NextRunAt)
	}
	// The paths belong on this route and nowhere else, so it has to carry
	// them; the diagnostics test asserts the other half.
	if !bytes.Contains(raw, []byte("storePath")) || !bytes.Contains(raw, []byte("settingsPath")) {
		t.Errorf("the maintenance document has no paths in it: %s", raw)
	}
}

// TestCheckRunsAndReportsThroughTheRoute walks the loop the page performs:
// start, poll, read the verdict.
func TestCheckRunsAndReportsThroughTheRoute(t *testing.T) {
	t.Parallel()
	srv, a := maintServer(t)
	seedBigRows(t, a, 4, 32<<10)

	code, st, raw := postMaintenance(t, srv.URL, "check")
	if code != http.StatusAccepted {
		t.Fatalf("POST check answered %d, want 202: %s", code, raw)
	}
	// 202 means accepted and not finished, so the document that comes back
	// has to say what was accepted, or the page makes a second call to find
	// out what it started.
	if st.Running != "check" && st.Last == nil {
		t.Errorf("the 202 said neither what is running nor what finished: %s", raw)
	}

	done := waitIdle(t, srv.URL)
	if done.Last == nil {
		t.Fatal("nothing was recorded after a check")
	}
	if !done.Last.OK || done.Last.Kind != "check" {
		t.Errorf("a healthy database came back as %+v", *done.Last)
	}
	if done.Last.Problems == nil {
		t.Error("problems is null in the JSON rather than an empty list")
	}
}

// TestASecondStartIsAConflict keeps two rewrites from queueing on one
// connection. 409 and not 429, since this is a conflict with a run that is
// happening, and the body says which so the page need not invent a message.
//
// The window is made real rather than hoped for: twenty megabytes of live rows
// keeps the compaction copying for hundreds of milliseconds while the second
// request is one round trip on a loopback socket. The claim is taken inside
// StartMaintenance before the 202 is written, so the second request cannot
// arrive early.
func TestASecondStartIsAConflict(t *testing.T) {
	t.Parallel()
	srv, a := maintServer(t)
	// Kept rather than deleted: a compaction's cost is the live bytes it
	// copies, so a database that is mostly free list would be rewritten
	// instantly and this test would have nothing to observe.
	seedBigRows(t, a, 40, 512<<10)

	code, first, raw := postMaintenance(t, srv.URL, "compact")
	if code != http.StatusAccepted {
		t.Fatalf("POST compact answered %d, want 202: %s", code, raw)
	}
	if first.Running != "compact" {
		t.Fatalf("the 202 does not say a compaction is running (%+v); the rest of this test cannot mean anything", first)
	}

	code, busy, raw := postMaintenance(t, srv.URL, "check")
	if code != http.StatusConflict {
		t.Fatalf("starting a check while a compaction was running answered %d, want 409: %s", code, raw)
	}
	if busy.Running != "compact" {
		t.Errorf("the 409 does not say what is running (%+v); the page could only show a generic error", busy)
	}

	done := waitIdle(t, srv.URL)
	if done.Last == nil || done.Last.Kind != "compact" {
		t.Fatalf("the compaction did not finish and record itself: %+v", done.Last)
	}
	if !done.Last.OK {
		t.Errorf("the compaction failed: %+v", *done.Last)
	}
}

// TestAnUnknownActionIsRefused keeps the route from answering 202 to a typo
// and then doing nothing, which is where a free-text verb ends up.
func TestAnUnknownActionIsRefused(t *testing.T) {
	t.Parallel()
	srv, _ := maintServer(t)
	for _, action := range []string{"", "vacuum", "Compact", "drop"} {
		code, _, raw := postMaintenance(t, srv.URL, action)
		if code != http.StatusBadRequest {
			t.Errorf("POST %q answered %d, want 400: %s", action, code, raw)
		}
		if !bytes.Contains(raw, []byte("check")) {
			t.Errorf("the refusal of %q does not name the actions that would work: %s", action, raw)
		}
	}

	// A body that is not JSON at all is the same class of mistake and gets the
	// same answer rather than a panic in the decoder.
	resp, err := http.Post(srv.URL+"/api/system/maintenance", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := readAll(resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST with a broken body answered %d, want 400: %s", resp.StatusCode, raw)
	}
}

// TestMaintenanceNeedsASession states the ordinary rule for everything under
// /api/ here, because this route sends the data directory's real paths and
// starts work that freezes every write on the box.
func TestMaintenanceNeedsASession(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	reg := newRegistry()
	registerDBMaintenance(reg, a)
	for _, r := range reg.Routes() {
		if r.Open {
			t.Errorf("%s %s is registered open; anybody who can reach the port could start a compaction", r.Method, r.Path)
		}
	}
}
