package rules_test

// The category action end to end, from the id the grammar advertises to the
// category a staged task lands in. It crosses three packages (rules describes
// it, settings refuses unknown ids, app copies the value onto the task), and
// rules cannot import settings, hence the external test package. Both tests
// build their Action from the grammar's advertised id through the JSON the
// browser posts, so they check that the form's key and the engine's field are
// the same. The price is that these tests build against app and everything it
// imports.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// categoryActionID is the JSON key the grammar tells the editor to post under,
// read from the grammar so a rename moves the tests with it.
func categoryActionID(t *testing.T) string {
	t.Helper()
	for _, a := range rules.Describe().Actions {
		if a.Kind == "category" {
			return a.ID
		}
	}
	t.Fatal("the grammar describes no category action, so nothing below is testing the editor's route into a drawer")
	return ""
}

// actionFromGrammar builds an Action the way the browser will: by writing the
// advertised id into the JSON body a saved rule set is made of.
func actionFromGrammar(t *testing.T, value string) rules.Action {
	t.Helper()
	body, err := json.Marshal(map[string]string{categoryActionID(t): value})
	if err != nil {
		t.Fatal(err)
	}
	var a rules.Action
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatal(err)
	}
	if a.Category != value {
		t.Fatalf("a rule posted under the advertised id came back with Category %q, want %q; the form is addressing a different field", a.Category, value)
	}
	return a
}

// TestARuleFilingLinksInADrawerThatIsNotThereIsRefused: such a rule would
// silently do nothing, since settings.CategoryFor answers an unknown id with
// the empty category, so it is refused at save time.
func TestARuleFilingLinksInADrawerThatIsNotThereIsRefused(t *testing.T) {
	s := settings.Settings{
		Categories: []settings.Category{{ID: "filme", Name: "Filme"}},
		Packagizer: rules.Set{Rules: []rules.Rule{
			{Name: "Serien nach Serien", Action: actionFromGrammar(t, "serien")},
		}},
	}
	err := s.ValidateCategories()
	if err == nil {
		t.Fatal("a rule filing links in a drawer that does not exist was accepted; it would have matched links and done nothing, silently")
	}
	// The message names the rule and the id, so the reader knows what to fix.
	for _, want := range []string{"Serien nach Serien", "serien"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
	s.Packagizer.Rules[0].Action = actionFromGrammar(t, "filme")
	if err := s.ValidateCategories(); err != nil {
		t.Errorf("a rule naming an existing drawer was refused: %v", err)
	}
}

// TestARuleNamingARealDrawerFilesTheTask runs the real staging path rather than
// Apply, so the hand-off in app.packagize is covered too.
func TestARuleNamingARealDrawerFilesTheTask(t *testing.T) {
	a, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	s := a.Settings.Get()
	s.DownloadDir = t.TempDir()
	s.Categories = []settings.Category{{ID: "serien", Name: "Serien", Dir: t.TempDir()}}
	s.Packagizer = rules.Set{Rules: []rules.Rule{{
		Name:       "Serien",
		Conditions: []rules.Condition{{Field: rules.FieldFilename, Op: rules.OpContains, Value: "s01e"}},
		Action:     actionFromGrammar(t, "serien"),
	}}}
	if err := s.ValidateCategories(); err != nil {
		t.Fatalf("the rule set this test is built on would not save: %v", err)
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	// AddResolvedLinksFrom takes links that are already named and sized, so
	// nothing reaches the network, and runs the same packagizer hand-off as a
	// paste.
	created := a.AddResolvedLinksFrom([]resolver.Result{
		{Name: "Doctor.Who.S01E03.mkv", DirectURL: "https://host.example/Doctor.Who.S01E03.mkv", Size: 1 << 20},
		{Name: "holiday.mp4", DirectURL: "https://host.example/holiday.mp4", Size: 1 << 20},
	}, "", app.OriginPaste)
	if len(created) != 2 {
		t.Fatalf("staged %d links, want 2", len(created))
	}
	if created[0].Category != "serien" {
		t.Errorf("Category = %q on the link the rule matched, want the drawer %q", created[0].Category, "serien")
	}
	// Otherwise a staging pass that filed everything would pass too.
	if created[1].Category != "" {
		t.Errorf("a link no rule matched came back filed in %q", created[1].Category)
	}
}
