package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheTestBuildKeepsItsOwnFolders(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no config folder here: %v", err)
	}
	t.Setenv("KL_DATA", "")
	defer func(p string) { product = p }(product)

	product = "KnightLoader"
	realData, realWebview := dataDir(), webviewDataDir()
	product = "KnightLoader Test"
	testData, testWebview := dataDir(), webviewDataDir()

	if want := filepath.Join(base, "KnightLoader"); realData != want {
		t.Errorf("real data folder = %q, want %q", realData, want)
	}
	if want := filepath.Join(base, "KnightLoader.exe"); realWebview != want {
		t.Errorf("real WebView2 folder = %q, want Wails' default %q", realWebview, want)
	}
	if want := filepath.Join(base, "KnightLoader Test"); testData != want {
		t.Errorf("test data folder = %q, want %q", testData, want)
	}
	if want := filepath.Join(base, "KnightLoader Test.exe"); testWebview != want {
		t.Errorf("test WebView2 folder = %q, want %q", testWebview, want)
	}
}

func TestKLDataNamesTheDataFolderForEitherBuild(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KL_DATA", dir)
	defer func(p string) { product = p }(product)
	product = "KnightLoader Test"

	if got := dataDir(); got != dir {
		t.Errorf("dataDir() = %q, want KL_DATA %q", got, dir)
	}
}
