package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestTransferReadingsListRunningDownloadsWithoutNamesOrLinks(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	a.mu.Lock()
	a.tasks["slow"] = &core.Task{ID: "slow", Status: core.StatusRunning, Resolver: "jd", Speed: 100,
		Name: "holiday.part1.rar", URL: "https://files.example/secret-token"}
	a.tasks["fast"] = &core.Task{ID: "fast", Status: core.StatusRunning, Resolver: "realdebrid", Speed: 900, Size: 2000, Loaded: 500}
	a.tasks["paused"] = &core.Task{ID: "paused", Status: core.StatusPaused, Resolver: "http", Speed: 5}
	a.active["slow"] = true
	a.active["fast"] = true
	a.active["paused"] = true
	a.mu.Unlock()

	got := a.TransferReadings()
	if len(got) != 2 {
		t.Fatalf("readings = %+v, want the two running downloads", got)
	}
	if got[0].Resolver != "realdebrid" || got[0].Meter != "engine" || got[0].Speed != 900 || got[0].Loaded != 500 {
		t.Errorf("first reading = %+v, want the debrid download through the engine, fastest first", got[0])
	}
	if got[1].Meter != "jd" {
		t.Errorf("second reading = %+v, want the JD download", got[1])
	}
	raw, _ := json.Marshal(got)
	for _, leak := range []string{"holiday", "files.example", "secret-token"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("readings carry %q: %s", leak, raw)
		}
	}
}
