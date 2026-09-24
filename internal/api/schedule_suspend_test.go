package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// suspendWire is the part of app.ScheduleState a suspension changes.
type suspendWire struct {
	Suspended      bool       `json:"suspended"`
	SuspendedUntil *time.Time `json:"suspendedUntil"`
}

func decodeSuspend(t *testing.T, raw []byte) suspendWire {
	t.Helper()
	var out suspendWire
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return out
}

func TestScheduleSuspendForMinutesEndsByItselfLater(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	before := time.Now()
	code, raw := postJSON(t, http.MethodPut, srv.URL+"/api/schedule/suspend", map[string]any{"minutes": 60})
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d: %s", code, raw)
	}
	got := decodeSuspend(t, raw)
	if !got.Suspended || got.SuspendedUntil == nil {
		t.Fatalf("answer %+v, want suspended with an end", got)
	}
	if end := *got.SuspendedUntil; end.Before(before.Add(59*time.Minute)) || end.After(time.Now().Add(61*time.Minute)) {
		t.Errorf("suspended until %s, want about an hour from now", end)
	}

	resp, err := http.Get(srv.URL + "/api/schedule")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var read suspendWire
	if err := json.NewDecoder(resp.Body).Decode(&read); err != nil {
		t.Fatal(err)
	}
	if !read.Suspended {
		t.Error("GET /api/schedule does not report the suspension")
	}
}

func TestScheduleSuspendWithoutAnEndLastsUntilLifted(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, raw := postJSON(t, http.MethodPut, srv.URL+"/api/schedule/suspend", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d: %s", code, raw)
	}
	if got := decodeSuspend(t, raw); !got.Suspended || got.SuspendedUntil != nil {
		t.Fatalf("answer %+v, want suspended with no end", got)
	}

	code, raw = postJSON(t, http.MethodDelete, srv.URL+"/api/schedule/suspend", nil)
	if code != http.StatusOK {
		t.Fatalf("DELETE answered %d: %s", code, raw)
	}
	if got := decodeSuspend(t, raw); got.Suspended {
		t.Error("the suspension is still reported after DELETE")
	}
}

func TestScheduleSuspendUntilAnInstant(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	until := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)
	code, raw := postJSON(t, http.MethodPut, srv.URL+"/api/schedule/suspend", map[string]any{"until": until})
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d: %s", code, raw)
	}
	if got := decodeSuspend(t, raw); got.SuspendedUntil == nil || !got.SuspendedUntil.Equal(until) {
		t.Errorf("answer %+v, want suspended until %s", got, until)
	}
}

func TestScheduleSuspendRefusesWhatCannotBeMeant(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	cases := map[string]map[string]any{
		"both a length and an instant": {"minutes": 5, "until": time.Now().Add(time.Hour)},
		"a negative length":            {"minutes": -5},
		"a length past a month":        {"minutes": maxSuspendMinutes + 1},
		"an instant already past":      {"until": time.Now().Add(-time.Hour)},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if code, raw := postJSON(t, http.MethodPut, srv.URL+"/api/schedule/suspend", body); code != http.StatusBadRequest {
				t.Errorf("PUT answered %d (%s), want 400", code, raw)
			}
		})
	}
	if a.ScheduleState().Suspended {
		t.Error("a refused request suspended the timetable")
	}
}
