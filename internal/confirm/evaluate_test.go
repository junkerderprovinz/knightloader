package confirm

import (
	"reflect"
	"strconv"
	"testing"
)

func TestEvaluatePlainItemsStart(t *testing.T) {
	items := []Item{{ID: "a"}, {ID: "b"}}
	res := Evaluate(items, Config{OnDupes: Exclude, OnOffline: Exclude})
	if !reflect.DeepEqual(res.Start, []string{"a", "b"}) {
		t.Errorf("Start = %v, want both plain items", res.Start)
	}
	if len(res.Remove) != 0 || len(res.Ask) != 0 {
		t.Errorf("an unmatched item produced Remove=%v Ask=%v", res.Remove, res.Ask)
	}
	if res.Summary != "" {
		t.Errorf("Summary = %q, want empty when nothing was held back", res.Summary)
	}
}

// TestOnlyOfflineIsEverExcludable is the caller's contract, restated at the
// boundary this package actually sees: Evaluate trusts Item.Offline exactly
// as given, so this only proves Evaluate does not second-guess a false. The
// real guarantee - that "unknown" and "uncheckable" availability never
// become Item.Offline=true - is the app-layer glue's job; see
// internal/app/app_confirm_test.go.
func TestOnlyOfflineIsEverExcludable(t *testing.T) {
	res := Evaluate([]Item{{ID: "a", Offline: false}}, Config{OnDupes: Exclude, OnOffline: Exclude})
	if !reflect.DeepEqual(res.Start, []string{"a"}) {
		t.Errorf("an item Evaluate was told is not offline was held back: %+v", res)
	}
}

