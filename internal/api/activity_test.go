package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func postAbort(t *testing.T, base, kind string) (int, int) {
	t.Helper()
	resp, err := http.Post(base+"/api/activity/"+kind+"/abort", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Cancelled int `json:"cancelled"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out.Cancelled
}

// TestAbortActivityRouteIsRegistered is the check this table exists for: a
// route nobody attached answers with the SPA's index.html and a 200, which
// looks exactly like a route that worked. Aborting on an idle instance is a
// legitimate zero, so a JSON body with a count is what tells the two apart.
func TestAbortActivityRouteIsRegistered(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, cancelled := postAbort(t, srv.URL, "crawl")
	if code != http.StatusOK {
		t.Fatalf("POST /api/activity/crawl/abort = %d, want 200", code)
	}
	if cancelled != 0 {
		t.Errorf("an idle instance cancelled %d runs, want 0", cancelled)
	}
}

// TestAbortActivityRefusesAnUnknownKind keeps a typo from looking like an
// answer. "nothing was running" and "that is not a kind" are the same number,
// and only one of them is a client bug worth reporting.
func TestAbortActivityRefusesAnUnknownKind(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if code, _ := postAbort(t, srv.URL, "download"); code != http.StatusBadRequest {
		t.Errorf("POST /api/activity/download/abort = %d, want 400", code)
	}
}
