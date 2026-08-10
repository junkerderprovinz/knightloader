package app

// Pins the add-links form's own precedence decision (build-plan.md §4
// conflict 5, §8's Wave 8 amendment): a Packagizer rule wins over the form's
// priority, unpacking switch and comment by default, Overrule inverts that,
// and the destination always wins regardless, because a hand-picked folder is
// not a property the form and a rule are contending over.

import (
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// filmsRule is the one rule every test below matches against: everything from
// films.example gets its own package, folder, comment, priority and
// auto-extract - the same fixture rules_wiring_test.go uses, so a form-vs-rule
// test and a rule-only test are provably looking at the same rule.
func filmsRule(base string) (rules.Set, string, int, bool) {
	dir := filepath.Join(base, "Films")
	prio, yes := 1, true
	return rules.Set{Rules: []rules.Rule{{
		Name: "films go together",
		Conditions: []rules.Condition{
			{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "films.example"},
		},
		Action: rules.Action{
			DownloadDir: dir,
			Comment:     "collected by the films rule",
			Priority:    &prio,
			AutoExtract: &yes,
		},
	}}}, dir, prio, yes
}

// TestFormOptionsApplyWithNoRuleInvolved is the plain case: nothing about the
// Packagizer, so every field the form supplied lands on the task unchanged.
func TestFormOptionsApplyWithNoRuleInvolved(t *testing.T) {
	a, base := newRuleApp(t, func(s *settings.Settings, base string) {})

	formDir := filepath.Join(base, "Elsewhere")
	prio, no := -2, false
	created, err := a.AddLinksWithOptions([]string{"https://host.example/one.bin"}, "", OriginPaste, LinkBatchOptions{
		Dir:              formDir,
		Password:         "archivepw",
		DownloadPassword: "linkpw",
		Comment:          "from the form",
		Priority:         &prio,
		AutoExtract:      &no,
	})
	if err != nil {
		t.Fatalf("AddLinksWithOptions: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]
	if got.Dir != formDir {
		t.Errorf("dir = %q, want %q", got.Dir, formDir)
	}
	if got.Password != "archivepw" {
		t.Errorf("archive password = %q, want the form's own", got.Password)
	}
	if got.DownloadPassword != "linkpw" {
		t.Errorf("link password = %q, want the form's own", got.DownloadPassword)
	}
	if got.Comment != "from the form" {
		t.Errorf("comment = %q, want the form's own", got.Comment)
	}
	if got.Priority != -2 {
		t.Errorf("priority = %d, want -2", got.Priority)
	}
	if got.AutoExtract == nil || *got.AutoExtract {
		t.Errorf("auto-extract = %v, want the form's own off", got.AutoExtract)
	}
	if a.dirFor(got) != formDir {
		t.Errorf("dirFor answered %q, want %q", a.dirFor(got), formDir)
	}
}

// TestPackagizerWinsOverFormByDefault is the default this wave had to decide:
// form values apply at stage time and the Packagizer runs after them, so a
// rule wins unless the form says otherwise - EXCEPT the destination, which the
// form always wins because a hand-picked folder was never something a rule
// and the form were contending over.
func TestPackagizerWinsOverFormByDefault(t *testing.T) {
	var ruleDir string
	a, base := newRuleApp(t, func(s *settings.Settings, base string) {
		var rule rules.Set
		rule, ruleDir, _, _ = filmsRule(base)
		s.Packagizer = rule
	})

	formDir := filepath.Join(base, "FormChoice")
	formPrio, formOff := -3, false
	created, err := a.AddLinksWithOptions([]string{"https://films.example/one.mkv"}, "", OriginPaste, LinkBatchOptions{
		Dir:         formDir,
		Comment:     "from the form",
		Priority:    &formPrio,
		AutoExtract: &formOff,
		// Overrule deliberately left off.
	})
	if err != nil {
		t.Fatalf("AddLinksWithOptions: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]

	// The rule's answer, because Overrule was never set.
	if got.Comment != "collected by the films rule" {
		t.Errorf("comment = %q, want the rule's own (Overrule was off)", got.Comment)
	}
	if got.Priority != 1 {
		t.Errorf("priority = %d, want the rule's own 1 (Overrule was off)", got.Priority)
	}
	if got.AutoExtract == nil || !*got.AutoExtract {
		t.Errorf("auto-extract = %v, want the rule's own on (Overrule was off)", got.AutoExtract)
	}

	// The form's own answer regardless, because a hand-picked destination is
	// not part of the Overrule bargain.
	if got.Dir != formDir {
		t.Errorf("dir = %q, want the form's own %q even with Overrule off", got.Dir, formDir)
	}
	if ruleDir == formDir {
		t.Fatal("test fixture bug: the rule and the form must disagree about the folder")
	}
	if a.dirFor(got) != formDir {
		t.Errorf("dirFor answered %q, want the form's folder %q", a.dirFor(got), formDir)
	}
}

// TestOverruleMakesTheFormWin is the checkbox's whole reason to exist: the
// same rule, the same form values, Overrule on this time - and now the form's
// priority, comment and auto-extract stand instead of the rule's.
func TestOverruleMakesTheFormWin(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
		rule, _, _, _ := filmsRule(base)
		s.Packagizer = rule
	})

	formPrio, formOff := -3, false
	created, err := a.AddLinksWithOptions([]string{"https://films.example/one.mkv"}, "", OriginPaste, LinkBatchOptions{
		Comment:     "from the form",
		Priority:    &formPrio,
		AutoExtract: &formOff,
		Overrule:    true,
	})
	if err != nil {
		t.Fatalf("AddLinksWithOptions: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d tasks", len(created))
	}
	got := created[0]

	if got.Comment != "from the form" {
		t.Errorf("comment = %q, want the form's own (Overrule was on)", got.Comment)
	}
	if got.Priority != -3 {
		t.Errorf("priority = %d, want the form's own -3 (Overrule was on)", got.Priority)
	}
	if got.AutoExtract == nil || *got.AutoExtract {
		t.Errorf("auto-extract = %v, want the form's own off (Overrule was on)", got.AutoExtract)
	}
	// The rule still named the package and matched: Overrule inverts what wins
	// per field, it does not switch the Packagizer off.
	if len(got.MatchedRules) != 1 || got.MatchedRules[0] != "films go together" {
		t.Errorf("matched rules = %v, want the rule to still say it fired", got.MatchedRules)
	}
}

// TestFormOptionsReachCrawledLinks pins the other half of why these values
// live on intake rather than being applied once after addLinksFrom returns: a
// pasted page that crawls into several files must hand every one of them the
// batch's own priority and comment, not just the page URL that never becomes
// a task.
func TestFormOptionsReachCrawledLinks(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) { s.Crawl = true })
	a.Crawler = &fakeCrawler{yield: []crawler.Result{
		{URL: "https://host.example/one.bin", Name: "one.bin"},
		{URL: "https://host.example/two.bin", Name: "two.bin"},
	}}

	prio := 2
	created, err := a.AddLinksWithOptions([]string{"https://host.example/gallery"}, "Batch", OriginPaste, LinkBatchOptions{
		Comment:  "from the form",
		Priority: &prio,
	})
	if err != nil {
		t.Fatalf("AddLinksWithOptions: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("staged %d tasks, want the 2 files the page pointed at", len(created))
	}
	for _, task := range created {
		if task.Comment != "from the form" {
			t.Errorf("%s comment = %q, want the batch's own", task.Name, task.Comment)
		}
		if task.Priority != 2 {
			t.Errorf("%s priority = %d, want the batch's own 2", task.Name, task.Priority)
		}
	}
}

// TestInvalidDestinationRefusesTheWholeBatch is the atomic-refusal choice: a
// destination that cannot be used stops the whole batch before anything is
// staged, rather than staging every link to the default folder and reporting
// the mistake only in a log nobody watching the form will read.
func TestInvalidDestinationRefusesTheWholeBatch(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})

	_, err := a.AddLinksWithOptions([]string{"https://host.example/one.bin"}, "", OriginPaste, LinkBatchOptions{
		Dir: "relative/not/absolute",
	})
	if err == nil {
		t.Fatal("want an error for a relative destination, got nil")
	}
	if n := len(a.Tasks()); n != 0 {
		t.Errorf("%d tasks staged despite the refused destination, want none", n)
	}
}

