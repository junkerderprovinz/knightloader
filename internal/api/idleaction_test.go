package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// idleActionStateWire mirrors idleaction.State field for field. A local copy
// rather than the type itself, so this reads the wire shape a client sees.
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

// TestIdleActionDefaultState: on a fresh install the queue reads idle and
// nothing is armed to act on it.
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

// TestIdleActionActionsRoute: the menu comes from what this build can carry
// out, in order, never guessed at by the client.
//
// A test server wires no RequestExit and no RequestSuspend, like any embedding
// that never set them, so quit and sleep are absent. The route reports the
// wiring rather than the deployment, the distinction idleaction.Capabilities
// draws, which is why it serves Offered while the settings sanitiser reads the
// unfiltered Actions.
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
	want := []idleaction.Action{idleaction.ActionNone, idleaction.ActionPause, idleaction.ActionCommand}
	if len(actions) != len(want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	for i, a := range want {
		if actions[i] != a {
			t.Errorf("actions[%d] = %q, want %q", i, actions[i], a)
		}
	}
}

// TestIdleActionCheckAnswers200EvenWhenTheAnswerIsBad: the preflight reports
// and runs nothing. A 500 for "that program is not in this image" could not be
// read by the page that asked, and that sentence is what the button is for.
func TestIdleActionCheckAnswers200EvenWhenTheAnswerIsBad(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	// Nothing configured yet, so the answer is "there is nothing to run"
	// rather than an error.
	code, check := postIdleActionCheck(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("POST /api/idle-action/check answered %d with nothing configured", code)
	}
	if check.Problem != idleaction.ProblemEmpty {
		t.Errorf("problem = %q, want %q", check.Problem, idleaction.ProblemEmpty)
	}
	if check.Deployment == "" {
		t.Error("deployment is empty; the browser needs it to pick between two explanations of one problem code")
	}

	s := settingsWith(func(s *settings.Settings) {
		s.IdleAction = idleaction.Config{
			Action:       idleaction.ActionCommand,
			DelaySeconds: 30,
			Command:      idleaction.CommandSpec{Program: notARealProgram, TimeoutSeconds: 30},
		}
	})
	if code, _, msg := putSettings(t, srv.URL, s); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	code, check = postIdleActionCheck(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("POST /api/idle-action/check answered %d for a program that is not there", code)
	}
	if check.Problem != idleaction.ProblemNotFound {
		t.Errorf("problem = %q, want %q", check.Problem, idleaction.ProblemNotFound)
	}
}

// TestIdleActionRunIsRefusedUnlessTheActionIsTheCommandOne: a test button that
// suspends the machine or quits the process is not a test. 409 rather than
// 400, since nothing about the request is malformed and the instance is not in
// a state where it means anything.
func TestIdleActionRunIsRefusedUnlessTheActionIsTheCommandOne(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	s := settingsWith(func(s *settings.Settings) {
		s.IdleAction = idleaction.Config{Action: idleaction.ActionPause, DelaySeconds: 30}
	})
	if code, _, msg := putSettings(t, srv.URL, s); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	resp, err := http.Post(srv.URL+"/api/idle-action/run", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /api/idle-action/run answered %d while the action was pause, want %d",
			resp.StatusCode, http.StatusConflict)
	}
}

