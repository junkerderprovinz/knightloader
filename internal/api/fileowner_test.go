package api

// The ownership readout as it reaches a browser, and the four things about it
// that are easier to get wrong later than now: it writes, so it must be a POST
// that only ever touches this instance's own folders; it describes THIS machine,
// so it must not travel to a peer; it goes into a public bug report, so it must
// not carry anybody's home directory; and it must never suggest that a variable
// nothing reads would change anything.

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
// a real, writable directory - which is the state the check has anything to say
// about at all.
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

// TestTheIdentityReadoutSaysWhatIsInForceAndWhatWasAskedForSideBySide is the
// three-in-the-morning assertion. PUID=99 beside an effective uid of 1000 is the
// one line that tells an operator their variable did nothing, and it only exists
// because the two halves travel together. A readout that sent the effective uid
// alone would leave them staring at a setting that is plainly there in
// `docker inspect` and has no visible effect, with no way to find out why.
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

// TestAnUnsetVariableIsEmptyAndNeverZero. "" means unset, which means the
// default applies - uid 1000 for PUID, the runtime's mask for UMASK. Sending 0
// for an unset PUID would report that somebody asked to run as root.
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

// TestTheCheckAnswersAListAndLeavesTheFolderExactlyAsItFoundIt is the promise
// the route makes to somebody's download share. It writes into folders that are
// shared over SMB and watched by media scanners, so a leaked probe file is a
// stray entry in a library or a file an operator finds later and dares not
// delete.
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

// TestTheCheckRefusesAFolderThisInstanceDoesNotWriteInto. The route writes, so
// an unfiltered dirs parameter would be "create and delete a dot-file anywhere
// on this host, as this process" behind one session. Nothing needs that: the
// page only ever names folders it read out of this same list a moment earlier.
//
// Refused rather than skipped, because a report that quietly measured three of
// the four folders it was asked about would look complete and not be, which is
// the exact failure this whole feature exists to remove.
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

// TestNamingOneFolderChecksOnlyThatFolder. The parameter exists so that saving a
// changed download folder re-checks that folder rather than writing a probe file
// into all fourteen configured directories, several of which may sit on mounts
// that are slow or asleep.
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

// TestTheBundleCarriesTheOwnersAndNotAnybodysHomeDirectory. The diagnostics
// bundle is a file people attach to public bug reports, and with no download
// folder configured the desktop build's default one is
// C:\Users\<their real name>\AppData\... - app.New puts it inside the data
// directory. routes_diagnostics.go states that rule for the store paths; this is
// the same rule, and the reason the ownership rows carry a role instead of a
// path.
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
	// Decoded rather than string-searched for the data directory, because that
	// search passes for the wrong reason on Windows: json.Marshal escapes every
	// backslash, so `C:\Users\...` in the document never matches `C:\Users\...`
	// in a.DataDir and the check silently stops testing anything. Asking the
	// document what fields it has is the same question with no platform in it.
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

// TestTheOwnershipRoutesAreNotForwardedToAPeer is the trap these routes set for
// whoever widens the relay allowlist next. routes_diskspace.go makes the
// argument for disks and it is stronger here: a peer's answer names THAT box's
// uids and THAT box's folders, and drawn under this box's name it would send an
// operator to run a chown against a path on the wrong machine.
func TestTheOwnershipRoutesAreNotForwardedToAPeer(t *testing.T) {
	if relayForwardable(http.MethodGet, "/api/fileowner") {
		t.Error("GET /api/fileowner is forwardable to a peer; one machine's uid answered under another machine's name is a wrong number nobody can spot")
	}
	if relayForwardable(http.MethodPost, "/api/fileowner/check") {
		t.Error("POST /api/fileowner/check is forwardable to a peer; it WRITES, and it would be writing into the peer's folders")
	}
}

// TestTheOwnershipRoutesNeedASession keeps both behind the guard everything else
// under /api/ is behind. Neither carries a credential of its own, one of them
// answers with this host's folder paths, and the other one writes.
func TestTheOwnershipRoutesNeedASession(t *testing.T) {
	reg := newRegistry()
	registerFileOwner(reg, testApp(t))
	for _, path := range []string{"/api/fileowner", "/api/fileowner/check"} {
		if reg.open(path) {
			t.Errorf("%s answers without a session; only the routes the login flow itself depends on may", path)
		}
	}
}

// TestTheReadoutDoesNotClaimPUIDWorksWhileTheImagePinsItsUser is the guard on
// the one sentence this feature must never say.
//
// Dockerfile declares USER knight, so the process starts as uid 1000 and cannot
// become another uid; PUID and PGID are read by nothing at any layer. The day
// somebody drops that line and adds an entrypoint, envReadByThisBuild and every
// piece of copy hanging off it have to change in the same commit - and this is
// what says so, in both directions, rather than leaving a stale "it did not
// take" explanation in front of a variable that now works perfectly.
func TestTheReadoutDoesNotClaimPUIDWorksWhileTheImagePinsItsUser(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("the image's Dockerfile could not be read (%v); this guard is about what that file declares, so it cannot be skipped quietly", err)
	}
	pinned := strings.Contains(string(b), "\nUSER knight")
	switch {
	case pinned && envReadByThisBuild:
		t.Error("the image still declares USER knight, so it starts as uid 1000 and cannot change uid, " +
			"but envReadByThisBuild is true - the readout would be telling people PUID works when it cannot")
	case !pinned && !envReadByThisBuild:
		t.Error("the image no longer pins its user. If an entrypoint now applies PUID/PGID/UMASK, set envReadByThisBuild " +
			"and rewrite the copy that currently explains that those variables are ignored; if it does not, say why here")
	}
}
