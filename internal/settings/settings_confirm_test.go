package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
)

// The old AutoStart conflated confirm and start, so an install that had it on
// loses something if only one of the two new fields inherits the value: either
// its links stop leaving the collector, or they leave it and never run.
func TestOldAutoStartTrueMapsToBothNewFieldsTrue(t *testing.T) {
	got := loadFrom(t, `{"autoStart":true}`)
	if !got.AutoConfirm {
		t.Error("AutoConfirm = false, want true - the old flag being on always skipped the collector")
	}
	if !got.AutoStart {
		t.Error("AutoStart = false, want true - the old flag being on always meant confirmed links ran immediately")
	}
}

// The other half: an install that never turned the old flag on must not gain
// auto-confirm, and "click start, it runs", which held with or without the old
// flag, survives.
func TestOldAutoStartFalseKeepsAutoConfirmOffAndAutoStartOn(t *testing.T) {
	got := loadFrom(t, `{"autoStart":false}`)
	if got.AutoConfirm {
		t.Error("AutoConfirm = true, want false - the old flag was off")
	}
	if !got.AutoStart {
		t.Error("AutoStart = false, want true - starting once confirmed is not something the old flag ever governed")
	}
}

// Defaults() read through Load with no document: what a new install gets, which
// has to look like an install that never touched the old AutoStart flag.
func TestAFreshInstallMatchesTheOldDefaultBehaviour(t *testing.T) {
	got := loadFrom(t, `{}`)
	if got.AutoConfirm {
		t.Error("a fresh install auto-confirms; the collector should wait for a person")
	}
	if !got.AutoStart {
		t.Error("a fresh install does not start a confirmed batch; every install before this split did")
	}
	if got.AutoConfirmDelay != 0 {
		t.Errorf("AutoConfirmDelay = %d on a fresh install, want 0", got.AutoConfirmDelay)
	}
	if got.AddAtTop {
		t.Error("a fresh install reorders the queue on confirm; nothing before this existed did")
	}
}

// The reasoning TestTheNewKeyWinsOverTheOldBoolean gives for ArchiveDisposal: a
// document that already carries autoConfirm, even explicitly false, has been
// through this once, and a client still sending the legacy autoStart key
// alongside it must not override that choice on every later load.
func TestANewFormatDocumentIsNotTouchedByTheMigration(t *testing.T) {
	got := loadFrom(t, `{"autoStart":true,"autoConfirm":false}`)
	if got.AutoConfirm {
		t.Error("AutoConfirm was flipped true by a legacy key a migrated document should no longer defer to")
	}
	// AutoStart still reads true, not because the migration touched it but
	// because it is the same JSON key going forward and the document says
	// true. Ordinary json.Unmarshal handles that without migrateAutoStart.
	if !got.AutoStart {
		t.Error("AutoStart = false, want true - the document says so directly, unmigrated")
	}
}

// The round trip: once a legacy document has been read and saved back, loading
// it again re-derives nothing, because the file now carries autoConfirm and
// looks like the already-migrated case above.
func TestAutoStartMigrationIsStableAcrossASave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"autoStart":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := s.Get()
	if !first.AutoConfirm || !first.AutoStart {
		t.Fatalf("first load = %+v, want both true", first)
	}
	// A save with AutoConfirm turned back off by hand.
	first.AutoConfirm = false
	if _, err := s.Set(first); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Get()
	if got.AutoConfirm {
		t.Error("AutoConfirm came back true after a reload; the migration re-ran over a document it should have left alone")
	}
	if !got.AutoStart {
		t.Error("AutoStart = false after a reload, want true")
	}
}

func TestOnDupesOnOfflineDefaultToExclude(t *testing.T) {
	got := loadFrom(t, `{}`)
	if got.OnDupes != string(confirm.Exclude) {
		t.Errorf("OnDupes = %q, want %q", got.OnDupes, confirm.Exclude)
	}
	if got.OnOffline != string(confirm.Exclude) {
		t.Errorf("OnOffline = %q, want %q", got.OnOffline, confirm.Exclude)
	}
}

// Nothing may be deleted by a default nobody touched, so a settings file this
// build cannot make sense of must never read as permission to delete.
func TestOnDupesOnOfflineNeverFoldToExcludeAndRemove(t *testing.T) {
	got := loadFrom(t, `{"onDupes":"","onOffline":"not-a-real-policy"}`)
	if got.OnDupes == string(confirm.ExcludeAndRemove) || got.OnOffline == string(confirm.ExcludeAndRemove) {
		t.Fatalf("garbled input folded to exclude-and-remove: onDupes=%q onOffline=%q", got.OnDupes, got.OnOffline)
	}
	if got.OnDupes != string(confirm.Exclude) || got.OnOffline != string(confirm.Exclude) {
		t.Errorf("onDupes=%q onOffline=%q, want both folded to exclude", got.OnDupes, got.OnOffline)
	}
}

// The other side: exclude-and-remove is refused only as a fallback, never as a
// stored value, so somebody who set it on purpose sees it saved.
func TestOnDupesOnOfflineHonourADeliberateChoice(t *testing.T) {
	doc, err := json.Marshal(map[string]string{
		"onDupes":   string(confirm.ExcludeAndRemove),
		"onOffline": string(confirm.Ask),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := loadFrom(t, string(doc))
	if got.OnDupes != string(confirm.ExcludeAndRemove) {
		t.Errorf("OnDupes = %q, want the deliberately stored exclude-and-remove", got.OnDupes)
	}
	if got.OnOffline != string(confirm.Ask) {
		t.Errorf("OnOffline = %q, want the deliberately stored ask", got.OnOffline)
	}
}

// An instance-level default cannot defer to itself, so a document saying
// use-global, hand-edited or written by a build that let the per-batch value
// leak into the global field, folds to the package default.
func TestOnDupesOnOfflineNeverAcceptUseGlobal(t *testing.T) {
	got := loadFrom(t, `{"onDupes":"use-global"}`)
	if got.OnDupes != string(confirm.DefaultPolicy) {
		t.Errorf("OnDupes = %q, want the global default to fall back to %q rather than use-global", got.OnDupes, confirm.DefaultPolicy)
	}
}

func TestAutoConfirmDelayIsClamped(t *testing.T) {
	cases := []struct {
		doc  string
		want int
	}{
		{`{"autoConfirmDelay":-5}`, 0},
		{`{"autoConfirmDelay":30}`, 30},
		{`{"autoConfirmDelay":999999}`, 24 * 60 * 60},
	}
	for _, c := range cases {
		if got := loadFrom(t, c.doc).AutoConfirmDelay; got != c.want {
			t.Errorf("%s -> AutoConfirmDelay = %d, want %d", c.doc, got, c.want)
		}
	}
}

func TestAddAtTopRoundTrips(t *testing.T) {
	doc, err := json.Marshal(map[string]bool{"addAtTop": true})
	if err != nil {
		t.Fatal(err)
	}
	if got := loadFrom(t, string(doc)).AddAtTop; !got {
		t.Error("AddAtTop = false, want the stored true to survive sanitize")
	}
}
