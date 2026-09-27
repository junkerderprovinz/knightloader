package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func appearanceCall(t *testing.T, method, url string, body []byte) map[string]any {
	t.Helper()
	req, err := http.NewRequest(method, url+"/api/appearance", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s /api/appearance answered %d", method, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The app reads the list cards from here, since /api/settings is not on the
// relay's allowlist.
func TestAppearanceCarriesTheListCards(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	got := appearanceCall(t, http.MethodGet, srv.URL, nil)
	if got["seedingCard"] != true || got["finishedCard"] != true {
		t.Fatalf("a fresh install answers seedingCard %v, finishedCard %v, want both true", got["seedingCard"], got["finishedCard"])
	}

	got = appearanceCall(t, http.MethodPost, srv.URL, []byte(`{"finishedCard": false}`))
	if got["finishedCard"] != false || got["seedingCard"] != true {
		t.Errorf("after switching the finished card off the answer is %v", got)
	}
	if s := a.Settings.Get(); s.FinishedCard || !s.SeedingCard {
		t.Errorf("stored finishedCard %v, seedingCard %v", s.FinishedCard, s.SeedingCard)
	}
}
