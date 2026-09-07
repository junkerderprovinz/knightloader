package app

// The speed limit, shared out. See app_budget.go for why one number had to
// become three.

import "testing"

// TestShareOutNeverExceedsTheLimit is the property the whole file exists for.
// The old behaviour handed the full limit to each of three meters, so somebody
// who set 10 MB/s with all three working got 30.
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
		// And nothing that is working may be starved to a standstill.
		for i, w := range c.working {
			if w && got[i] < budgetFloor {
				t.Errorf("%s: working meter %d got %d, under the floor of %d", c.name, i, got[i], budgetFloor)
			}
		}
	}
}

// TestShareOutGivesOneWorkingMeterTheWholeLimit pins the case that must NOT
// regress: with only the engine downloading, the engine still gets everything.
// A fair-share scheme that quietly thirded the limit for a single transfer
// would be a worse bug than the one it fixes.
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

// TestShareOutHandsSpareCapacityToTheSaturatedOne is the "by measured share"
// half. An equal split alone would leave a meter that only wants 1 KiB/s
// sitting on half the budget while the other one is capped.
func TestShareOutHandsSpareCapacityToTheSaturatedOne(t *testing.T) {
	const limit = 10 << 20
	// Engine barely moving, JD wants everything it can get.
	got := shareOut(limit, [familyCount]int64{1 << 10, 100 << 20, 0}, [familyCount]bool{true, true, false})
	if got[familyJD] <= limit/2 {
		t.Errorf("the saturated meter got %d, no more than its equal share of %d - the spare was not handed over", got[familyJD], limit/2)
	}
	if got[familyEngine] > limit/2 {
		t.Errorf("the idle meter kept %d, more than its equal share", got[familyEngine])
	}
}

// TestShareOutLeavesUnlimitedUnlimited: zero means off, and off must not be
// turned into three finite numbers nobody asked for.
func TestShareOutLeavesUnlimitedUnlimited(t *testing.T) {
	got := shareOut(0, [familyCount]int64{5 << 20, 5 << 20, 5 << 20}, [familyCount]bool{true, true, true})
	for i, v := range got {
		if v != 0 {
			t.Errorf("meter %d was given a limit of %d although the setting is unlimited", i, v)
		}
	}
}

// TestMeterForSendsDebridThroughTheEngine pins the mapping that is easy to get
// wrong: a debrid service is an account, not a meter. TorBox and AllDebrid
// resolve a link to a direct URL and hand it to the engine, so their bytes go
// through the engine's own throttle.
func TestMeterForSendsDebridThroughTheEngine(t *testing.T) {
	for _, id := range []string{"torbox", "alldebrid", "realdebrid", "debridlink", "direct", "http", "torrent"} {
		if got := meterFor(id); got != familyEngine {
			t.Errorf("meterFor(%q) = %v, want the engine - these all hand their bytes to it", id, got)
		}
	}
	if meterFor("jd") != familyJD {
		t.Error("jd is not mapped to its own meter, and it meters in its own process")
	}
	if meterFor("ytdlp") != familyYtdlp {
		t.Error("ytdlp is not mapped to its own meter, and it is told per spawn")
	}
}
