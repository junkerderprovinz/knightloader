package reconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// TestSanitizeNormalisesMethod covers the aliases a JD user would type and, more
// importantly, pins that an unrecognised method lands on "off" rather than on
// whichever branch happens to be first in the switch.
func TestSanitizeNormalisesMethod(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"command", MethodCommand},
		{"COMMAND", MethodCommand},
		{"  External ", MethodCommand},
		{"batch", MethodCommand},
		{"http", MethodHTTP},
		{"LiveHeader", MethodHTTP},
		{"curl", MethodHTTP},
		{"none", MethodNone},
		{"", MethodNone},
		{"reboot-the-router", MethodNone},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := Sanitize(Config{Method: tc.in}).Method; got != tc.want {
				t.Errorf("method %q sanitised to %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizeClampsTiming proves the poll loop can never be configured into a
// shape that either hammers the check service or ends before it has looked once.
func TestSanitizeClampsTiming(t *testing.T) {
	tests := []struct {
		name                     string
		interval, timeout        int
		wantInterval, wantTimout int
	}{
		{"unset falls back", 0, 0, defaultIntervalSeconds, defaultTimeoutSeconds},
		{"negative falls back", -9, -1, defaultIntervalSeconds, defaultTimeoutSeconds},
		{"interval floor", 0, 60, defaultIntervalSeconds, 60},
		{"interval ceiling", 3600, 900, maxIntervalSeconds, 900},
		{"timeout floor", 1, 1, 1, minTimeoutSeconds},
		{"timeout ceiling", 5, 999999, 5, maxTimeoutSeconds},
		{"timeout below interval is raised", 60, 30, 60, 60},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Sanitize(Config{IntervalSeconds: tc.interval, TimeoutSeconds: tc.timeout})
			if got.IntervalSeconds != tc.wantInterval {
				t.Errorf("interval = %d, want %d", got.IntervalSeconds, tc.wantInterval)
			}
			if got.TimeoutSeconds != tc.wantTimout {
				t.Errorf("timeout = %d, want %d", got.TimeoutSeconds, tc.wantTimout)
			}
			if got.TimeoutSeconds < got.IntervalSeconds {
				t.Errorf("timeout %d ends before the first check at %d", got.TimeoutSeconds, got.IntervalSeconds)
			}
		})
	}
}

// TestSanitizeLeavesThePasswordAlone is the failure the comment in Sanitize
// names: a password whose spaces were trimmed away logs in nowhere, and the
// router never says why.
func TestSanitizeLeavesThePasswordAlone(t *testing.T) {
	got := Sanitize(Config{Username: "  admin\t", Password: "  spaces matter  "})
	if got.Username != "admin" {
		t.Errorf("username = %q, want %q", got.Username, "admin")
	}
	if got.Password != "  spaces matter  " {
		t.Errorf("password = %q, the surrounding spaces were eaten", got.Password)
	}
}

// TestSanitizeFillsRequestDefaults checks the per-request tidying, including the
// nameless header that would otherwise be written to the wire.
func TestSanitizeFillsRequestDefaults(t *testing.T) {
	got := Sanitize(Config{
		Method: MethodHTTP,
		Requests: []Request{{
			Method:  " post ",
			URL:     "  http://router/login  ",
			Headers: map[string]string{"  ": "orphan", " X-Token ": "abc"},
		}, {
			URL: "http://router/reboot",
		}},
	})
	if got.Requests[0].Method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.Requests[0].Method)
	}
	if got.Requests[0].URL != "http://router/login" {
		t.Errorf("url = %q", got.Requests[0].URL)
	}
	if _, ok := got.Requests[0].Headers["  "]; ok {
		t.Error("a header with a blank name survived sanitising")
	}
	if got.Requests[0].Headers["X-Token"] != "abc" {
		t.Errorf("headers = %v, want the trimmed name to carry the value", got.Requests[0].Headers)
	}
	if got.Requests[1].Method != http.MethodGet {
		t.Errorf("a request without a method became %q, want GET", got.Requests[1].Method)
	}
}

