package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

func targetWithToken() notify.Target {
	return notify.Target{
		ID: "1", Name: "phone", Enabled: true,
		URL:      "https://ntfy.example/knightloader",
		Method:   notify.MethodPOST,
		Headers:  map[string]string{"Authorization": "Bearer real-token"},
		Body:     "%%task.name%%",
		Triggers: []script.Trigger{script.TriggerTaskDone},
	}
}

// TestAFreshInstallHasNoEventTargetsAtAll is the upgrade-safety guarantee
// written as a test: the field is absent from every settings.json ever written
// before it existed, decodes to nil, and produces no goroutine, no request and
// no key in the file.
func TestAFreshInstallHasNoEventTargetsAtAll(t *testing.T) {
	if Defaults().EventTargets != nil {
		t.Fatalf("a fresh install starts with %d event target(s); nothing may send until somebody says so", len(Defaults().EventTargets))
	}
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Set(Defaults()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "eventTargets") {
		t.Errorf("a fresh settings.json names eventTargets; the field is omitempty precisely so that an upgrade "+
			"changes nothing about the file:\n%s", b)
	}
}

// TestAnEventTargetSecretSurvivesTheRedactedRoundTrip is the trip every
// settings page makes: the browser is served Redacted(), edits one unrelated
// field, and posts the whole document back. Without the merge in setLocked the
// first save from ANY page writes eight literal stars over the token.
func TestAnEventTargetSecretSurvivesTheRedactedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.EventTargets = []notify.Target{targetWithToken()}
	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}

	shown := saved.Redacted()
	if got := shown.EventTargets[0].Headers["Authorization"]; got != notify.RedactedValue {
		t.Fatalf("the browser is served %q; the moment a client is shown a header value, the merge is protecting a value it already holds", got)
	}
	// The redaction must not have reached the stored copy through a shared map.
	if got := saved.EventTargets[0].Headers["Authorization"]; got != "Bearer real-token" {
		t.Fatalf("Redacted() blanked the stored token as well (%q)", got)
	}

	// What the browser sends back: the whole document, stars and all.
	shown.MaxConcurrent = 7
	back, err := st.Set(shown)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.EventTargets[0].Headers["Authorization"]; got != "Bearer real-token" {
		t.Errorf("after a save from a page that was shown stars the token is %q; every settings page would clear it", got)
	}

	// And on disk, which is the copy the next start reads.
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().EventTargets[0].Headers["Authorization"]; got != "Bearer real-token" {
		t.Errorf("the file holds %q", got)
	}
}

// TestAnEventTargetSecretDoesNotFollowAChangedAddress is the security guard at
// the layer that actually writes the file.
//
// The browser was never shown the token and the browser is what types the
// address, so a token that followed a changed address could be aimed at a
// machine the client controls. Advanced.tsx hands the whole list back as raw
// editable JSON, so this is one paste rather than a theory.
func TestAnEventTargetSecretDoesNotFollowAChangedAddress(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.EventTargets = []notify.Target{targetWithToken()}
	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}

	moved := saved.Redacted()
	moved.EventTargets[0].URL = "https://attacker.example/collect"
	back, err := st.Set(moved)
	if err != nil {
		t.Fatal(err)
	}
	got := back.EventTargets[0].Headers["Authorization"]
	if got == "Bearer real-token" {
		t.Fatal("the stored token was carried onto an address the client typed; a client that was never allowed to read it " +
			"can now have this server post it wherever it likes")
	}
	if strings.Contains(got, "*") {
		t.Errorf("the header kept the literal placeholder (%q); eight stars sent as a bearer token is a 401 nobody can diagnose", got)
	}
}

// TestSanitizeReachesTheEventTargets proves the one line in sanitize() is
// actually wired, rather than the field simply being stored verbatim.
func TestSanitizeReachesTheEventTargets(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.EventTargets = []notify.Target{{
		Name: "  spaced  ", URL: "  https://x.example/  ", Method: "delete", TimeoutSeconds: 9999,
	}}
	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}
	got := saved.EventTargets[0]
	if got.ID == "" {
		t.Error("no id was assigned, so the health table and the secret merge have nothing to join on")
	}
	if got.Name != "spaced" || got.URL != "https://x.example/" {
		t.Errorf("name %q address %q, want both trimmed", got.Name, got.URL)
	}
	if got.Method != notify.MethodPOST {
		t.Errorf("method %q survived; the closed list is what keeps a route that sends from inside the instance to three verbs", got.Method)
	}
	if got.TimeoutSeconds != notify.MaxTimeoutSeconds {
		t.Errorf("timeout %d was not clamped", got.TimeoutSeconds)
	}
}

// TestTheWholeDocumentStillDecodesWithEventTargetsPresent guards the one thing
// ApplyPatch's marshal/merge/unmarshal round trip can quietly break: a field
// whose JSON shape does not survive being encoded and decoded again.
func TestTheWholeDocumentStillDecodesWithEventTargetsPresent(t *testing.T) {
	base := Defaults()
	base.EventTargets = []notify.Target{targetWithToken()}
	patched, err := ApplyPatch(base, map[string]json.RawMessage{"maxConcurrent": json.RawMessage("9")})
	if err != nil {
		t.Fatal(err)
	}
	if len(patched.EventTargets) != 1 {
		t.Fatalf("a patch of an unrelated field lost the event targets: %+v", patched.EventTargets)
	}
	if patched.EventTargets[0].Headers["Authorization"] != "Bearer real-token" {
		t.Error("the header did not survive the JSON round trip ApplyPatch makes")
	}
	if len(patched.EventTargets[0].Triggers) != 1 {
		t.Error("the trigger list did not survive the JSON round trip")
	}
}
