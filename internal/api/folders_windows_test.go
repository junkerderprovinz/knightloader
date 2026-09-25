//go:build windows

package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// junction links link to target the way any user can, without the symlink
// privilege.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J is not available here: %v: %s", err, out)
	}
}

func TestAJunctionOutOfTheBoundaryIsRefused(t *testing.T) {
	allowed, forbidden := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(forbidden, "private"), 0o755); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(allowed, "escape")
	junction(t, escape, forbidden)
	t.Setenv(envBrowseRoots, allowed)
	_, srv := foldersServer(t)

	for _, e := range getFolders(t, srv, allowed).Entries {
		if e.Name == "escape" {
			t.Error("a junction pointing out of the boundary was offered as a folder to browse into")
		}
	}
	if code, raw := getRaw(t, foldersURL(srv, escape)); code != http.StatusForbidden {
		t.Errorf("listing through the junction answered %d, want 403: %s", code, raw)
	}
	if code, out := postFolder(t, srv, escape, "new"); code != http.StatusForbidden || out["code"] != "outside" {
		t.Errorf("creating through the junction answered %d %v, want 403 outside", code, out)
	}
	if _, err := os.Stat(filepath.Join(forbidden, "new")); err == nil {
		t.Fatal("a folder was created at the far end of the junction")
	}
}

func TestAJunctionInsideTheBoundaryIsOffered(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	here := filepath.Join(base, "here")
	for _, dir := range []string{real, here} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	linked := filepath.Join(here, "linked")
	junction(t, linked, real)
	t.Setenv(envBrowseRoots, base)
	_, srv := foldersServer(t)

	got := getFolders(t, srv, here)
	if len(got.Entries) != 1 || got.Entries[0].Path != linked {
		t.Fatalf("entries are %+v, want the junction under its own name", got.Entries)
	}
	if code, out := postFolder(t, srv, linked, "new"); code != http.StatusCreated || out["path"] != filepath.Join(linked, "new") {
		t.Errorf("creating through the junction answered %d %v", code, out)
	}
	if fi, err := os.Stat(filepath.Join(real, "new")); err != nil || !fi.IsDir() {
		t.Errorf("the folder did not land in the junction's target: %v", err)
	}
}

// The file route shares the boundary: a task folder that is a junction out of
// the download tree is refused as outside it, not reported as empty.
func TestServingAFileThroughAJunctionOutOfTheDownloadsIsRefused(t *testing.T) {
	a, base, srv := filesServer(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "settings.json"), []byte("not for this task"), 0o644); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(base, "escape")
	junction(t, escape, outside)
	id := stage(t, a, "https://host.example/settings.json")[0].ID
	if err := a.SetTaskOptions([]string{id}, app.TaskOptions{Dir: strp(escape), Name: strp("settings.json")}); err != nil {
		t.Fatal(err)
	}

	code, body := getRaw(t, srv.URL+"/api/tasks/"+id+"/file")
	if code != http.StatusForbidden {
		t.Fatalf("serving through the junction answered %d, want 403: %q", code, body)
	}
}

func TestServingAFileThroughAJunctionInsideTheDownloadsWorks(t *testing.T) {
	a, base, srv := filesServer(t)
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(base, "linked")
	junction(t, linked, real)
	id := stage(t, a, "https://host.example/notes.txt")[0].ID
	if err := a.SetTaskOptions([]string{id}, app.TaskOptions{Dir: strp(linked), Name: strp("notes.txt")}); err != nil {
		t.Fatal(err)
	}

	if code, body := getRaw(t, srv.URL+"/api/tasks/"+id+"/file"); code != http.StatusOK || string(body) != "hello" {
		t.Fatalf("serving through a junction that stays inside answered %d: %q", code, body)
	}
}
