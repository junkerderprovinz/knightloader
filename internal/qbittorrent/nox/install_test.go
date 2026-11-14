package nox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
)

// fakeRelease stands in for GitHub: the files it serves by name, and the
// release metadata a lookup answers.
type fakeRelease struct {
	files   map[string][]byte
	release ghrelease.Release
	lookups int
	fetched []string
	version string
}

func (f *fakeRelease) installer(goos, goarch string) installer {
	return installer{
		goos:   goos,
		goarch: goarch,
		lookup: func(ctx context.Context, repo, tag string) (ghrelease.Release, error) {
			f.lookups++
			return f.release, nil
		},
		fetch: func(ctx context.Context, a ghrelease.Asset, dst string) error {
			f.fetched = append(f.fetched, a.Name)
			b, ok := f.files[a.Name]
			if !ok {
				return errors.New("404")
			}
			return os.WriteFile(dst, b, 0o644)
		},
		smoke: func(ctx context.Context, bin string) (string, error) {
			if _, err := os.Stat(bin); err != nil {
				return "", err
			}
			return f.version, nil
		},
	}
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func linuxPin(body []byte) Version {
	return Version{Version: "4.5.5", Libtorrent: "1.2.19", Assets: map[string]Asset{
		"linux-amd64": {Repo: "userdocs/qbittorrent-nox-static", Tag: "release-4.5.5_v1.2.19", Name: "x86_64-qbittorrent-nox", Size: int64(len(body)), SHA256: digest(body)},
	}}
}

func TestAPinnedBuildIsFetchedVerifiedAndPutInPlace(t *testing.T) {
	data := t.TempDir()
	body := []byte("\x7fELF a static qbittorrent-nox")
	f := &fakeRelease{files: map[string][]byte{"x86_64-qbittorrent-nox": body}, version: "4.5.5"}
	rec, current, err := f.installer("linux", "amd64").install(context.Background(), data, linuxPin(body), nil)
	if err != nil || current {
		t.Fatalf("install: %v, current %v", err, current)
	}
	if f.lookups != 0 {
		t.Error("a pinned build asked GitHub for its release")
	}
	bin := binaryIn(dirFor(data, "4.5.5"), "linux")
	got, err := os.ReadFile(bin)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("installed %q, %v", got, err)
	}
	if rec.Version != "4.5.5" || rec.SHA256 != digest(body) || rec.Libtorrent != "1.2.19" {
		t.Errorf("record %+v", rec)
	}
	if back, err := loadRecord(dirFor(data, "4.5.5")); err != nil || back == nil || back.SHA256 != rec.SHA256 {
		t.Errorf("record on disk %+v, %v", back, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(data, "tools", "qbittorrent", ".*"))
	if len(leftovers) != 0 {
		t.Errorf("staging left behind: %v", leftovers)
	}
}

func TestABuildThatDoesNotMatchItsPinIsThrownAway(t *testing.T) {
	data := t.TempDir()
	pin := linuxPin([]byte("the real one"))
	f := &fakeRelease{files: map[string][]byte{"x86_64-qbittorrent-nox": []byte("the real one, changed")}, version: "4.5.5"}
	_, _, err := f.installer("linux", "amd64").install(context.Background(), data, pin, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(dirFor(data, "4.5.5")); !os.IsNotExist(err) {
		t.Fatal("something was put in place")
	}
}

func TestABuildInPlaceIsNotFetchedAgain(t *testing.T) {
	data := t.TempDir()
	body := []byte("nox")
	f := &fakeRelease{files: map[string][]byte{"x86_64-qbittorrent-nox": body}, version: "4.5.5"}
	in := f.installer("linux", "amd64")
	if _, _, err := in.install(context.Background(), data, linuxPin(body), nil); err != nil {
		t.Fatal(err)
	}
	_, current, err := in.install(context.Background(), data, linuxPin(body), nil)
	if err != nil || !current {
		t.Fatalf("second install: %v, current %v", err, current)
	}
	if len(f.fetched) != 1 {
		t.Fatalf("fetched %v", f.fetched)
	}
}

func TestAnUnpinnedBuildIsCheckedAgainstGitHubsOwnDigest(t *testing.T) {
	data := t.TempDir()
	body := []byte("nox")
	pin := Version{Version: "4.5.5", Assets: map[string]Asset{"linux-amd64": {Tag: "t", Name: "n"}}}
	f := &fakeRelease{
		files:   map[string][]byte{"n": body},
		release: ghrelease.Release{Tag: "t", Assets: []ghrelease.Asset{{Name: "n", Digest: "sha256:" + digest(body)}}},
		version: "4.5.5",
	}
	if _, _, err := f.installer("linux", "amd64").install(context.Background(), data, pin, nil); err != nil {
		t.Fatal(err)
	}
	f.release.Assets[0].Digest = "sha256:" + digest([]byte("other"))
	if _, _, err := f.installer("linux", "amd64").install(context.Background(), t.TempDir(), pin, nil); err == nil {
		t.Fatal("a build that does not match GitHub's digest was taken")
	}
}

func TestAnUnpinnedBuildFallsBackToTheChecksumsBesideIt(t *testing.T) {
	body := []byte("nox")
	pin := Version{Version: "4.5.5", Assets: map[string]Asset{"linux-amd64": {Tag: "t", Name: "n"}}}
	f := &fakeRelease{
		files: map[string][]byte{"n": body, "SHA256SUMS": []byte(digest(body) + "  n\n")},
		release: ghrelease.Release{Tag: "t", Assets: []ghrelease.Asset{
			{Name: "n"}, {Name: "SHA256SUMS"},
		}},
		version: "4.5.5",
	}
	if _, _, err := f.installer("linux", "amd64").install(context.Background(), t.TempDir(), pin, nil); err != nil {
		t.Fatal(err)
	}
}

func TestABuildNothingVouchesForIsRefused(t *testing.T) {
	pin := Version{Version: "4.5.5", Assets: map[string]Asset{"linux-amd64": {Tag: "t", Name: "n"}}}
	f := &fakeRelease{
		files:   map[string][]byte{"n": []byte("nox")},
		release: ghrelease.Release{Tag: "t", Assets: []ghrelease.Asset{{Name: "n"}}},
		version: "4.5.5",
	}
	_, _, err := f.installer("linux", "amd64").install(context.Background(), t.TempDir(), pin, nil)
	if err == nil || len(f.fetched) != 0 {
		t.Fatalf("err = %v, fetched %v", err, f.fetched)
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(f, body)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAZippedBuildIsUnpackedWithItsFolders(t *testing.T) {
	data := t.TempDir()
	body := zipOf(t, map[string]string{
		"qbittorrent-nox.exe":     "MZ nox",
		"tls/qopensslbackend.dll": "MZ plugin",
		"COPYING":                 "GPL",
	})
	pin := Version{Version: "4.5.5", Assets: map[string]Asset{"windows-amd64": {Tag: "qbittorrent-nox-v4.5.5", Name: "qbittorrent-nox-4.5.5-windows-amd64.zip", SHA256: digest(body)}}}
	f := &fakeRelease{files: map[string][]byte{pin.Assets["windows-amd64"].Name: body}, version: "4.5.5"}
	if _, _, err := f.installer("windows", "amd64").install(context.Background(), data, pin, nil); err != nil {
		t.Fatal(err)
	}
	dir := dirFor(data, "4.5.5")
	for _, p := range []string{"qbittorrent-nox.exe", filepath.Join("tls", "qopensslbackend.dll"), "COPYING"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, pin.Assets["windows-amd64"].Name)); !os.IsNotExist(err) {
		t.Error("the archive itself was left in place")
	}
}

func TestAZipEntryThatClimbsOutIsRefused(t *testing.T) {
	data := t.TempDir()
	body := zipOf(t, map[string]string{"qbittorrent-nox.exe": "MZ", "../../escape.txt": "x"})
	pin := Version{Version: "4.5.5", Assets: map[string]Asset{"windows-amd64": {Tag: "t", Name: "b.zip", SHA256: digest(body)}}}
	f := &fakeRelease{files: map[string][]byte{"b.zip": body}, version: "4.5.5"}
	if _, _, err := f.installer("windows", "amd64").install(context.Background(), data, pin, nil); err == nil {
		t.Fatal("an archive with an escaping entry was installed")
	}
	if _, err := os.Stat(filepath.Join(data, "tools", "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("the escaping entry was written")
	}
}

func TestABuildThatCallsItselfAnotherVersionIsRefused(t *testing.T) {
	data := t.TempDir()
	body := []byte("nox")
	f := &fakeRelease{files: map[string][]byte{"x86_64-qbittorrent-nox": body}, version: "5.1.0"}
	_, _, err := f.installer("linux", "amd64").install(context.Background(), data, linuxPin(body), nil)
	if err == nil || !strings.Contains(err.Error(), "5.1.0") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPlatformWithoutABuildSaysSo(t *testing.T) {
	f := &fakeRelease{version: "4.5.5"}
	_, _, err := f.installer("freebsd", "amd64").install(context.Background(), t.TempDir(), linuxPin(nil), nil)
	if err == nil || !strings.Contains(err.Error(), "freebsd/amd64") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheVersionLineIsRead(t *testing.T) {
	for out, want := range map[string]string{
		"qBittorrent v4.5.5\n":      "4.5.5",
		"qBittorrent v5.0.5\r\n":    "5.0.5",
		"something else entirely\n": "",
	} {
		if got := versionLine(out); got != want {
			t.Errorf("versionLine(%q) = %q, want %q", out, got, want)
		}
	}
}
