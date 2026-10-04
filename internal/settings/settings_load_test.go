package settings

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeSettings(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A crash in the middle of a save leaves a cut-off file. The instance comes up
// on defaults, and the first save after that must not be the end of what the
// file held.
func TestACutOffSettingsFileIsKeptBeforeTheNextSave(t *testing.T) {
	dir := t.TempDir()
	cut := `{"speedLimit": 5000, "downloadDir": "/mnt/us`
	writeSettings(t, dir, cut)

	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	backup, why := s.Unreadable()
	if why == nil {
		t.Fatal("Load read a cut-off file without a word")
	}
	if backup == "" {
		t.Fatal("no copy of the unreadable file was kept")
	}
	if got := s.Get().SpeedLimit; got != Defaults().SpeedLimit {
		t.Errorf("speedLimit = %d, want the default", got)
	}

	if _, err := s.Set(s.Get()); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != cut {
		t.Errorf("the copy holds %q, want the file as it was", kept)
	}
}

// One hand-edited value of the wrong type costs that value and nothing else.
func TestAMistypedValueCostsOnlyItsOwnSetting(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, `{"maxConcurrent": "three", "speedLimit": 5000}`)

	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().SpeedLimit; got != 5000 {
		t.Errorf("speedLimit = %d, want the 5000 beside the mistyped value", got)
	}
	if got := s.Get().MaxConcurrent; got != Defaults().MaxConcurrent {
		t.Errorf("maxConcurrent = %d, want the default", got)
	}
	if backup, why := s.Unreadable(); why == nil || backup == "" {
		t.Errorf("Unreadable = %q, %v; want the copy and the reason", backup, why)
	}
}

// A file that stays broken across restarts is copied once, not once per start.
func TestTheSameUnreadableFileIsCopiedOnce(t *testing.T) {
	dir := t.TempDir()
	body := `{"speedLimit": `
	writeSettings(t, dir, body)
	earlier := filepath.Join(dir, unreadablePrefix+"20260101-120000")
	if err := os.WriteFile(earlier, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if backup, _ := s.Unreadable(); backup != earlier {
		t.Errorf("Unreadable names %q, want the copy an earlier start kept", backup)
	}
	copies, err := filepath.Glob(filepath.Join(dir, unreadablePrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 1 {
		t.Errorf("found %d copies, want one: %v", len(copies), copies)
	}
}

func TestAReadableFileReportsNothing(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, `{"speedLimit": 5000}`)
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if backup, why := s.Unreadable(); why != nil || backup != "" {
		t.Errorf("Unreadable = %q, %v; want nothing", backup, why)
	}
}

// A save leaves the settings file and the kept instance id, and nothing else in
// the folder.
func TestASaveLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.Set(s.Get()); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{instanceIDFile, "settings.json"}; !slices.Equal(names, want) {
		t.Errorf("folder holds %v, want %v", names, want)
	}
}
