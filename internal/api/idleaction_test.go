package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// idleActionStateWire mirrors idleaction.State field-for-field - a local copy
// rather than importing the type directly, the same reason schedule_test.go's
// scheduleStateWire exists: this reads the wire shape a client actually sees.
type idleActionStateWire struct {
	Config idleaction.Config `json:"config"`
	Idle   bool              `json:"idle"`
	Armed  bool              `json:"armed"`
	Action idleaction.Action `json:"action,omitempty"`
	FireAt *time.Time        `json:"fireAt,omitempty"`
}

func getIdleAction(t *testing.T, url string) (int, idleActionStateWire) {
	t.Helper()
	resp, err := http.Get(url + "/api/idle-action")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out idleActionStateWire
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

func postIdleActionCancel(t *testing.T, url string) (int, idleActionStateWire) {
	t.Helper()
	resp, err := http.Post(url+"/api/idle-action/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out idleActionStateWire
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

// TestIdleActionDefaultState is a fresh install: nothing configured, nothing
// in the list, so the queue reads idle but nothing is armed to act on it.
func TestIdleActionDefaultState(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, st := getIdleAction(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("GET /api/idle-action answered %d", code)
	}
	if st.Config.Action != idleaction.ActionNone {
		t.Errorf("Config.Action = %q, want %q on a fresh install", st.Config.Action, idleaction.ActionNone)
	}
	if !st.Idle {
		t.Error("Idle = false on a server with nothing added to the list")
	}
	if st.Armed {
		t.Error("Armed = true despite Config.Action being none")
	}
	if st.FireAt != nil {
		t.Errorf("FireAt = %v, want nil while not armed", st.FireAt)
	}
}

// TestIdleActionActionsRoute is the menu's source of truth: exactly the
// build-compiled-in list, in order, never guessed at by the client.
func TestIdleActionActionsRoute(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/idle-action/actions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/idle-action/actions answered %d", resp.StatusCode)
	}
	var actions []idleaction.Action
	if err := json.NewDecoder(resp.Body).Decode(&actions); err != nil {
		t.Fatal(err)
	}
	want := []idleaction.Action{idleaction.ActionNone, idleaction.ActionPause}
	if len(actions) != len(want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	for i, a := range want {
		if actions[i] != a {
			t.Errorf("actions[%d] = %q, want %q", i, actions[i], a)
		}
	}
}

// TestIdleActionConfigReachesGETThroughSettingsPUT is the point of NOT having
// a dedicated write route for this one (routes_idleaction.go's own doc
// comment): the ordinary settings document is the single writer, and GET
// /api/idle-action has to read back exactly what was saved through it.
func TestIdleActionConfigReachesGETThroughSettingsPUT(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	s := settingsWith(func(s *settings.Settings) {
		s.IdleAction = idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 30}
	})
	if code, _, msg := putSettings(t, srv.URL, s); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	code, st := getIdleAction(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("GET /api/idle-action answered %d", code)
	}
	if st.Config.Action != idleaction.ActionPause {
		t.Errorf("Config.Action = %q, want %q", st.Config.Action, idleaction.ActionPause)
	}
	if st.Config.DelaySeconds != 30 {
		t.Errorf("Config.DelaySeconds = %d, want 30", st.Config.DelaySeconds)
	}
}

// TestIdleActionConfigIsSanitizedThroughSettingsPUT: an out-of-range delay
// sent through the generic settings PUT is clamped the same way every other
// plain number on that document is (MaxRetries, AutoConfirmDelay), never
// rejected outright - see idleaction.Config.Sanitize's own doc comment.
func TestIdleActionConfigIsSanitizedThroughSettingsPUT(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	s := settingsWith(func(s *settings.Settings) {
		s.IdleAction = idleaction.Config{Action: "not-a-real-action", DelaySeconds: 1}
	})
	code, _, msg := putSettings(t, srv.URL, s)
	if code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d for an out-of-range idle action: %s", code, msg)
	}

	_, st := getIdleAction(t, srv.URL)
	if st.Config.Action != idleaction.ActionNone {
		t.Errorf("Config.Action = %q, want the unknown value folded to %q", st.Config.Action, idleaction.ActionNone)
	}
	if st.Config.DelaySeconds != idleaction.DefaultDelaySeconds {
		t.Errorf("Config.DelaySeconds = %d, want the too-low value folded to the default %d",
			st.Config.DelaySeconds, idleaction.DefaultDelaySeconds)
	}
}

// TestIdleActionCancelIsANoOpWhenNothingArmed exercises the route with
// nothing to call off - it must answer 200 rather than error, the same as
// internal/idleaction.Controller.Cancel's own no-op case.
func TestIdleActionCancelIsANoOpWhenNothingArmed(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, st := postIdleActionCancel(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("POST /api/idle-action/cancel answered %d", code)
	}
	if st.Armed {
		t.Error("Armed = true immediately after Cancel with nothing armed to begin with")
	}
}

// TestIdleActionArmsFiresAndCanBeCancelledOverHTTP is the one end-to-end
// check at this layer: the state machine's own correctness is
// internal/idleaction's job (a fake clock, no waiting), and the App-level
// wiring against a real task list is internal/app's job
// (app_idle_test.go) - this is only proof the two routes and the JSON they
// speak actually connect a real save to a real countdown a real client can
// read and cancel.
func TestIdleActionArmsFiresAndCanBeCancelledOverHTTP(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	s := settingsWith(func(s *settings.Settings) {
		s.IdleAction = idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: idleActionTestDelaySeconds}
	})
	if code, _, msg := putSettings(t, srv.URL, s); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	deadline := time.Now().Add(15 * time.Second)
	var armed idleActionStateWire
	for time.Now().Before(deadline) {
		_, armed = getIdleAction(t, srv.URL)
		if armed.Armed {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !armed.Armed {
		t.Fatal("never armed over HTTP within the expected window")
	}
	if armed.FireAt == nil {
		t.Fatal("armed but FireAt is nil")
	}

	code, cancelled := postIdleActionCancel(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("POST /api/idle-action/cancel answered %d", code)
	}
	if cancelled.Armed {
		t.Fatal("still armed in the cancel response")
	}

	time.Sleep(idleActionTestDelaySeconds * time.Second)
	if a.Queue().Halted {
		t.Error("the queue was halted despite the countdown having been cancelled over HTTP")
	}
}

// idleActionTestDelaySeconds is idleaction.minDelaySeconds' value, copied
// rather than imported: that constant is unexported on purpose (a floor
// nothing outside the package needs to name), and 5 is small enough this
// test costs single-digit seconds rather than a minute.
const idleActionTestDelaySeconds = 5
