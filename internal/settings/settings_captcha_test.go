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
