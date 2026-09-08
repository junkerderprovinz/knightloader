package notify

import (
	"errors"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

func TestSanitizeAssignsIDsAndKeepsTheOnesAlreadyHandedOut(t *testing.T) {
	in := []Target{
		{Name: "first", URL: " https://one.example/ "},
		{ID: "7", Name: "kept", URL: "https://two.example/"},
		{Name: "third", URL: "https://three.example/"},
	}
	out := Sanitize(in)
	if len(out) != 3 {
		t.Fatalf("Sanitize kept %d rows, want 3", len(out))
	}
	if out[1].ID != "7" {
		t.Errorf("the row that already had id 7 came back as %q; an id the API handed to a client has to survive an edit elsewhere in the list", out[1].ID)
	}
	if out[0].ID == "" || out[2].ID == "" || out[0].ID == out[2].ID {
		t.Errorf("ids %q and %q, want two distinct non-empty ids", out[0].ID, out[2].ID)
	}
	if out[0].URL != "https://one.example/" {
		t.Errorf("the address came back as %q, want it trimmed", out[0].URL)
	}
}

func TestSanitizeDropsOnlyTheUntouchedRow(t *testing.T) {
	out := Sanitize([]Target{
		{},                                 // an Add button nobody typed into
		{Name: "half typed"},               // somebody mid-edit: kept
		{URL: "not a url", Name: "broken"}, // refused at save, never dropped here
	})
	if len(out) != 2 {
		t.Fatalf("Sanitize kept %d rows, want the blank one gone and the other two kept: %+v", len(out), out)
	}
	if out[1].URL != "not a url" {
		t.Errorf("the unparseable address was rewritten to %q; a row that vanishes on save is a row the operator goes on believing in", out[1].URL)
	}
}

func TestSanitizeNormalisesTheMethodToTheClosedList(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", MethodPOST},
		{" put ", MethodPUT},
		{"get", MethodGET},
		{"DELETE", MethodPOST},
		{"TRACE", MethodPOST},
	} {
		out := Sanitize([]Target{{Name: "n", URL: "https://x.example/", Method: tc.in}})
		if out[0].Method != tc.want {
			t.Errorf("method %q became %q, want %q", tc.in, out[0].Method, tc.want)
		}
	}
}

func TestSanitizeClampsAndKeepsTheNoOpinionZero(t *testing.T) {
	out := Sanitize([]Target{
		{Name: "a", URL: "https://x.example/", Attempts: 0, TimeoutSeconds: 0},
		{Name: "b", URL: "https://x.example/", Attempts: 99, TimeoutSeconds: 9999},
		{Name: "c", URL: "https://x.example/", Attempts: -4, TimeoutSeconds: -1},
	})
	if out[0].Attempts != 0 || out[0].TimeoutSeconds != 0 {
		t.Errorf("a row with no opinion came back as %d/%d, want the zeroes kept: a spinner that rewrites itself to 3 is one nobody can read",
			out[0].Attempts, out[0].TimeoutSeconds)
	}
	if out[0].ResolvedAttempts() != DefaultAttempts {
		t.Errorf("zero resolved to %d attempts, want %d", out[0].ResolvedAttempts(), DefaultAttempts)
	}
	if out[1].Attempts != MaxAttempts || out[1].TimeoutSeconds != MaxTimeoutSeconds {
		t.Errorf("out of range came back as %d/%d, want %d/%d", out[1].Attempts, out[1].TimeoutSeconds, MaxAttempts, MaxTimeoutSeconds)
	}
	if out[2].Attempts != 0 || out[2].TimeoutSeconds != 0 {
		t.Errorf("negatives came back as %d/%d, want them filed as the same no-opinion zero", out[2].Attempts, out[2].TimeoutSeconds)
	}
}

func TestSanitizeDropsATriggerThisBuildNeverFires(t *testing.T) {
	out := Sanitize([]Target{{
		Name: "n", URL: "https://x.example/",
		Triggers: []script.Trigger{script.TriggerTaskDone, "task.invented", script.TriggerTaskDone},
	}})
	if len(out[0].Triggers) != 1 || out[0].Triggers[0] != script.TriggerTaskDone {
		t.Errorf("triggers came back as %v, want the invented one and the duplicate gone", out[0].Triggers)
	}
}

