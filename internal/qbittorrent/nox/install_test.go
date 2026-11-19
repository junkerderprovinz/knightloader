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
	"runtime"
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
	tags    []string
	fetched []string
	urls    []string
	version string
}

func (f *fakeRelease) installer(goos, goarch string) installer {
	return installer{
		goos:   goos,
		goarch: goarch,
		lookup: func(ctx context.Context, repo, tag string) (ghrelease.Release, error) {
			f.lookups++
			f.tags = append(f.tags, tag)
			return f.release, nil
		},
		fetch: func(ctx context.Context, a ghrelease.Asset, dst string) error {
			f.fetched = append(f.fetched, a.Name)
			f.urls = append(f.urls, a.URL)
			b, ok := f.files[a.Name]
			if !ok {
				return errors.New("404")
			}
			return os.WriteFile(dst, b, 0o644)
		},
		smoke: func(ctx context.Context, bin, home string) (string, error) {
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
	bin := binaryIn(dirFor(data, "4.5.5"), "linux", Asset{})
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
	pin := Version{Version: "4.5.5", Assets: map[string]Asset{"windows-amd64": {Name: "qbittorrent-nox-4.5.5-windows-amd64.zip", SHA256: digest(body)}}}
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

func TestAReleaseThatCannotBeFoundNamesWhereItWasLookedFor(t *testing.T) {
	pin := Version{Version: "4.6.7", Assets: map[string]Asset{"windows-amd64": {Repo: "someone/builds", Tag: "builds-4.6.7", Name: "b.zip"}}}
	in := installer{
		goos: "windows", goarch: "amd64",
		lookup: func(context.Context, string, string) (ghrelease.Release, error) {
			return ghrelease.Release{}, errors.New("github answered 404 Not Found")
		},
	}
	_, _, err := in.install(context.Background(), t.TempDir(), pin, nil)
	if err == nil || !strings.Contains(err.Error(), "someone/builds") || !strings.Contains(err.Error(), "builds-4.6.7") {
		t.Fatalf("err = %v", err)
	}
}

func TestAZippedBuildNamesWhereItsProgramLies(t *testing.T) {
	data := t.TempDir()
	inner := "qbittorrent-nox.app/Contents/MacOS/qbittorrent-nox"
	body := zipOf(t, map[string]string{
		inner: "nox",
		"qbittorrent-nox.app/Contents/Frameworks/QtCore.framework/QtCore": "lib",
	})
	pin := Version{Version: "4.5.5", Assets: map[string]Asset{"darwin-arm64": {Tag: "t", Name: "b.zip", Bin: inner, SHA256: digest(body)}}}
	f := &fakeRelease{files: map[string][]byte{"b.zip": body}, version: "4.5.5"}
	var ran string
	in := f.installer("darwin", "arm64")
	in.smoke = func(ctx context.Context, bin, home string) (string, error) {
		ran = bin
		return "4.5.5", nil
	}
	if _, _, err := in.install(context.Background(), data, pin, nil); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dirFor(data, "4.5.5"), filepath.FromSlash(inner)); binaryIn(dirFor(data, "4.5.5"), "darwin", pin.Assets["darwin-arm64"]) != want {
		t.Fatalf("binaryIn = %q, want %q", binaryIn(dirFor(data, "4.5.5"), "darwin", pin.Assets["darwin-arm64"]), want)
	}
	if filepath.Base(filepath.Dir(ran)) != "MacOS" {
		t.Fatalf("the smoke test ran %q", ran)
	}
}

func TestAnOwnBuildIsFetchedFromTheKnightLoaderReleaseThatRuns(t *testing.T) {
	t.Setenv(releasesEnv, "")
	body := zipOf(t, map[string]string{"qbittorrent-nox.exe": "MZ nox"})
	pin := Version{Version: "5.0.5", Assets: map[string]Asset{"windows-amd64": {Name: "qbittorrent-nox-5.0.5-windows-amd64.zip", SHA256: digest(body)}}}
	f := &fakeRelease{files: map[string][]byte{pin.Assets["windows-amd64"].Name: body}, version: "5.0.5"}
	in := f.installer("windows", "amd64")
	in.running = "v3.0.0"
	rec, _, err := in.install(context.Background(), t.TempDir(), pin, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://github.com/junkerderprovinz/knightloader/releases/download/v3.0.0/qbittorrent-nox-5.0.5-windows-amd64.zip"
	if len(f.urls) != 1 || f.urls[0] != want || f.lookups != 0 {
		t.Fatalf("fetched %v after %d lookups, want %s alone", f.urls, f.lookups, want)
	}
	if rec.Tag != "v3.0.0" || rec.Repo != defaultReleases {
		t.Errorf("recorded %s at %s", rec.Repo, rec.Tag)
	}
}

func TestAnOwnBuildWithoutADigestIsVouchedForByTheLatestRelease(t *testing.T) {
	t.Setenv(releasesEnv, "")
	body := zipOf(t, map[string]string{"qbittorrent-nox.exe": "MZ nox"})
	name := "qbittorrent-nox-5.0.5-windows-amd64.zip"
	pin := Version{Version: "5.0.5", Assets: map[string]Asset{"windows-amd64": {Name: name}}}
	f := &fakeRelease{
		files:   map[string][]byte{name: body},
		release: ghrelease.Release{Assets: []ghrelease.Asset{{Name: name, Digest: "sha256:" + digest(body)}}},
		version: "5.0.5",
	}
	in := f.installer("windows", "amd64")
	in.running = "dev"
	if _, _, err := in.install(context.Background(), t.TempDir(), pin, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.tags) != 1 || f.tags[0] != "" {
		t.Errorf("looked up the releases %q, want the latest alone", f.tags)
	}
}

// fakeNox stands in for qbittorrent-nox --version, which aborts when it cannot
// create its cache folder under HOME.
const fakeNox = `#!/bin/sh
mkdir -p "$HOME/.cache/qBittorrent" 2>/dev/null || {
	echo "Could not create required directory '$HOME/.cache/qBittorrent'" >&2
	kill -ABRT $$
}
echo "qBittorrent v5.0.5"
`

func writeFakeNox(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	bin := filepath.Join(t.TempDir(), "qbittorrent-nox")
	if err := os.WriteFile(bin, []byte(fakeNox), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestTheVersionRunWorksWhenHomeCannotBeWritten(t *testing.T) {
	bin := writeFakeNox(t)
	t.Setenv("HOME", "/proc")
	t.Setenv("XDG_CACHE_HOME", "/proc/.cache")
	got, err := smokeTest(context.Background(), bin, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != "5.0.5" {
		t.Fatalf("the version read %q", got)
	}
}

func TestABuildThatAbortsSaysWhatItPrinted(t *testing.T) {
	bin := writeFakeNox(t)
	_, err := smokeTest(context.Background(), bin, "/proc")
	if err == nil {
		t.Fatal("a build that aborted passed the smoke test")
	}
	if !strings.Contains(err.Error(), "Could not create required directory") {
		t.Fatalf("the error leaves out what qbittorrent-nox printed: %v", err)
	}
}
