package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/extract"
)

// loadFrom writes a settings document and reads it back the way the app does.
func loadFrom(t *testing.T, doc string) Settings {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s.Get()
}

// The whole reason the migration exists: `deleteArchive` was a boolean and
// `archiveDisposal` is a word. An install that had chosen to delete has to go
// on deleting, and one that had not has to go on keeping.
func TestDeleteArchiveBooleanBecomesADisposal(t *testing.T) {
	for _, c := range []struct {
		doc  string
		want extract.Disposal
	}{
		{`{"deleteArchive":true}`, extract.DisposalDelete},
		{`{"deleteArchive":false}`, extract.DisposalKeep},
	} {
		if got := loadFrom(t, c.doc).ArchiveDisposal; got != string(c.want) {
			t.Errorf("%s loaded as %q, want %q", c.doc, got, c.want)
		}
	}
}

// A document that already carries the new key has been through this once. The
// old boolean must not be allowed to undo the choice on every restart, which is
// what a client still sending it would otherwise do.
func TestTheNewKeyWinsOverTheOldBoolean(t *testing.T) {
	got := loadFrom(t, `{"deleteArchive":true,"archiveDisposal":"trash"}`).ArchiveDisposal
	if got != string(extract.DisposalTrash) {
		t.Errorf("archiveDisposal = %q, want the stored trash rather than the old boolean", got)
	}
}

// A document with neither key is a fresh install, and the default may not be
// the destructive one.
func TestAFreshInstallKeepsItsArchives(t *testing.T) {
	if got := loadFrom(t, `{}`).ArchiveDisposal; got != string(extract.DisposalKeep) {
		t.Errorf("archiveDisposal on a fresh document = %q, want keep", got)
	}
}

// The old spelling is read once and never written again. A file that still held
// it after a save would put the two keys in the document at the same time, and
// the next build to read only one of them would pick the stale answer.
func TestTheOldKeyIsNotWrittenBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"deleteArchive":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(s.Get()); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if _, still := doc["deleteArchive"]; still {
		t.Error("the saved document still carries deleteArchive")
	}
	if doc["archiveDisposal"] != string(extract.DisposalDelete) {
		t.Errorf("archiveDisposal = %v, want the migrated delete", doc["archiveDisposal"])
	}
}

// Sanitize folds an unknown word rather than refusing the file, and it stores
// the folded value: a settings file has to say what the app will actually do.
func TestArchiveSettingsAreFoldedOnTheWayIn(t *testing.T) {
	got := loadFrom(t, `{"archiveDisposal":"recycle","extractCollision":"ask","trashRetentionDays":-3,"extractTo":"relative/path"}`)
	if got.ArchiveDisposal != string(extract.DefaultDisposal) {
		t.Errorf("archiveDisposal = %q, want %q", got.ArchiveDisposal, extract.DefaultDisposal)
	}
	// Ask parks a task until a human answers, and an extraction has nobody to
	// ask, so it may never survive into the stored settings.
	if got.ExtractCollision != string(extract.DefaultCollision) {
		t.Errorf("extractCollision = %q, want %q", got.ExtractCollision, extract.DefaultCollision)
	}
	if got.TrashRetentionDays != 0 {
		t.Errorf("trashRetentionDays = %d, want 0", got.TrashRetentionDays)
	}
	if got.ExtractTo != "" {
		t.Errorf("extractTo = %q, want a relative path dropped", got.ExtractTo)
	}
}

// A template destination is absolute in the only part that is a real path, and
// refusing it for the angle brackets in its tail would make the wildcards
// unusable on this field.
func TestATemplateDestinationSurvives(t *testing.T) {
	// Built from a real absolute path for this platform rather than written out
	// as "/unpacked/...": a leading slash is not an absolute path on Windows,
	// and the test would then be checking the wrong refusal.
	want := filepath.Join(t.TempDir(), "<jd:packagename>")
	doc, err := json.Marshal(map[string]string{"extractTo": want})
	if err != nil {
		t.Fatal(err)
	}
	if got := loadFrom(t, string(doc)).ExtractTo; got != want {
		t.Errorf("extractTo = %q, want the template kept", got)
	}
}
