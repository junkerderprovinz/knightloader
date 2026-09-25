package rules

// The editor renders a control per action Kind and falls through to the
// accept/reject control for a kind it does not know, so a wrong grammar entry
// renders a control that edits Action.Reject. These tests check the grammar
// and, against the renderer's source, that every kind has a branch.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ruleEditor is the renderer the grammar describes itself to. Nothing else in
// the build compares the Go and TypeScript halves, as with routes_test.go's
// registration guard.
var ruleEditor = filepath.Join("..", "..", "web", "src", "components", "RuleEditor.tsx")

// fallThroughKind is the one Kind with no branch of its own, because it is the
// last branch.
const fallThroughKind = "reject"

// TestTheGrammarOffersTheCategoryAction pins what the browser reads off this
// entry. The id is the JSON key the form posts, so a typo there would edit
// nothing and report success.
func TestTheGrammarOffersTheCategoryAction(t *testing.T) {
	var got ActionGrammar
	for _, a := range Describe().Actions {
		if a.ID == "category" {
			got = a
		}
	}
	if got.ID == "" {
		t.Fatal("the grammar describes no category action, so the rule editor cannot offer one")
	}
	if got.Kind != "category" {
		t.Errorf("kind = %q; every kind the renderer does not know falls through to the reject switch", got.Kind)
	}
	if got.Flavour != "packagizer" {
		t.Errorf("flavour = %q, want packagizer: a rejected link never gets a drawer", got.Flavour)
	}
	if got.Max == nil || *got.Max != MaxCategoryRef {
		t.Errorf("max = %v, want the %d categoryProblem refuses above", got.Max, MaxCategoryRef)
	}
	// Marshalled rather than read off the tag, since that is what the browser
	// gets.
	b, err := json.Marshal(Action{Category: "serien"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"`+got.ID+`":"serien"`) {
		t.Errorf("an action posted under %q does not land on Action.Category; it marshals as %s", got.ID, b)
	}
}

// TestTheCategoryActionIsNotATemplate: a template would let the link's host
// pick its own category and put the id out of reach of
// settings.ValidateCategories.
func TestTheCategoryActionIsNotATemplate(t *testing.T) {
	for _, a := range Describe().Actions {
		if a.ID == "category" && a.Kind == "template" {
			t.Fatal("the category action is described as a template; a drawer assembled at match time cannot be checked against the table by anything")
		}
	}
	if _, probs := Compile(Set{Rules: []Rule{{Name: "vom hoster", Action: Action{Category: "<jd:hoster>"}}}}); len(probs) != 1 {
		t.Fatalf("the engine accepted a category built out of the link; problems = %v", probs)
	}
}

// TestEveryActionKindHasAControl: a Kind the renderer does not know renders as
// a working accept/reject switch under the new action's label. Every kind
// Describe hands out must be in the editor's ActionGrammar union, and every
// one except the fall-through must have a branch, written as an if-chain or a
// switch.
func TestEveryActionKindHasAControl(t *testing.T) {
	src, err := os.ReadFile(ruleEditor)
	if err != nil {
		t.Fatalf("the rule editor could not be read, so nothing here is checked: %v", err)
	}
	text := string(src)
	union, ok := kindUnion(text)
	if !ok {
		t.Fatalf("%s no longer declares a kind on interface ActionGrammar; this guard cannot see what the renderer handles", ruleEditor)
	}
	seen := map[string]bool{}
	for _, a := range Describe().Actions {
		if seen[a.Kind] {
			continue
		}
		seen[a.Kind] = true
		if !strings.Contains(union, "'"+a.Kind+"'") && !strings.Contains(union, `"`+a.Kind+`"`) {
			t.Errorf("the grammar hands out the kind %q for action %q, and %s does not list it on ActionGrammar.kind; "+
				"add it to the union and give it a branch, or the control renders as the reject switch",
				a.Kind, a.ID, ruleEditor)
		}
		if a.Kind == fallThroughKind {
			continue
		}
		if !rendersKind(text, a.Kind) {
			t.Errorf("the grammar hands out the kind %q for action %q, and %s never compares against it; "+
				"the %q control on the rules page is therefore the accept/reject switch, writing Action.Reject. "+
				"Add a branch (action.kind === %q) before the final return",
				a.Kind, a.ID, ruleEditor, a.ID, a.Kind)
		}
	}
}

// kindUnion is the declared list of kinds on ActionGrammar. TypeScript refuses
// a branch on a literal the union lacks, so an entry here without a branch is
// the fall-through.
func kindUnion(text string) (string, bool) {
	_, after, ok := strings.Cut(text, "interface ActionGrammar {")
	if !ok {
		return "", false
	}
	body, _, ok := strings.Cut(after, "}")
	if !ok {
		return "", false
	}
	for _, line := range strings.Split(body, "\n") {
		if field, rest, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(field) == "kind" {
			return rest, true
		}
	}
	return "", false
}

// rendersKind reports whether the editor branches on this kind anywhere, as an
// if-chain or a switch, in either quote style.
func rendersKind(text, kind string) bool {
	for _, form := range []string{
		"kind === '" + kind + "'",
		`kind === "` + kind + `"`,
		"case '" + kind + "'",
		`case "` + kind + `"`,
	} {
		if strings.Contains(text, form) {
			return true
		}
	}
	return false
}

// The engine moves unpacked files to Action.ExtractDir, so the editor has to be
// able to write it: an action the grammar leaves out exists only for imported
// rule sets.
func TestTheGrammarOffersTheFolderUnpackedFilesMoveTo(t *testing.T) {
	var got ActionGrammar
	for _, a := range Describe().Actions {
		if a.ID == "extractDir" {
			got = a
		}
	}
	if got.Kind != "template" || got.Flavour != "packagizer" {
		t.Fatalf("the extractDir action is %+v, want a packagizer template", got)
	}
	body, err := json.Marshal(map[string]string{got.ID: "/serien/<jd:packagename>"})
	if err != nil {
		t.Fatal(err)
	}
	var a Action
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatal(err)
	}
	if a.ExtractDir != "/serien/<jd:packagename>" {
		t.Errorf("a folder posted under %q lands as %+v, not on Action.ExtractDir", got.ID, a)
	}
}
