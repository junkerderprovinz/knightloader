package api

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/eventprog"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

const (
	storedPath  = "/home/someone/bin/no-such-program-7c1d"
	storedToken = "--token=real-token-value"
)

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %d: %s", url, resp.StatusCode, b)
	}
	return string(b)
}

func TestNeitherTheSettingsNorTheStatusServeAProgramsCommandLine(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()

	s := a.Settings.Get()
	s.EventPrograms = []eventprog.Program{{
		ID: "1", Name: "after", Enabled: true,
		Command:  idleaction.CommandSpec{Program: storedPath, Args: []string{storedToken}},
		Triggers: []script.Trigger{script.TriggerTaskDone},
	}}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/settings", "/api/eventprograms"} {
		body := getBody(t, srv.URL+path)
		for _, secret := range []string{"no-such-program-7c1d", "real-token-value", "someone"} {
			if strings.Contains(body, secret) {
				t.Errorf("GET %s serves %q:\n%s", path, secret, body)
			}
		}
	}

	var rows []eventProgramRow
	if err := json.Unmarshal([]byte(getBody(t, srv.URL+"/api/eventprograms")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "1" || rows[0].Name != "after" {
		t.Fatalf("rows = %+v, want the one configured program", rows)
	}
	if rows[0].Check != idleaction.ProblemNotFound {
		t.Errorf("Check = %q for a program that is not there, want %q", rows[0].Check, idleaction.ProblemNotFound)
	}
}

func TestTheStatusOfAnInstanceWithoutProgramsIsAnEmptyList(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	if body := strings.TrimSpace(getBody(t, srv.URL+"/api/eventprograms")); body != "[]" {
		t.Errorf("GET /api/eventprograms = %s, want []", body)
	}
}

func TestTheArgumentPickerOffersTheFilePlaceholder(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(getBody(t, srv.URL+"/api/eventprograms/placeholders")), &list); err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.Name
	}
	for _, want := range []string{"file", "folder", "category", "task.name"} {
		if !slices.Contains(names, want) {
			t.Errorf("the picker lacks %q: %v", want, names)
		}
	}
}

func TestTheEventProgramsModuleSwitchesOffAndBackOn(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	if err := setFeature(a, "eventprograms", false); err != nil {
		t.Fatal(err)
	}
	if got := a.Settings.Get().ModulesOff; !slices.Equal(got, []string{"eventprograms"}) {
		t.Fatalf("ModulesOff = %q after switching event programs off", got)
	}
	if row := featureRow(t, a, "eventprograms"); row.Enabled || row.Page != "automation" {
		t.Errorf("the row reads %+v, want it off and on the Automation page", row)
	}
	if err := setFeature(a, "eventprograms", true); err != nil {
		t.Fatal(err)
	}
	if got := a.Settings.Get().ModulesOff; len(got) != 0 {
		t.Errorf("ModulesOff = %q after switching back on", got)
	}
}
