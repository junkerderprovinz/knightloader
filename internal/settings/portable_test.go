package settings

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
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

// TestPortableWithSecretsRoundTripsIntoAnEqualDocument is the whole promise of
// the export: the file, applied on the far side, reproduces the configuration
// it was taken from. Everything except the two keys that are this box's own
// identity, which is the one deliberate difference and is asserted separately
// below.
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

	// Applied onto a FRESH document, the way a second box would, and through the
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

// TestPortableNeverCarriesThisBoxIdentity is the guard on NeverPortable. Two
// boxes carrying one instanceId occupy a single slot in a relay group, and the
// symptom is a sibling that "keeps going offline" with nothing naming the
// cause - so the id must not be in the file at all, whether or not the caller
// asked for secrets.
func TestPortableNeverCarriesThisBoxIdentity(t *testing.T) {
	// Spelled out here rather than read back from NeverPortable(), and that is
	// the difference between a test and a tautology: ranging over the very list
	// under test means an empty NeverPortable() asserts nothing at all and the
	// test goes green while every identity key travels. Measured, not guessed -
	// emptying that function left this test passing until the two keys were
	// written down here.
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
		// The identity is dropped, and the name beside it is not: instanceName is
		// a label somebody chose and is perfectly portable. Asserted so a future
		// widening of NeverPortable has to be deliberate.
		if _, ok := doc.Settings["instanceName"]; !ok {
			t.Errorf("includeSecrets=%v: instanceName was dropped too, and it is not an identity", withSecrets)
		}
	}
}

// TestPortableWithoutSecretsStripsAllThreeAndSaysSo covers the trap that
// Redacted() alone does not close: it hides the router and proxy passwords and
// deliberately leaves ArchivePasswords alone, because that field is ordinary
// visible config on the Archives page. An export that only called Redacted()
// would ship every archive password in clear text under a toggle claiming
// otherwise.
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
	// Searched over the whole encoded document rather than field by field: the
	// question this test asks is "can somebody who opens this file in an editor
	// read my passwords", and that question is about the bytes.
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

	// And the file has to SAY which passwords are missing, because both of them
	// fail silently on the far side: a proxy dials with a username and no
	// password, and a reconnect posts an empty password and reports "the address
	// did not change", which points the operator at their router.
	want := []string{SecretlessReconnect, SecretlessConnections, SecretlessArchivePasswords}
	if got := doc.Secretless(); !reflect.DeepEqual(got, want) {
		t.Errorf("Secretless() = %v, want %v", got, want)
	}
}

// TestSecretlessIsQuietWhenNothingIsMissing pins the other half. A list that
// cries wolf on a complete document is a list people learn to ignore, and the
// two cases that would produce a false alarm are a UPnP reconnect (which needs
// no password at all) and an anonymous proxy.
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

// TestSecretlessJudgesTheDocumentAndNotItsClaim is why Secretless reads
// d.Settings rather than d.Secrets: the secrets field is a string in a file
// somebody can edit, and a document claiming "included" while carrying the
// redaction placeholder is exactly what a well-meaning hand edit produces.
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
		// would make the feature untestable by the person writing it.
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