func TestValidateNamesWhatIsWrong(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Target
		want string
	}{
		{"no address", Target{Name: "n"}, ProblemNoURL},
		{"not a url", Target{URL: "://nope"}, ProblemBadURL},
		{"wrong scheme", Target{URL: "ftp://x.example/"}, ProblemBadURL},
		{"no host", Target{URL: "http:///topic"}, ProblemBadURL},
		{"method outside the list", Target{URL: "https://x.example/", Method: "DELETE"}, ProblemBadMethod},
		{"unclosed in the url", Target{URL: "https://x.example/%%task.name"}, ProblemBadTemplate},
		{"unclosed in a header", Target{URL: "https://x.example/", Headers: map[string]string{"X-Title": "%%task.name"}}, ProblemBadTemplate},
		{"unclosed in the body", Target{URL: "https://x.example/", Body: "%%task.name"}, ProblemBadTemplate},
		{"trigger this build never fires", Target{URL: "https://x.example/", Triggers: []script.Trigger{"task.invented"}}, ProblemUnknownTrigger},
	} {
		p := Validate(tc.in)
		if p == nil {
			t.Errorf("%s: Validate accepted it, want %s", tc.name, tc.want)
			continue
		}
		if p.Code != tc.want {
			t.Errorf("%s: Validate said %q, want %q", tc.name, p.Code, tc.want)
		}
		if !errors.Is(p, ErrBadTarget) {
			t.Errorf("%s: the problem does not unwrap to ErrBadTarget, so validateRows' own fmt.Errorf wrapper loses it", tc.name)
		}
	}
}

func TestValidateAcceptsAnAddressWithPlaceholdersInIt(t *testing.T) {
	// "%%" is not a valid percent-escape, so url.Parse refuses this outright.
	// Validating the raw template rather than the stripped one would therefore
	// refuse every address that uses a placeholder at all, and report it as a
	// malformed address.
	for _, addr := range []string{
		"https://ntfy.example/topic?message=%%task.name%%",
		"https://%%instance%%.example/hook",
	} {
		if p := Validate(Target{URL: addr}); p != nil {
			t.Errorf("Validate refused %q: %v", addr, p)
		}
	}
	if got := (Target{URL: "https://ntfy.example/topic?message=%%task.name%%"}).Host(); got != "ntfy.example" {
		t.Errorf("Host() of an address with a placeholder is %q, want the host", got)
	}
}

func TestValidateAcceptsAWholeRow(t *testing.T) {
	ok := Target{
		Name: "phone", URL: "https://ntfy.example/knightloader?token=abc", Method: MethodPOST,
		Headers:  map[string]string{"X-Title": "%%instance%%", "Content-Type": "text/plain"},
		Body:     "%%task.name%% is done",
		Triggers: []script.Trigger{script.TriggerTaskDone, script.TriggerPackageDone},
	}
	if p := Validate(ok); p != nil {
		t.Fatalf("Validate refused a usable row: %v", p)
	}
}

func TestValidateDoesNotComplainAboutAnUnknownPlaceholderName(t *testing.T) {
	// A typo has to survive as far as the message, where it can be seen. Refused
	// here it would be indistinguishable from a name a NEWER build knows, and
	// this build would be refusing to save a row a later one fills in correctly.
	if p := Validate(Target{URL: "https://x.example/", Body: "%%task.nmae%%"}); p != nil {
		t.Fatalf("Validate refused an unknown placeholder name (%v); only an UNCLOSED one is a broken row", p)
	}
}

func TestRedactedReplacesEveryHeaderValueAndCopiesTheMap(t *testing.T) {
	src := Target{Headers: map[string]string{"Authorization": "Bearer real-token", "X-Empty": ""}}
	out := Redacted(src)
	if out.Headers["Authorization"] != RedactedValue {
		t.Errorf("Authorization came back as %q, want the placeholder", out.Headers["Authorization"])
	}
	if out.Headers["X-Empty"] != "" {
		t.Errorf("an empty value became %q; there is no secret to hide there and stars would invent one", out.Headers["X-Empty"])
	}
	if src.Headers["Authorization"] != "Bearer real-token" {
		t.Error("Redacted edited the caller's own map, which would blank the settings the dispatcher is sending with")
	}
}

