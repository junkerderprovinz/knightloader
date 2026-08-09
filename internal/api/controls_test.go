package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// postControls sends a patch and hands back the status with either the decoded
// controls or the plain-text refusal.
func postControls(t *testing.T, base string, patch map[string]any) (int, controls, string) {
	t.Helper()
	b, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(base+"/api/controls", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		_, _ = msg.ReadFrom(resp.Body)
		return resp.StatusCode, controls{}, strings.TrimSpace(msg.String())
	}
	var got controls
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, got, ""
}

// TestControlsPatchLeavesTheRestOfTheConfigurationAlone is the whole reason this
// route exists rather than the shell reusing PUT /api/settings.
//
// The failure it prevents is not hypothetical: a widget that lives above every
// page holds whatever the configuration was when it mounted, and a PUT replaces
// the document — so one click on a spinner in the shell would put back the
// download folder, the rule sets and the timetable as they were an hour ago.
func TestControlsPatchLeavesTheRestOfTheConfigurationAlone(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	// Something in the configuration that the quick panel neither shows nor knows
	// about, and that a whole-document write would therefore lose.
	before := a.Settings.Get()
	before.DownloadDir = t.TempDir()
	before.MaxRetries = 11
	before.WatchDir = ""
	if _, err := a.ApplySettings(before); err != nil {
		t.Fatal(err)
	}

	code, got, msg := postControls(t, srv.URL, map[string]any{"maxConcurrent": 7})
	if code != http.StatusOK {
		t.Fatalf("patching one number answered %d: %s", code, msg)
	}
	if got.MaxConcurrent != 7 {
		t.Errorf("maxConcurrent = %d, want 7", got.MaxConcurrent)
	}

	after := a.Settings.Get()
	if after.MaxRetries != 11 {
		t.Errorf("maxRetries = %d, want the stored 11: the patch replaced fields it was not sent", after.MaxRetries)
	}
	if after.DownloadDir != before.DownloadDir {
		t.Errorf("downloadDir = %q, want the stored %q", after.DownloadDir, before.DownloadDir)
	}
}

// TestControlsTellZeroApartFromAbsent keeps the two answers the pointer fields
// exist for. Zero is a real value here — no speed limit, no opinion about chunks
// — so a patch that sends it has to be obeyed, and one that omits the field has
// to leave it standing.
func TestControlsTellZeroApartFromAbsent(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	s := a.Settings.Get()
	s.SpeedLimit = 5 << 20
	s.Chunks = 8
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	if _, got, msg := postControls(t, srv.URL, map[string]any{"speedLimit": 0}); got.SpeedLimit != 0 {
		t.Errorf("speedLimit = %d after being sent 0, want the limit lifted (%s)", got.SpeedLimit, msg)
	}
	if got := a.Settings.Get(); got.Chunks != 8 {
		t.Errorf("chunks = %d, want the stored 8: an omitted field was written anyway", got.Chunks)
	}
}

// TestControlsRefuseAChunkCountTheEngineWouldCut refuses instead of clamping.
// connsFor cuts an over-large count at dispatch, so a value accepted here would
// be stored, read back into the spinner, and then not honoured by the only thing
// it was set for.
func TestControlsRefuseAChunkCountTheEngineWouldCut(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, _, msg := postControls(t, srv.URL, map[string]any{"chunks": rules.MaxChunks + 1})
	if code != http.StatusBadRequest {
		t.Fatalf("a chunk count past the engine's ceiling answered %d, want 400", code)
	}
	if !strings.Contains(msg, "outside 0..") {
		t.Errorf("the refusal does not say what the bound is: %q", msg)
	}
}

// TestControlsAnswerWithWhatWasStored is what lets the interface carry no copy of
// the concurrency bounds: the number the user typed is sanitized on the way to
// disk and the answer is the sanitized one, so the field settles on the truth
// rather than on a limit compiled into the client.
func TestControlsAnswerWithWhatWasStored(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	_, got, _ := postControls(t, srv.URL, map[string]any{"maxConcurrent": 999})
	if got.MaxConcurrent == 999 {
		t.Fatalf("maxConcurrent came back as 999; sanitizeQueue caps it and the answer has to say so")
	}
	if got.MaxChunks != rules.MaxChunks {
		t.Errorf("maxChunks = %d, want the engine's own %d", got.MaxChunks, rules.MaxChunks)
	}
	// The reply has to describe the store, not the request, or the spinner shows a
	// number nothing downstream agrees with.
	if got.MaxPerHost != settings.Defaults().MaxPerHost {
		t.Errorf("maxPerHost = %d, want the untouched default %d", got.MaxPerHost, settings.Defaults().MaxPerHost)
	}
}
