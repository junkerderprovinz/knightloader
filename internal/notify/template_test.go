package notify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

// awkwardName is the file name this whole escaping design exists for: a quote
// breaks a JSON body, a newline fails a header outright, and an ampersand
// starts a query parameter nobody wrote.
const awkwardName = "Der \"Direktor\"\n1080p & more.mkv"

func firingWithTask(name string) script.Firing {
	return script.Firing{
		Trigger: script.TriggerTaskDone,
		At:      time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		Queue:   script.QueueView{Files: 2, Disabled: 1, Running: 0},
		Task:    &script.TaskView{ID: "t1", Name: name, Host: "example.invalid", Size: 100, Loaded: 50},
	}
}

func TestExpandKeepsAnUnknownNameAndEmptiesAKnownOne(t *testing.T) {
	// A reconnect firing carries no task, so %%task.name%% is KNOWN and absent.
	f := script.Firing{Trigger: script.TriggerReconnectDone, At: time.Now(), Reconnect: &script.ReconnectView{OK: true}}

	if got := Expand("[%%task.name%%]", SlotBodyPlain, f, ""); got != "[]" {
		t.Errorf("a known placeholder this trigger does not carry expanded to %q, want empty; "+
			"otherwise a reconnect target sends the literal text %%%%task.name%%%% to somebody's phone", got)
	}
	if got := Expand("[%%task.nmae%%]", SlotBodyPlain, f, ""); got != "[%%task.nmae%%]" {
		t.Errorf("a typo expanded to %q, want it left standing so it can be seen and fixed", got)
	}
}

func TestExpandMatchesNamesLooselyTheWayReconnectDoes(t *testing.T) {
	f := firingWithTask("x.mkv")
	for _, in := range []string{"%%task.name%%", "%%TASK.NAME%%", "%% task.name %%"} {
		if got := Expand(in, SlotBodyPlain, f, ""); got != "x.mkv" {
			t.Errorf("%s expanded to %q, want the value", in, got)
		}
	}
}

func TestExpandEscapesForTheSlotItIsGoingInto(t *testing.T) {
	f := firingWithTask(awkwardName)

	// JSON: the document has to survive, which is trap 6 in one line.
	body := Expand(`{"message":"%%task.name%%"}`, SlotBodyJSON, f, "")
	var decoded struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("a file name with a quote in it produced invalid JSON (%v): %s", err, body)
	}
	if decoded.Message != awkwardName {
		t.Errorf("the decoded message is %q, want the file name back unchanged", decoded.Message)
	}

	// Header: net/http refuses a value with a newline in it at write time, so
	// the message would never go and the error would name nothing recognisable.
	header := Expand("%%task.name%%", SlotHeader, f, "")
	if strings.ContainsAny(header, "\r\n") {
		t.Errorf("the header value still carries a line break: %q", header)
	}
	if !strings.Contains(header, `Der "Direktor"`) {
		t.Errorf("stripping went too far; the value is %q", header)
	}

	// URL: an ampersand in a value must not start a second query parameter.
	addr := Expand("https://x.example/?t=%%task.name%%", SlotURL, f, "")
	if strings.Contains(strings.TrimPrefix(addr, "https://x.example/?t="), "&") {
		t.Errorf("the ampersand was not escaped: %q", addr)
	}

	// Plain: nothing is escaped, because there is no structure to break.
	if got := Expand("%%task.name%%", SlotBodyPlain, f, ""); got != awkwardName {
		t.Errorf("a plain body changed the value to %q", got)
	}
}

func TestExpandLeavesTheOperatorsOwnPunctuationAlone(t *testing.T) {
	// Only the VALUE is escaped. The braces and quotes around it are the
	// document the operator wrote by hand, and escaping those would break the
	// only part of the body they are responsible for.
	f := firingWithTask("plain.mkv")
	got := Expand(`{"message":"%%task.name%%","priority":5}`, SlotBodyJSON, f, "")
	if got != `{"message":"plain.mkv","priority":5}` {
		t.Errorf("the template itself was escaped: %s", got)
	}
}

func TestBodySlotComesFromTheRowsOwnContentType(t *testing.T) {
	for _, tc := range []struct {
		headers map[string]string
		want    Slot
	}{
		{nil, SlotBodyPlain},
		{map[string]string{"content-type": "application/json"}, SlotBodyJSON},
		{map[string]string{"Content-Type": "application/json; charset=utf-8"}, SlotBodyJSON},
		{map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, SlotBodyForm},
		{map[string]string{"Content-Type": "text/plain"}, SlotBodyPlain},
	} {
		if got := BodySlot(tc.headers); got != tc.want {
			t.Errorf("BodySlot(%v) = %v, want %v", tc.headers, got, tc.want)
		}
	}
}

func TestExpandBodyIsEmptyForAGet(t *testing.T) {
	f := firingWithTask("x.mkv")
	if got := ExpandBody(Target{Method: MethodGET, Body: "%%task.name%%"}, f, ""); got != "" {
		t.Errorf("a GET carried a body (%q); most servers and every proxy in between treat that differently", got)
	}
}

func TestExpandHeadersLeavesTheNameAlone(t *testing.T) {
	f := firingWithTask("x.mkv")
	out := ExpandHeaders(map[string]string{"X-%%task.name%%": "%%task.name%%"}, f, "")
	if _, ok := out["X-%%task.name%%"]; !ok {
		t.Errorf("the header NAME was expanded; the name in the row has to be the name on the wire, "+
			"or Merge's \"same header name\" rule means nothing: %v", out)
	}
}

func TestPlaceholdersAndTheExpanderCannotDrift(t *testing.T) {
	f := SampleFiring(time.Now())
	for _, p := range Placeholders() {
		if p.Name == "" || p.Scope == "" {
			t.Errorf("placeholder %+v has no name or no scope", p)
		}
		// Every name the picker offers has to be one the expander fills in. The
		// sample firing carries every payload, so anything still expanding to
		// its own literal is a name in the table with no value function behind
		// it.
		in := marker + p.Name + marker
		if got := Expand(in, SlotBodyPlain, f, "box"); got == in {
			t.Errorf("%s is offered by Placeholders() but the expander left it standing", in)
		}
	}
}

func TestSampleFiringExercisesTheEscaping(t *testing.T) {
	// The sample's task name carries a double quote on purpose: a test button
	// that only proved the connection would leave the JSON escaping untested,
	// and the escaping is what actually breaks at three in the morning.
	f := SampleFiring(time.Now())
	if !strings.Contains(f.Task.Name, `"`) {
		t.Fatalf("the sample task name is %q and has no quote in it", f.Task.Name)
	}
	body := Expand(`{"m":"%%task.name%%"}`, SlotBodyJSON, f, "")
	var out map[string]string
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("the sample produced invalid JSON: %v (%s)", err, body)
	}
}
