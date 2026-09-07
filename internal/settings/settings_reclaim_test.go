package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/reclaim"
)

// TestAnUpgradeGetsNoDiskRunAtBoot is the promise the whole "already on the
// disk" pass has to keep to somebody who did nothing but install an update:
// the settings document gains a POLICY and no switch, so there is nothing in
// here that a new version could set and thereby hand them a scan of a
// ten-thousand-row list in front of the queue.
//
// It is written as an assertion about the keys rather than as a comment,
// because "there is no boot switch" is exactly the kind of promise a later
// wave adds a field to without noticing.
func TestAnUpgradeGetsNoDiskRunAtBoot(t *testing.T) {
	b, err := json.Marshal(Defaults())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for key := range doc {
		switch key {
		case "reclaimOnStart", "reclaimAtBoot", "reclaimAutomatic", "reclaimOnBoot":
			t.Errorf("settings.json carries %q; the disk pass is a press, not something an update can switch on for somebody", key)
		}
	}
	if _, ok := doc["reclaimTrust"]; !ok {
		t.Error("reclaimTrust is missing from the document, so the frontend cannot type a field that is sometimes absent")
	}
}

// TestTheDefaultTrustIsTheRecordTier pins the middle rung. The strict tier
// would make the feature a no-op for anybody whose hosters publish no hashes,
// and the most trusting one is wrong often enough to matter on a build whose
// download library creates the destination file at full length before it
// fetches a byte (see reclaim.Trust).
func TestTheDefaultTrustIsTheRecordTier(t *testing.T) {
	if got := Defaults().ReclaimTrust; got != ReclaimTrustRecord {
		t.Errorf("default ReclaimTrust = %q, want %q", got, ReclaimTrustRecord)
	}
	if ReclaimTrustRecord != string(reclaim.DefaultTrust) {
		t.Errorf("this package's default (%q) and internal/reclaim's own (%q) disagree",
			ReclaimTrustRecord, reclaim.DefaultTrust)
	}
}

// TestAnInstallFromBeforeThisKeyBehavesLikeAFreshOne. Nothing rewrites
// settings.json on upgrade, so the key is simply absent for every existing
// install, and an absent key must not become a fourth, unnamed tier.
func TestAnInstallFromBeforeThisKeyBehavesLikeAFreshOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"maxConcurrent":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	if got.ReclaimTrust != ReclaimTrustRecord {
		t.Errorf("ReclaimTrust = %q on a document that predates the key, want %q", got.ReclaimTrust, ReclaimTrustRecord)
	}
	if got.MaxConcurrent != 9 {
		t.Errorf("MaxConcurrent = %d; the rest of the document was not read", got.MaxConcurrent)
	}
}

// TestAnUnusableTrustValueIsFoldedOntoTheDefault keeps a hand-edited or
// foreign-build value from becoming the most trusting tier by accident, which
// is the only direction that costs anything here.
func TestAnUnusableTrustValueIsFoldedOntoTheDefault(t *testing.T) {
	for _, in := range []string{"", "everything", "SIZE!", "true"} {
		n := sanitizeReclaim(Settings{ReclaimTrust: in})
		if n.ReclaimTrust != ReclaimTrustRecord {
			t.Errorf("sanitizeReclaim(%q) = %q, want %q", in, n.ReclaimTrust, ReclaimTrustRecord)
		}
	}
	for _, in := range ReclaimTrustModes() {
		if n := sanitizeReclaim(Settings{ReclaimTrust: in}); n.ReclaimTrust != in {
			t.Errorf("sanitizeReclaim(%q) = %q, want it left alone", in, n.ReclaimTrust)
		}
	}
}

// TestAChosenTrustSurvivesASave is the round trip through the one path
// everything written to disk goes down. A value that sanitize silently
// rewrote would be a setting the user re-picks and never sees stick.
func TestAChosenTrustSurvivesASave(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := s.Get()
	n.ReclaimTrust = ReclaimTrustChecksum
	if applied, err := s.Set(n); err != nil {
		t.Fatal(err)
	} else if applied.ReclaimTrust != ReclaimTrustChecksum {
		t.Fatalf("Set returned %q, want %q", applied.ReclaimTrust, ReclaimTrustChecksum)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get().ReclaimTrust; got != ReclaimTrustChecksum {
		t.Errorf("after a reload ReclaimTrust = %q, want %q", got, ReclaimTrustChecksum)
	}
}
