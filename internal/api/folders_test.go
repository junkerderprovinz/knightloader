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
// but a 200 - the status is asserted separately where it is the point.
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

// TestTheSplitMatchesTheFolderThatGetsCreated is the assertion the whole feature
// rests on, and it is deliberately not a table of strings: it checks the split
// against the directory internal/settings actually creates for the same
// template. splitTemplate is a twin of that package's unexported fixedPrefix, so
// the only test worth having is one that fails when the twins come apart - a
// chooser that splits one segment deeper offers a folder the app will never
// create, and one that splits earlier writes a fixed segment out of the path.
func TestTheSplitMatchesTheFolderThatGetsCreated(t *testing.T) {
	base := t.TempDir()
	tpl := filepath.Join(base, "downloads", "<jd:date>", "<jd:hoster>")
	if err := settings.Validate(tpl); err != nil {
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
	// Concatenation is how the interface puts the value back together, so the
	// two halves have to be exactly the template that came in.
	if fixed+tail != tpl {
		t.Errorf("%q + %q is not %q", fixed, tail, tpl)
	}
}

// TestBrowsingReportsTheTemplateTail is the same rule seen through the wire. The
// interface cannot preserve a naming scheme it was never told about, and a
// response that quietly answered with only the fixed part would have every
// caller writing the plain folder back into the setting.
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

// TestAFolderThatDoesNotExistYetSaysSo covers the case a chooser normally gets
// wrong: somebody types the folder they are about to use. Answering with an
// empty dialog and no explanation reads as a broken picker, so the route walks
// up to what is really there and says the rest is new.
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

// TestOnlyFoldersAreListed is the route's other half of "never file contents":
// it does not open a file, and it does not even say one is there. A picker that
// lists file names is a directory listing of the host with extra steps.
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

// TestPathsOutsideTheRootsAreRefused checks the narrowing knob does something.
// An operator who sets it has decided the chooser may not see the rest of the
// machine, and a boundary that only stops the paths the interface happens to
// offer is not a boundary.
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
	// The allowed root still works, or the test above proves only that
	// everything is refused.
	if got := getFolders(t, srv, allowed); got.Listed != allowed {
		t.Errorf("the allowed root listed %q", got.Listed)
	}
}

// TestASymlinkOutOfTheBoundaryIsNotOffered is why the boundary check resolves
// instead of comparing prefixes. One `ln -s` inside an allowed folder would
// otherwise hand out the whole disk through a path that passes any prefix test.
func TestASymlinkOutOfTheBoundaryIsNotOffered(t *testing.T) {
	allowed, forbidden := t.TempDir(), t.TempDir()
	if err := os.Symlink(forbidden, filepath.Join(allowed, "escape")); err != nil {
		// Windows needs a privilege for this; the rule is the same either way and
		// the platform that ships is the one that can make the link.
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
	// Following it by hand is refused too, so the filtering above is a courtesy
	// and not the protection.
	code, _ := getRaw(t, foldersURL(srv, filepath.Join(allowed, "escape")))
	if code != http.StatusForbidden {
		t.Errorf("asking for the link directly answered %d, want 403", code)
	}
}

// TestASymlinkedFolderInsideTheBoundaryIsOffered is the other half: resolving
// must not mean hiding. A machine whose /downloads is a link to the array would
// otherwise show a chooser with nothing in it.
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
	// The path offered is the one the user typed their way into, not the link's
	// target: browsing must never rewrite somebody's folder to wherever a link
	// happened to point.
	if got.Entries[0].Path != filepath.Join(here, "linked") {
		t.Errorf("the entry's path is %q, want %q", got.Entries[0].Path, filepath.Join(here, "linked"))
	}
}

// TestARootsListThatNamesNothingIsRefusedLoudly pins the direction a mistake in
// the narrowing knob has to fail in. A typo that fell back to the whole
// filesystem would widen exactly the boundary somebody set it to narrow, and
// nothing on screen would ever mention it.
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

// TestARelativePathIsRefused keeps the route agreeing with settings.Validate:
// a relative folder is resolved against the process's working directory, which
// is not something a user can reason about.
func TestARelativePathIsRefused(t *testing.T) {
	_, srv := foldersServer(t)
	code, raw := getRaw(t, srv.URL+"/api/folders?path=downloads")
	if code != http.StatusBadRequest {
		t.Fatalf("a relative path answered %d: %s", code, raw)
	}
}

// TestTheChooserOpensWhereDownloadsGo is why the route reads the settings at
// all. Opened on anything else, the first thing every user does is navigate away
// from it.
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

// TestTheParentIsOnlyOfferedInsideTheBoundary stops the dialog from showing a
// way up that answers 403 when it is pressed.
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

// TestEntriesAreNeverNull keeps an empty folder from arriving as JSON null,
// which a list renderer has to guard for separately every single time.
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

// TestTheDefaultBoundaryIsTheWholeFilesystem states the boundary out loud, so
// narrowing it later is a deliberate edit to a test that says what changed. With
// nothing set, anything absolute this process can read may be listed - see the
// file header for why that is the right default for a download manager.
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
