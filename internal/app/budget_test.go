package app

// The speed limit shared out between the three meters (see app_budget.go).

import (
	"math/rand/v2"
	"testing"
	"time"
)

// meters is a tick's readings: the speed of every working meter, or -1 for a
// meter with nothing running.
func meters(engine, jd, ytdlp int64) budgetTick {
	var t budgetTick
	for i, v := range [familyCount]int64{engine, jd, ytdlp} {
		if v >= 0 {
			t.speed[i], t.working[i] = v, true
		}
	}
	return t
}

// after is a previous tick that handed out share.
func after(t budgetTick, share [familyCount]int64) budgetTick {
	t.share = share
	return t
}

// The shares never add up to more than the limit; handing each meter the full
// limit would triple it.
func TestShareOutNeverExceedsTheLimit(t *testing.T) {
	const limit = 10 << 20 // 10 MiB/s

	cases := []struct {
		name string
		now  budgetTick
	}{
		{"all three saturated", meters(9<<20, 9<<20, 9<<20)},
		{"two saturated", meters(9<<20, 9<<20, -1)},
		{"one sluggish", meters(1<<10, 9<<20, -1)},
		{"only one", meters(9<<20, -1, -1)},
		{"two just started", meters(0, 0, -1)},
		{"all three just started", meters(0, 0, 0)},
	}
	lasts := []budgetTick{
		{},
		after(meters(-1, -1, -1), [familyCount]int64{limit, limit, limit}),
		after(meters(5<<20, 5<<20, -1), [familyCount]int64{limit / 2, limit / 2, 0}),
		after(meters(0, 3<<20, 1<<10), [familyCount]int64{budgetFloor, 6 << 20, 4 << 20}),
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		var l budgetTick
		for i := range l.share {
			l.working[i] = rng.IntN(2) == 0
			if l.working[i] {
				l.speed[i] = rng.Int64N(limit)
				l.share[i] = budgetFloor + rng.Int64N(limit)
			}
		}
		lasts = append(lasts, l)
	}
	for _, c := range cases {
		for _, last := range lasts {
			got := shareOut(limit, c.now, last)
			var sum int64
			for _, v := range got {
				sum += v
			}
			if sum > limit {
				t.Errorf("%s after %+v: shares %v add up to %d, over the limit of %d", c.name, last, got, sum, limit)
			}
			for i, w := range c.now.working {
				if w && got[i] < budgetFloor {
					t.Errorf("%s after %+v: working meter %d got %d, under the floor of %d", c.name, last, i, got[i], budgetFloor)
				}
			}
		}
	}
}

// With only the engine downloading, the engine gets the whole limit, whatever
// it happens to read: 0 while a debrid service unlocks the link, a trickle on
// a slow start or after a stall.
func TestShareOutGivesOneWorkingMeterTheWholeLimit(t *testing.T) {
	const limit = 4 << 20
	alone := [familyCount]int64{limit, 0, 0}
	cases := []struct {
		name      string
		now, last budgetTick
	}{
		{"at full speed", meters(4<<20, -1, -1), after(meters(4<<20, -1, -1), alone)},
		{"just started, at 0", meters(0, -1, -1), after(meters(-1, -1, -1), [familyCount]int64{limit, limit, limit})},
		{"at 0 for a second tick", meters(0, -1, -1), after(meters(0, -1, -1), alone)},
		{"at 1 KiB/s", meters(1<<10, -1, -1), after(meters(1<<10, -1, -1), alone)},
	}
	for _, c := range cases {
		got := shareOut(limit, c.now, c.last)
		if got[familyEngine] != limit {
			t.Errorf("%s: the only working meter got %d of %d", c.name, got[familyEngine], limit)
		}
		if got[familyJD] != 0 || got[familyYtdlp] != 0 {
			t.Errorf("%s: an idle meter was given a share: %v", c.name, got)
		}
	}
}

// When one of two downloading meters finishes, the other gets everything at
// once, even while its averaged speed still shows the half it had.
func TestShareOutGivesTheSurvivorTheWholeLimit(t *testing.T) {
	const limit = 10 << 20
	last := after(meters(5<<20, 5<<20, -1), [familyCount]int64{limit / 2, limit / 2, 0})
	got := shareOut(limit, meters(5<<20, -1, -1), last)
	if got[familyEngine] != limit {
		t.Errorf("the remaining meter got %d of %d", got[familyEngine], limit)
	}
}

// Shares follow measured speed, so a near-idle meter does not sit on half the
// budget while the other is capped.
func TestShareOutHandsSpareCapacityToTheSaturatedOne(t *testing.T) {
	const limit = 10 << 20
	last := after(meters(1<<10, 5<<20, -1), [familyCount]int64{limit / 2, limit / 2, 0})
	// Engine barely moving, JD using all it was given.
	got := shareOut(limit, meters(1<<10, 5<<20, -1), last)
	if got[familyJD] <= limit/2 {
		t.Errorf("the saturated meter got %d, no more than its equal share of %d; the spare was not handed over", got[familyJD], limit/2)
	}
	if got[familyEngine] > limit/2 {
		t.Errorf("the idle meter kept %d, more than its equal share", got[familyEngine])
	}
}

