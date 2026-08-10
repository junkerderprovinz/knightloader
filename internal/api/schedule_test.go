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

// scheduleStateWire mirrors app.ScheduleState field-for-field. A local copy
// rather than importing internal/app's type directly keeps this file reading
// the wire shape a client actually sees, the same reason settings_test.go
// decodes PUT /api/settings into a bare map rather than into settings.Settings.
type scheduleStateWire struct {
	Entries []schedule.Entry `json:"entries"`
	State   schedule.State   `json:"state"`
	Next    *time.Time       `json:"next"`
}

// putSchedule sends a timetable and returns the status plus the decoded
// ScheduleState body. The body is the zero value for anything but 200 - a
// refusal has a different shape, and putScheduleExpectingRefusal decodes that
// one instead.
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

// putScheduleExpectingRefusal is putSchedule's other half: the per-row errors
// a 400 carries, which putSchedule has nowhere to put because a refusal is
// not a ScheduleState.
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

// days is the readable way to write a weekday list: 0 = Sunday, matching both
// time.Weekday and, not coincidentally, JavaScript's Date.getDay - the
// timetable editor built on this route leans on that agreement to avoid a
// day-numbering conversion of its own.
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

// TestScheduleRoundTrip pins the basic shape: what is PUT is what GET answers
// with next, in the same order.
func TestScheduleRoundTrip(t *testing.T) {
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

// TestScheduleRefusesEachBadRowByPosition is the point of the dedicated
// route: a table with two mistakes in it is told about both, by row, rather
// than stopping at the first the way PUT /api/settings's validateRows does.
func TestScheduleRefusesEachBadRowByPosition(t *testing.T) {
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

// TestScheduleRefusalDoesNotPartiallyApply is the atomicity half: a table
// with one bad row among good ones must leave the STORED timetable exactly as
// it was, not the good rows written and the bad one dropped. A save that
// silently keeps two of three rows is worse than a save that is refused
// outright, because nothing on screen says a row went missing.
func TestScheduleRefusalDoesNotPartiallyApply(t *testing.T) {
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

// TestScheduleWriteLeavesOtherSettingsAlone is the property the whole route
// is built around (see routes_schedule.go's doc comment): saving the
// timetable through this door must never carry along a stale copy of some
// other setting, the way a second browser tab's PUT /api/settings could. It
// reads the live document and rewrites only Schedule, so a value changed by
// another request beforehand is untouched.
func TestScheduleWriteLeavesOtherSettingsAlone(t *testing.T) {
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

// TestScheduleWriteReachesTheLiveRunner proves ApplySettings, not a direct
// store write, is what this route calls - the distinction that matters
// because only ApplySettings re-arms a.sched (see app.go's ApplySettings and
// routes_schedule.go's doc comment). It is checked without depending on the
// wall clock, since whether a window happens to cover the instant the test
// runs is not the thing under test: an empty timetable's Next is always nil
// (schedule.Schedule.Next on zero rules), and any non-empty one has a
// boundary somewhere in its two-week horizon, so Next turning non-nil is
// proof the runner recompiled against the new rows.
func TestScheduleWriteReachesTheLiveRunner(t *testing.T) {
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
