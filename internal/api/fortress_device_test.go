package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheServerNameFromTheFlashIsTheDefaultDevice(t *testing.T) {
	flash := t.TempDir()
	if err := os.MkdirAll(filepath.Join(flash, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flash, "config", "ident.cfg"), []byte("# Unraid\nNAME=\"Tower\"\nCOMMENT=\"media\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := unraidServerName(flash); got != "Tower" {
		t.Errorf("unraidServerName = %q, want Tower", got)
	}
	if got := unraidServerName(t.TempDir()); got != "" {
		t.Errorf("without ident.cfg unraidServerName = %q, want nothing", got)
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
