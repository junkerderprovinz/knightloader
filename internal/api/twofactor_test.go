package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/secret"
)

// armTwoFactor puts an instance in the state this file is about: a password, a
// confirmed second factor, and the secret in the test's hands so it can
// produce codes the way an authenticator app would.
func armTwoFactor(t *testing.T, a *app.App, password string) (totpSecret string, recovery []string) {
	t.Helper()
	if err := a.Auth.SetPassword("", password); err != nil {
		t.Fatal(err)
	}
	sec, _, err := a.Auth.BeginTwoFactor("KnightLoader", "this instance")
	if err != nil {
		t.Fatal(err)
	}
	code, err := secret.TOTPCode(sec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := a.Auth.ConfirmTwoFactor(code)
	if err != nil {
		t.Fatal(err)
	}
	return sec, codes
}

// loginWith posts a password and a code and hands back the status and the
// decoded body, which is where the second factor's own answer lives.
func loginWith(t *testing.T, c *http.Client, base, password, code string) (int, loginAnswer) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password, "code": code})
	resp, err := c.Post(base+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out loginAnswer
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

type loginAnswer struct {
	Enabled           bool `json:"enabled"`
	Authenticated     bool `json:"authenticated"`
	TwoFactorRequired bool `json:"twoFactorRequired"`
}

// TestThePasswordAloneIsNotEnoughOnceAFactorIsArmed: the right password with
// no code opens nothing, and says why.
func TestThePasswordAloneIsNotEnoughOnceAFactorIsArmed(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	sec, _ := armTwoFactor(t, a, "a-good-password")

	client := &http.Client{Jar: newJar(t)}
	status, ans := loginWith(t, client, srv.URL, "a-good-password", "")
	if status != http.StatusOK {
		t.Fatalf("the password with no code answered %d, want 200 - it is a question, not a refusal", status)
	}
	if ans.Authenticated {
		t.Fatal("the password alone signed the caller in")
	}
	if !ans.TwoFactorRequired {
		t.Error("the answer does not say a second factor is wanted, so the screen cannot ask for one")
	}
	// And no session came with it.
	resp, err := client.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after a password-only login the API answered %d, want 401", resp.StatusCode)
	}

	// A wrong code is a refusal, and still says which step it is on.
	status, ans = loginWith(t, client, srv.URL, "a-good-password", "000000")
	if status != http.StatusUnauthorized {
		t.Errorf("a wrong code answered %d, want 401", status)
	}
	if !ans.TwoFactorRequired {
		t.Error("a rejected code must still say the screen is on the code step")
	}

	// The right one gets in.
	code, err := secret.TOTPCode(sec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	status, ans = loginWith(t, client, srv.URL, "a-good-password", code)
	if status != http.StatusOK || !ans.Authenticated {
		t.Fatalf("the password and a live code answered %d authenticated=%v", status, ans.Authenticated)
	}
	resp, err = client.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("after a full login the API answered %d, want 200", resp.StatusCode)
	}
}

// TestAWrongPasswordIsStillAWrongPassword: the code field must not become a
// way past the first factor, so a valid code with the wrong password opens
// nothing and the answer says nothing about the code either way.
func TestAWrongPasswordIsStillAWrongPassword(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	sec, _ := armTwoFactor(t, a, "a-good-password")
	code, err := secret.TOTPCode(sec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: newJar(t)}
	status, ans := loginWith(t, client, srv.URL, "not-the-password", code)
	if status != http.StatusUnauthorized {
		t.Fatalf("a wrong password with a valid code answered %d, want 401", status)
	}
	if ans.Authenticated {
		t.Fatal("a valid code let a wrong password in")
	}
	if ans.TwoFactorRequired {
		t.Error("the answer to a wrong password advertises that a second factor exists")
	}
}

// TestARecoveryCodeIsAWayIn: the login route takes one wherever it takes a
// six-digit code, which is what the recovery sheet is for.
func TestARecoveryCodeIsAWayIn(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	_, codes := armTwoFactor(t, a, "a-good-password")

	client := &http.Client{Jar: newJar(t)}
	status, ans := loginWith(t, client, srv.URL, "a-good-password", codes[0])
	if status != http.StatusOK || !ans.Authenticated {
		t.Fatalf("a recovery code answered %d authenticated=%v", status, ans.Authenticated)
	}
	// Once, and only once.
	other := &http.Client{Jar: newJar(t)}
	status, _ = loginWith(t, other, srv.URL, "a-good-password", codes[0])
	if status != http.StatusUnauthorized {
		t.Errorf("the same recovery code was accepted a second time, status %d", status)
	}
}

