package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/privdrop"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fileOwnerServer is the two routes on a throwaway app whose download folder is
// a real, writable directory.
func fileOwnerServer(t *testing.T) (*app.App, *httptest.Server, string) {
	t.Helper()
	a := testApp(t)
	dl := t.TempDir()
	s := settings.Defaults()
	s.DownloadDir = dl
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	reg := newRegistry()
	registerFileOwner(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv, dl
}

// TestTheIdentityReadoutSaysWhatIsInForceAndWhatWasAskedForSideBySide checks
// that the variables the operator set travel beside the effective identity,
// so a PUID that did nothing is visible.
func TestTheIdentityReadoutSaysWhatIsInForceAndWhatWasAskedForSideBySide(t *testing.T) {
	t.Setenv("PUID", "99")
	t.Setenv("PGID", "100")
	_, srv, _ := fileOwnerServer(t)

	code, raw := getRaw(t, srv.URL+"/api/fileowner")
	if code != http.StatusOK {
		t.Fatalf("GET /api/fileowner answered %d: %s", code, raw)
	}
	var got OwnerIdentity
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Env.PUID != "99" || got.Env.PGID != "100" {
		t.Errorf("the variables the operator set did not reach the readout: %+v", got.Env)
	}
	if got.IDsRead {
		t.Error("the readout claims PUID/PGID were applied, but this test process never started as root and switched; " +
			"saying otherwise tells somebody a variable worked that was never read")
	}
	if got.Deployment == "" {
		t.Error("no deployment kind, so the page cannot tell a container from the desktop app and has to guess which sentence to show")
	}
}

// TestAnUnsetVariableIsEmptyAndNeverZero checks that an unset PUID is not
// reported as 0, which would read as a request to run as root.
func TestAnUnsetVariableIsEmptyAndNeverZero(t *testing.T) {
	t.Setenv("PUID", "")
	t.Setenv("UMASK", "")
	if err := os.Unsetenv("PUID"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("UMASK"); err != nil {
		t.Fatal(err)
	}
	id := ownerIdentity()
	if id.Env.PUID != "" || id.Env.Umask != "" {
		t.Errorf("an unset variable came back as %q/%q rather than empty", id.Env.PUID, id.Env.Umask)
	}
}

// TestTheCheckAnswersAListAndLeavesTheFolderExactlyAsItFoundIt checks that no
// probe file is left in a folder that media scanners watch.
func TestTheCheckAnswersAListAndLeavesTheFolderExactlyAsItFoundIt(t *testing.T) {
	t.Parallel()
	_, srv, dl := fileOwnerServer(t)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fileowner/check", nil)
	if code != http.StatusOK {
		t.Fatalf("POST /api/fileowner/check answered %d: %s", code, raw)
	}
	var rep FolderOwnerReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Folders == nil {
		t.Fatalf("the folders came back as null rather than as a list: %s", raw)
	}
	var found bool
	for _, f := range rep.Folders {
		if f.Dir != filepath.Clean(dl) {
			continue
		}
		found = true
		if f.Verdict == "missing" || f.Verdict == "notWritable" {
			t.Errorf("a fresh writable folder was judged %q (%s); the probe is broken, not the folder", f.Verdict, f.Detail)
		}
		if f.Role != "downloads" {
			t.Errorf("the download folder came back with role %q", f.Role)
		}
	}
	if !found {
		t.Errorf("no row for the download folder; the report named %d folders", len(rep.Folders))
	}
	if rep.CheckedAt.IsZero() {
		t.Error("the report carries no timestamp, so the page cannot say when it last ran")
	}

	left, err := os.ReadDir(dl)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		var names []string
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Errorf("the check left %v behind in the download folder", names)
	}
}

// TestTheCheckRefusesAFolderThisInstanceDoesNotWriteInto checks that a foreign
// folder is refused with a 400 and left untouched, since the check writes.
func TestTheCheckRefusesAFolderThisInstanceDoesNotWriteInto(t *testing.T) {
	t.Parallel()
	_, srv, _ := fileOwnerServer(t)
	stranger := t.TempDir()

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fileowner/check", map[string]any{"dirs": []string{stranger}})
	if code != http.StatusBadRequest {
		t.Fatalf("a folder outside this instance's own list answered %d, want 400: %s", code, raw)
	}
	if !strings.Contains(string(raw), stranger) {
		t.Errorf("the refusal does not name the folder it refused: %s", raw)
	}
	if entries, err := os.ReadDir(stranger); err != nil || len(entries) != 0 {
		t.Errorf("the refused folder was written into anyway (%v, %v)", entries, err)
	}
}

func TestNamingOneFolderChecksOnlyThatFolder(t *testing.T) {
	t.Parallel()
	a, srv, dl := fileOwnerServer(t)
	s := a.Settings.Get()
	s.WorkDir = t.TempDir()
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fileowner/check", map[string]any{"dirs": []string{dl}})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var rep FolderOwnerReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Folders) != 1 || rep.Folders[0].Dir != filepath.Clean(dl) {
		t.Errorf("asking about one folder measured %d of them: %+v", len(rep.Folders), rep.Folders)
	}
}

