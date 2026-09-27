package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheUpdateSwitchCanLiveInAFileOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "machine.json")

	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.KeepAutoUpdateIn(shared)
	if !st.Get().AutoUpdate {
		t.Error("a missing shared file does not read as on")
	}
	off := st.Get()
	off.AutoUpdate = false
	if _, err := st.Set(off); err != nil {
		t.Fatalf("set: %v", err)
	}
	if ReadAutoUpdate(shared) {
		t.Error("turning updates off did not reach the shared file")
	}

	// The shared file wins over whatever settings.json says.
	if err := os.WriteFile(shared, []byte(`{"autoUpdate": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	again.KeepAutoUpdateIn(shared)
	if !again.Get().AutoUpdate {
		t.Error("the shared file's switch was not read")
	}
}

func TestASharedFileThatRefusesTheSwitchKeepsItsValue(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A folder where the file should be cannot be written as a file.
	shared := filepath.Join(dir, "machine.json")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	st.KeepAutoUpdateIn(shared)

	off := st.Get()
	off.AutoUpdate = false
	if _, err := st.Set(off); err == nil {
		t.Error("a refused write reported no error")
	}
	if !st.Get().AutoUpdate {
		t.Error("the page shows updates off although the file still says on")
	}
}

func TestACorruptSharedFileReadsAsOn(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "machine.json")
	if err := os.WriteFile(shared, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ReadAutoUpdate(shared) {
		t.Error("a corrupt file turned updates off")
	}
}