// TestTheStoredCommandIsNeverServedBackAndASaveDoesNotWipeIt covers both
// halves of the redaction, which only make sense together. The command line
// does not travel in GET /api/settings, which is what keeps it out of the
// diagnostics bundle built from the same Settings.Redacted(), and a form shown
// the placeholder must not delete the stored command by posting it back.
// Without the merge in Store.setLocked, any save on the Downloads page empties
// it.
func TestTheStoredCommandIsNeverServedBackAndASaveDoesNotWipeIt(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	s := settingsWith(func(s *settings.Settings) {
		s.IdleAction = idleaction.Config{
			Action:       idleaction.ActionCommand,
			DelaySeconds: 30,
			Command: idleaction.CommandSpec{
				Program:        notARealProgram,
				Args:           []string{"--token=abc123"},
				TimeoutSeconds: 30,
			},
		}
	})
	if code, _, msg := putSettings(t, srv.URL, s); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d: %s", code, msg)
	}

	body := getSettings(t, srv.URL)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(notARealProgram)) {
		t.Error("the stored program came back from GET /api/settings in clear text, so it also reaches the diagnostics bundle")
	}
	if bytes.Contains(raw, []byte("abc123")) {
		t.Error("a stored argument came back from GET /api/settings in clear text, so it also reaches the diagnostics bundle")
	}

	// Now save again the way the page does: the redacted document, straight
	// back, with one unrelated field changed.
	back := s
	back.IdleAction.Command = back.IdleAction.Command.Redacted()
	back.MaxConcurrent = 3
	if code, _, msg := putSettings(t, srv.URL, back); code != http.StatusOK {
		t.Fatalf("PUT /api/settings answered %d on the round trip: %s", code, msg)
	}

	// Asked of the one route that reports what is stored: the run record names
	// the program it tried to start. A check will not do, because a wiped
	// command is stored as the placeholder rather than as an empty string, so
	// the preflight answers "not found" either way and would pass while the
	// operator's command was gone.
	//
	// Running is safe because the program is made up: exec fails to start it,
	// nothing is executed, and the record still carries the name it tried.
	code, run := postIdleActionRun(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("POST /api/idle-action/run answered %d", code)
	}
	if run.Program != notARealProgram {
		t.Errorf("the run tried to start %q, want %q - the save that only sent the placeholder back wiped the stored command",
			run.Program, notARealProgram)
	}
	if run.Problem != string(idleaction.ProblemNotFound) {
		t.Errorf("problem = %q, want %q for a program no machine has", run.Problem, idleaction.ProblemNotFound)
	}
	if run.OK {
		t.Error("ok = true for a program that could not be started")
	}
}

// idleRunWire mirrors app.IdleRun on the wire, a local copy like
// idleActionStateWire so this reads the shape a client sees.
type idleRunWire struct {
	Action   string `json:"action"`
	OK       bool   `json:"ok"`
	Problem  string `json:"problem,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
	Output   string `json:"output,omitempty"`
	Program  string `json:"program,omitempty"`
}

func postIdleActionRun(t *testing.T, url string) (int, idleRunWire) {
	t.Helper()
	resp, err := http.Post(url+"/api/idle-action/run", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out idleRunWire
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

// notARealProgram is a name no machine has, so the preflight answers the same
// everywhere. Naming a real one, sh or systemctl, would make this suite pass
// or fail on what the runner happens to have installed.
const notARealProgram = "knightloader-not-a-real-program-8749"

func postIdleActionCheck(t *testing.T, url string) (int, idleaction.Check) {
	t.Helper()
	resp, err := http.Post(url+"/api/idle-action/check", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out idleaction.Check
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

// TestIdleActionConfigReachesGETThroughSettingsPUT: the settings document is
// the single writer (routes_idleaction.go), so GET /api/idle-action has to
// read back what was saved through it.
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

// TestIdleActionConfigIsSanitizedThroughSettingsPUT: an out-of-range delay is
// clamped the way every other plain number on that document is (MaxRetries,
// AutoConfirmDelay) rather than rejected. See idleaction.Config.Sanitize.
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

// TestIdleActionCancelIsANoOpWhenNothingArmed: with nothing to call off the
// route answers 200 rather than an error, like
// internal/idleaction.Controller.Cancel.
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

// TestIdleActionArmsFiresAndCanBeCancelledOverHTTP is the end-to-end check at
// this layer. The state machine is internal/idleaction's own test, with a fake
// clock, and the App-level wiring is app_idle_test.go's; this only shows that
// the two routes and their JSON connect a save to a countdown a client can
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
// because that constant is unexported, and small enough that this test costs
// single-digit seconds.
const idleActionTestDelaySeconds = 5
