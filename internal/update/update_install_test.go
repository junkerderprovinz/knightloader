package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPlatformSlugMatchesDesktopWorkflow(t *testing.T) {
	// The three slugs .github/workflows/desktop.yml's matrix ever produces a
	// zip for - a fourth GOOS/GOARCH this package might someday be built for
	// has nothing published to download, which is exactly what the error
	// path below covers.
	slug, err := platformSlug()
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" && (err != nil || slug != "windows-amd64") {
		t.Fatalf("platformSlug() on windows/amd64 = (%q, %v), want (\"windows-amd64\", nil)", slug, err)
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && (err != nil || slug != "linux-amd64") {
		t.Fatalf("platformSlug() on linux/amd64 = (%q, %v), want (\"linux-amd64\", nil)", slug, err)
	}
	if runtime.GOOS == "darwin" && (err != nil || slug != "macos-universal") {
		t.Fatalf("platformSlug() on darwin = (%q, %v), want (\"macos-universal\", nil)", slug, err)
	}
}

func TestAssetNameMatchesDesktopWorkflowPackaging(t *testing.T) {
	// Exactly desktop.yml's own `zip -qr "../../dist/knightloader-${GITHUB_REF_NAME}-${slug}.zip"`.
	got := assetName("v1.2.3", "windows-amd64")
	want := "knightloader-v1.2.3-windows-amd64.zip"
	if got != want {
		t.Fatalf("assetName() = %q, want %q", got, want)
	}
}

func TestExecutableNameByPlatform(t *testing.T) {
	got := executableName()
	switch runtime.GOOS {
	case "windows":
		if got != "KnightLoader.exe" {
			t.Fatalf("executableName() on windows = %q, want KnightLoader.exe", got)
		}
	case "darwin":
		if got != "KnightLoader.app" {
			t.Fatalf("executableName() on darwin = %q, want KnightLoader.app", got)
		}
	default:
		if got != "KnightLoader" {
			t.Fatalf("executableName() on %s = %q, want KnightLoader", runtime.GOOS, got)
		}
	}
}

// buildZip writes a minimal zip containing one top-level entry named `name`
// with `content`, mirroring what desktop.yml's "Package" step produces
// (the zipped contents of Wails' own build output directory - one
// executable/bundle at the top level, no extra wrapper folder).
func buildZip(t *testing.T, path, name, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	fw, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractExecutableFindsTheTopLevelBinary(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")
	buildZip(t, zipPath, executableName(), "new-binary-contents")

	staged, err := extractExecutable(zipPath, dir)
	if err != nil {
		t.Fatalf("extractExecutable: %v", err)
	}
	defer os.RemoveAll(staged)

	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("reading staged binary: %v", err)
	}
	if string(got) != "new-binary-contents" {
		t.Fatalf("staged binary content = %q, want %q", got, "new-binary-contents")
	}
}

func TestExtractExecutableRefusesAZipWithoutTheExpectedEntry(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")
	buildZip(t, zipPath, "some-other-file.txt", "irrelevant")

	if _, err := extractExecutable(zipPath, dir); err == nil {
		t.Fatal("extractExecutable: want an error for a zip missing the expected top-level binary")
	}
}

func TestExtractExecutableRefusesPathTraversal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")
	buildZip(t, zipPath, "../../etc/evil", "irrelevant")

	if _, err := extractExecutable(zipPath, dir); err == nil {
		t.Fatal("extractExecutable: want an error for a zip entry escaping the extraction root")
	}
}

// TestApplySwapsAtomicallyAndBacksUpTheOldVersion exercises Apply end to end
// against real temp files (no network, no zip.OpenReader mocking) - the
// part of the install flow that most needs to actually work: the running
// "old" file is preserved as installPath+".old" until the new one is
// successfully in place, never leaving neither version present.
func TestApplySwapsAtomicallyAndBacksUpTheOldVersion(t *testing.T) {
	dir := t.TempDir()
	installPath := filepath.Join(dir, executableName())
	if err := os.WriteFile(installPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(dir, "release.zip")
	buildZip(t, zipPath, executableName(), "new-binary")

	if err := Apply(zipPath, installPath); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatalf("reading installPath after Apply: %v", err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("installPath content after Apply = %q, want %q", got, "new-binary")
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Fatalf("Apply should have consumed the zip; stat error = %v", err)
	}
	// The .old backup is best-effort cleaned up immediately when nothing
	// holds it open, which is always true in this test (no real running
	// process) - so it should be gone too.
	if _, err := os.Stat(installPath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("Apply should have cleaned up the .old backup; stat error = %v", err)
	}
}

func TestApplyLeavesTheOldVersionRunnableWhenThereWasNoPreviousInstall(t *testing.T) {
	// The very first Apply on a fresh install has nothing to rename aside -
	// Lstat(installPath) returning IsNotExist must not be treated as a
	// failure.
	dir := t.TempDir()
	installPath := filepath.Join(dir, executableName())
	zipPath := filepath.Join(dir, "release.zip")
	buildZip(t, zipPath, executableName(), "first-install")

	if err := Apply(zipPath, installPath); err != nil {
		t.Fatalf("Apply with no prior install: %v", err)
	}
	got, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatalf("reading installPath: %v", err)
	}
	if string(got) != "first-install" {
		t.Fatalf("installPath content = %q, want %q", got, "first-install")
	}
}
