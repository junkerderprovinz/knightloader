package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// scheduleStateWire mirrors app.ScheduleState, so the test reads the wire shape
// a client sees.
type scheduleStateWire struct {
	Entries []schedule.Entry `json:"entries"`
	State   schedule.State   `json:"state"`
	Next    *time.Time       `json:"next"`
}

// putSchedule sends a timetable and returns the status plus the decoded
// ScheduleState, zero for anything but 200.
func putSchedule(t *testing.T, url string, entries []schedule.Entry) (int, scheduleStateWire) {
	t.Helper()
	code, raw := doPutSchedule(t, url, entries)
	var out scheduleStateWire
	if code == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return code, out
}

// putScheduleExpectingRefusal decodes the per-row errors of a refused save.
func putScheduleExpectingRefusal(t *testing.T, url string, entries []schedule.Entry) (int, scheduleValidationError) {
	t.Helper()
	code, raw := doPutSchedule(t, url, entries)
	var out scheduleValidationError
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return code, out
}

func doPutSchedule(t *testing.T, url string, entries []schedule.Entry) (int, []byte) {
	t.Helper()
	if entries == nil {
		entries = []schedule.Entry{}
	}
	b, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, url+"/api/schedule", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, buf.Bytes()
}

func getSchedule(t *testing.T, url string) scheduleStateWire {
	t.Helper()
	resp, err := http.Get(url + "/api/schedule")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out scheduleStateWire
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// days builds a weekday list with 0 as Sunday, as in time.Weekday and
// JavaScript's Date.getDay.
func days(nums ...int) []time.Weekday {
	out := make([]time.Weekday, len(nums))
	for i, n := range nums {
		out[i] = time.Weekday(n)
	}
	return out
}

// weeknight is a plain, unremarkable valid row: every weeknight, pause.
func weeknight() schedule.Entry {
	return schedule.Entry{
		Name:   "weeknight",
		Days:   days(1, 2, 3, 4, 5),
		Start:  "22:00",
		End:    "06:00",
		Action: schedule.ActionPause,
	}
}

func TestScheduleRoundTrip(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	entries := []schedule.Entry{
		{Name: "night", Days: days(1, 2, 3, 4, 5), Start: "22:00", End: "06:00", Action: schedule.ActionPause},
		{Name: "lunch break", Days: days(1, 2, 3, 4, 5), Start: "12:00", End: "13:00", Action: schedule.ActionResume},
	}
	code, saved := putSchedule(t, srv.URL, entries)
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d", code)
	}
	if len(saved.Entries) != 2 || saved.Entries[0].Name != "night" || saved.Entries[1].Name != "lunch break" {
		t.Fatalf("PUT echoed %+v, want both rows back in order", saved.Entries)
	}

	loaded := getSchedule(t, srv.URL)
	if len(loaded.Entries) != 2 || loaded.Entries[0].Name != "night" || loaded.Entries[1].Name != "lunch break" {
		t.Fatalf("GET returned %+v after the PUT, want the same two rows in order", loaded.Entries)
	}
}

// TestScheduleRefusesEachBadRowByPosition checks that every bad row is
// reported, not only the first.
func TestScheduleRefusesEachBadRowByPosition(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	entries := []schedule.Entry{
		weeknight(), // row 1: fine
		{Days: nil, Start: "10:00", End: "11:00", Action: schedule.ActionPause},     // row 2: no weekday
		{Days: days(0), Start: "09:00", End: "09:00", Action: schedule.ActionPause}, // row 3: zero-length window
	}
	code, refusal := putScheduleExpectingRefusal(t, srv.URL, entries)
	if code != http.StatusBadRequest {
		t.Fatalf("PUT answered %d, want 400 for the two bad rows", code)
	}
	if len(refusal.Errors) != 2 {
		t.Fatalf("reported %d row errors, want exactly the two bad ones: %+v", len(refusal.Errors), refusal.Errors)
	}
	got := map[int]bool{}
	for _, e := range refusal.Errors {
		got[e.Row] = true
		if e.Error == "" {
			t.Errorf("row %d was refused with no reason", e.Row)
		}
	}
	if !got[2] || !got[3] {
		t.Errorf("rows reported = %v, want 2 and 3 (1-indexed) and not 1", got)
	}
}

// TestScheduleRefusalDoesNotPartiallyApply checks that one bad row leaves the
// stored timetable untouched rather than saving the good rows.
func TestScheduleRefusalDoesNotPartiallyApply(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	code, _ := putSchedule(t, srv.URL, []schedule.Entry{weeknight()})
	if code != http.StatusOK {
		t.Fatalf("seeding the good row answered %d", code)
	}

	bad := []schedule.Entry{
		weeknight(),
		{Days: nil, Start: "10:00", End: "11:00", Action: schedule.ActionPause},
	}
	code, _ = putScheduleExpectingRefusal(t, srv.URL, bad)
	if code != http.StatusBadRequest {
		t.Fatalf("the mixed save answered %d, want 400", code)
	}

	loaded := getSchedule(t, srv.URL)
	if len(loaded.Entries) != 1 || loaded.Entries[0].Name != "weeknight" {
		t.Fatalf("the stored timetable changed to %+v after a refused save, want the seeded row untouched", loaded.Entries)
	}
}

// TestScheduleWriteLeavesOtherSettingsAlone checks that saving the timetable
// rewrites only Schedule.
func TestScheduleWriteLeavesOtherSettingsAlone(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	distinctive := settings.Defaults()
	distinctive.MaxConcurrent = 7
	distinctive.DownloadDir = t.TempDir()
	if code, _, msg := putSettings(t, srv.URL, distinctive); code != http.StatusOK {
		t.Fatalf("seeding settings answered %d: %s", code, msg)
	}

	if code, _ := putSchedule(t, srv.URL, []schedule.Entry{weeknight()}); code != http.StatusOK {
		t.Fatalf("the schedule save answered %d", code)
	}

	stored := a.Settings.Get()
	if stored.MaxConcurrent != 7 {
		t.Errorf("MaxConcurrent = %d after a schedule save, want 7 untouched", stored.MaxConcurrent)
	}
	if stored.DownloadDir != distinctive.DownloadDir {
		t.Errorf("DownloadDir = %q after a schedule save, want %q untouched", stored.DownloadDir, distinctive.DownloadDir)
	}
	if len(stored.Schedule) != 1 || stored.Schedule[0].Name != "weeknight" {
		t.Errorf("Schedule = %+v, want the row just saved", stored.Schedule)
	}
}

// TestScheduleWriteReachesTheLiveRunner checks that the route re-arms the
// runner. An empty timetable has no next change and any real window has one
// within the horizon, so the test does not depend on the wall clock.
func TestScheduleWriteReachesTheLiveRunner(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	if before := getSchedule(t, srv.URL); before.Next != nil {
		t.Fatalf("a fresh install already reports a next change: %v", *before.Next)
	}

	code, saved := putSchedule(t, srv.URL, []schedule.Entry{weeknight()})
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d", code)
	}
	if saved.Next == nil {
		t.Fatal("PUT's own response reports no next change after a real window was saved")
	}

	after := getSchedule(t, srv.URL)
	if after.Next == nil {
		t.Fatal("GET reports no next change after the save, want the runner's recompiled answer")
	}
}