// TestValidate walks every way a configuration can be incomplete. Each one must
// come back as ErrNotConfigured so the caller can tell "the user has not
// finished setting this up" from "the router refused".
func TestValidate(t *testing.T) {
	ok := Config{Method: MethodCommand, Command: "/usr/bin/reconnect", CheckURL: "http://check"}
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"complete command", ok, false},
		{"complete http", Config{Method: MethodHTTP, Requests: []Request{{URL: "http://router"}}, CheckURL: "http://check"}, false},
		{"switched off", Config{Method: MethodNone, CheckURL: "http://check"}, true},
		{"command without a program", Config{Method: MethodCommand, Command: "  ", CheckURL: "http://check"}, true},
		{"http without requests", Config{Method: MethodHTTP, CheckURL: "http://check"}, true},
		{"http with an empty step", Config{
			Method:   MethodHTTP,
			Requests: []Request{{URL: "http://router/login"}, {URL: " "}},
			CheckURL: "http://check",
		}, true},
		{"no check url", Config{Method: MethodCommand, Command: "/bin/true"}, true},
		// "igd" is a word Sanitize understands and folds into MethodUPnP. Raw, it
		// has not been through Sanitize yet, and Validate is not Sanitize: it
		// refuses what it does not recognise rather than guessing. Picking a
		// synonym rather than nonsense is deliberate, because the near-miss is
		// what actually reaches here from unsanitised form input.
		{"unsanitised synonym", Config{Method: "igd", CheckURL: "http://check"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate() = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrNotConfigured) {
				t.Errorf("Validate() = %v, want it to wrap ErrNotConfigured", err)
			}
		})
	}
}

// TestValidateNamesAnUnknownMethod: Sanitize turns a method it cannot place into
// "off", so by the time anything runs, the word the user typed is gone. A caller
// that validates the raw form input still has it, and must say it - "reconnect is
// switched off" sends somebody who mistyped "liveheader" to the on/off toggle
// they already turned on.
func TestValidateNamesAnUnknownMethod(t *testing.T) {
	err := Config{Method: "liveheda", CheckURL: "http://check"}.Validate()
	if err == nil {
		t.Fatal("an unknown method validated")
	}
	if !strings.Contains(err.Error(), "liveheda") {
		t.Errorf("Validate() = %q, which never mentions what the user typed", err)
	}

	// The genuinely switched-off case must keep saying so, not accuse the user of
	// a typo they did not make.
	off := Config{Method: MethodNone, CheckURL: "http://check"}.Validate()
	if off == nil || !strings.Contains(off.Error(), "switched off") {
		t.Errorf("Validate() on a switched-off config = %v", off)
	}
}

