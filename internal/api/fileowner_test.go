package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
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
	if got.EnvRead {
		t.Error("the readout claims this build acts on PUID/PGID/UMASK; nothing in it does, and saying otherwise sends somebody to set a variable that will not work")
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
	if relayForwardable(http.MethodGet, "/api/fileowner") {
		t.Error("GET /api/fileowner is forwardable to a peer; one machine's uid answered under another machine's name is a wrong number nobody can spot")
	}
	if relayForwardable(http.MethodPost, "/api/fileowner/check") {
		t.Error("POST /api/fileowner/check is forwardable to a peer; it writes, and it would be writing into the peer's folders")
	}
}

func TestTheOwnershipRoutesNeedASession(t *testing.T) {
	reg := newRegistry()
	registerFileOwner(reg, testApp(t))
	for _, path := range []string{"/api/fileowner", "/api/fileowner/check"} {
		if reg.open(path) {
			t.Errorf("%s answers without a session; only the routes the login flow itself depends on may", path)
		}
	}
}

// TestTheReadoutDoesNotClaimPUIDWorksWhileTheImagePinsItsUser keeps
// envReadByThisBuild in step with the Dockerfile's USER line, in both
// directions.
func TestTheReadoutDoesNotClaimPUIDWorksWhileTheImagePinsItsUser(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("the image's Dockerfile could not be read (%v); this guard is about what that file declares, so it cannot be skipped quietly", err)
	}
	pinned := strings.Contains(string(b), "\nUSER knight")
	switch {
	case pinned && envReadByThisBuild:
		t.Error("the image still declares USER knight, so it starts as uid 1000 and cannot change uid, " +
			"but envReadByThisBuild is true; the readout would be telling people PUID works when it cannot")
	case !pinned && !envReadByThisBuild:
		t.Error("the image no longer pins its user. If an entrypoint now applies PUID/PGID/UMASK, set envReadByThisBuild " +
			"and rewrite the copy that currently explains that those variables are ignored; if it does not, say why here")
	}
}
