package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigMatchesJDDefaults(t *testing.T) {
	// TrayConfig.getOnCloseAction() is EXIT by default and getOnMinimizeAction()
	// is TO_TASKBAR by default (docs/jd-feature-census.md line 47) - a fresh
	// KnightLoader install should not surprise a JD refugee with different
	// close/minimize behaviour before they have touched a single setting.
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
		OnClose:          "ask", // JD's fourth option, deliberately not built - see tray.go's doc comment
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
	// sanitize must never touch a field it does not itself own the enum for.
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
	// save must not leave its temp file behind for load to trip over later.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file %s.tmp still exists after save", path)
	}

	got := loadConfig(path)
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestLoadConfigOldValueDegradesNotErrors(t *testing.T) {
	// A config written by a future version with a value this build does not
	// know must degrade to the default for that field, the same rule
	// rules.Disabled and the settings sanitize hooks already use - not fail
	// to start, and not silently carry an enum value with no case for it.
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
