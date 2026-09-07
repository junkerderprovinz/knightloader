package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
)

func TestHeaderProfileIsAppliedLikeEveryOtherAction(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{
			Name:       "forum",
			Conditions: []Condition{{Field: FieldHoster, Op: OpEquals, Value: "forum.example.org"}},
			Action:     Action{Headers: "forum"},
		},
		{
			Name:       "the seedbox wins for its own host",
			Conditions: []Condition{{Field: FieldHoster, Op: OpEquals, Value: "box.example.net"}},
			Action:     Action{Headers: "seedbox"},
		},
	}})
	if len(problems) > 0 {
		t.Fatalf("Compile: %v", problems)
	}
	cases := []struct {
		url  string
		want string
	}{
		{"https://forum.example.org/attachments/1/x.rar", "forum"},
		{"https://box.example.net/files/x.mkv", "seedbox"},
		{"https://elsewhere.example.com/x.rar", ""},
	}
	for _, c := range cases {
		if got := m.Apply(Candidate{URL: c.url}).Headers; got != c.want {
			t.Errorf("Apply(%q).Headers = %q, want %q", c.url, got, c.want)
		}
	}
}

// TestALaterRuleWinsTheProfileAndAnEmptyOneLeavesItAlone. Same convention as
// every other field: an empty string is "this rule has no opinion", never
// "clear it", so a rule that only sets a folder must not strip the credential
// an earlier rule attached.
func TestALaterRuleWinsTheProfileAndAnEmptyOneLeavesItAlone(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "everything gets the forum profile", Action: Action{Headers: "forum"}},
		{Name: "rars land in their own folder", Action: Action{DownloadDir: "/dl/rar"}},
	}})
	if len(problems) > 0 {
		t.Fatalf("Compile: %v", problems)
	}
	e := m.Apply(Candidate{URL: "https://forum.example.org/x.rar"})
	if e.Headers != "forum" {
		t.Errorf("Headers = %q, want the earlier rule's profile to survive a later rule that says nothing", e.Headers)
	}

	m2, _ := Compile(Set{Rules: []Rule{
		{Name: "first", Action: Action{Headers: "forum"}},
		{Name: "second", Action: Action{Headers: "seedbox"}},
	}})
	if got := m2.Apply(Candidate{URL: "https://x.example.org/y.rar"}).Headers; got != "seedbox" {
		t.Errorf("Headers = %q, want the later rule to win", got)
	}
}

// TestAProfileNameIsNotATemplate is the security assertion on this side.
//
// Expanding it would let the link's own host decide which credential gets
// attached to it - a page that names itself "forum" would collect the forum
// profile - and that is the one decision this feature must never hand to the
// far end.
func TestAProfileNameIsNotATemplate(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{{
		Name:   "would be a placeholder if this were a template",
		Action: Action{Headers: "profile-for-hoster", Comment: "<jd:hoster>"},
	}}})
	if len(problems) > 0 {
		t.Fatalf("Compile: %v", problems)
	}
	e := m.Apply(Candidate{URL: "https://forum.example.org/x.rar"})
	if e.Comment != "forum.example.org" {
		t.Fatalf("Comment = %q, so this test is not actually exercising expansion", e.Comment)
	}
	if e.Headers != "profile-for-hoster" {
		t.Errorf("Headers = %q, want the name through verbatim", e.Headers)
	}
	// And a name that LOOKS like a template is refused outright rather than
	// carried through with the angle brackets in it, which would produce a
	// lookup that can never hit.
	_, probs := Compile(Set{Rules: []Rule{{Action: Action{Headers: "<jd:hoster>"}}}})
	if len(probs) == 0 {
		t.Error("a profile name holding a placeholder was accepted")
	}
}

// TestTheRuleEditorAndTheProfileStoreAgreeOnWhatANameIs. The user types this
// string in two places - the rule and the profile list - and the two have to
// match exactly. A rule that compiles cleanly and can never address a profile
// looks exactly like the feature not working.
func TestTheRuleEditorAndTheProfileStoreAgreeOnWhatANameIs(t *testing.T) {
	cases := []string{
		"forum", "Forum", "my-box_2.0", "  padded  ",
		"has space", "a/b", "a:b", "nul\x00", "<jd:hoster>",
		strings.Repeat("x", MaxHeaderProfile), strings.Repeat("x", MaxHeaderProfile+1),
	}
	for _, name := range cases {
		accepted := headerProfileProblem(name) == ""
		addressable := hostheaders.ProfileID(name) != ""
		if accepted != addressable {
			t.Errorf("%q: the rule engine accepts=%v, the profile store can address it=%v", name, accepted, addressable)
		}
	}
	if MaxHeaderProfile != hostheaders.MaxProfileID {
		t.Errorf("MaxHeaderProfile = %d, hostheaders.MaxProfileID = %d; a rule could name a profile nothing can store",
			MaxHeaderProfile, hostheaders.MaxProfileID)
	}
}

// TestABrokenProfileNameCostsTheRuleAndNothingElse. Compile drops a rule with
// any problem at all rather than applying part of it, which is what makes a
// typo cost one rule instead of a whole list.
func TestABrokenProfileNameCostsTheRuleAndNothingElse(t *testing.T) {
	m, problems := Compile(Set{Rules: []Rule{
		{Name: "broken", Action: Action{Headers: "not a name", DownloadDir: "/dl/wrong"}},
		{Name: "fine", Action: Action{DownloadDir: "/dl/right"}},
	}})
	if len(problems) != 1 || problems[0].Rule != "broken" {
		t.Fatalf("problems = %v, want exactly one about the broken rule", problems)
	}
	if !strings.Contains(problems[0].Message, "header profile") {
		t.Errorf("message = %q, want it to name the box that is wrong", problems[0].Message)
	}
	if got := m.Apply(Candidate{URL: "https://x.example.org/y.rar"}).Dir; got != "/dl/right" {
		t.Errorf("Dir = %q, want the surviving rule still applied", got)
	}
}

// TestTheGrammarDoesNotYetOfferTheProfileAction pins a deliberate absence, and
// the pin is the point: the editor's action renderer switches on Kind and
// falls through to the accept/reject control for a kind it does not know, so
// describing this action before that renderer has a "profile" branch would put
// a working switch on the Packagizer tab that edits Action.Reject.
//
// The day somebody adds the branch, this test is what tells them the grammar
// line is the other half of the job. See grammar.go, where the line is written
// out.
func TestTheGrammarDoesNotYetOfferTheProfileAction(t *testing.T) {
	for _, a := range Describe().Actions {
		if a.ID == "headers" {
			t.Fatalf("the grammar describes the headers action as kind %q; the editor has to learn that kind first", a.Kind)
		}
	}
	// The JSON id is still pinned, because that is what the form will address
	// once it does offer the control - and a typo there is a control that
	// quietly does nothing.
	b, err := json.Marshal(Action{Headers: "forum"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"headers":"forum"`) {
		t.Errorf("Action marshals as %s, want a headers key", b)
	}
}
