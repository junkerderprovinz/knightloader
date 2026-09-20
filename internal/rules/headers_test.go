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

// TestALaterRuleWinsTheProfileAndAnEmptyOneLeavesItAlone: as with every other
// field, a rule that only sets a folder keeps the profile an earlier rule
// attached.
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

// TestAProfileNameIsNotATemplate: expanding it would let the link's own host
// decide which credential gets attached to it.
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
	// A name that looks like a template is refused, since a lookup with the
	// angle brackets in it could never hit.
	_, probs := Compile(Set{Rules: []Rule{{Action: Action{Headers: "<jd:hoster>"}}}})
	if len(probs) == 0 {
		t.Error("a profile name holding a placeholder was accepted")
	}
}

// TestTheRuleEditorAndTheProfileStoreAgreeOnWhatANameIs: the name is typed in
// the rule and in the profile list, and both sides must accept the same set.
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

// TestTheGrammarDoesNotYetOfferTheProfileAction: the editor falls through to
// the reject control for an unknown kind, so the headers action must stay out
// of the grammar until RuleEditor.tsx has a "profile" branch (see grammar.go).
func TestTheGrammarDoesNotYetOfferTheProfileAction(t *testing.T) {
	for _, a := range Describe().Actions {
		if a.ID == "headers" {
			t.Fatalf("the grammar describes the headers action as kind %q; the editor has to learn that kind first", a.Kind)
		}
	}
	// The JSON key is pinned, since it is what the form will address.
	b, err := json.Marshal(Action{Headers: "forum"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"headers":"forum"`) {
		t.Errorf("Action marshals as %s, want a headers key", b)
	}
}
