package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// foldersServer is the chooser's route on a throwaway app.
func foldersServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerFolders(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// foldersURL escapes the path the same way the browser will, so a Windows
// backslash cannot make the test disagree with the server about what was asked.
func foldersURL(srv *httptest.Server, path string) string {
	return srv.URL + "/api/folders?path=" + url.QueryEscape(path)
}

// getFolders asks for one listing and decodes it, failing the test on anything
// but a 200.
func getFolders(t *testing.T, srv *httptest.Server, path string) folderListing {
	t.Helper()
	code, raw := getRaw(t, foldersURL(srv, path))
	if code != http.StatusOK {
		t.Fatalf("GET /api/folders?path=%s answered %d: %s", path, code, raw)
	}
	var got folderListing
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestTheSplitMatchesTheFolderThatGetsCreated checks splitTemplate against the
// directory settings.Validate actually creates for the same template, so it
// fails when the two splits come apart.
func TestTheSplitMatchesTheFolderThatGetsCreated(t *testing.T) {
	base := t.TempDir()
	tpl := filepath.Join(base, "downloads", "<jd:date>", "<jd:hoster>")
	if err := settings.Validate("the download folder", tpl); err != nil {
		t.Fatal(err)
	}

	fixed, tail := splitTemplate(tpl)
	want := filepath.Join(base, "downloads")
	if fixed != want {
		t.Errorf("splitTemplate kept %q as the real path, want %q", fixed, want)
	}
	if fi, err := os.Stat(fixed); err != nil || !fi.IsDir() {
		t.Errorf("the folder %q the chooser would browse is not the one that was created", fixed)
	}
	if _, err := os.Stat(filepath.Join(want, "<jd:date>")); err == nil {
		t.Error("a folder literally named <jd:date> exists, so the two splits no longer agree")
	}
	// The interface re-assembles the value by concatenation.
	if fixed+tail != tpl {
		t.Errorf("%q + %q is not %q", fixed, tail, tpl)
	}
}

// TestBrowsingReportsTheTemplateTail checks the same rule over the wire, since
// the interface can only keep a naming scheme it is told about.
func TestBrowsingReportsTheTemplateTail(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, srv := foldersServer(t)

	sep := string(filepath.Separator)
	got := getFolders(t, srv, filepath.Join(base, "downloads")+sep+"<jd:date>"+sep+"<jd:hoster>")
	if got.Path != filepath.Join(base, "downloads") {
		t.Errorf("browsed %q, want the fixed part %q", got.Path, filepath.Join(base, "downloads"))
	}
	if got.Tail != sep+"<jd:date>"+sep+"<jd:hoster>" {
		t.Errorf("the tail came back as %q, so the naming scheme is lost on save", got.Tail)
	}
	if !got.Exists {
		t.Error("the fixed part exists but came back as new")
	}
}

// TestAFolderThatDoesNotExistYetSaysSo checks that a typed folder that does not
// exist yet is reported as new, with the deepest existing folder listed.
func TestAFolderThatDoesNotExistYetSaysSo(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "already-here"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, srv := foldersServer(t)

	asked := filepath.Join(base, "not-yet", "deeper")
	got := getFolders(t, srv, asked)
	if got.Exists {
		t.Errorf("%q does not exist but came back as an existing folder", asked)
	}
	if got.Path != asked {
		t.Errorf("the asked-for path came back as %q, want %q", got.Path, asked)
	}
	if got.Listed != base {
		t.Errorf("listed %q, want the deepest folder that is really there (%q)", got.Listed, base)
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "already-here" {
		t.Errorf("the existing folder above it was not offered: %+v", got.Entries)
	}
}

// TestOnlyFoldersAreListed checks that files are neither opened nor named.
func TestOnlyFoldersAreListed(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, srv := foldersServer(t)

	got := getFolders(t, srv, base)
	for _, e := range got.Entries {
		if e.Name == "secret.txt" {
			t.Fatal("a file was offered as a folder")
		}
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "keep" {
		t.Errorf("entries are %+v, want only the one folder", got.Entries)
	}
	if got.Entries[0].Path != filepath.Join(base, "keep") {
		t.Errorf("the entry's path is %q, which is not what the next request should ask for", got.Entries[0].Path)
	}
}

// TestPathsOutsideTheRootsAreRefused checks that KL_BROWSE_ROOTS refuses a
// path asked for directly, not only the paths the interface offers.
func TestPathsOutsideTheRootsAreRefused(t *testing.T) {
	allowed, forbidden := t.TempDir(), t.TempDir()
	t.Setenv(envBrowseRoots, allowed)
	_, srv := foldersServer(t)

	code, raw := getRaw(t, foldersURL(srv, forbidden))
	if code != http.StatusForbidden {
		t.Fatalf("listing %q outside the roots answered %d: %s", forbidden, code, raw)
	}
	if strings.TrimSpace(string(raw)) == "" {
		t.Error("the refusal says nothing, so the dialog has nothing to show")
	}
	// The allowed root still works, or the check above would pass on a route
	// that refuses everything.
	if got := getFolders(t, srv, allowed); got.Listed != allowed {
		t.Errorf("the allowed root listed %q", got.Listed)
	}
}

// TestASymlinkOutOfTheBoundaryIsNotOffered checks that the boundary resolves
// symlinks rather than comparing prefixes.
func TestASymlinkOutOfTheBoundaryIsNotOffered(t *testing.T) {
	allowed, forbidden := t.TempDir(), t.TempDir()
	if err := os.Symlink(forbidden, filepath.Join(allowed, "escape")); err != nil {
		// Windows needs a privilege to create symlinks.
		t.Skipf("symlinks are not available here: %v", err)
	}
	if err := os.Mkdir(filepath.Join(allowed, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envBrowseRoots, allowed)
	_, srv := foldersServer(t)

	got := getFolders(t, srv, allowed)
	for _, e := range got.Entries {
		if e.Name == "escape" {
			t.Error("a link pointing out of the boundary was offered as a folder to browse into")
		}
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "inside" {
		t.Errorf("entries are %+v, want only the folder that is really inside", got.Entries)
	}
	// Asking for the link directly is refused too.
	code, _ := getRaw(t, foldersURL(srv, filepath.Join(allowed, "escape")))
	if code != http.StatusForbidden {
		t.Errorf("asking for the link directly answered %d, want 403", code)
	}
}

// TestASymlinkedFolderInsideTheBoundaryIsOffered checks that resolving links
// does not hide the ones that stay inside the boundary.
func TestASymlinkedFolderInsideTheBoundaryIsOffered(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	here := filepath.Join(base, "here")
	if err := os.Mkdir(here, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(here, "linked")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	t.Setenv(envBrowseRoots, base)
	_, srv := foldersServer(t)

	got := getFolders(t, srv, here)
	if len(got.Entries) != 1 || got.Entries[0].Name != "linked" {
		t.Fatalf("entries are %+v, want the linked folder", got.Entries)
	}
	// The offered path keeps the link, not its target.
	if got.Entries[0].Path != filepath.Join(here, "linked") {
		t.Errorf("the entry's path is %q, want %q", got.Entries[0].Path, filepath.Join(here, "linked"))
	}
}

// TestARootsListThatNamesNothingIsRefusedLoudly checks that a typo in
// KL_BROWSE_ROOTS fails instead of widening the boundary to everything.
func TestARootsListThatNamesNothingIsRefusedLoudly(t *testing.T) {
	t.Setenv(envBrowseRoots, "relative/path")
	_, srv := foldersServer(t)

	code, raw := getRaw(t, foldersURL(srv, t.TempDir()))
	if code != http.StatusInternalServerError {
		t.Fatalf("a roots list with nothing usable in it answered %d: %s", code, raw)
	}
	if !strings.Contains(string(raw), envBrowseRoots) {
		t.Errorf("the refusal %q never names the variable that caused it", raw)
	}
}

// TestARelativePathIsRefused keeps the route agreeing with settings.Validate.
func TestARelativePathIsRefused(t *testing.T) {
	_, srv := foldersServer(t)
	code, raw := getRaw(t, srv.URL+"/api/folders?path=downloads")
	if code != http.StatusBadRequest {
		t.Fatalf("a relative path answered %d: %s", code, raw)
	}
}

func TestTheChooserOpensWhereDownloadsGo(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, srv := foldersServer(t)
	s := a.Settings.Get()
	s.DownloadDir = base
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	code, raw := getRaw(t, srv.URL+"/api/folders")
	if code != http.StatusOK {
		t.Fatalf("GET /api/folders with no path answered %d: %s", code, raw)
	}
	var got folderListing
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != base {
		t.Errorf("the chooser opened at %q, want the configured download folder %q", got.Path, base)
	}
}

func TestTheParentIsOnlyOfferedInsideTheBoundary(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envBrowseRoots, base)
	_, srv := foldersServer(t)

	if got := getFolders(t, srv, sub); got.Parent != base {
		t.Errorf("the parent of %q came back as %q, want %q", sub, got.Parent, base)
	}
	if got := getFolders(t, srv, base); got.Parent != "" {
		t.Errorf("the root offered %q as a way up, which is outside the boundary", got.Parent)
	}
}

func TestEntriesAreNeverNull(t *testing.T) {
	_, srv := foldersServer(t)
	empty := t.TempDir()
	code, raw := getRaw(t, foldersURL(srv, empty))
	if code != http.StatusOK {
		t.Fatalf("GET on an empty folder answered %d: %s", code, raw)
	}
	if !strings.Contains(string(raw), `"entries":[]`) {
		t.Errorf("an empty folder came back as %s", raw)
	}
}

// TestTheDefaultBoundaryIsTheWholeFilesystem pins the default boundary, so
// narrowing it takes an edit here.
func TestTheDefaultBoundaryIsTheWholeFilesystem(t *testing.T) {
	roots, err := browseRoots(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		want = filepath.VolumeName(t.TempDir()) + want
	}
	if len(roots) != 1 || roots[0] != want {
		t.Errorf("the default boundary is %v, want just %q", roots, want)
	}
}
