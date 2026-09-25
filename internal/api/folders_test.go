package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/user"
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

// TestTheSplitMatchesTheFolderThatGetsCreated checks splitTemplate against
// settings.FixedPrefix, the folder a save checks and a download creates for the
// same template, so it fails when the two splits come apart.
func TestTheSplitMatchesTheFolderThatGetsCreated(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	tpl := filepath.Join(base, "downloads", "<jd:date>", "<jd:hoster>")

	fixed, tail := splitTemplate(tpl)
	want := filepath.Join(base, "downloads")
	if fixed != want {
		t.Errorf("splitTemplate kept %q as the real path, want %q", fixed, want)
	}
	if created := settings.FixedPrefix(tpl); fixed != created {
		t.Errorf("the chooser would browse %q, but %q is the folder that gets created", fixed, created)
	}
	// The interface re-assembles the value by concatenation.
	if fixed+tail != tpl {
		t.Errorf("%q + %q is not %q", fixed, tail, tpl)
	}
}

// A template that starts at a drive root browses that root. "D:" alone would be
// the drive's current directory, which the chooser refuses as relative.
func TestATemplateAtADriveRootBrowsesTheRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters are a Windows path form")
	}
	t.Parallel()
	root := filepath.VolumeName(t.TempDir()) + `\`
	tpl := root + "<jd:packagename>"

	fixed, tail := splitTemplate(tpl)
	if fixed != root || tail != `\<jd:packagename>` {
		t.Errorf("splitTemplate(%q) = %q, %q, want %q, %q", tpl, fixed, tail, root, `\<jd:packagename>`)
	}
	if created := settings.FixedPrefix(tpl); fixed != created {
		t.Errorf("the chooser would browse %q, but %q is the folder that gets created", fixed, created)
	}

	_, srv := foldersServer(t)
	got := getFolders(t, srv, tpl)
	if got.Path != root || !got.Exists {
		t.Errorf("browsed %q (exists %v), want the existing root %q", got.Path, got.Exists, root)
	}
}

// TestBrowsingReportsTheTemplateTail checks the same rule over the wire, since
// the interface can only keep a naming scheme it is told about.
func TestBrowsingReportsTheTemplateTail(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

	code, out := listingRefusal(t, foldersURL(srv, forbidden))
	if code != http.StatusForbidden || out["code"] != "outside" {
		t.Fatalf("listing %q outside the roots answered %d %v, want 403 outside", forbidden, code, out)
	}
	if strings.TrimSpace(out["error"]) == "" {
		t.Error("the refusal says nothing, so a client without the code has nothing to show")
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

	code, out := listingRefusal(t, foldersURL(srv, t.TempDir()))
	if code != http.StatusInternalServerError || out["code"] != "roots" {
		t.Fatalf("a roots list with nothing usable in it answered %d %v, want 500 roots", code, out)
	}
	if !strings.Contains(out["error"], envBrowseRoots) {
		t.Errorf("the refusal %q never names the variable that caused it", out["error"])
	}
}

// TestARelativePathIsRefused keeps the route agreeing with settings.Validate.
func TestARelativePathIsRefused(t *testing.T) {
	t.Parallel()
	_, srv := foldersServer(t)
	code, out := listingRefusal(t, srv.URL+"/api/folders?path=downloads")
	if code != http.StatusBadRequest || out["code"] != "relative" {
		t.Fatalf("a relative path answered %d %v, want 400 relative", code, out)
	}
}

// On Windows such a folder can still be stat-ed but not opened, so the refusal
// comes from resolving it, and it must not claim the folder is outside roots
// that nobody set.
func TestAFolderThatMayNotBeReadSaysSo(t *testing.T) {
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockFolder(t, locked)
	_, srv := foldersServer(t)

	code, out := listingRefusal(t, foldersURL(srv, locked))
	if code != http.StatusForbidden || out["code"] != "unreadable" {
		t.Errorf("listing a folder nobody may read answered %d %v, want 403 unreadable", code, out)
	}
	if !strings.Contains(out["error"], locked) {
		t.Errorf("the refusal %q does not name the folder", out["error"])
	}
}

func TestCreatingInAFolderThatMayNotBeOpenedIsDenied(t *testing.T) {
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockFolder(t, locked)
	_, srv := foldersServer(t)

	if code, out := postFolder(t, srv, locked, "new"); code != http.StatusForbidden || out["code"] != "denied" {
		t.Errorf("creating in a folder nobody may open answered %d %v, want 403 denied", code, out)
	}
}

// lockFolder takes every right on dir away from this process's user. The
// folder still shows in its parent's listing, as a folder with such rights
// does to a user who clicks it in the chooser.
func lockFolder(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		if os.Geteuid() == 0 {
			t.Skip("root reads folders whatever the mode says")
		}
		if err := os.Chmod(dir, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		return
	}
	u, err := user.Current()
	if err != nil {
		t.Skipf("no current user to take the rights from: %v", err)
	}
	if out, err := exec.Command("icacls", dir, "/deny", u.Username+":(F)").CombinedOutput(); err != nil {
		t.Skipf("icacls cannot deny access here: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", dir, "/remove:d", u.Username).Run() })
}

// listingRefusal asks for a listing that is expected to be refused and
// decodes the refusal, which is JSON like a refused new folder.
func listingRefusal(t *testing.T, url string) (int, map[string]string) {
	t.Helper()
	code, raw := getRaw(t, url)
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("GET %s answered %d with %q, which is not JSON", url, code, raw)
	}
	return code, out
}

func TestTheChooserOpensWhereDownloadsGo(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	b, err := browseRoots(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := b.roots
	want := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		want = filepath.VolumeName(t.TempDir()) + want
	}
	if len(roots) != 1 || roots[0] != want {
		t.Errorf("the default boundary is %v, want just %q", roots, want)
	}
}

// postFolder asks for a new folder and decodes the answer, which is JSON
// whether the folder was made or not.
func postFolder(t *testing.T, srv *httptest.Server, parent, name string) (int, map[string]string) {
	t.Helper()
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/folders", map[string]string{"parent": parent, "name": name})
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("POST /api/folders answered %d with %q, which is not JSON", code, raw)
	}
	return code, out
}

func TestANewFolderIsCreatedAndOfferedByTheChooser(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	_, srv := foldersServer(t)

	code, out := postFolder(t, srv, base, "Filme 2026")
	if code != http.StatusCreated {
		t.Fatalf("creating a folder answered %d: %v", code, out)
	}
	want := filepath.Join(base, "Filme 2026")
	if out["path"] != want {
		t.Errorf("the new folder came back as %q, want %q", out["path"], want)
	}
	if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
		t.Fatalf("%q was reported as created but is not a folder: %v", want, err)
	}
	got := getFolders(t, srv, base)
	if len(got.Entries) != 1 || got.Entries[0].Path != want {
		t.Errorf("the chooser does not offer the new folder: %+v", got.Entries)
	}
}

func TestCreatingAFolderThatExistsIsAConflict(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, srv := foldersServer(t)

	for _, name := range []string{"taken", "notes.txt"} {
		code, out := postFolder(t, srv, base, name)
		if code != http.StatusConflict || out["code"] != "exists" {
			t.Errorf("creating %q where something of that name exists answered %d %v, want 409 exists", name, code, out)
		}
	}
}

func TestAFolderNameMustBeOnePlainName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := filepath.Join(root, "here")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	_, srv := foldersServer(t)

	cases := map[string]string{
		"":                       "empty",
		"   ":                    "empty",
		".":                      "dots",
		"..":                     "dots",
		"../escape":              "separator",
		`..\escape`:              "separator",
		"a/b":                    "separator",
		elsewhere:                "separator",
		"<jd:date>":              "character",
		"a:b":                    "character",
		"what?":                  "character",
		"tab\there":              "character",
		"ends in a dot.":         "trailing",
		"ends in a space ":       "trailing",
		"CON":                    "reserved",
		"nul.old":                "reserved",
		"Lpt1":                   "reserved",
		strings.Repeat("a", 256): "tooLong",
	}
	for name, want := range cases {
		code, out := postFolder(t, srv, base, name)
		if code != http.StatusBadRequest || out["code"] != want {
			t.Errorf("the name %q answered %d %v, want 400 %s", name, code, out, want)
		}
	}

	if items, err := os.ReadDir(base); err != nil || len(items) != 0 {
		t.Errorf("refused names still left something behind: %v %v", items, err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
		t.Error("a name with .. in it created a folder above the one asked for")
	}
	if _, err := os.Stat(elsewhere); err == nil {
		t.Error("an absolute name created a folder outside the one asked for")
	}
}

func TestCreatingOutsideTheRootsIsRefused(t *testing.T) {
	allowed, forbidden := t.TempDir(), t.TempDir()
	t.Setenv(envBrowseRoots, allowed)
	_, srv := foldersServer(t)

	code, out := postFolder(t, srv, forbidden, "new")
	if code != http.StatusForbidden || out["code"] != "outside" {
		t.Errorf("creating in %q outside the roots answered %d %v, want 403 outside", forbidden, code, out)
	}
	// The two temporary folders are siblings, so this climbs out of allowed
	// into forbidden unless the path is cleaned before the check.
	sep := string(filepath.Separator)
	climb := allowed + sep + ".." + sep + filepath.Base(forbidden)
	if code, out := postFolder(t, srv, climb, "new"); code != http.StatusForbidden {
		t.Errorf("creating in %q answered %d %v, want 403", climb, code, out)
	}
	if _, err := os.Stat(filepath.Join(forbidden, "new")); err == nil {
		t.Fatal("a folder was created outside the roots")
	}
	// The allowed root still works, or the checks above would pass on a route
	// that refuses everything.
	if code, out := postFolder(t, srv, allowed, "new"); code != http.StatusCreated {
		t.Errorf("creating inside the allowed root answered %d %v", code, out)
	}
}

func TestCreatingThroughASymlinkOutOfTheBoundaryIsRefused(t *testing.T) {
	allowed, forbidden := t.TempDir(), t.TempDir()
	if err := os.Symlink(forbidden, filepath.Join(allowed, "escape")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	t.Setenv(envBrowseRoots, allowed)
	_, srv := foldersServer(t)

	code, out := postFolder(t, srv, filepath.Join(allowed, "escape"), "new")
	if code != http.StatusForbidden || out["code"] != "outside" {
		t.Errorf("creating through a link out of the boundary answered %d %v, want 403 outside", code, out)
	}
	if _, err := os.Stat(filepath.Join(forbidden, "new")); err == nil {
		t.Fatal("a folder was created at the far end of the link")
	}
	// The link's own name is taken, and naming it does not follow it.
	if code, out := postFolder(t, srv, allowed, "escape"); code != http.StatusConflict {
		t.Errorf("creating a folder named like the link answered %d %v, want 409", code, out)
	}
}

func TestCreatingInAParentThatIsNotThereIsRefused(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	_, srv := foldersServer(t)

	if code, out := postFolder(t, srv, filepath.Join(base, "gone"), "new"); code != http.StatusNotFound || out["code"] != "missing" {
		t.Errorf("creating in a missing folder answered %d %v, want 404 missing", code, out)
	}
	if code, out := postFolder(t, srv, "downloads", "new"); code != http.StatusBadRequest || out["code"] != "relative" {
		t.Errorf("creating in a relative folder answered %d %v, want 400 relative", code, out)
	}
	if _, err := os.Stat(filepath.Join(base, "gone")); err == nil {
		t.Error("the missing parent was created along the way")
	}
}

func TestAFolderThatMayNotBeWrittenToSaysSo(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a read-only mode does not stop creating folders on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root creates folders whatever the mode says")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	_, srv := foldersServer(t)

	code, out := postFolder(t, srv, locked, "new")
	if code != http.StatusForbidden || out["code"] != "denied" {
		t.Errorf("creating in a read-only folder answered %d %v, want 403 denied", code, out)
	}
	if !strings.Contains(out["error"], locked) {
		t.Errorf("the refusal %q does not name the folder", out["error"])
	}
}

func TestCreatingAFolderNeedsASessionAndStaysOnThisMachine(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	registerFolders(reg, testApp(t))
	if reg.open("/api/folders") {
		t.Error("the folder routes answer without a session")
	}
	if relayForwardable(http.MethodPost, "/api/folders") {
		t.Error("POST /api/folders is forwardable to a peer, where it would create folders on the peer's disk")
	}
}