// TestTheBundleCarriesTheOwnersAndNotAnybodysHomeDirectory checks that the
// ownership rows in the diagnostics bundle carry a role and no path, since a
// desktop download folder lies inside the user's home directory.
func TestTheBundleCarriesTheOwnersAndNotAnybodysHomeDirectory(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	id, folders := ownershipDiagnostics(a)
	if len(folders) == 0 {
		t.Fatal("no folder at all in the bundle, though every instance has a download folder")
	}
	raw, err := json.Marshal(folders)
	if err != nil {
		t.Fatal(err)
	}
	// Decoded rather than searched for the data directory: on Windows the
	// escaped backslashes would never match and the check would pass
	// vacuously.
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for key, v := range row {
			if key == "dir" || key == "path" || key == "detail" {
				t.Errorf("the bundle carries %q (%v); this file is attached to public bug reports and a folder path in it is somebody's home directory", key, v)
			}
			if s, ok := v.(string); ok && (strings.ContainsRune(s, '/') || strings.ContainsRune(s, '\\')) {
				t.Errorf("the bundle's %q field looks like a path: %q", key, s)
			}
		}
	}
	if !strings.Contains(string(raw), `"role":"downloads"`) {
		t.Errorf("the bundle names no download folder at all:\n%s", raw)
	}
	if id.Deployment == "" {
		t.Error("the bundle's ownership block does not say whether this is the container or the desktop build")
	}
}

// TestTheOwnershipRoutesAreNotForwardedToAPeer guards against a peer's uids
// and folders being shown under this machine's name.
func TestTheOwnershipRoutesAreNotForwardedToAPeer(t *testing.T) {
	t.Parallel()
	if groupReaches(t, http.MethodGet, "/api/fileowner") {
		t.Error("GET /api/fileowner is forwardable to a peer; one machine's uid answered under another machine's name is a wrong number nobody can spot")
	}
	if groupReaches(t, http.MethodPost, "/api/fileowner/check") {
		t.Error("POST /api/fileowner/check is forwardable to a peer; it writes, and it would be writing into the peer's folders")
	}
}

func TestTheOwnershipRoutesNeedASession(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	registerFileOwner(reg, testApp(t))
	for _, path := range []string{"/api/fileowner", "/api/fileowner/check"} {
		if reg.open(path) {
			t.Errorf("%s answers without a session; only the routes the login flow itself depends on may", path)
		}
	}
}

// TestTheImageStartsAsRootSoPUIDCanTakeEffect holds the Dockerfile to what
// internal/privdrop and the manual promise: PUID and PGID are read only by a
// process that starts as root, so a USER line in the image would turn them off
// for everyone, and the account it creates has to be the default the switch
// falls back to.
func TestTheImageStartsAsRootSoPUIDCanTakeEffect(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("the image's Dockerfile could not be read (%v); this guard is about what that file declares, so it cannot be skipped quietly", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "USER ") {
			t.Errorf("the Dockerfile declares %q, so the server never starts as root, PUID and PGID are never read, "+
				"and a folder Docker creates stays root's and cannot be written", strings.TrimSpace(line))
		}
	}
	want := fmt.Sprintf("adduser -D -u %d knight", privdrop.DefaultUID)
	if !strings.Contains(string(b), want) {
		t.Errorf("the Dockerfile does not create the account the switch falls back to (%q), so an unset PUID would run as an id with no name and no home", want)
	}
}

// TestAnUnappliedUmaskIsNotReportedAsApplied checks that UMASK set in the
// environment is not taken for applied: only startup applies it, and a
// readout that echoed the variable as in force would hide a value that was
// refused.
func TestAnUnappliedUmaskIsNotReportedAsApplied(t *testing.T) {
	t.Setenv("UMASK", "000")
	id := ownerIdentity()
	if id.Env.Umask != "000" {
		t.Errorf("the UMASK the operator set came back as %q", id.Env.Umask)
	}
	if id.UmaskApplied {
		t.Error("the readout says UMASK was applied, but nothing in this test process applied it")
	}
}