func TestEvaluateExcludeLeavesItInTheCollector(t *testing.T) {
	res := Evaluate([]Item{{ID: "a", Offline: true}}, Config{OnDupes: Exclude, OnOffline: Exclude})
	if len(res.Start) != 0 || len(res.Remove) != 0 || len(res.Ask) != 0 {
		t.Fatalf("Exclude produced Start=%v Remove=%v Ask=%v, want none of the three", res.Start, res.Remove, res.Ask)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Verdict != Exclude {
		t.Errorf("Outcomes = %+v, want one Exclude verdict", res.Outcomes)
	}
}

func TestEvaluateExcludeAndRemoveDeletesIt(t *testing.T) {
	res := Evaluate([]Item{{ID: "a", Offline: true}}, Config{OnDupes: Exclude, OnOffline: ExcludeAndRemove})
	if !reflect.DeepEqual(res.Remove, []string{"a"}) {
		t.Errorf("Remove = %v, want [a]", res.Remove)
	}
	if len(res.Start) != 0 || len(res.Ask) != 0 {
		t.Errorf("exclude-and-remove also produced Start=%v Ask=%v", res.Start, res.Ask)
	}
}

func TestEvaluateAskListsThePersonHasToAnswerFor(t *testing.T) {
	res := Evaluate([]Item{{ID: "a", Duplicate: true}}, Config{OnDupes: Ask, OnOffline: Exclude})
	if !reflect.DeepEqual(res.Ask, []string{"a"}) {
		t.Errorf("Ask = %v, want [a]", res.Ask)
	}
	if len(res.Start) != 0 || len(res.Remove) != 0 {
		t.Errorf("ask also produced Start=%v Remove=%v; nothing may be settled until it is answered", res.Start, res.Remove)
	}
}

// TestEvaluateAskOutranksExcludeAndRemoveForTheSameItem is the precedence
// this package settles on for one item two facts disagree about: a fact that
// wants a person to look at it holds the whole item back from anything
// destructive, even when the other fact, taken alone, would have deleted it
// outright. See the severity table's own comment.
func TestEvaluateAskOutranksExcludeAndRemoveForTheSameItem(t *testing.T) {
	res := Evaluate(
		[]Item{{ID: "a", Duplicate: true, Offline: true}},
		Config{OnDupes: Ask, OnOffline: ExcludeAndRemove},
	)
	if !reflect.DeepEqual(res.Ask, []string{"a"}) {
		t.Fatalf("Ask = %v, want [a] - a pending question must hold off the deletion", res.Ask)
	}
	if len(res.Remove) != 0 {
		t.Errorf("Remove = %v, want nothing removed while a's duplicate question is unanswered", res.Remove)
	}
	if got := res.Outcomes[0].Reasons; !reflect.DeepEqual(got, []Reason{ReasonDuplicate}) {
		t.Errorf("Reasons = %v, want only [duplicate] - offline's exclude-and-remove did not decide anything here", got)
	}
}

// TestEvaluateReasonsOnlyBlameTheDecidingFact is the bug this package's own
// first draft had: an item that is a duplicate AND reported offline, where
// onDupes is happy to include duplicates but onOffline is not, must blame
// only "offline" - the duplicate fact applied but never actually held
// anything back.
func TestEvaluateReasonsOnlyBlameTheDecidingFact(t *testing.T) {
	res := Evaluate(
		[]Item{{ID: "a", Duplicate: true, Offline: true}},
		Config{OnDupes: Include, OnOffline: Exclude},
	)
	if len(res.Outcomes) != 1 {
		t.Fatalf("Outcomes = %+v, want exactly one", res.Outcomes)
	}
	got := res.Outcomes[0]
	if got.Verdict != Exclude {
		t.Fatalf("Verdict = %q, want exclude", got.Verdict)
	}
	if !reflect.DeepEqual(got.Reasons, []Reason{ReasonOffline}) {
		t.Errorf("Reasons = %v, want only [offline]; onDupes included duplicates on purpose", got.Reasons)
	}
	if len(res.Start) != 0 {
		t.Errorf("Start = %v, want a held back even though its duplicate side was included", res.Start)
	}
}

// TestEvaluateCombinesBothReasonsInOnePass pins the worked example this
// whole feature was written around: two independent reasons, reported once,
// combined - not as two prompts back to back.
func TestEvaluateCombinesBothReasonsInOnePass(t *testing.T) {
	var items []Item
	for i := 0; i < 3; i++ {
		items = append(items, Item{ID: "offline" + strconv.Itoa(i), Offline: true})
	}
	for i := 0; i < 2; i++ {
		items = append(items, Item{ID: "dupe" + strconv.Itoa(i), Duplicate: true})
	}
	res := Evaluate(items, Config{OnDupes: Exclude, OnOffline: Exclude})

	want := "3 offline and 2 duplicate links were not started."
	if res.Summary != want {
		t.Errorf("Summary = %q, want %q", res.Summary, want)
	}
	if len(res.Start) != 0 {
		t.Errorf("Start = %v, want none - every item matched one of the two policies", res.Start)
	}
	if len(res.Outcomes) != 5 {
		t.Errorf("Outcomes has %d entries, want 5 (one per item)", len(res.Outcomes))
	}
}

func TestSummarySingularWording(t *testing.T) {
	res := Evaluate([]Item{{ID: "a", Offline: true}}, Config{OnDupes: Exclude, OnOffline: Exclude})
	if want := "1 offline link was not started."; res.Summary != want {
		t.Errorf("Summary = %q, want %q", res.Summary, want)
	}
}

func TestSummaryExcludeAndRemoveWordingIsHonest(t *testing.T) {
	res := Evaluate([]Item{{ID: "a", Offline: true}}, Config{OnDupes: Exclude, OnOffline: ExcludeAndRemove})
	if want := "1 offline link was removed."; res.Summary != want {
		t.Errorf("Summary = %q, want %q - removal must read differently from merely not starting", res.Summary, want)
	}
}

func TestSummaryReportsExcludeAndExcludeAndRemoveAsSeparateClauses(t *testing.T) {
	res := Evaluate(
		[]Item{{ID: "held", Offline: true}, {ID: "gone", Duplicate: true}},
		Config{OnDupes: ExcludeAndRemove, OnOffline: Exclude},
	)
	want := "1 offline link was not started; 1 duplicate link was removed."
	if res.Summary != want {
		t.Errorf("Summary = %q, want %q", res.Summary, want)
	}
}
