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

// TestSanitizeNormalisesMethod covers the aliases a JDownloader user would
// type, and that an unrecognised method lands on "off".
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

// TestSanitizeClampsTiming: the poll loop can neither hammer the check service
// nor end before it has looked once.
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

func TestSanitizeLeavesThePasswordAlone(t *testing.T) {
	got := Sanitize(Config{Username: "  admin\t", Password: "  spaces matter  "})
	if got.Username != "admin" {
		t.Errorf("username = %q, want %q", got.Username, "admin")
	}
	if got.Password != "  spaces matter  " {
		t.Errorf("password = %q, the surrounding spaces were eaten", got.Password)
	}
}

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

// TestValidate walks every way a configuration can be incomplete; each wraps
// ErrNotConfigured.
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
		// Sanitize folds "igd" into MethodUPnP, but Validate does not guess at
		// unsanitised input.
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

// TestValidateNamesAnUnknownMethod: on raw form input the typed word is still
// there, and naming it beats "switched off", which points at the toggle.
func TestValidateNamesAnUnknownMethod(t *testing.T) {
	err := Config{Method: "liveheda", CheckURL: "http://check"}.Validate()
	if err == nil {
		t.Fatal("an unknown method validated")
	}
	if !strings.Contains(err.Error(), "liveheda") {
		t.Errorf("Validate() = %q, which never mentions what the user typed", err)
	}

	off := Config{Method: MethodNone, CheckURL: "http://check"}.Validate()
	if off == nil || !strings.Contains(off.Error(), "switched off") {
		t.Errorf("Validate() on a switched-off config = %v", off)
	}
}

// TestExpandVars pins the substitution rules, including that an unknown or
// half-written placeholder stays visible in the output.
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

// TestExpandVarsWithoutAddress: the zero netip.Addr's "invalid IP" text must
// not end up in a URL.
func TestExpandVarsWithoutAddress(t *testing.T) {
	got := expandVars("http://router/?from=%%ip%%", Config{}.vars(netip.Addr{}))
	if got != "http://router/?from=" {
		t.Errorf("got %q, want an empty substitution", got)
	}
}

// TestRedactedRoundTrip is the settings-form loop: an untouched placeholder
// keeps the stored password, and a deliberate clear still gets through.
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

// TestStringHidesThePassword: a %v on the settings struct in a log line must
// not print the password.
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

// TestRedactedErrorHasNoWayBackToTheSecret: errors.Is keeps working, but
// errors.Unwrap and errors.As must not hand back the original, which still
// holds the password.
func TestRedactedErrorHasNoWayBackToTheSecret(t *testing.T) {
	const pw = "hunter2"
	cfg := Config{Password: pw}

	inner := fmt.Errorf("%w: dial tcp http://admin:%s@router.invalid/: refused", context.Canceled, pw)
	err := cfg.redact(inner)
	if strings.Contains(err.Error(), pw) {
		t.Fatalf("redact left the password in %q", err)
	}

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

	if !errors.Is(err, context.Canceled) {
		t.Error("errors.Is no longer recognises the sentinel behind a redacted error")
	}
	if errors.Is(err, ErrUnchanged) {
		t.Error("errors.Is matched a sentinel that was never in the chain")
	}
}

// TestRedactErrorLeavesOtherErrorsAlone: an error with nothing to hide comes
// back identical.
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
