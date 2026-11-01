package jre

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// entry is one member of a crafted archive: a file with body, or a link to
// target when link is a tar link type.
type entry struct {
	name, body, target string
	link               byte
}

func tarGz(t *testing.T, dir string, entries ...entry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(e.body))}
		if e.link != 0 {
			h = &tar.Header{Name: e.name, Mode: 0o777, Typeflag: e.link, Linkname: e.target}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.link == 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "runtime.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func zipOf(t *testing.T, dir string, entries ...entry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "runtime.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// unpackInto unpacks archive into a fresh folder inside parent and returns it.
func unpackInto(t *testing.T, parent, archive string) (string, error) {
	t.Helper()
	dir := filepath.Join(parent, "runtime")
	return dir, unpack(archive, dir)
}

func TestARuntimeArchiveUnpacksWithItsLinksAsCopies(t *testing.T) {
	work := t.TempDir()
	archive := tarGz(t, work,
		entry{name: "jdk-21.0.5+11-jre/bin/java", body: "java"},
		entry{name: "jdk-21.0.5+11-jre/legal/java.base/LICENSE", body: "licence"},
		entry{name: "jdk-21.0.5+11-jre/legal/java.desktop/LICENSE", link: tar.TypeSymlink, target: "../java.base/LICENSE"},
		entry{name: "jdk-21.0.5+11-jre/legal/java.sql/LICENSE", link: tar.TypeLink, target: "jdk-21.0.5+11-jre/legal/java.base/LICENSE"},
	)
	dir, err := unpackInto(t, work, archive)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, "bin", "java")); got != "java" {
		t.Errorf("bin/java holds %q", got)
	}
	for _, module := range []string{"java.desktop", "java.sql"} {
		p := filepath.Join(dir, "legal", module, "LICENSE")
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("%s's licence is missing: %v", module, err)
		}
		if !fi.Mode().IsRegular() {
			t.Errorf("%s's licence is a %s, want a plain copy", module, fi.Mode().Type())
		}
		if got := mustRead(t, p); got != "licence" {
			t.Errorf("%s's licence holds %q", module, got)
		}
	}
}

func TestAZipRuntimeUnpacksWithoutItsTopFolder(t *testing.T) {
	work := t.TempDir()
	archive := zipOf(t, work, entry{name: "jdk-21.0.5+11-jre/bin/java.exe", body: "java"})
	dir, err := unpackInto(t, work, archive)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, "bin", "java.exe")); got != "java" {
		t.Errorf("bin/java.exe holds %q", got)
	}
}

func TestARuntimeArchiveThatReachesOutsideIsRefused(t *testing.T) {
	hostile := map[string][]entry{
		"a name that climbs out":        {{name: "jdk/../../evil", body: "x"}},
		"an absolute name":              {{name: "/tmp/evil", body: "x"}},
		"a drive letter":                {{name: "C:/evil", body: "x"}},
		"a symbolic link out":           {{name: "jdk/lib/escape", link: tar.TypeSymlink, target: "../../../evil"}},
		"a symbolic link to a root":     {{name: "jdk/lib/escape", link: tar.TypeSymlink, target: "/etc/passwd"}},
		"a hard link out of the folder": {{name: "jdk/lib/escape", link: tar.TypeLink, target: "../evil"}},
	}
	for name, entries := range hostile {
		t.Run(name, func(t *testing.T) {
			work := t.TempDir()
			if _, err := unpackInto(t, work, tarGz(t, work, entries...)); !errors.Is(err, errOutside) {
				t.Errorf("unpack = %v, want it refused", err)
			}
			assertNothingOutside(t, work)
		})
	}
	for name, entries := range map[string][]entry{
		"a name that climbs out": {{name: "jdk/../../evil", body: "x"}},
		"an absolute name":       {{name: "/tmp/evil", body: "x"}},
	} {
		t.Run("zip with "+name, func(t *testing.T) {
			work := t.TempDir()
			if _, err := unpackInto(t, work, zipOf(t, work, entries...)); !errors.Is(err, errOutside) {
				t.Errorf("unpack = %v, want it refused", err)
			}
			assertNothingOutside(t, work)
		})
	}
}

// assertNothingOutside fails when work holds anything but the archive and the
// runtime's own folder.
func assertNothingOutside(t *testing.T, work string) {
	t.Helper()
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		switch e.Name() {
		case "runtime", "runtime.tar.gz", "runtime.zip":
		default:
			t.Errorf("%s was written beside the runtime", e.Name())
		}
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(work), "evil")); err == nil {
		t.Error("evil was written above the work folder")
	}
}
