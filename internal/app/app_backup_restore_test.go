package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/access"
	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

func newKey(t *testing.T) *appkey.Key {
	t.Helper()
	k, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func openApp(t *testing.T, dir string, key *appkey.Key) *App {
	t.Helper()
	appBuild.Lock()
	a, err := NewWithKey(dir, key)
	appBuild.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// sourceBackup fills an instance on key with one of everything a restore has
// to bring back and returns its backup archive and API token.
func sourceBackup(t *testing.T, key *appkey.Key) (archive []byte, token string) {
	t.Helper()
	a := openApp(t, t.TempDir(), key)
	defer a.Close()
	session := setPassword(t, a, "source-login-password")
	if err := a.Accounts.SetCredential("torbox", "", accounts.Credential{APIKey: "torbox-api-key"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.SetCredential("rapidgator", "second", accounts.Credential{Username: "me", Password: "hoster-password"}); err != nil {
		t.Fatal(err)
	}
	cfg := a.Settings.Get()
	cfg.Reconnect.Password = "router-password"
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Save(&core.Task{ID: "t1", Name: "set.rar", Password: "archive-password", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	token = kitToken(t, a, session)

	parts, err := a.PrepareBackup()
	if err != nil {
		t.Fatal(err)
	}
	defer parts.Discard()
	var buf bytes.Buffer
	if err := a.WriteBackup(&buf, parts); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), token
}

// restoreOnto restores archive on a fresh instance on key whose own login is
// target-login-password, the way the upload route and the next start do, and
// opens it.
func restoreOnto(t *testing.T, archive []byte, sourceKey, key *appkey.Key) *App {
	t.Helper()
	dir := t.TempDir()
	before := openApp(t, dir, key)
	setPassword(t, before, "target-login-password")
	before.Close()

	plain, err := backup.Unseal(archive, key, sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(dir, plain, buildinfo.Version, key); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	applied, _, err := backup.ApplyPending(dir, key)
	if err != nil || !applied {
		t.Fatalf("ApplyPending = %v, %v", applied, err)
	}
	a := openApp(t, dir, key)
	t.Cleanup(func() { a.Close() })
	return a
}

func assertRestored(t *testing.T, a *App, token string) {
	t.Helper()
	if c, err := a.Accounts.GetCredential("torbox", ""); err != nil || c.APIKey != "torbox-api-key" {
		t.Errorf("the TorBox account reads %+v, %v", c, err)
	}
	if c, err := a.Accounts.GetCredential("rapidgator", "second"); err != nil || c.Password != "hoster-password" {
		t.Errorf("the second Rapidgator account reads %+v, %v", c, err)
	}
	if got := a.Settings.Get().Reconnect.Password; got != "router-password" {
		t.Errorf("the router password reads %q", got)
	}
	tasks, err := a.Store.All()
	if err != nil || len(tasks) != 1 || tasks[0].Password != "archive-password" {
		t.Errorf("the task reads %+v, %v", tasks, err)
	}
	if !tokenCounts(a, token) {
		t.Error("the API token from the backup is refused")
	}
}

func TestABackupRestoresUnderAnotherAppKey(t *testing.T) {
	keyA, keyB := newKey(t), newKey(t)
	archive, token := sourceBackup(t, keyA)
	a := restoreOnto(t, archive, keyA, keyB)
	assertRestored(t, a, token)

	// Sealed under B now, with nothing left in the clear.
	for _, name := range []string{"settings.json", access.KeyFile, accounts.KeyringName} {
		b, err := os.ReadFile(filepath.Join(a.DataDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "router-password") || (name != "settings.json" && !appkey.IsSealed(strings.TrimSpace(string(b)))) {
			t.Errorf("%s is not sealed under the target's key", name)
		}
	}
	if copies, _ := filepath.Glob(filepath.Join(a.DataDir, appkey.CopiesPrefix+"*")); len(copies) != 0 {
		t.Errorf("the restore left plain copies behind: %v", copies)
	}
	// The sign-in's key travels in the archive, so the backup's password
	// comes along under any app key.
	if !signsIn(a, "source-login-password") || signsIn(a, "target-login-password") {
		t.Error("the backup's login did not replace the target's")
	}
}

func TestABackupWithAKeyRestoresWithoutOne(t *testing.T) {
	keyA := newKey(t)
	archive, token := sourceBackup(t, keyA)
	a := restoreOnto(t, archive, keyA, nil)
	assertRestored(t, a, token)
	if !signsIn(a, "source-login-password") {
		t.Error("the backup's login does not work after the restore")
	}
}

func TestTheLoginComesAlongWhereTheTargetCanCheckIt(t *testing.T) {
	keyB := newKey(t)
	for name, c := range map[string]struct{ source, target *appkey.Key }{
		"without a key into one with a key": {nil, keyB},
		"with the same key":                 {keyB, keyB},
	} {
		t.Run(name, func(t *testing.T) {
			archive, token := sourceBackup(t, c.source)
			a := restoreOnto(t, archive, c.source, c.target)
			assertRestored(t, a, token)
			if !signsIn(a, "source-login-password") {
				t.Error("the backup's login password does not work after the restore")
			}
			if signsIn(a, "target-login-password") {
				t.Error("the target's old login password still works")
			}
		})
	}
}
