package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

const flatpakID = "io.github.junkerderprovinz.knightloader"

// The sandbox lets the app own bus names under its own ID alone, and the
// desktop finds the app's launcher for a window by the same name.
func TestAFlatpakGoesByItsID(t *testing.T) {
	t.Setenv("FLATPAK_ID", flatpakID)
	t.Setenv("KL_DATA", "")
	opts := linuxOptions()
	if opts.ApplicationID != flatpakID || opts.ProgramName != flatpakID {
		t.Errorf("application ID %q, program name %q, want both %q", opts.ApplicationID, opts.ProgramName, flatpakID)
	}
	if !opts.DisableQuitOnLastWindowClosed {
		t.Error("closing the main window would end the program while the tray still shows it")
	}
	if got := instanceID("/home/jane/.var/app/" + flatpakID + "/config/KnightLoader"); got != flatpakID {
		t.Errorf("single-instance lock %q, want %q", got, flatpakID)
	}
}

func TestAFlatpakOnItsOwnDataFolderKeepsItsID(t *testing.T) {
	t.Setenv("FLATPAK_ID", flatpakID)
	t.Setenv("KL_DATA", "/home/jane/kl-test")
	if got := instanceID("/home/jane/kl-test"); !strings.HasPrefix(got, flatpakID+".") {
		t.Errorf("single-instance lock %q is outside the names the sandbox allows", got)
	}
}

func TestOutsideAFlatpakTheNamesStay(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("KL_DATA", "")
	opts := linuxOptions()
	if opts.ApplicationID != "" || opts.ProgramName != "KnightLoader" {
		t.Errorf("application ID %q, program name %q", opts.ApplicationID, opts.ProgramName)
	}
	if got := instanceID("/home/jane/.config/KnightLoader"); got != appID {
		t.Errorf("single-instance lock %q, want %q", got, appID)
	}
}

// Wails takes the lock's name for a D-Bus name on Linux, whose elements may
// not start with a digit, and refuses the lock otherwise.
func TestEveryDataFolderGetsALockDBusAccepts(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	for _, dir := range []string{"/a", "/b", "/c", "/d", "/e", "/f", "/g", "/h", "/srv/kl", "/tmp/kl-1", "/tmp/kl-2", "/tmp/kl-3"} {
		t.Setenv("KL_DATA", dir)
		id := instanceID(dir)
		for _, element := range strings.Split(id, ".") {
			if element == "" || element[0] >= '0' && element[0] <= '9' {
				t.Errorf("lock for %s is %q, whose element %q D-Bus refuses", dir, id, element)
			}
		}
	}
}

func TestAFlatpakNeverUpdatesItself(t *testing.T) {
	t.Setenv("FLATPAK_ID", flatpakID)
	asked := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case asked <- struct{}{}:
		default:
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	first, version := firstCheck, buildinfo.Version
	firstCheck, buildinfo.Version = 0, "v2.8.0"
	t.Cleanup(func() { firstCheck, buildinfo.Version = first, version })

	up := newUpdater(t.Logf, func(string) {})
	up.u.API = srv.URL
	up.u.Client = srv.Client()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		up.run(ctx, func() bool { return true })
		close(done)
	}()

	select {
	case <-done:
	case <-asked:
		t.Fatal("a Flatpak looked for a release to replace itself with")
	case <-time.After(5 * time.Second):
		t.Fatal("a Flatpak keeps waiting to update itself")
	}

	if err := up.install(ctx, false); !errors.Is(err, errUpdatesFromStore) {
		t.Errorf("Install in a Flatpak = %v, want errUpdatesFromStore", err)
	}
}