// TestTheLoginThrottleMakesGuessingSixDigitsPointless: a million guesses at
// machine speed is a few minutes, so a second factor with unlimited attempts
// is not one. The throttle turns that into years, and it covers the password
// half of the route as well.
func TestTheLoginThrottleMakesGuessingSixDigitsPointless(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	sec, _ := armTwoFactor(t, a, "a-good-password")

	client := &http.Client{Jar: newJar(t)}
	var last int
	for i := 0; i < loginFailBurst+1; i++ {
		last, _ = loginWith(t, client, srv.URL, "a-good-password", "000000")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong codes the route still answered %d, want 429", loginFailBurst+1, last)
	}
	// The throttle refuses a correct code too while it is in force; letting
	// one through would tell an attacker when a guess was right.
	code, err := secret.TOTPCode(sec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := loginWith(t, client, srv.URL, "a-good-password", code); status != http.StatusTooManyRequests {
		t.Errorf("a valid code during the cool-off answered %d, want 429", status)
	}
}

// TestAuthStateTellsASignedInCallerAboutTheFactorAndNobodyElse: the card needs
// to know, while an anonymous prober is told only whether a password exists,
// which the login screen has to be told anyway.
func TestAuthStateTellsASignedInCallerAboutTheFactorAndNobodyElse(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	sec, _ := armTwoFactor(t, a, "a-good-password")

	var anon struct {
		Enabled      bool  `json:"enabled"`
		TwoFactor    bool  `json:"twoFactor"`
		RecoveryLeft *int  `json:"recoveryLeft"`
		Extra        *bool `json:"-"`
	}
	resp, err := http.Get(srv.URL + "/api/auth")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&anon); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !anon.Enabled {
		t.Error("an anonymous caller is not told a password is set, which the login screen needs")
	}
	if anon.TwoFactor {
		t.Error("an anonymous caller is told the instance has a second factor")
	}
	if anon.RecoveryLeft != nil {
		t.Error("an anonymous caller is told how many recovery codes are left")
	}

	client := &http.Client{Jar: newJar(t)}
	code, err := secret.TOTPCode(sec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := loginWith(t, client, srv.URL, "a-good-password", code); status != http.StatusOK {
		t.Fatalf("login answered %d", status)
	}
	var in struct {
		TwoFactor    bool `json:"twoFactor"`
		RecoveryLeft *int `json:"recoveryLeft"`
	}
	resp, err = client.Get(srv.URL + "/api/auth")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&in); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !in.TwoFactor {
		t.Error("a signed-in caller is not told the factor is on, so the card cannot report it")
	}
	if in.RecoveryLeft == nil || *in.RecoveryLeft != secret.RecoveryCodeCount {
		t.Errorf("recoveryLeft = %v, want %d", in.RecoveryLeft, secret.RecoveryCodeCount)
	}
}

// TestTheEnrolmentRoutesNeedASession: everything about the second factor is
// behind the lock except the login itself, since an anonymous caller who could
// start an enrolment could arm a factor on somebody else's instance.
func TestTheEnrolmentRoutesNeedASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/auth/2fa/begin", "/api/auth/2fa/confirm", "/api/auth/2fa/disable"} {
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader([]byte(`{"code":"000000"}`)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("POST %s without a session = %d, want 401", path, resp.StatusCode)
		}
	}
}

// TestTheEnrolmentRunsEndToEndOverHTTP walks the three steps a person walks, so
// the routes are pinned together rather than one at a time.
func TestTheEnrolmentRunsEndToEndOverHTTP(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: newJar(t)}
	if code := login(t, client, srv.URL, "a-good-password"); code != http.StatusOK {
		t.Fatalf("login answered %d", code)
	}

	var begun struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
	}
	resp, err := client.Post(srv.URL+"/api/auth/2fa/begin", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("begin answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&begun); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if begun.Secret == "" || begun.URI == "" {
		t.Fatalf("begin answered %+v", begun)
	}
	if a.Auth.TwoFactorEnabled() {
		t.Fatal("begin armed the factor")
	}

	code, err := secret.TOTPCode(begun.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	var confirmed struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	resp, err = client.Post(srv.URL+"/api/auth/2fa/confirm", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&confirmed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(confirmed.RecoveryCodes) != secret.RecoveryCodeCount {
		t.Fatalf("confirm handed back %d recovery codes", len(confirmed.RecoveryCodes))
	}
	if !a.Auth.TwoFactorEnabled() {
		t.Fatal("the factor is not armed after a confirmed code")
	}

	// Turning it off costs a code, as using it does.
	resp, err = client.Post(srv.URL+"/api/auth/2fa/disable", "application/json", bytes.NewReader([]byte(`{"code":"000000"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("the factor came off without a valid code")
	}
	off, err := secret.TOTPCode(begun.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]string{"code": off})
	resp, err = client.Post(srv.URL+"/api/auth/2fa/disable", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable with a live code answered %d", resp.StatusCode)
	}
	if a.Auth.TwoFactorEnabled() {
		t.Error("the factor is still armed after being turned off")
	}
}
