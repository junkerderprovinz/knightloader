package rules

// What the editor is told about the category action, and the one guard that
// keeps the telling honest.
//
// The grammar is a promise made to a browser this package cannot see, so a
// wrong answer here is not a failing call, it is a control that renders and
// edits the wrong field. That is not hypothetical for this particular action:
// the renderer switches on Kind and falls through to the accept/reject control
// for a kind it does not know, so the day "category" was added to Describe was
// the day a switch labelled "Category" could have started writing
// Action.Reject on the Packagizer tab. Both halves of that are checked below,
// the second one against the renderer's own source, because the note in
// grammar.go that used to carry the warning is a note and notes are read once.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ruleEditor is the renderer the grammar is describing itself to. Reading it
// from a Go test is the same move routes_test.go's registration guard makes:
// the two halves live in different languages and nothing else in the build
// compares them, so either they are compared here or they are not compared.
var ruleEditor = filepath.Join("..", "..", "web", "src", "components", "RuleEditor.tsx")

// fallThroughKind is the one Kind that is allowed to have no branch of its
// own, because it IS the last branch. Written down rather than derived: if the
// fall-through ever becomes some other action, this line has to change with it,
// and until it does the test says so out loud instead of quietly checking
// nothing.
const fallThroughKind = "reject"

// TestTheGrammarOffersTheCategoryAction pins the four things the browser reads
// off this entry. The id is the one worth the most: it is the JSON key the form
// posts, so a typo there is a control that edits nothing and reports success.
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
	// The id addresses Action.Category and not some neighbouring field. Marshal
	// rather than read the tag, because marshalling is what the browser will be
	// handed.
	b, err := json.Marshal(Action{Category: "serien"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"`+got.ID+`":"serien"`) {
		t.Errorf("an action posted under %q does not land on Action.Category; it marshals as %s", got.ID, b)
	}
}

// TestTheCategoryActionIsNotATemplate is the refusal the whole action rests on,
// checked here from the grammar's side rather than the engine's.
//
// Kind "template" would be the obvious entry to write, every other packagizer
// string being one, and it would be the wrong one twice over: the link's own
// host would get to spell its own drawer, and an id that does not exist until a
// link arrives is an id settings.ValidateCategories can never weigh, so the one
// check standing between a rule and a drawer that is not there would stop
// applying to exactly the rules that need it.
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

// TestEveryActionKindHasAControl is the interlock, and it exists because this
// grammar's failure mode is silent on both sides. A Kind the renderer does not
// know does not throw and does not render blank: it renders the LAST branch, a
// working accept/reject switch wired to Action.Reject, under whichever label
// the new action carries. Somebody switches "Category" to "reject" and the
// link is dropped.
//
// So the pairing is checked rather than remembered. Every kind Describe hands
// out has to be one the editor's own ActionGrammar union admits to, and every
// one except the fall-through has to be compared against somewhere in that
// file. Both spellings of the comparison are accepted so this fails when a
// branch is missing and not when somebody moves the chain into a switch.
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

// kindUnion is the declared list of kinds on ActionGrammar, which is the
// editor's own statement of what it can render. It is worth reading separately
// from the branches: TypeScript refuses a comparison against a literal the
// union does not hold, so a branch without an entry here does not compile and
// an entry here without a branch is the fall-through.
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

// rendersKind reports whether the editor branches on this kind anywhere. Both
// the if-chain the file uses today and a switch are accepted, and both quote
// styles, because the shape of the chain is the renderer's business and only
// the presence of the branch is this package's.
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