// Spare capacity nobody is using right now is not held back from the meters
// that are working.
func TestShareOutSplitsSpareCapacityWhenNoMeterIsSaturated(t *testing.T) {
	const limit = 10 << 20
	last := after(meters(1<<20, 1<<20, -1), [familyCount]int64{limit / 2, limit / 2, 0})
	got := shareOut(limit, meters(1<<20, 1<<20, -1), last)
	if sum := got[familyEngine] + got[familyJD]; sum < limit-2 {
		t.Errorf("shares %v hand out %d of %d with nobody saturated", got, sum, limit)
	}
}

// A JD download sitting on a captcha reads 0 for as long as nobody answers.
// It starts at its equal share, since nothing tells a captcha from an unlock
// on the first tick, and the engine has nearly everything from the second.
func TestAMeterWaitingOnACaptchaGivesItsShareBackOnTheSecondTick(t *testing.T) {
	const limit = 36 << 20
	alone := after(meters(36<<20, -1, -1), [familyCount]int64{limit, 0, 0})

	first := shareOut(limit, meters(36<<20, 0, -1), alone)
	if first[familyJD] < limit/2 {
		t.Errorf("JD just started and got %d, less than its equal share of %d", first[familyJD], limit/2)
	}
	second := shareOut(limit, meters(30<<20, 0, -1), after(meters(36<<20, 0, -1), first))
	if second[familyEngine] < limit*9/10 {
		t.Errorf("with JD still at 0 the engine got %d, under 90%% of %d", second[familyEngine], limit)
	}
}

// Between two files, or while a new link unlocks, the engine reads 0 for a
// tick. That tick does not cost it its share, or the next file would start at
// the floor.
func TestAMeterBetweenTwoFilesKeepsItsShareForATick(t *testing.T) {
	const limit = 20 << 20
	last := after(meters(10<<20, 10<<20, -1), [familyCount]int64{limit / 2, limit / 2, 0})
	got := shareOut(limit, meters(0, 10<<20, -1), last)
	if got[familyEngine] < limit/2 {
		t.Errorf("the engine read 0 once and was cut to %d, under its equal share of %d", got[familyEngine], limit/2)
	}
}

// simMeter is a backend behind its share. Every half second it moves what its
// source gives, up to the share, and it reads out the mean of its last five
// seconds of that, which is how Gopeed measures a transfer.
type simMeter struct {
	// start and stop bound when it has something running, a zero stop meaning
	// never; from flow on its source gives rate.
	start, flow, stop time.Duration
	rate              int64
	samples           []int64
}

func (m *simMeter) running(at time.Duration) bool {
	return at >= m.start && (m.stop == 0 || at < m.stop)
}

func (m *simMeter) reading() int64 {
	if len(m.samples) == 0 {
		return 0
	}
	var sum int64
	for _, s := range m.samples {
		sum += s
	}
	return sum / int64(len(m.samples))
}

// simulate runs the budget over the meters for d, as budgetLoop does, and
// returns what each meter moved per second in each tick interval.
func simulate(limit int64, ms [familyCount]*simMeter, d time.Duration) [][familyCount]int64 {
	const step = 500 * time.Millisecond
	var b budget
	// Nothing ran before, so every meter starts with the whole limit.
	share := [familyCount]int64{limit, limit, limit}
	var moved [][familyCount]int64
	// Bytes times nanoseconds, so the mean per interval comes out exact.
	var interval [familyCount]int64
	for at := time.Duration(0); at < d; at += step {
		if at > 0 && at%budgetInterval == 0 {
			var speed [familyCount]int64
			var working [familyCount]bool
			for i, m := range ms {
				if m != nil && m.running(at) {
					speed[i], working[i] = m.reading(), true
				}
			}
			share = b.next(limit, speed, working)
			for i := range interval {
				interval[i] /= int64(budgetInterval)
			}
			moved = append(moved, interval)
			interval = [familyCount]int64{}
		}
		for i, m := range ms {
			if m == nil || !m.running(at) {
				continue
			}
			rate := int64(0)
			if at >= m.flow {
				rate = m.rate
				// A share of 0 is no limit, as it is for the throttle.
				if share[i] > 0 {
					rate = min(rate, share[i])
				}
			}
			m.samples = append(m.samples, rate)
			if len(m.samples) > 10 {
				m.samples = m.samples[1:]
			}
			interval[i] += rate * int64(step)
		}
	}
	return moved
}

