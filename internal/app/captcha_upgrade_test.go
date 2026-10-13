package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// dataDirFrom19 is a data folder as 1.9 left it: settings.json with the given
// captchaSolverOrder value, written as raw JSON, and a key for each solver.
func dataDirFrom19(t *testing.T, order string, solvers ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"captchaSolverOrder": `+order+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range solvers {
		if err := st.SetCredential(id, "", accounts.Credential{APIKey: "fake-" + id + "-key"}); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func openCaptchaApp(t *testing.T, dir string) *App {
	t.Helper()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// In 1.9 a solver was tried only while it stood in the order, so after the
// upgrade the ones it left out stay unused until somebody switches them on.
func TestAnUpgradeFrom19KeepsTheSolversTheOrderLeftOutSwitchedOff(t *testing.T) {
	for _, c := range []struct {
		order string
		want  []string
	}{
		{`["anticaptcha"]`, []string{"Anti-Captcha"}},
		{`[]`, []string{}},
		{`null`, []string{}},
	} {
		a := openCaptchaApp(t, dataDirFrom19(t, c.order, "2captcha", "anticaptcha"))
		if got := labels(a.captchaSolvers()); !slices.Equal(got, c.want) {
			t.Errorf("with the order %s, the solvers after the upgrade are %q, want %q", c.order, got, c.want)
		}
	}
}

func TestAFreshInstallKeepsEverySolverItIsGiven(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.SetCredential("2captcha", "", accounts.Credential{APIKey: "fake-2captcha-key"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again := openCaptchaApp(t, dir)
	if got := labels(again.captchaSolvers()); !slices.Equal(got, []string{"2Captcha"}) {
		t.Fatalf("after a restart the solvers are %q, want 2Captcha, whose key was stored with no order", got)
	}
}

func TestASolverSwitchedOnAgainAfterTheUpgradeStaysOn(t *testing.T) {
	dir := dataDirFrom19(t, `["anticaptcha"]`, "2captcha", "anticaptcha")
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.SetAccountEnabled("2captcha", "", true)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again := openCaptchaApp(t, dir)
	if got := labels(again.captchaSolvers()); !slices.Equal(got, []string{"Anti-Captcha", "2Captcha"}) {
		t.Fatalf("after the next start the solvers are %q, want 2Captcha back on after Anti-Captcha", got)
	}
}
