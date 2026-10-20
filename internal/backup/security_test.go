package backup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

func checksPassword(t *testing.T, dir, password string) bool {
	t.Helper()
	g, err := auth.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return g.Check(password)
}

// A backup from an instance without a password never takes the password off
// one that has it: the instance would come back open to its whole network.
func TestABackupWithoutAPasswordKeepsTheLiveOne(t *testing.T) {
	for name, strip := range map[string]bool{"with an empty login": false, "without auth.json": true} {
		t.Run(name, func(t *testing.T) {
			source, _ := liveInstance(t, "source-key", "")
			archive := archiveOfInstance(t, source)
			if strip {
				archive = rebuildWithout(t, archive, auth.FileName)
			}
			target, _ := liveInstance(t, "target-key", "target-password")
			report, err := Check(archive, "v1.2.3", nil, true)
			if err != nil || report.Login != LoginKept {
				t.Errorf("Check = %+v, %v; want the login kept", report, err)
			}
			if _, err := Stage(target, archive, "v1.2.3", nil); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ApplyPending(target, nil); err != nil {
				t.Fatal(err)
			}
			if !checksPassword(t, target, "target-password") {
				t.Error("the restore took the password off the instance")
			}
		})
	}
}

func TestCheckSaysWhatBecomesOfTheLogin(t *testing.T) {
	withPassword, _ := liveInstance(t, "source-key", "source-password")
	without, _ := liveInstance(t, "source-key", "")
	for name, c := range map[string]struct {
		source       string
		livePassword bool
		want         string
	}{
		"a password this instance can check": {withPassword, false, LoginRestored},
		"no password, over one that is set":  {without, true, LoginKept},
		"no password, over none":             {without, false, LoginNone},
	} {
		t.Run(name, func(t *testing.T) {
			report, err := Check(archiveOfInstance(t, c.source), "v1.2.3", nil, c.livePassword)
			if err != nil || report.Login != c.want {
				t.Errorf("Check = %+v, %v; want login %s", report, err, c.want)
			}
		})
	}
}

// A password hashed under another app key cannot be checked here. Over an
// instance without a password that leaves none, and Check says so.
func TestCheckWarnsWhenTheRestoreLeavesNoPassword(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	manifest := testManifest()
	manifest.Files = StateFiles
	manifest.KeyID = "another-key"
	archive := withManifest(t, archiveOfInstance(t, source), manifest)
	archive = rebuildWith(t, archive, auth.FileName, []byte(`{"hash":"argon2id$19456$2$1$c2FsdA$c3Vt","key":"00"}`))
	report, err := Check(archive, "v1.2.3", nil, false)
	if err != nil || report.Login != LoginNone {
		t.Errorf("Check = %+v, %v; want login none", report, err)
	}
	if report, _ := Check(archive, "v1.2.3", nil, true); report.Login != LoginKept {
		t.Errorf("over an instance with a password Check = %+v; want login kept", report)
	}
}

// What waits for the next start holds every secret of the backup, so it is
// sealed with the instance's key and readable by this account alone.
func TestAStagedRestoreIsSealedWithTheInstancesKey(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	target, _ := liveInstance(t, "target-key", "target-password")
	key, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	acc, _ := accounts.Open(source)
	keyring := acc.Keyring()
	if _, err := Stage(target, archiveOfInstance(t, source), "v1.2.3", key); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(target, pendingDirName)
	err = filepath.WalkDir(pending, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, keyring) || !bytes.HasPrefix(b, sealedMagic) {
			t.Errorf("%s is in the clear", path)
		}
		if info, _ := d.Info(); runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s has mode %o", path, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied, _, err := ApplyPending(target, key); err != nil || !applied {
		t.Fatalf("ApplyPending = %v, %v", applied, err)
	}
}

func TestStageRefusesAnOversizedEntry(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	archive := archiveOfInstance(t, source)
	for name, size := range map[string]int{"scripts.json": maxStateFileBytes + 1, settingsEntry: maxSettingsBytes + 1} {
		big := []byte(`"` + strings.Repeat("x", size) + `"`)
		if _, err := Stage(t.TempDir(), rebuildWith(t, archive, name, big), "v1.2.3", nil); err == nil {
			t.Errorf("Stage took a %s of %d bytes", name, len(big))
		}
	}
}

// rebuildWithout returns archive without the entry name.
func rebuildWithout(t *testing.T, archive []byte, name string) []byte {
	t.Helper()
	return rebuildWith(t, archive, name, nil, true)
}

// withManifest returns archive with its manifest replaced.
func withManifest(t *testing.T, archive []byte, manifest Manifest) []byte {
	t.Helper()
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return rebuildWith(t, archive, manifestEntry, b)
}