// TestExpandVars pins the substitution rules, above all that an unknown or
// half-written placeholder survives into the output where somebody can see it.
func TestExpandVars(t *testing.T) {
	vars := Config{Username: "admin", Password: "s3cret"}.vars(netip.MustParseAddr("203.0.113.9"))
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "http://router/login?u=%%username%%&p=%%password%%", "http://router/login?u=admin&p=s3cret"},
		{"address", "old was %%ip%%", "old was 203.0.113.9"},
		{"case insensitive", "%%IP%% %%UserName%%", "203.0.113.9 admin"},
		{"padded name", "%% ip %%", "203.0.113.9"},
		{"repeated", "%%ip%%/%%ip%%", "203.0.113.9/203.0.113.9"},
		{"unknown name stays visible", "%%adress%%", "%%adress%%"},
		{"unclosed stays visible", "%%password", "%%password"},
		{"jd triple form is not ours", "%%%ip%%%", "%%%ip%%%"},
		{"nothing to do", "http://router/reboot", "http://router/reboot"},
		{"empty placeholder", "%%%%", "%%%%"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandVars(tc.in, vars); got != tc.want {
				t.Errorf("expandVars(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExpandVarsWithoutAddress covers the placeholder before anything is known:
// it must not write the zero netip.Addr's "invalid IP" text into a URL.
func TestExpandVarsWithoutAddress(t *testing.T) {
	got := expandVars("http://router/?from=%%ip%%", Config{}.vars(netip.Addr{}))
	if got != "http://router/?from=" {
		t.Errorf("got %q, want an empty substitution", got)
	}
}

// TestRedactedRoundTrip is the settings-form loop: show a redacted config, take
// it back, and the stored password must survive - while an intentional clear
// must still get through.
func TestRedactedRoundTrip(t *testing.T) {
	stored := Config{Method: MethodCommand, Password: "hunter2"}

	shown := stored.Redacted()
	if shown.Password != RedactedPassword {
		t.Fatalf("Redacted() left %q in the password", shown.Password)
	}
	if strings.Contains(fmt.Sprint(shown), "hunter2") {
		t.Error("the redacted config still prints the password")
	}

	if got := shown.WithSecretsFrom(stored); got.Password != "hunter2" {
		t.Errorf("an untouched form wiped the password: %q", got.Password)
	}
	cleared := shown
	cleared.Password = ""
	if got := cleared.WithSecretsFrom(stored); got.Password != "" {
		t.Errorf("clearing the password did not take: %q", got.Password)
	}
	retyped := shown
	retyped.Password = "new one"
	if got := retyped.WithSecretsFrom(stored); got.Password != "new one" {
		t.Errorf("a retyped password was ignored: %q", got.Password)
	}
	if stored.Redacted().Password == "" {
		t.Error("Redacted() mutated the receiver")
	}
	if empty := (Config{}).Redacted(); empty.Password != "" {
		t.Errorf("a config with no password gained one: %q", empty.Password)
	}
}

// TestStringHidesThePassword guards the accident this method exists for: a %v on
// the settings struct in some log line, now or years from now.
func TestStringHidesThePassword(t *testing.T) {
	cfgs := []Config{
		{Method: MethodCommand, Command: "/usr/bin/reconnect", Args: []string{"--pass", "hunter2"}, Password: "hunter2", CheckURL: "http://check"},
		{Method: MethodHTTP, Requests: []Request{{URL: "http://admin:hunter2@router/"}}, Password: "hunter2", CheckURL: "http://check"},
		{Method: MethodNone, Password: "hunter2"},
	}
	for _, cfg := range cfgs {
		for _, verb := range []string{"%v", "%+v", "%s"} {
			if out := fmt.Sprintf(verb, cfg); strings.Contains(out, "hunter2") {
				t.Errorf("%s of a %s config printed the password: %s", verb, cfg.Method, out)
			}
		}
	}
}

// TestRedactedErrorHasNoWayBackToTheSecret is the hole a redaction of this shape
// normally has. Wrapping the original so errors.Is keeps working also leaves it
// reachable through errors.Unwrap and errors.As, and the original still spells
// the password out - so a caller that wants more detail in a log line, or a
// %+v somewhere up the stack that walks the chain, gets the plain text back. The
// sentinel has to survive without the message surviving with it.
func TestRedactedErrorHasNoWayBackToTheSecret(t *testing.T) {
	const pw = "hunter2"
	cfg := Config{Password: pw}

	inner := fmt.Errorf("%w: dial tcp http://admin:%s@router.invalid/: refused", context.Canceled, pw)
	err := cfg.redact(inner)
	if strings.Contains(err.Error(), pw) {
		t.Fatalf("redact left the password in %q", err)
	}

	// Every route out of the error, not just the one that is easy to remember.
	if got := errors.Unwrap(err); got != nil {
		t.Errorf("errors.Unwrap handed back %q, which still holds the password", got)
	}
	var target *url.Error
	if errors.As(err, &target) {
		t.Errorf("errors.As handed back a %T whose fields hold the password", target)
	}
	for _, rendered := range []string{fmt.Sprint(err), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%s", err)} {
		if strings.Contains(rendered, pw) {
			t.Errorf("the password came back through a format verb: %s", rendered)
		}
	}

	// ...while the one thing a caller actually asks of the error still works.
	if !errors.Is(err, context.Canceled) {
		t.Error("errors.Is no longer recognises the sentinel behind a redacted error")
	}
	if errors.Is(err, ErrUnchanged) {
		t.Error("errors.Is matched a sentinel that was never in the chain")
	}
}

// TestRedactErrorLeavesOtherErrorsAlone makes sure the choke point is a filter
// and not a rewriter: an error with nothing to hide comes back identical, so
// errors.Is keeps working on it.
func TestRedactErrorLeavesOtherErrorsAlone(t *testing.T) {
	cfg := Config{Password: "hunter2"}
	if got := cfg.redact(nil); got != nil {
		t.Errorf("redact(nil) = %v", got)
	}
	if got := cfg.redact(ErrUnchanged); got != ErrUnchanged {
		t.Errorf("redact wrapped an error that had nothing to hide: %v", got)
	}
	if got := (Config{}).redact(ErrUnchanged); got != ErrUnchanged {
		t.Errorf("redact with no password configured returned %v", got)
	}
}
