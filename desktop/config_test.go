package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigMatchesJDDefaults(t *testing.T) {
	// JD's TrayConfig defaults to EXIT on close and TO_TASKBAR on minimize, and
	// someone coming from JD should find the same.
	c := defaultConfig()
	if c.StartHidden {
		t.Errorf("StartHidden default = true, want false")
	}
	if c.OnClose != CloseExit {
		t.Errorf("OnClose default = %q, want %q", c.OnClose, CloseExit)
	}
	if c.OnMinimize != MinimizeTaskbar {
		t.Errorf("OnMinimize default = %q, want %q", c.OnMinimize, MinimizeTaskbar)
	}
	if c.RaiseOnAttention != RaiseOff {
		t.Errorf("RaiseOnAttention default = %q, want %q", c.RaiseOnAttention, RaiseOff)
	}
}

func TestSanitizeCoercesUnknownValuesToDefault(t *testing.T) {
	c := Config{
		StartHidden:      true,
		OnClose:          "ask", // JD's option that KnightLoader does not offer
		OnMinimize:       "bogus",
		RaiseOnAttention: "",
	}.sanitize()

	if c.OnClose != CloseExit {
		t.Errorf("OnClose = %q, want fallback %q", c.OnClose, CloseExit)
	}
	if c.OnMinimize != MinimizeTaskbar {
		t.Errorf("OnMinimize = %q, want fallback %q", c.OnMinimize, MinimizeTaskbar)
	}
	if c.RaiseOnAttention != RaiseOff {
		t.Errorf("RaiseOnAttention = %q, want fallback %q", c.RaiseOnAttention, RaiseOff)
	}
	if !c.StartHidden {
		t.Errorf("StartHidden was reset by sanitize, want left alone")
	}
}

func TestSanitizeLeavesValidValuesAlone(t *testing.T) {
	want := Config{StartHidden: true, OnClose: CloseTray, OnMinimize: MinimizeTray, RaiseOnAttention: RaiseFocus}
	got := want.sanitize()
	if got != want {
		t.Errorf("sanitize() changed a fully valid config: got %+v, want %+v", got, want)
	}
}

func TestLoadConfigMissingFileReturnsDefault(t *testing.T) {
	got := loadConfig(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if got != defaultConfig() {
		t.Errorf("loadConfig(missing) = %+v, want defaults", got)
	}
}

func TestLoadConfigCorruptFileReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadConfig(path)
	if got != defaultConfig() {
		t.Errorf("loadConfig(corrupt) = %+v, want defaults", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "desktop.json")
	want := Config{StartHidden: true, OnClose: CloseTray, OnMinimize: MinimizeTray, RaiseOnAttention: RaiseFront}

	if err := want.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file %s.tmp still exists after save", path)
	}

	got := loadConfig(path)
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestLoadConfigOldValueDegradesNotErrors(t *testing.T) {
	// A value from a newer version falls back to the default for that field.
	path := filepath.Join(t.TempDir(), "desktop.json")
	raw := `{"startHidden":true,"onClose":"someFutureValue","onMinimize":"tray","raiseOnAttention":"front"}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadConfig(path)
	if got.OnClose != CloseExit {
		t.Errorf("OnClose = %q, want fallback %q", got.OnClose, CloseExit)
	}
	if !got.StartHidden || got.OnMinimize != MinimizeTray || got.RaiseOnAttention != RaiseFront {
		t.Errorf("valid fields were not preserved: %+v", got)
	}
}
