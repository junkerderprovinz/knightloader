package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// Not parallel: it points unraidFlash, where the template mounts the flash,
// at a folder of its own.
func TestTheServerNameFromTheFlashIsTheDefaultDevice(t *testing.T) {
	flash := t.TempDir()
	if err := os.MkdirAll(filepath.Join(flash, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flash, "config", "ident.cfg"), []byte("# Unraid\r\nNAME=\"Tower\"\r\nCOMMENT=\"media\"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mounted := unraidFlash
	t.Cleanup(func() { unraidFlash = mounted })
	unraidFlash = flash
	srv, a := testServer(t)
	defer srv.Close()
	s := a.Settings.Get()
	s.InstanceName = "e8fc22d4db7b"
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}

	var v deviceAnswer
	if code := getJSON(t, srv.URL+"/api/fortress", &v); code != http.StatusOK || v.Device != "Tower" || v.DefaultDevice != "Tower" {
		t.Errorf("with the flash mounted the fortress reads %d %+v, want Tower", code, v)
	}
	unraidFlash = t.TempDir()
	if code := getJSON(t, srv.URL+"/api/fortress", &v); code != http.StatusOK || v.DefaultDevice != "e8fc22d4db7b" {
		t.Errorf("without ident.cfg the fortress reads %d %+v, want the instance name", code, v)
	}
}

type deviceAnswer struct {
	Device        string `json:"device"`
	DefaultDevice string `json:"defaultDevice"`
	Code          string `json:"code"`
}

func putDevice(t *testing.T, base, device string) (int, deviceAnswer) {
	t.Helper()
	code, raw := postJSON(t, http.MethodPut, base+"/api/fortress/device", map[string]string{"device": device})
	var v deviceAnswer
	_ = json.Unmarshal(raw, &v)
	return code, v
}

func TestTheFortressDeviceIsNamedAndFallsBackToTheInstanceName(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	s := a.Settings.Get()
	s.InstanceName = "attic"
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}

	if code, v := putDevice(t, srv.URL, "  cellar "); code != http.StatusOK || v.Device != "cellar" || v.DefaultDevice != "attic" {
		t.Fatalf("naming the device answered %d %+v", code, v)
	}
	if a.Settings.Get().FortressDevice != "cellar" {
		t.Errorf("the device name was not kept: %q", a.Settings.Get().FortressDevice)
	}
	if code, v := putDevice(t, srv.URL, "two\nlines"); code != http.StatusBadRequest || v.Code != "fortress.deviceName" {
		t.Errorf("a name over two lines answered %d %+v", code, v)
	}
	if code, v := putDevice(t, srv.URL, strings.Repeat("a", 65)); code != http.StatusBadRequest || v.Code != "fortress.deviceName" {
		t.Errorf("a name of 65 characters answered %d %+v", code, v)
	}
	if code, v := putDevice(t, srv.URL, " "); code != http.StatusOK || v.Device != "attic" {
		t.Errorf("an empty name answered %d %+v, want the instance name back", code, v)
	}
}

func TestTheOtherMembersSeeTheFortressDevice(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	keeper, _ := backupKeeperIn(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	if code, v := putDevice(t, srv.URL, "Tower"); code != http.StatusOK {
		t.Fatalf("naming the device answered %d %+v", code, v)
	}
	loader := memberKey(a).ID()
	waitFor(t, "the backup app to see KnightLoader on Tower", func() bool {
		for _, m := range keeper.Members() {
			if m.ID == loader && m.Device == "Tower" {
				return true
			}
		}
		return false
	})
}

// The template runs KnightLoader as 99:100, and the flash keeps ident.cfg for
// root alone, so the name Unraid hands every container has to do.
func TestUnraidsHostnameNamesTheServerWhenTheFlashIsUnreadable(t *testing.T) {
	mounted := unraidFlash
	t.Cleanup(func() { unraidFlash = mounted })
	unraidFlash = t.TempDir()
	t.Setenv("HOST_HOSTNAME", " Tower ")
	srv, a := testServer(t)
	defer srv.Close()
	s := a.Settings.Get()
	s.InstanceName = "e8fc22d4db7b"
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}

	var v deviceAnswer
	if code := getJSON(t, srv.URL+"/api/fortress", &v); code != http.StatusOK || v.Device != "Tower" || v.DefaultDevice != "Tower" {
		t.Errorf("with HOST_HOSTNAME set the fortress reads %d %+v, want Tower", code, v)
	}
}

func TestTheOtherMembersSeeKnightLoadersLogoAndServerUnderAnyName(t *testing.T) {
	t.Setenv("HOST_HOSTNAME", "Tower")
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	keeper, _ := backupKeeperIn(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	if code, v := putDevice(t, srv.URL, "Attic"); code != http.StatusOK {
		t.Fatalf("naming the device answered %d %+v", code, v)
	}
	loader := memberKey(a).ID()
	waitFor(t, "the backup app to see KnightLoader's logo on Tower", func() bool {
		for _, m := range keeper.Members() {
			if m.ID == loader && m.Device == "Attic" && m.Host == "Tower" && m.App.Icon == app.Icon {
				return true
			}
		}
		return false
	})
}
