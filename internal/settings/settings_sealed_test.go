package settings

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/eventprog"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// everySecret is configured() with a value in each field mapSecrets covers.
func everySecret() Settings {
	s := configured()
	s.Torrent.QBittorrent.URL = "http://qbittorrent:8080"
	s.Torrent.QBittorrent.Username = "admin"
	s.Torrent.QBittorrent.Password = "qbit-secret"
	target := targetWithToken()
	target.URL = "https://discord.example/api/webhooks/1/hook-token"
	target.Body = `{"token":"body-token"}`
	s.EventTargets = []notify.Target{target}
	s.Feeds = []feed.Subscription{{URL: "https://indexer.example/api?t=search&apikey=feed-key"}}
	s.Reconnect.Requests = []reconnect.Request{{
		URL:     "https://192.168.0.1/reconnect",
		Headers: map[string]string{"Cookie": "sid=request-cookie"},
		Body:    "password=request-body",
	}}
	s.Reconnect.Script = "router-script --token=script-token"
	s.EventPrograms = []eventprog.Program{programWithToken()}
	s.IdleAction.Command.Program = "/home/someone/bin/idle-secret.sh"
	s.IdleAction.Command.Args = []string{"--token=idle-token"}
	return s
}

var secretsInEverySecret = []string{
	"router-secret", "proxy-secret", "hunter2", "qbit-secret", "Bearer real-token",
	"--token=real-token", "after.sh", "idle-secret.sh", "--token=idle-token", "hook-token", "body-token",
	"feed-key", "request-cookie", "request-body", "script-token",
}

func appKey(t *testing.T) *appkey.Key {
	t.Helper()
	k, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func saveEverySecret(t *testing.T, dir string, key *appkey.Key) Settings {
	t.Helper()
	st, err := LoadWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := st.Set(everySecret())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	// Each value has to survive the save, or the file proves nothing about it.
	var kept []string
	_, _ = saved.mapSecrets(func(v string) (string, error) {
		kept = append(kept, v)
		return v, nil
	})
	for _, want := range secretsInEverySecret {
		found := false
		for _, v := range kept {
			found = found || bytes.Contains([]byte(v), []byte(want))
		}
		if !found {
			t.Fatalf("%q did not survive the save, so the test cannot see whether it is sealed", want)
		}
		if key == nil && !bytes.Contains(raw, []byte(want)) {
			t.Errorf("without a key %q is missing from settings.json", want)
		}
		if key != nil && bytes.Contains(raw, []byte(want)) {
			t.Errorf("settings.json holds %q in the clear", want)
		}
	}
	return saved
}

func TestSecretsInSettingsNeedTheAppKey(t *testing.T) {
	dir := t.TempDir()
	key := appKey(t)
	saved := saveEverySecret(t, dir, key)
	raw, _ := os.ReadFile(filepath.Join(dir, FileName))
	if !bytes.Contains(raw, []byte("proxy.example.invalid")) {
		t.Error("sealing hid a value that is not a secret")
	}

	copied := t.TempDir()
	writeSettings(t, copied, string(raw))
	if _, err := Load(copied); !errors.Is(err, appkey.ErrMissing) {
		t.Fatalf("loading a sealed copy without the key = %v, want ErrMissing", err)
	}
	if _, err := LoadWithKey(copied, appKey(t)); !errors.Is(err, appkey.ErrWrongKey) {
		t.Fatalf("loading a sealed copy with another key = %v, want ErrWrongKey", err)
	}

	back, err := LoadWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Get(); !reflect.DeepEqual(got.Reconnect, saved.Reconnect) ||
		!reflect.DeepEqual(got.Connections, saved.Connections) ||
		!reflect.DeepEqual(got.ArchivePasswords, saved.ArchivePasswords) ||
		!reflect.DeepEqual(got.EventTargets, saved.EventTargets) ||
		!reflect.DeepEqual(got.EventPrograms, saved.EventPrograms) ||
		!reflect.DeepEqual(got.IdleAction, saved.IdleAction) ||
		got.Torrent.QBittorrent != saved.Torrent.QBittorrent {
		t.Fatal("the secrets read back with the key are not the ones saved")
	}
}

func TestSettingsWrittenBeforeTheKeyAreSealedOnLoad(t *testing.T) {
	dir := t.TempDir()
	saved := saveEverySecret(t, dir, nil)

	key := appKey(t)
	st, err := LoadWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, FileName))
	for _, want := range secretsInEverySecret {
		if bytes.Contains(raw, []byte(want)) {
			t.Errorf("the first load with a key left %q in the clear", want)
		}
	}
	if got := st.Get(); got.Reconnect.Password != saved.Reconnect.Password || got.ArchivePasswords[0] != saved.ArchivePasswords[0] {
		t.Fatal("sealing changed the values the instance runs on")
	}
}