func TestMergeCarriesASecretBackOnlyToTheSameAddress(t *testing.T) {
	prev := []Target{{
		ID: "1", URL: "https://ntfy.example/mine",
		Headers: map[string]string{"Authorization": "Bearer real-token", "X-Title": "box"},
	}}

	// The ordinary round trip: the browser was shown stars and sent them back.
	same := Merge([]Target{{
		ID: "1", URL: "https://ntfy.example/mine",
		Headers: map[string]string{"Authorization": RedactedValue, "X-Title": RedactedValue},
	}}, prev)
	if same[0].Headers["Authorization"] != "Bearer real-token" {
		t.Errorf("an untouched row lost its token (%q); every save from any settings page would clear it",
			same[0].Headers["Authorization"])
	}

	// THE GUARD. The browser never saw the token and is the thing that types the
	// address, so a token that followed a changed address could be aimed at a
	// machine the client controls - proxycfg.Merge writes the same rule down for
	// proxy passwords.
	moved := Merge([]Target{{
		ID: "1", URL: "https://attacker.example/collect",
		Headers: map[string]string{"Authorization": RedactedValue},
	}}, prev)
	if got := moved[0].Headers["Authorization"]; got != "" {
		t.Errorf("the stored token was carried onto a CHANGED address as %q; it must be dropped, "+
			"or a client that was never allowed to read it can have this server post it wherever it likes", got)
	}

	// A renamed header is a different place to put a token.
	renamed := Merge([]Target{{
		ID: "1", URL: "https://ntfy.example/mine",
		Headers: map[string]string{"X-Debug": RedactedValue},
	}}, prev)
	if got := renamed[0].Headers["X-Debug"]; got != "" {
		t.Errorf("the token followed a renamed header as %q, want it dropped", got)
	}

	// A different row that happens to sit in the same position.
	other := Merge([]Target{{
		ID: "2", URL: "https://ntfy.example/mine",
		Headers: map[string]string{"Authorization": RedactedValue},
	}}, prev)
	if got := other[0].Headers["Authorization"]; got != "" {
		t.Errorf("the token was carried onto a different row id as %q, want it dropped", got)
	}
}

func TestMergeNeverLeavesTheLiteralPlaceholderOnTheWire(t *testing.T) {
	// Eight literal stars sent as a bearer token is a 401 the operator cannot
	// tell from a wrong token. An empty value is visibly a header waiting to be
	// filled in, and Send skips it rather than writing a bare header line.
	out := Merge([]Target{{ID: "1", URL: "https://new.example/", Headers: map[string]string{"Authorization": RedactedValue}}}, nil)
	if got := out[0].Headers["Authorization"]; strings.Contains(got, "*") {
		t.Errorf("the header value came back as %q, want it empty", got)
	}
}

func TestMergeLetsAnOperatorClearAndRetypeAValue(t *testing.T) {
	prev := []Target{{ID: "1", URL: "https://x.example/", Headers: map[string]string{"Authorization": "old"}}}
	typed := Merge([]Target{{ID: "1", URL: "https://x.example/", Headers: map[string]string{"Authorization": "new"}}}, prev)
	if typed[0].Headers["Authorization"] != "new" {
		t.Errorf("a retyped value came back as %q, want it taken at its word", typed[0].Headers["Authorization"])
	}
	cleared := Merge([]Target{{ID: "1", URL: "https://x.example/", Headers: map[string]string{"Authorization": ""}}}, prev)
	if cleared[0].Headers["Authorization"] != "" {
		t.Errorf("an emptied value came back as %q; empty has to keep meaning \"clear it\"", cleared[0].Headers["Authorization"])
	}
}

func TestHostIsTheHostAndNeverTheQuery(t *testing.T) {
	// ntfy and Gotify both take the credential in the query, and this string is
	// what every browser polling the status route is shown.
	got := Target{URL: "https://ntfy.example:8443/topic?token=SUPERSECRET"}.Host()
	if got != "ntfy.example:8443" {
		t.Fatalf("Host() is %q, want the host alone", got)
	}
	if strings.Contains(got, "SUPERSECRET") {
		t.Fatal("the token is in the status row")
	}
}

func TestBalancedTemplateCountsPairsAndNotMarkers(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"", true},
		{"nothing here", true},
		{"%%a%%", true},
		{"%%a%%%%b%%", true},
		{"100%% done", false},
		{"%%a%% and %%b", false},
	} {
		if got := balancedTemplate(tc.in); got != tc.want {
			t.Errorf("balancedTemplate(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
