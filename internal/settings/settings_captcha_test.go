package settings

import (
	"reflect"
	"testing"
)

func TestSanitizeCaptchaDropsUnknownAndDuplicateIDs(t *testing.T) {
	n := sanitizeCaptcha(Settings{CaptchaSolverOrder: []string{"2captcha", "bogus", "2captcha", "anticaptcha"}})
	want := []string{"2captcha", "anticaptcha"}
	if !reflect.DeepEqual(n.CaptchaSolverOrder, want) {
		t.Errorf("CaptchaSolverOrder = %v, want %v (unknown dropped, duplicate collapsed, order preserved)", n.CaptchaSolverOrder, want)
	}
}

func TestSanitizeCaptchaEmptyStaysNil(t *testing.T) {
	if got := sanitizeCaptcha(Settings{}).CaptchaSolverOrder; got != nil {
		t.Errorf("CaptchaSolverOrder = %v, want nil for an install that never touched this setting", got)
	}
	if got := sanitizeCaptcha(Settings{CaptchaSolverOrder: []string{"bogus"}}).CaptchaSolverOrder; got != nil {
		t.Errorf("CaptchaSolverOrder = %v, want nil once every entry is filtered out, not an empty non-nil slice", got)
	}
}

// Nothing is tried automatically on a fresh install, straight to the human
// prompt, until somebody configures a solver.
func TestDefaultsHaveNoCaptchaSolverOrder(t *testing.T) {
	if got := Defaults().CaptchaSolverOrder; len(got) != 0 {
		t.Errorf("Defaults().CaptchaSolverOrder = %v, want empty", got)
	}
}

// The solvers start at once on a fresh install, and a minute is what they wait
// once somebody asks them to wait for a watcher.
func TestDefaultsStartTheSolversWithoutWaiting(t *testing.T) {
	d := Defaults()
	if d.CaptchaSolverOnlyUnwatched {
		t.Error("CaptchaSolverOnlyUnwatched is on by default, want off")
	}
	if d.CaptchaSolverWait != DefaultCaptchaSolverWait {
		t.Errorf("CaptchaSolverWait = %d, want %d", d.CaptchaSolverWait, DefaultCaptchaSolverWait)
	}
}

func TestCaptchaSolverWaitIsClampedIntoItsBounds(t *testing.T) {
	cases := map[int]int{0: MinCaptchaSolverWait, 3: MinCaptchaSolverWait, 90: 90, 10000: MaxCaptchaSolverWait}
	for in, want := range cases {
		if got := sanitizeCaptcha(Settings{CaptchaSolverWait: in}).CaptchaSolverWait; got != want {
			t.Errorf("sanitizeCaptcha(wait %d) = %d, want %d", in, got, want)
		}
	}
}