// ticksToReach is how many whole tick intervals pass after from before the
// meter moves at least want per second, or -1 if it never does.
func ticksToReach(moved [][familyCount]int64, meter budgetFamily, from time.Duration, want int64) int {
	first := int(from / budgetInterval)
	for i := first; i < len(moved); i++ {
		if moved[i][meter] >= want {
			return i - first
		}
	}
	return -1
}

// A debrid download reports running at 0 B/s while its link unlocks. Once the
// bytes flow it reaches the limit within two ticks instead of climbing from
// the floor for minutes.
func TestADownloadStartingFromNothingReachesTheLimitWithinTwoTicks(t *testing.T) {
	const limit = 36 << 20
	engine := &simMeter{flow: 4 * time.Second, rate: 100 << 20}
	moved := simulate(limit, [familyCount]*simMeter{engine, nil, nil}, time.Minute)
	if n := ticksToReach(moved, familyEngine, engine.flow, limit*9/10); n < 0 || n > 2 {
		t.Errorf("the engine reached 90%% of the limit %d ticks after its first byte (-1 is never); per tick: %v", n, moved)
	}
}

// The same with a JD download beside it that waits on a captcha the whole
// time, and then with that captcha answered: each gets 90% of what it can have
// within two ticks.
func TestTwoMetersReachTheirSharesWithinTwoTicks(t *testing.T) {
	const limit = 36 << 20
	engine := &simMeter{flow: 4 * time.Second, rate: 100 << 20}
	jd := &simMeter{start: time.Second, flow: 40 * time.Second, rate: 100 << 20}
	moved := simulate(limit, [familyCount]*simMeter{engine, jd, nil}, time.Minute)
	if n := ticksToReach(moved, familyEngine, engine.flow, limit*9/10); n < 0 || n > 2 {
		t.Errorf("beside a captcha the engine reached 90%% of the limit %d ticks after its first byte; per tick: %v", n, moved)
	}
	if n := ticksToReach(moved, familyJD, jd.flow, limit/2*9/10); n < 0 || n > 2 {
		t.Errorf("after its captcha JD reached 90%% of its equal share %d ticks after its first byte; per tick: %v", n, moved)
	}
}

// A meter held back by its server, not by its share, settles on a share of its
// own and stays there, and the other meter keeps the rest. Nothing swings from
// tick to tick.
func TestAMeterHeldBackByItsServerSettles(t *testing.T) {
	const limit = 36 << 20
	engine := &simMeter{rate: 100 << 20}
	jd := &simMeter{rate: 4 << 20}
	moved := simulate(limit, [familyCount]*simMeter{engine, jd, nil}, 2*time.Minute)
	settled := moved[4:]
	for i, m := range settled {
		if m[familyJD] < 4<<20*9/10 {
			t.Errorf("tick %d: JD moved %d, well under the %d its server gives", i+4, m[familyJD], 4<<20)
		}
		if m[familyEngine] < limit-2*(4<<20) {
			t.Errorf("tick %d: the engine moved %d; JD needs only %d of %d", i+4, m[familyEngine], 4<<20, limit)
		}
		if i > 0 && m != settled[i-1] {
			t.Errorf("tick %d moved %v after %v; the split still swings", i+4, m, settled[i-1])
		}
	}
}

// A meter whose server gives it less than its equal share leaves what it cannot
// use to the other one: a tenth of the limit at most goes unused, whatever the
// server gives, and the split holds still once it has settled.
func TestAMeterCappedByItsServerLeavesLittleUnused(t *testing.T) {
	const limit = 36 * mib
	for capped := int64(mib); capped < limit/2; capped += mib {
		engine := &simMeter{rate: 100 * mib}
		jd := &simMeter{rate: capped}
		moved := simulate(limit, [familyCount]*simMeter{engine, jd, nil}, 2*time.Minute)
		settled := moved[8:]
		last := settled[len(settled)-1]
		t.Logf("JD capped at %2d MiB/s: engine %5.2f, JD %5.2f, unused %5.2f MiB/s", capped/mib,
			float64(last[familyEngine])/mib, float64(last[familyJD])/mib, float64(limit-last[familyEngine]-last[familyJD])/mib)
		for i, m := range settled {
			if unused := limit - m[familyEngine] - m[familyJD]; unused > limit/10 {
				t.Errorf("JD capped at %d MiB/s, tick %d: %.2f of 36 MiB/s unused (engine %.2f, JD %.2f)", capped/mib, i+8,
					float64(unused)/mib, float64(m[familyEngine])/mib, float64(m[familyJD])/mib)
			}
			if i > 0 && m != settled[i-1] {
				t.Errorf("JD capped at %d MiB/s, tick %d moved %v after %v; the split still swings", capped/mib, i+8, m, settled[i-1])
			}
		}
	}
}

// A limit of zero means off and stays off for every meter.
func TestShareOutLeavesUnlimitedUnlimited(t *testing.T) {
	got := shareOut(0, meters(5<<20, 5<<20, 5<<20), budgetTick{})
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
