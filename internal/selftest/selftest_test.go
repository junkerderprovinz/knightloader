package selftest

// The one piece of logic in the vocabulary itself: how a parent row summarises
// its children.

import "testing"

func TestWorstRanksByHowMuchAttentionEachDeserves(t *testing.T) {
	cases := []struct {
		name string
		in   []Status
		want Status
	}{
		{"nothing at all was checked", nil, StatusSkipped},
		{"everything passed", []Status{StatusPass, StatusPass}, StatusPass},
		{"one warning among passes", []Status{StatusPass, StatusWarn, StatusPass}, StatusWarn},
		{"a failure beats a warning", []Status{StatusWarn, StatusFail}, StatusFail},
		// The pairing this function exists to get right. "Nothing is
		// configured here" and "it is configured and I cannot find out" are
		// not the same answer, and a parent holding one of each has an open
		// question in it - so the open question is what it must show.
		{"an open question beats a non-answer", []Status{StatusSkipped, StatusUnknown}, StatusUnknown},
		{"a pass does not hide an open question", []Status{StatusPass, StatusUnknown}, StatusUnknown},
		{"a pass does not hide a skip", []Status{StatusPass, StatusSkipped}, StatusSkipped},
		// A value from outside the five is a bug in whatever produced it.
		// Promoting it to "fail" would put an alarm in front of somebody for a
		// typo in a code path they cannot see.
		{"a status this function does not know is ignored", []Status{StatusPass, Status("nonsense")}, StatusPass},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Worst(c.in...); got != c.want {
				t.Fatalf("Worst(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestEveryPlannedCheckHasAnIdAndTheListIsWhatThePageDraws keeps Order and the
// seven constants from drifting apart. The page draws one pending row per entry
// in Order before a single result has landed, so an id that is in the list and
// nowhere else is a row that stays "waiting" for ever.
func TestEveryPlannedCheckHasAnIdAndTheListIsWhatThePageDraws(t *testing.T) {
	want := map[string]bool{
		CheckJD: true, CheckYtdlp: true, CheckFolders: true, CheckAccounts: true,
		CheckRelay: true, CheckClock: true, CheckTorrentPort: true,
	}
	if len(Order) != len(want) {
		t.Fatalf("Order has %d entries and there are %d check constants; a check in one and not the other "+
			"is either a row that never fills in or a result nothing draws", len(Order), len(want))
	}
	seen := map[string]bool{}
	for _, id := range Order {
		if !want[id] {
			t.Errorf("Order names %q, which is not one of the check constants", id)
		}
		if seen[id] {
			t.Errorf("Order names %q twice; the page would draw two rows for one check", id)
		}
		seen[id] = true
	}
}
