package app

// The speed limit shared out between the three meters (see app_budget.go).

import "testing"

// The shares never add up to more than the limit; handing each meter the full
// limit would triple it.
func TestShareOutNeverExceedsTheLimit(t *testing.T) {
	const limit = 10 << 20 // 10 MiB/s

	cases := []struct {
		name    string
		speed   [familyCount]int64
		working [familyCount]bool
	}{
		{"alle drei saturiert", [familyCount]int64{9 << 20, 9 << 20, 9 << 20}, [familyCount]bool{true, true, true}},
		{"zwei saturiert", [familyCount]int64{9 << 20, 9 << 20, 0}, [familyCount]bool{true, true, false}},
		{"einer traege", [familyCount]int64{1 << 10, 9 << 20, 0}, [familyCount]bool{true, true, false}},
		{"nur einer", [familyCount]int64{9 << 20, 0, 0}, [familyCount]bool{true, false, false}},
	}
	for _, c := range cases {
		got := shareOut(limit, c.speed, c.working)
		var sum int64
		for _, v := range got {
			sum += v
		}
		if sum > limit {
			t.Errorf("%s: shares %v add up to %d, over the limit of %d", c.name, got, sum, limit)
		}
		// No working meter is starved below the floor.
		for i, w := range c.working {
			if w && got[i] < budgetFloor {
				t.Errorf("%s: working meter %d got %d, under the floor of %d", c.name, i, got[i], budgetFloor)
			}
		}
	}
}

// With only the engine downloading, the engine gets the whole limit.
func TestShareOutGivesOneWorkingMeterTheWholeLimit(t *testing.T) {
	const limit = 4 << 20
	got := shareOut(limit, [familyCount]int64{4 << 20, 0, 0}, [familyCount]bool{true, false, false})
	if got[familyEngine] != limit {
		t.Errorf("the only working meter got %d of %d", got[familyEngine], limit)
	}
	if got[familyJD] != 0 || got[familyYtdlp] != 0 {
		t.Errorf("an idle meter was given a share: %v", got)
	}
}

// Shares follow measured speed, so a near-idle meter does not sit on half the
// budget while the other is capped.
func TestShareOutHandsSpareCapacityToTheSaturatedOne(t *testing.T) {
	const limit = 10 << 20
	// Engine barely moving, JD wants everything it can get.
	got := shareOut(limit, [familyCount]int64{1 << 10, 100 << 20, 0}, [familyCount]bool{true, true, false})
	if got[familyJD] <= limit/2 {
		t.Errorf("the saturated meter got %d, no more than its equal share of %d; the spare was not handed over", got[familyJD], limit/2)
	}
	if got[familyEngine] > limit/2 {
		t.Errorf("the idle meter kept %d, more than its equal share", got[familyEngine])
	}
}

// A limit of zero means off and stays off for every meter.
func TestShareOutLeavesUnlimitedUnlimited(t *testing.T) {
	got := shareOut(0, [familyCount]int64{5 << 20, 5 << 20, 5 << 20}, [familyCount]bool{true, true, true})
	for i, v := range got {
		if v != 0 {
			t.Errorf("meter %d was given a limit of %d although the setting is unlimited", i, v)
		}
	}
}

// A debrid service is an account, not a meter: it resolves to a direct URL the
// engine downloads, so its bytes go through the engine's throttle.
func TestMeterForSendsDebridThroughTheEngine(t *testing.T) {
	for _, id := range []string{"torbox", "alldebrid", "realdebrid", "debridlink", "direct", "http", "torrent"} {
		if got := meterFor(id); got != familyEngine {
			t.Errorf("meterFor(%q) = %v, want the engine; these all hand their bytes to it", id, got)
		}
	}
	if meterFor("jd") != familyJD {
		t.Error("jd is not mapped to its own meter, and it meters in its own process")
	}
	if meterFor("ytdlp") != familyYtdlp {
		t.Error("ytdlp is not mapped to its own meter, and it is told per spawn")
	}
}
