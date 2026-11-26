package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// readLimits reads an answer of the queue limits route, failing on any key
// besides the two it serves.
func readLimits(t *testing.T, body []byte) (speedLimit int64, maxConcurrent int) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("the limits answer %s is no object: %v", body, err)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"maxConcurrent", "speedLimit"}) {
		t.Fatalf("the limits answer carries %v, want speedLimit and maxConcurrent alone", keys)
	}
	var out struct {
		SpeedLimit    int64 `json:"speedLimit"`
		MaxConcurrent int   `json:"maxConcurrent"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.SpeedLimit, out.MaxConcurrent
}

func TestAMemberReadsAndSetsTheSpeedLimitAndTheParallelDownloads(t *testing.T) {
	t.Parallel()
	a, _, _, member, owner := groupServer(t)

	status, body := member(http.MethodGet, "/api/queue/limits", "")
	if status != http.StatusOK {
		t.Fatalf("a member reading the limits = %d (%s), want 200", status, body)
	}
	if speed, n := readLimits(t, body); speed != 0 || n != settings.Defaults().MaxConcurrent {
		t.Errorf("the limits read %d B/s and %d at once, want the stored 0 and %d", speed, n, settings.Defaults().MaxConcurrent)
	}

	status, body = member(http.MethodPut, "/api/queue/limits", `{"speedLimit":2500000,"maxConcurrent":6}`)
	if status != http.StatusOK {
		t.Fatalf("a member setting the limits = %d (%s), want 200", status, body)
	}
	if speed, n := readLimits(t, body); speed != 2500000 || n != 6 {
		t.Errorf("the answer reads %d B/s and %d at once, want 2500000 and 6", speed, n)
	}

	// The fields the web's quick settings write, so the web shows the change.
	status, body = owner(http.MethodGet, "/api/settings", "")
	if status != http.StatusOK {
		t.Fatalf("the owner reading the settings = %d (%s)", status, body)
	}
	var shown struct {
		SpeedLimit    int64 `json:"speedLimit"`
		MaxConcurrent int   `json:"maxConcurrent"`
	}
	if err := json.Unmarshal(body, &shown); err != nil {
		t.Fatal(err)
	}
	if shown.SpeedLimit != 2500000 || shown.MaxConcurrent != 6 {
		t.Errorf("the settings show %d B/s and %d at once, want 2500000 and 6", shown.SpeedLimit, shown.MaxConcurrent)
	}
	if got := a.Settings.Get(); got.SpeedLimit != 2500000 || got.MaxConcurrent != 6 {
		t.Errorf("stored %d B/s and %d at once, want 2500000 and 6", got.SpeedLimit, got.MaxConcurrent)
	}
}

func TestSettingOneLimitLeavesTheOther(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	if status, body := member(http.MethodPut, "/api/queue/limits", `{"maxConcurrent":3}`); status != http.StatusOK {
		t.Fatalf("setting the parallel downloads = %d (%s)", status, body)
	}
	if status, body := member(http.MethodPut, "/api/queue/limits", `{"speedLimit":1000}`); status != http.StatusOK {
		t.Fatalf("setting the speed limit = %d (%s)", status, body)
	}
	if got := a.Settings.Get(); got.SpeedLimit != 1000 || got.MaxConcurrent != 3 {
		t.Errorf("stored %d B/s and %d at once, want 1000 and 3", got.SpeedLimit, got.MaxConcurrent)
	}
}

// The bounds are the settings document's own, so the route and the settings
// page cannot store different values for the same input.
func TestTheLimitsKeepTheBoundsOfTheSettings(t *testing.T) {
	t.Parallel()
	_, _, _, member, owner := groupServer(t)
	for _, c := range []struct {
		body string
		key  string
	}{
		{`{"maxConcurrent":100}`, "maxConcurrent"},
		{`{"maxConcurrent":0}`, "maxConcurrent"},
		{`{"maxConcurrent":-3}`, "maxConcurrent"},
		{`{"speedLimit":-5}`, "speedLimit"},
	} {
		status, body := owner(http.MethodPatch, "/api/settings", c.body)
		if status != http.StatusOK {
			t.Fatalf("the owner patching %s = %d (%s)", c.body, status, body)
		}
		var viaSettings map[string]json.RawMessage
		if err := json.Unmarshal(body, &viaSettings); err != nil {
			t.Fatal(err)
		}
		status, body = member(http.MethodPut, "/api/queue/limits", c.body)
		if status != http.StatusOK {
			t.Fatalf("a member putting %s = %d (%s)", c.body, status, body)
		}
		var viaLimits map[string]json.RawMessage
		if err := json.Unmarshal(body, &viaLimits); err != nil {
			t.Fatal(err)
		}
		if string(viaLimits[c.key]) != string(viaSettings[c.key]) {
			t.Errorf("%s stored %s through the limits and %s through the settings", c.body, viaLimits[c.key], viaSettings[c.key])
		}
	}
}

func TestTheLimitsRouteChangesNoOtherSetting(t *testing.T) {
	t.Parallel()
	a, downloads, _, member, _ := groupServer(t)
	before := a.Settings.Get()
	for _, body := range []string{
		`{"speedLimit":1000,"downloadDir":"/elsewhere"}`,
		`{"maxConcurrent":2,"maxPerHost":1}`,
		`{"maxConcurrent":2,"relayUrl":"wss://relay.example.com"}`,
		`{"speedlimit":1000,"password":"set by a member"}`,
		`{"speedLimit":"fast"}`,
		`{}`,
		`[]`,
		`not json`,
	} {
		if status, answer := member(http.MethodPut, "/api/queue/limits", body); status != http.StatusBadRequest {
			t.Errorf("a member putting %s = %d (%s), want 400", body, status, answer)
		}
	}
	after := a.Settings.Get()
	if after.SpeedLimit != before.SpeedLimit || after.MaxConcurrent != before.MaxConcurrent || after.MaxPerHost != before.MaxPerHost {
		t.Errorf("a refused body changed the limits: %d/%d/%d, was %d/%d/%d",
			after.SpeedLimit, after.MaxConcurrent, after.MaxPerHost, before.SpeedLimit, before.MaxConcurrent, before.MaxPerHost)
	}
	if after.DownloadDir != downloads || after.RelayURL != before.RelayURL {
		t.Errorf("a refused body moved the download folder to %q or the relay to %q", after.DownloadDir, after.RelayURL)
	}
	if closed(a) {
		t.Error("a member gave the instance a password through the limits route")
	}
}

func TestAMemberStillDoesNotReachTheSettings(t *testing.T) {
	t.Parallel()
	_, _, _, member, _ := groupServer(t)
	for _, c := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"speedLimit":1000}`},
		{http.MethodPut, `{"speedLimit":1000}`},
	} {
		if status, body := member(c.method, "/api/settings", c.body); !refusedForGroup(status, body) {
			t.Errorf("a member calling %s /api/settings = %d (%s), want ownLoginNeeded", c.method, status, body)
		}
	}
}
