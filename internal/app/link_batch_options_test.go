package app

// Precedence between the add-links form and the Packagizer: a rule wins over
// the form's priority, unpacking switch and comment by default, Overrule
// inverts that, and the form's destination always wins, because a hand-picked
// folder is not a property the two are contending over.

import (
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// filmsRule gives everything from films.example its own package, folder,
// comment, priority and auto-extract. It is the fixture rules_wiring_test.go
// uses, so both files look at the same rule.
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

// With no rule involved, every field the form supplied lands on the task
// unchanged.
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

// Form values apply at stage time and the Packagizer runs after them, so a rule
// wins unless the form says otherwise. The destination is the exception: a
// hand-picked folder stands either way.
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
		// Overrule left off.
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

// The same rule and the same form values with Overrule on: the form's priority,
// comment and auto-extract stand instead of the rule's.
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

// The batch's values live on intake rather than being applied to what
// addLinksFrom returns, so a pasted page that crawls into several files hands
// every one of them the batch's priority and comment.
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

// A batch filed under a category starts at the category's priority, as a link
// a rule files there does, and a rule that names a priority still wins.
func TestABatchCategorySetsThePriorityItsLinksStartAt(t *testing.T) {
	low := -2
	serien := settings.Category{ID: "serien", Name: "Serien", Priority: &low}

	t.Run("no rule involved", func(t *testing.T) {
		a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
			s.Categories = []settings.Category{serien}
		})
		created, err := a.AddLinksWithOptions([]string{"https://host.example/one.mkv"}, "", OriginPaste, LinkBatchOptions{Category: "serien"})
		if err != nil || len(created) != 1 {
			t.Fatalf("staged %d tasks: %v", len(created), err)
		}
		if got := created[0]; got.Category != "serien" || got.Priority != low {
			t.Errorf("category %q at priority %d, want serien at its %d", got.Category, got.Priority, low)
		}
	})

	t.Run("a rule naming a priority", func(t *testing.T) {
		var rulePrio int
		a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
			s.Categories = []settings.Category{serien}
			s.Packagizer, _, rulePrio, _ = filmsRule(base)
		})
		created, err := a.AddLinksWithOptions([]string{"https://films.example/one.mkv"}, "", OriginPaste, LinkBatchOptions{Category: "serien"})
		if err != nil || len(created) != 1 {
			t.Fatalf("staged %d tasks: %v", len(created), err)
		}
		if got := created[0]; got.Category != "serien" || got.Priority != rulePrio {
			t.Errorf("category %q at priority %d, want serien at the rule's %d", got.Category, got.Priority, rulePrio)
		}
	})
}

// A batch added stopped stays in the collector even when auto-confirm would
// send it on at once, while the next one goes.
func TestABatchKeptInTheCollectorIsNotAutoConfirmed(t *testing.T) {
	a, _ := newRuleApp(t, func(s *settings.Settings, base string) {
		s.AutoConfirm, s.AutoConfirmDelay = true, 0
	})
	a.SetHalted(true)

	kept, err := a.AddLinksWithOptions([]string{"https://host.example/kept.bin"}, "", OriginPaste, LinkBatchOptions{KeepCollected: true})
	if err != nil || len(kept) != 1 {
		t.Fatalf("staged %d tasks: %v", len(kept), err)
	}
	sent, err := a.AddLinksWithOptions([]string{"https://host.example/sent.bin"}, "", OriginPaste, LinkBatchOptions{})
	if err != nil || len(sent) != 1 {
		t.Fatalf("staged %d tasks: %v", len(sent), err)
	}
	if got := liveTask(a, kept[0].ID).Status; got != core.StatusCollected {
		t.Errorf("the batch added stopped is %s, want it left in the collector", got)
	}
	if got := liveTask(a, sent[0].ID).Status; got == core.StatusCollected {
		t.Error("the ordinary batch is still in the collector, so auto-confirm is not on and the test proves nothing")
	}
}

// A destination that cannot be used stops the whole batch before anything is
// staged, rather than sending every link to the default folder and reporting
// the mistake in a log alone.
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

// As with a Click'n'Load submission, a password the form supplies is folded
// into the global list, so a later archive from the same source can be opened.
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

// The route calls AddLinksWithOptions for every paste, which is only safe if an
// all-zero LinkBatchOptions changes nothing about the ordinary path.
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
