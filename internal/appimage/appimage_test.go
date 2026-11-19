package appimage

import "testing"

func TestTheRuntimeNamesTheFile(t *testing.T) {
	t.Setenv("APPIMAGE", "/home/jane doe/Apps/KnightLoader.AppImage")
	if got := Path(); got != "/home/jane doe/Apps/KnightLoader.AppImage" {
		t.Errorf("Path() = %q inside an AppImage", got)
	}
}

func TestOutsideAnAppImageThereIsNoFile(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	if got := Path(); got != "" {
		t.Errorf("Path() = %q outside an AppImage", got)
	}
}
