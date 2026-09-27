package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const assetName = "prog-test-asset.zip"

type fakeRelease struct {
	tag        string
	prerelease bool
	files      map[string][]byte
}

// withSums adds a checksums.txt holding the real hash of every file, then
// applies edit to it.
func (r fakeRelease) withSums(edit func(string) string) fakeRelease {
	var b strings.Builder
	for name, data := range r.files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	r.files[ChecksumsFile] = []byte(edit(b.String()))
	return r
}

func keep(s string) string { return s }

// serve stands in for GitHub: the latest-release endpoint and the downloads it
// points at.
func serve(t *testing.T, rel fakeRelease) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/prog/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		body := struct {
			TagName    string  `json:"tag_name"`
			HTMLURL    string  `json:"html_url"`
			Prerelease bool    `json:"prerelease"`
			Assets     []asset `json:"assets"`
		}{TagName: rel.tag, HTMLURL: srv.URL + "/releases/" + rel.tag, Prerelease: rel.prerelease, Assets: []asset{}}
		for name := range rel.files {
			body.Assets = append(body.Assets, asset{name, srv.URL + "/download/" + name})
		}
		json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("GET /download/{name}", func(w http.ResponseWriter, r *http.Request) {
		data, ok := rel.files[r.PathValue("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newUpdater points at srv and at a program file in a fresh folder.
func newUpdater(t *testing.T, srv *httptest.Server, version string) *Updater {
	t.Helper()
	dir := t.TempDir()
	prog := filepath.Join(dir, "prog")
	if err := os.WriteFile(prog, []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Updater{
		Repo:    "owner/prog",
		Version: version,
		Assets:  map[string]string{runtime.GOOS + "/" + runtime.GOARCH: assetName},
		API:     srv.URL,
		Client:  srv.Client(),
		Path:    prog,
		Logf:    t.Logf,
	}
}

// programZip is a release zip as desktop.yml packs one: the program alone at
// the top level, under the name the build gave it.
func programZip(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "KnightLoader", Method: zip.Deflate}
	h.SetMode(0o755)
	fw, err := w.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte(body))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func release(t *testing.T, tag string) fakeRelease {
	return fakeRelease{tag: tag, files: map[string][]byte{assetName: programZip(t, "new program "+tag)}}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestCheckOffersANewerRelease(t *testing.T) {
	srv := serve(t, release(t, "v1.4.0").withSums(keep))
	rel, err := newUpdater(t, srv, "v1.3.0").Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil || rel.Version != "1.4.0" {
		t.Fatalf("got %+v, want release 1.4.0", rel)
	}
}

func TestCheckIgnoresTheSameOrAnOlderRelease(t *testing.T) {
	for _, tag := range []string{"v1.3.0", "v1.2.9", "v0.9.0"} {
		srv := serve(t, release(t, tag).withSums(keep))
		rel, err := newUpdater(t, srv, "v1.3.0").Check(context.Background())
		if err != nil || rel != nil {
			t.Errorf("%s: got %+v, %v; want nothing", tag, rel, err)
		}
	}
}

func TestCheckIgnoresPreReleases(t *testing.T) {
	flagged := release(t, "v2.0.0").withSums(keep)
	flagged.prerelease = true
	for _, rel := range []fakeRelease{flagged, release(t, "v2.0.0-rc1").withSums(keep)} {
		srv := serve(t, rel)
		got, err := newUpdater(t, srv, "v1.3.0").Check(context.Background())
		if err != nil || got != nil {
			t.Errorf("%s: got %+v, %v; want nothing", rel.tag, got, err)
		}
	}
}

func TestCheckRefusesADevBuild(t *testing.T) {
	srv := serve(t, release(t, "v1.4.0").withSums(keep))
	for _, v := range []string{"", "dev"} {
		if _, err := newUpdater(t, srv, v).Check(context.Background()); err == nil {
			t.Errorf("a build with version %q checked for updates", v)
		}
	}
}

func TestCheckFailsWithoutAFileForThisPlatform(t *testing.T) {
	srv := serve(t, release(t, "v1.4.0").withSums(keep))
	u := newUpdater(t, srv, "v1.3.0")
	u.Assets = map[string]string{"plan9/mips": assetName}
	if _, err := u.Check(context.Background()); err == nil {
		t.Error("checked for a platform the program has no file for")
	}

	missing := fakeRelease{tag: "v1.4.0", files: map[string][]byte{"other": []byte("x")}}
	srv = serve(t, missing.withSums(keep))
	if _, err := newUpdater(t, srv, "v1.3.0").Check(context.Background()); err == nil {
		t.Error("offered a release that lacks this platform's file")
	}
}

func TestFetchVerifiesTheChecksum(t *testing.T) {
	cases := []struct {
		name string
		rel  fakeRelease
		ok   bool
	}{
		{"right", release(t, "v1.4.0").withSums(keep), true},
		{"wrong", release(t, "v1.4.0").withSums(func(s string) string {
			return strings.Replace(s, s[:8], "00000000", 1)
		}), false},
		{"not listed", release(t, "v1.4.0").withSums(func(string) string { return "" }), false},
		{"no checksums.txt", release(t, "v1.4.0"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpdater(t, serve(t, c.rel), "v1.3.0")
			rel, err := u.Check(context.Background())
			if err != nil || rel == nil {
				t.Fatalf("check: %+v, %v", rel, err)
			}
			download, err := u.Fetch(context.Background(), rel)
			if !c.ok {
				if err == nil {
					t.Fatal("fetched a file that does not match its checksum")
				}
				if got := dirEntries(t, filepath.Dir(u.Path)); len(got) != 1 {
					t.Fatalf("left %v behind", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(download)
			if err != nil || !bytes.Equal(data, c.rel.files[assetName]) {
				t.Fatalf("download differs from the release file: %v", err)
			}
			if filepath.Dir(download) != filepath.Dir(u.Path) {
				t.Errorf("downloaded to %s, away from the program", download)
			}
		})
	}
}

func TestAnUpdateReplacesTheProgramAndIsNotFetchedTwice(t *testing.T) {
	u := newUpdater(t, serve(t, release(t, "v1.4.0").withSums(keep)), "v1.3.0")
	ctx := context.Background()
	rel, err := u.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	download, err := u.Fetch(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Swap(download, rel); err != nil {
		t.Fatal(err)
	}
	os.Remove(download)
	if data, _ := os.ReadFile(u.Path); string(data) != "new program v1.4.0" {
		t.Fatalf("program holds %q after the swap", data)
	}
	if again, err := u.Check(ctx); err != nil || again != nil {
		t.Errorf("after staging 1.4.0, check offered %+v, %v", again, err)
	}

	u.Cleanup()
	if got := dirEntries(t, filepath.Dir(u.Path)); len(got) != 1 {
		t.Errorf("cleanup left %v", got)
	}
}

func TestSwapRefusesAZipThatIsNotTheProgramAlone(t *testing.T) {
	cases := map[string][]zipEntry{
		"empty":     nil,
		"two files": {{"KnightLoader", "a", 0o755}, {"README", "b", 0o644}},
		"a folder":  {{"KnightLoader/", "", os.ModeDir | 0o755}, {"KnightLoader/KnightLoader", "a", 0o755}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			u := newUpdater(t, serve(t, release(t, "v1.4.0")), "v1.3.0")
			download := filepath.Join(filepath.Dir(u.Path), downloadPrefix(u.Path)+"zip")
			writeZip(t, download, entries)
			if err := u.Swap(download, &Release{Version: "1.4.0", asset: assetName}); err == nil {
				t.Fatal("swapped in a zip that does not hold the program alone")
			}
			if got := readFile(t, u.Path); got != "old program" {
				t.Errorf("program holds %q", got)
			}
		})
	}
}

func TestCleanupRemovesTheOldProgramAndStrayDownloads(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "prog")
	for _, p := range []string{prog, prog + ".old", filepath.Join(dir, downloadPrefix(prog)+"123")} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	(&Updater{Path: prog, Logf: t.Logf}).Cleanup()
	if got := dirEntries(t, dir); len(got) != 1 || got[0] != "prog" {
		t.Errorf("left %v, want only prog", got)
	}
}

func TestInfoReportsANewerRelease(t *testing.T) {
	srv := serve(t, release(t, "v1.4.0"))
	got := newUpdater(t, srv, "v1.3.0").Info(context.Background())
	want := Info{Checked: true, Available: true, Current: "v1.3.0", Latest: "v1.4.0", URL: srv.URL + "/releases/v1.4.0"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestInfoSaysCurrentForTheSameReleaseOrAPreRelease(t *testing.T) {
	flagged := release(t, "v2.0.0")
	flagged.prerelease = true
	for _, rel := range []fakeRelease{release(t, "v1.3.0"), flagged, release(t, "v2.0.0-rc1")} {
		got := newUpdater(t, serve(t, rel), "v1.3.0").Info(context.Background())
		if !got.Checked || got.Available {
			t.Errorf("%s: got %+v, want checked and current", rel.tag, got)
		}
	}
}

func TestInfoIsUncheckedWhenGitHubCannotBeAsked(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	got := newUpdater(t, srv, "v1.3.0").Info(context.Background())
	if got.Checked {
		t.Errorf("got %+v after a 404", got)
	}
}

// A dev build is every untagged build, and asking GitHub about it would only
// ever answer "current".
func TestCheckSaysCurrentForADevBuildWithoutAsking(t *testing.T) {
	info := Check(context.Background(), "dev")
	if !info.Checked || info.Available {
		t.Fatalf("got %+v, want checked and current", info)
	}
}

func TestParseVersion(t *testing.T) {
	for s, want := range map[string]bool{
		"1.3.0": true, "v1.3.0": true, "10.20.30": true,
		"": false, "dev": false, "1.3": false, "1.3.0-rc1": false, "1.3.0.1": false, "v1.x.0": false,
	} {
		if _, ok := parseVersion(s); ok != want {
			t.Errorf("parseVersion(%q) ok = %v, want %v", s, ok, want)
		}
	}
	a, _ := parseVersion("1.10.0")
	b, _ := parseVersion("1.9.9")
	if compare(a, b) <= 0 {
		t.Error("1.10.0 is not newer than 1.9.9")
	}
}

func TestNewerNeedsTwoReleaseVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.3.0", "v1.2.9", true},
		{"1.2.0", "v1.2.0", false},
		{"1.1.0", "1.2.0", false},
		{"1.3.0", "dev", false},
		{"", "1.2.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestLookupSumReadsSha256sumOutput(t *testing.T) {
	sums := []byte("ABCDEF  one.exe\n123456 *two.zip\n")
	if got, ok := lookupSum(sums, "one.exe"); !ok || got != "abcdef" {
		t.Errorf("one.exe: %q, %v", got, ok)
	}
	if got, ok := lookupSum(sums, "two.zip"); !ok || got != "123456" {
		t.Errorf("two.zip: %q, %v", got, ok)
	}
	if _, ok := lookupSum(sums, "one"); ok {
		t.Error("matched a name only by its prefix")
	}
}

// An asset desktop.yml does not build is an update that never downloads, and a
// bundle it builds without an entry here is one no installed copy updates to.
func TestAssetsMatchTheDesktopWorkflow(t *testing.T) {
	yml, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "desktop.yml"))
	if err != nil {
		t.Fatal(err)
	}
	built := regexp.MustCompile(`(?m)^\s+platform: (\S+)\n\s+slug: (\S+)$`).FindAllStringSubmatch(string(yml), -1)
	if len(built) == 0 {
		t.Fatal("desktop.yml: no platform line followed by a slug line in the matrix")
	}
	published := map[string]bool{}
	for _, m := range built {
		platform, name := m[1], "knightloader-"+m[2]+".zip"
		published[name] = true
		targets := []string{platform}
		if platform == "darwin/universal" {
			targets = []string{"darwin/amd64", "darwin/arm64"}
		}
		for _, target := range targets {
			if got := Assets[target]; got != name {
				t.Errorf("desktop.yml builds %s into %s, but Assets[%q] = %q", platform, name, target, got)
			}
		}
	}
	for target, name := range Assets {
		if !published[name] {
			t.Errorf("Assets maps %s to %s, which desktop.yml does not build", target, name)
		}
	}
}

// The updater asks for the zip without a version in its name, so the release
// has to publish it under that name and list it in checksums.txt.
func TestTheReleasePublishesTheUnversionedZipsWithTheirChecksums(t *testing.T) {
	yml, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"dist/knightloader-${slug}.zip"`,
		"sha256sum *.zip",
		"dist/" + ChecksumsFile,
	} {
		if !strings.Contains(string(yml), want) {
			t.Errorf("release.yml does not contain %s", want)
		}
	}
}
