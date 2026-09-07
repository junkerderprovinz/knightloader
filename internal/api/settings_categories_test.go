package api

// The named drawers over the settings API. There is no interface for them yet,
// so this is the whole door: if a category cannot be created and changed here,
// it cannot be created and changed at all.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// patchSettings sends a partial settings body the way the form's own section
// saves do, and returns the status with either the decoded body or the plain
// error text. PATCH rather than PUT is what a category editor would use: a page
// that posts the WHOLE document back would carry a stale copy of every other
// field with it.
func patchSettings(t *testing.T, url, body string) (int, map[string]any, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, url+"/api/settings", bytes.NewReader([]byte(body)))
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
		var msg bytes.Buffer
		_, _ = msg.ReadFrom(resp.Body)
		return resp.StatusCode, nil, msg.String()
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out, ""
}

// TestADrawerIsCreatedAndChangedThroughTheSettingsAPI walks the whole life of a
// category through the one door there is: create it with a name alone, read it
// back off the page, change it, and read the change back. The id derived on
// creation has to be the id it still has after the rename, or every download
// already filed in it would quietly stop being in it.
func TestADrawerIsCreatedAndChangedThroughTheSettingsAPI(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	code, saved, msg := patchSettings(t, srv.URL, `{"categories":[{"name":"Serien","priority":2,"extract":true}]}`)
	if code != http.StatusOK {
		t.Fatalf("creating a drawer answered %d: %s", code, msg)
	}
	drawers := categoriesOf(t, saved)
	if len(drawers) != 1 {
		t.Fatalf("%d drawers came back, want 1", len(drawers))
	}
	if got := drawers[0]["id"]; got != "serien" {
		t.Errorf("the created drawer's id = %v, want it derived from the name", got)
	}

	// The folder is added by a second save, which is how somebody actually
	// fills a form in. It has to be a real one: the route creates and probes it
	// exactly as it does the global download folder, so a drawer that names a
	// folder nobody can write to is refused at the moment it is typed.
	dir := filepath.Join(t.TempDir(), "serien")
	body, err := json.Marshal(map[string]any{"categories": []settings.Category{{
		ID: "serien", Name: "TV", Dir: dir, Priority: intPtr(2), Extract: boolPtr(true),
		Collision: string(collide.Skip),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	code, saved, msg = patchSettings(t, srv.URL, string(body))
	if code != http.StatusOK {
		t.Fatalf("changing the drawer answered %d: %s", code, msg)
	}
	drawers = categoriesOf(t, saved)
	if got := drawers[0]["id"]; got != "serien" {
		t.Errorf("the id changed to %v when the name did; the downloads filed there are now orphans", got)
	}
	if got := drawers[0]["dir"]; got != dir {
		t.Errorf("dir = %v, want %q", got, dir)
	}
	if got := drawers[0]["collision"]; got != string(collide.Skip) {
		t.Errorf("collision = %v, want %q", got, collide.Skip)
	}

	// And on the next visit to the page, which is the read that matters: the
	// person comes back tomorrow and the drawers have to still be there.
	loaded := categoriesOf(t, getSettings(t, srv.URL))
	if len(loaded) != 1 || loaded[0]["name"] != "TV" {
		t.Errorf("the drawer did not survive to the next load: %v", loaded)
	}
}

// TestARefusedDrawerSaysWhy is the difference between this route and the
// sanitiser behind it. sanitize drops what it cannot use, silently; a person
// who has just typed two drawers with one name has to be told, or they are left
// with two rows on screen, one save, and one row.
func TestARefusedDrawerSaysWhy(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	for _, c := range []struct{ name, body, wants string }{
		{
			name:  "two drawers with one key",
			body:  `{"categories":[{"name":"Serien"},{"id":"serien","name":"Series"}]}`,
			wants: "serien",
		},
		{
			name:  "a drawer nothing could point at",
			body:  `{"categories":[{"name":"###"}]}`,
			wants: "neither an id nor a name",
		},
		{
			name:  "a collision rule nothing can answer",
			body:  `{"categories":[{"name":"Filme","collision":"ask"}]}`,
			wants: "rename, skip or overwrite",
		},
		{
			name:  "a folder nobody can locate",
			body:  `{"categories":[{"name":"Filme","dir":"relative/path"}]}`,
			wants: "absolute",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, _, msg := patchSettings(t, srv.URL, c.body)
			if code != http.StatusBadRequest {
				t.Fatalf("answered %d, want 400: %s", code, msg)
			}
			if !strings.Contains(msg, c.wants) {
				t.Errorf("the refusal says %q, want it to mention %q", strings.TrimSpace(msg), c.wants)
			}
		})
	}
}

// TestARuleFilingLinksInAMissingDrawerIsRefused is the reference check the
// whole model turns on, over the wire. A Packagizer rule naming a category that
// does not exist does nothing at all, silently, on every link it matches - so
// the save is refused and the message names both the rule and the drawer.
//
// The second half is the half that keeps this from being a cage: the same
// document with the drawer in it saves cleanly, so deleting a category is a
// matter of sending the rule change with it rather than something that cannot
// be done.
func TestARuleFilingLinksInAMissingDrawerIsRefused(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	withRule := settingsWith(func(s *settings.Settings) {
		s.Packagizer = rules.Set{Rules: []rules.Rule{{
			Name:       "Serien nach Serien",
			Conditions: []rules.Condition{{Field: rules.FieldFilename, Op: rules.OpContains, Value: "s01e"}},
			Action:     rules.Action{Category: "serien"},
		}}}
	})
	code, _, msg := putSettings(t, srv.URL, withRule)
	if code != http.StatusBadRequest {
		t.Fatalf("a rule filing links in a drawer that does not exist answered %d, want 400", code)
	}
	for _, want := range []string{"Serien nach Serien", "serien"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal %q does not name %q", strings.TrimSpace(msg), want)
		}
	}

	withRule.Categories = []settings.Category{{ID: "serien", Name: "Serien"}}
	code, saved, msg := putSettings(t, srv.URL, withRule)
	if code != http.StatusOK {
		t.Fatalf("the same rule with the drawer present answered %d: %s", code, msg)
	}
	if n := problemCount(t, saved, "packagizer"); n != 0 {
		t.Errorf("the rule engine reported %d problems about a rule that is fine", n)
	}
}

// TestTheCategoriesKeyIsAlwaysOnThePage is what "no omitempty" buys the
// frontend: a fresh install has to serve the key as null rather than leave it
// out, or there is no way to type a field that is sometimes simply absent.
func TestTheCategoriesKeyIsAlwaysOnThePage(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	body := getSettings(t, srv.URL)
	if _, ok := body["categories"]; !ok {
		t.Fatal(`a fresh install's settings have no "categories" key at all`)
	}
	if body["categories"] != nil {
		t.Errorf("categories = %v, want null on a fresh install", body["categories"])
	}
}

// categoriesOf digs the drawers out of a settings response.
func categoriesOf(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["categories"].([]any)
	if !ok {
		t.Fatalf("categories came back as %T, want a list", body["categories"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("a category came back as %T, want an object", e)
		}
		out = append(out, m)
	}
	return out
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
