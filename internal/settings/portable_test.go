package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/eventprog"
	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// configured is a settings document with something in every corner this feature
// has an opinion about: two secrets, an identity, a list, and a plain number.
func configured() Settings {
	s := Defaults()
	s.SpeedLimit = 4 << 20
	s.MaxConcurrent = 7
	s.InstanceID = "0123456789abcdef0123456789abcdef01234567"
	s.InstanceName = "the cellar box"
	s.KnownDomains = []string{"kl.example.invalid"}
	s.ArchivePasswords = []string{"hunter2", "correct horse"}
	s.Reconnect = reconnect.Config{
		Method:   reconnect.MethodHTTP,
		Username: "admin",
		Password: "router-secret",
		CheckURL: "https://example.invalid/ip",
		Requests: []reconnect.Request{{URL: "https://192.168.0.1/reconnect"}},
	}
	s.Connections = []proxycfg.Entry{{
		ID:       "c1",
		Kind:     proxycfg.KindHTTP,
		Host:     "proxy.example.invalid",
		Port:     3128,
		Username: "vpn",
		Password: "proxy-secret",
		Enabled:  true,
	}}
	return s
}

// The file, applied on the far side, reproduces the configuration it was taken
// from, apart from the two identity keys asserted separately below.
func TestPortableWithSecretsRoundTripsIntoAnEqualDocument(t *testing.T) {
	src := configured()
	doc, err := Portable(src, true, "v1.2.3", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != PortableKind {
		t.Errorf("kind = %q, want %q", doc.Kind, PortableKind)
	}
	if doc.Secrets != SecretsIncluded {
		t.Errorf("secrets = %q, want %q", doc.Secrets, SecretsIncluded)
	}

	// Applied onto a fresh document, the way a second box would, and through the
	// same ApplyPatch the import route uses rather than a hand-rolled merge.
	got, err := ApplyPatch(Defaults(), doc.Settings)
	if err != nil {
		t.Fatal(err)
	}

	// The identity is expected to differ, so it is taken out of both sides
	// rather than being allowed to hide a real difference somewhere else.
	want := src
	want.InstanceID, got.InstanceID = "", ""
	want.KnownDomains, got.KnownDomains = nil, nil
	if !reflect.DeepEqual(want, got) {
		t.Errorf("the round trip lost or changed something:\n src = %#v\n got = %#v", want, got)
	}
	if got.Reconnect.Password != "router-secret" {
		t.Errorf("the router password did not survive an export that asked for it: %q", got.Reconnect.Password)
	}
	if len(got.Connections) != 1 || got.Connections[0].Password != "proxy-secret" {
		t.Errorf("the proxy password did not survive an export that asked for it: %#v", got.Connections)
	}
}

// Two boxes carrying one instanceId occupy a single slot in a relay group, and
// the symptom is a sibling that keeps going offline with nothing naming the
// cause. The id must not be in the file, whether or not the caller asked for
// secrets.
func TestPortableNeverCarriesThisBoxIdentity(t *testing.T) {
	// Spelled out rather than read back from NeverPortable(): ranging over the
	// list under test would leave an empty NeverPortable() asserting nothing,
	// green while every identity key travels.
	identity := []string{"instanceId", "knownDomains"}
	named := map[string]bool{}
	for _, k := range NeverPortable() {
		named[k] = true
	}
	for _, k := range identity {
		if !named[k] {
			t.Errorf("NeverPortable() does not name %q", k)
		}
	}

	for _, withSecrets := range []bool{true, false} {
		doc, err := Portable(configured(), withSecrets, "v1.2.3", "container", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range identity {
			if _, ok := doc.Settings[k]; ok {
				t.Errorf("includeSecrets=%v: %q is in the exported document and must never be", withSecrets, k)
			}
		}
		// The identity is dropped and the name beside it is not: instanceName is
		// a label somebody chose, and it travels.
		if _, ok := doc.Settings["instanceName"]; !ok {
			t.Errorf("includeSecrets=%v: instanceName was dropped too, and it is not an identity", withSecrets)
		}
	}
}

// Redacted() hides the router and proxy passwords and leaves ArchivePasswords
// alone, because that field is ordinary visible config on the Archives page. An
// export that only called Redacted() would ship every archive password in clear
// text under a toggle claiming otherwise.
func TestPortableWithoutSecretsStripsAllThreeAndSaysSo(t *testing.T) {
	doc, err := Portable(configured(), false, "v1.2.3", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Secrets != SecretsOmitted {
		t.Errorf("secrets = %q, want %q", doc.Secrets, SecretsOmitted)
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Searched over the whole encoded document rather than field by field,
	// because the question is what somebody opening the file in an editor reads.
	for _, secret := range []string{"router-secret", "proxy-secret", "hunter2", "correct horse"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the secretless export still contains %q", secret)
		}
	}

	var archives []string
	if err := json.Unmarshal(doc.Settings["archivePasswords"], &archives); err != nil {
		t.Fatalf("archivePasswords is not a list any more: %v", err)
	}
	if len(archives) != 0 {
		t.Errorf("archivePasswords = %v, want empty", archives)
	}

	// The file has to say which passwords are missing, because both fail
	// silently on the far side: a proxy dials with a username and no password,
	// and a reconnect posts an empty password and reports that the address did
	// not change, which points the operator at their router.
	want := []string{SecretlessReconnect, SecretlessConnections, SecretlessArchivePasswords}
	if got := doc.Secretless(); !reflect.DeepEqual(got, want) {
		t.Errorf("Secretless() = %v, want %v", got, want)
	}
}

// A list that cries wolf on a complete document is one people learn to ignore.
// The two cases that would produce a false alarm are a UPnP reconnect, which
// needs no password, and an anonymous proxy.
func TestSecretlessIsQuietWhenNothingIsMissing(t *testing.T) {
	s := Defaults()
	s.Reconnect = reconnect.Config{Method: reconnect.MethodUPnP, CheckURL: "https://example.invalid/ip"}
	s.Connections = []proxycfg.Entry{{ID: "c1", Kind: proxycfg.KindHTTP, Host: "h", Port: 8080, Enabled: true}}
	s.ArchivePasswords = []string{"kept"}
	doc, err := Portable(s, true, "v1.2.3", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Secretless(); len(got) != 0 {
		t.Errorf("Secretless() = %v on a document that is missing nothing", got)
	}
}

// A program row that travels without its command line is imported with no
// program at all, which would otherwise pass for a complete import.
func TestAnEventProgramExportedWithoutSecretsIsReportedIncomplete(t *testing.T) {
	s := Defaults()
	s.EventPrograms = eventprog.Sanitize([]eventprog.Program{{
		Name: "file it", Enabled: true,
		Command:  idleaction.CommandSpec{Program: "/home/someone/bin/file-it.sh"},
		Triggers: []script.Trigger{script.TriggerTaskDone},
	}})
	doc, err := Portable(s, false, "v1.2.3", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Secretless(); !slices.Contains(got, SecretlessEventPrograms) {
		t.Errorf("Secretless() = %v, want %s among them", got, SecretlessEventPrograms)
	}
	whole, err := Portable(s, true, "v1.2.3", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := whole.Secretless(); slices.Contains(got, SecretlessEventPrograms) {
		t.Errorf("Secretless() = %v for an export that carries the command line", got)
	}
}

// Secretless reads d.Settings rather than d.Secrets, because the secrets field
// is a string in a file somebody can edit: a document claiming "included" while
// carrying the redaction placeholder is what a hand edit produces.
func TestSecretlessJudgesTheDocumentAndNotItsClaim(t *testing.T) {
	doc, err := Portable(configured(), false, "v1.2.3", "container", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	doc.Secrets = SecretsIncluded
	got := doc.Secretless()
	if len(got) == 0 {
		t.Fatal("a relabelled secretless document reported nothing missing")
	}
}

func TestPortableDocCheck(t *testing.T) {
	ok := func() PortableDoc {
		d, err := Portable(Defaults(), false, "v1.2.0", "container", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	t.Run("a document from an older build is the normal case", func(t *testing.T) {
		if err := ok().Check("v1.9.0"); err != nil {
			t.Errorf("an older document was refused: %v", err)
		}
	})

	t.Run("an untagged build compares nothing", func(t *testing.T) {
		// "dev" is not semver, and refusing to import into a development build
		// would make the feature untestable.
		d := ok()
		d.Version = "dev"
		if err := d.Check("dev"); err != nil {
			t.Errorf("a dev-to-dev import was refused: %v", err)
		}
	})

	t.Run("a document from a newer build is refused, typed", func(t *testing.T) {
		d := ok()
		d.Version = "v2.0.0"
		err := d.Check("v1.9.0")
		if err == nil {
			t.Fatal("a document from the future was accepted")
		}
		var ve *PortableVersionError
		if !errors.As(err, &ve) {
			t.Fatalf("the refusal is not typed, so the interface cannot translate it: %T", err)
		}
		if ve.DocVersion != "v2.0.0" || ve.RunningVersion != "v1.9.0" {
			t.Errorf("the refusal names the wrong versions: %#v", ve)
		}
	})

	t.Run("something that is not one of ours", func(t *testing.T) {
		d := ok()
		d.Kind = "knightloader-backup"
		if err := d.Check("v1.9.0"); err == nil {
			t.Error("a file claiming to be a different kind was accepted")
		}
		d.Kind = ""
		if err := d.Check("v1.9.0"); err == nil {
			t.Error("a file that does not say what it is was accepted")
		}
	})

	t.Run("an empty document", func(t *testing.T) {
		d := ok()
		d.Settings = nil
		if err := d.Check("v1.9.0"); err == nil {
			t.Error("a document carrying no settings at all was accepted")
		}
	})
}

// A migration looks at the whole document but changes only what the import
// names. The legacy autoStart below would also set autoConfirm, which nobody
// named, so the patch stays one key long.
func TestMigratedRewritesOnlyTheNamedKeys(t *testing.T) {
	doc := PortableDoc{Kind: PortableKind, Settings: map[string]json.RawMessage{
		"autoStart":    json.RawMessage(`true`),
		"stallTimeout": json.RawMessage(`0`),
		"speedLimit":   json.RawMessage(`1048576`),
	}}

	got := doc.Migrated(map[string]json.RawMessage{
		"stallTimeout": doc.Settings["stallTimeout"],
		"speedLimit":   doc.Settings["speedLimit"],
	})

	if len(got) != 2 {
		t.Fatalf("the patch holds %d keys, want the 2 that were named: %v", len(got), got)
	}
	if s := string(got["stallTimeout"]); s != "120" {
		t.Errorf("stallTimeout = %s, want the 120 an older build's 0 is read as", s)
	}
	if s := string(got["speedLimit"]); s != "1048576" {
		t.Errorf("speedLimit = %s, want it untouched", s)
	}
}
