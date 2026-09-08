package rules_test

// The category action end to end, from the id the grammar advertises to the
// drawer a staged task lands in.
//
// It is an EXTERNAL test package, which is the only unusual thing here and is
// the whole point of the file. What this action promises crosses three
// packages: rules describes the control and carries the value, settings owns
// the table and is the only thing that refuses a rule naming a drawer that is
// not there, app copies the value onto the task. rules cannot import settings
// (settings imports rules), so an in-package test can see the first third of
// that and nothing else. A test that can only reach a third of the chain would
// have reported success for a grammar entry pointing at a field nobody reads.
//
// The two claims below are the two halves that would fail silently:
//
//   - The refusal is REACHABLE. The check exists whether or not the editor can
//     produce the value; the question this answers is whether the id the
//     grammar hands the browser is the id settings weighs against the table.
//   - The value ARRIVES. A rule naming a real drawer puts it on Task.Category,
//     which is what every category behaviour downstream reads.
//
// Both build their Action out of the grammar's own advertised id, through the
// JSON the browser posts, rather than by setting Action.Category directly.
// Setting the field would test the field; the promise being tested is that the
// form's key and the engine's field are the same key.
//
// The cost of the arrangement, paid knowingly: this package's TESTS now build
// against app and everything app reaches, so an unrelated package that will not
// compile takes these tests down with it while the engine itself is fine. That
// is the price of the only vantage point the whole promise is visible from, and
// a guard that can see two thirds of a wire is a guard that reports nothing
// about the third.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// categoryActionID is the JSON key the grammar tells the editor to post under.
// Read rather than hard-coded, so a rename of the grammar entry moves both
// tests with it instead of leaving them passing against a key nothing sends.
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

// TestARuleFilingLinksInADrawerThatIsNotThereIsRefused is the check the grammar
// entry must not go around.
//
// A rule pointing at a category that does not exist does nothing at all, on
// every link it matches, for ever, with no error anywhere: the id is copied
// onto the task, settings.CategoryFor finds nothing under it and answers with
// the empty category, and the download lands wherever it would have landed with
// no rule at all. That is the failure this whole subsystem exists to refuse,
// and it is refused at save time by the one place that can see both halves.
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
	// The message has to name the rule and the id, because the person reading it
	// is looking at a list of rules and has to know which box to fix.
	for _, want := range []string{"Serien nach Serien", "serien"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
	// And the same rule against the drawer that does exist has to pass, or the
	// check is refusing the feature rather than the mistake.
	s.Packagizer.Rules[0].Action = actionFromGrammar(t, "filme")
	if err := s.ValidateCategories(); err != nil {
		t.Errorf("a rule naming an existing drawer was refused: %v", err)
	}
}

// TestARuleNamingARealDrawerFilesTheTask is the other end of the same wire, run
// through the real staging path rather than through Apply.
//
// Apply returning a category proves the engine agrees with itself. What matters
// to somebody who wrote the rule is that the link ends up IN the drawer, and
// between those two there is a hand-off in app.packagize that only copies a
// field a rule actually set. Staging the link is the only way to see that
// hand-off happen.
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

	// AddResolvedLinksFrom and not AddLinks: the link is already named and
	// sized, so nothing here reaches the network, and it runs the same
	// packagizer hand-off the pasted path does.
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
	// The link no rule matched must stay unfiled. An empty category is "nobody
	// had an opinion" and not "the default drawer", and a staging pass that
	// filed everything would look exactly like this test passing.
	if created[1].Category != "" {
		t.Errorf("a link no rule matched came back filed in %q", created[1].Category)
	}
}
