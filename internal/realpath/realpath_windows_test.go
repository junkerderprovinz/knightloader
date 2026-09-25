//go:build windows

package realpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAJunctionResolvesToItsTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J is not available here: %v: %s", err, out)
	}
	want, err := Resolve(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, filepath.Join(link, "."), link + `\`} {
		if got, err := Resolve(p); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want the target %q", p, got, err, want)
		}
	}
}

func TestAPlainFolderResolvesToItselfWithoutTheLongPathPrefix(t *testing.T) {
	dir := t.TempDir()
	got, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || !sameFolder(t, got, dir) {
		t.Errorf("Resolve(%q) = %q", dir, got)
	}
	if strings.HasPrefix(got, `\\?\`) {
		t.Errorf("Resolve kept the long path prefix: %q", got)
	}
}

func TestAMissingPathIsAnError(t *testing.T) {
	if _, err := Resolve(filepath.Join(t.TempDir(), "gone")); !os.IsNotExist(err) {
		t.Errorf("Resolve of a missing path answered %v, want a not-exist error", err)
	}
}

func sameFolder(t *testing.T, a, b string) bool {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(fa, fb)
}