// TestArchivePasswordIsRememberedForLaterArchives mirrors what
// AddLinksWithPasswords already does for a Click'n'Load submission: a
// password the form supplies is folded into the global list too, so an
// unrelated later archive from the same source can still be opened.
func TestArchivePasswordIsRememberedForLaterArchives(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})

	if _, err := a.AddLinksWithOptions([]string{"https://host.example/one.rar"}, "", OriginPaste, LinkBatchOptions{
		Password: "hunter2",
	}); err != nil {
		t.Fatalf("AddLinksWithOptions: %v", err)
	}
	pw := a.Settings.Get().ArchivePasswords
	found := false
	for _, p := range pw {
		if p == "hunter2" {
			found = true
		}
	}
	if !found {
		t.Errorf("archive passwords = %v, want the form's password kept for later archives", pw)
	}
}

// TestZeroValueOptionsBehaveLikeAPlainPaste guards the route's own
// simplification: it always calls AddLinksWithOptions once body.Passwords is
// empty, which is only safe if an all-zero LinkBatchOptions changes nothing
// about the ordinary paste path.
func TestZeroValueOptionsBehaveLikeAPlainPaste(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {})

	withOptions, err := a.AddLinksWithOptions([]string{"https://host.example/plain.bin"}, "Batch", OriginPaste, LinkBatchOptions{})
	if err != nil {
		t.Fatalf("AddLinksWithOptions: %v", err)
	}
	plain := a.AddLinks([]string{"https://host.example/plain2.bin"}, "Batch")
	if len(withOptions) != 1 || len(plain) != 1 {
		t.Fatalf("staged %d and %d tasks, want one each", len(withOptions), len(plain))
	}
	a1, a2 := withOptions[0], plain[0]
	if a1.Dir != a2.Dir || a1.Password != a2.Password || a1.DownloadPassword != a2.DownloadPassword ||
		a1.Comment != a2.Comment || a1.Priority != a2.Priority || (a1.AutoExtract == nil) != (a2.AutoExtract == nil) {
		t.Errorf("a zero-value batch diverged from a plain paste:\n%+v\n%+v", a1, a2)
	}
}
