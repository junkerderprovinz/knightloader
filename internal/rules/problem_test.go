package rules

import "testing"

// TestProblemNamesTheCondition is what lets the editor draw the message on the
// row that caused it. Without the number, a rule with six conditions reports
// "condition 3 (filename): ..." under the rule and leaves the user counting.
func TestProblemNamesTheCondition(t *testing.T) {
	_, problems := Compile(Set{Rules: []Rule{{
		Name: "mixed",
		Conditions: []Condition{
			{Field: FieldURL, Op: OpContains, Value: "films"},
			{Field: FieldFilename, Op: OpMatches, Value: "("},
		},
		// An int action outside its bound, so the same rule also produces a
		// problem that belongs to no condition at all.
		Action: Action{Priority: intPtr(PriorityMax + 5)},
	}}})

	if len(problems) != 2 {
		t.Fatalf("got %d problems, want one for the pattern and one for the priority: %v", len(problems), problems)
	}
	var conditionProblems, actionProblems int
	for _, p := range problems {
		switch p.Condition {
		case 0:
			actionProblems++
		case 2:
			conditionProblems++
		default:
			t.Errorf("problem %q points at condition %d, which is not the broken one", p.Message, p.Condition)
		}
	}
	if conditionProblems != 1 || actionProblems != 1 {
		t.Errorf("got %d condition problems and %d action problems, want one of each",
			conditionProblems, actionProblems)
	}
}
