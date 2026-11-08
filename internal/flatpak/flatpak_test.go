package flatpak

import "testing"

func TestTheSandboxNamesTheApp(t *testing.T) {
	t.Setenv("FLATPAK_ID", "io.github.junkerderprovinz.knightloader")
	if got := ID(); got != "io.github.junkerderprovinz.knightloader" {
		t.Errorf("ID() = %q inside a Flatpak", got)
	}
}

func TestOutsideASandboxThereIsNoID(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	if got := ID(); got != "" {
		t.Errorf("ID() = %q outside a Flatpak", got)
	}
}
