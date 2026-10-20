package backup

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// liveInstance is a data directory with a database, settings, a login and one
// stored account, and returns the content of every file in it by name.
func liveInstance(t *testing.T, apiKey, password string) (string, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, dbEntry))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.WriteFile(filepath.Join(dir, settingsEntry), testSettingsJSON(t), 0o600); err != nil {
		t.Fatal(err)
	}
	acc, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := acc.Set("torbox", apiKey); err != nil {
		t.Fatal(err)
	}
	g, err := auth.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetPassword("", password); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts.json"), []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, readAll(t, dir)
}

func readAll(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = b
		}
	}
	return out
}

// archiveOfInstance builds the backup of a data directory made by
// liveInstance, with its state files in the clear.
func archiveOfInstance(t *testing.T, dir string) []byte {
	t.Helper()
	acc, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := g.State()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{auth.FileName: state, accounts.KeyringName: acc.Keyring()}
	for _, name := range []string{"accounts.json", "scripts.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	manifest := testManifest()
	manifest.Files = StateFiles
	var archive bytes.Buffer
	if err := Build(&archive, manifest, testSettingsJSON(t), snapshotDB(t, filepath.Join(dir, dbEntry)), files); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestARestoreBringsTheAccountsAndTheLoginAlong(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	target, _ := liveInstance(t, "target-key", "target-password")
	if _, err := Stage(target, archiveOfInstance(t, source), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if applied, _, err := ApplyPending(target, nil); err != nil || !applied {
		t.Fatalf("ApplyPending = %v, %v", applied, err)
	}
	acc, err := accounts.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := acc.Get("torbox"); err != nil || got != "source-key" {
		t.Errorf("the restored account reads %q, %v", got, err)
	}
	g, err := auth.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Check("source-password") {
		t.Error("the restored login does not take the backup's password")
	}
}

func TestAStateFileTheBackupLacksIsRemoved(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	if err := os.Remove(filepath.Join(source, "scripts.json")); err != nil {
		t.Fatal(err)
	}
	target, _ := liveInstance(t, "target-key", "target-password")
	acc, _ := accounts.Open(source)
	g, _ := auth.Open(source)
	state, _ := g.State()
	accountsJSON, _ := os.ReadFile(filepath.Join(source, "accounts.json"))
	manifest := testManifest()
	manifest.Files = StateFiles
	var archive bytes.Buffer
	files := map[string][]byte{auth.FileName: state, accounts.KeyringName: acc.Keyring(), "accounts.json": accountsJSON}
	if err := Build(&archive, manifest, testSettingsJSON(t), snapshotDB(t, filepath.Join(source, dbEntry)), files); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(target, archive.Bytes(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyPending(target, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "scripts.json")); !os.IsNotExist(err) {
		t.Errorf("the target keeps a scripts.json the backed-up instance did not have: %v", err)
	}
}

// A restore that stops while it swaps the files puts back every file it had
// already replaced, and the next start finishes it from the staged copy.
func TestAnInterruptedRestoreLeavesTheOldStateIntact(t *testing.T) {
	for name, failOn := range map[string]func(from string) bool{
		"while the live files go aside": func(from string) bool { return !strings.Contains(from, readyDirName) },
		"while the new files go in":     func(from string) bool { return strings.Contains(from, readyDirName) },
	} {
		t.Run(name, func(t *testing.T) {
			source, _ := liveInstance(t, "source-key", "source-password")
			target, before := liveInstance(t, "target-key", "target-password")
			if _, err := Stage(target, archiveOfInstance(t, source), "v1.2.3"); err != nil {
				t.Fatal(err)
			}

			matching := 0
			renameFile = func(from, to string) error {
				if failOn(from) {
					if matching++; matching == 3 {
						return errors.New("the disk went away")
					}
				}
				return os.Rename(from, to)
			}
			t.Cleanup(func() { renameFile = os.Rename })
			if _, _, err := ApplyPending(target, nil); err == nil {
				t.Fatal("ApplyPending succeeded with a rename that failed")
			}
			after := readAll(t, target)
			for name, want := range before {
				if !bytes.Equal(after[name], want) {
					t.Errorf("%s changed although the restore failed", name)
				}
			}
			for name := range after {
				if _, ok := before[name]; !ok {
					t.Errorf("the failed restore left %s behind", name)
				}
			}

			renameFile = os.Rename
			if applied, _, err := ApplyPending(target, nil); err != nil || !applied {
				t.Fatalf("the next start's ApplyPending = %v, %v", applied, err)
			}
			acc, err := accounts.Open(target)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := acc.Get("torbox"); got != "source-key" {
				t.Errorf("after the retry the account reads %q", got)
			}
		})
	}
}

func TestStageRefusesAStateFileItDoesNotKnow(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	manifest := testManifest()
	manifest.Files = append([]string{"../outside.json"}, StateFiles...)
	var archive bytes.Buffer
	if err := Build(&archive, manifest, testSettingsJSON(t), snapshotDB(t, filepath.Join(source, dbEntry)), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(t.TempDir(), archive.Bytes(), "v1.2.3"); err == nil {
		t.Error("Stage took a manifest naming a file outside the data directory")
	}
}

func TestStageRefusesAccountsItCannotOpen(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	other, _ := liveInstance(t, "other-key", "other-password")
	archive := archiveOfInstance(t, source)
	// The keyring of another instance does not open these accounts.
	acc, _ := accounts.Open(other)
	tampered := rebuildWith(t, archive, accounts.KeyringName, acc.Keyring())
	if _, err := Stage(t.TempDir(), tampered, "v1.2.3"); err == nil {
		t.Error("Stage took accounts its keyring does not open")
	}
}

// rebuildWith returns archive with the entry name replaced by data.
func rebuildWith(t *testing.T, archive []byte, name string, data []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		b := data
		if f.Name != name {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			if b, err = io.ReadAll(r); err != nil {
				t.Fatal(err)
			}
			r.Close()
		}
		mustWriteEntry(t, zw, f.Name, b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCheckSaysWhetherTheBackupCameFromAPairedInstance(t *testing.T) {
	source, _ := liveInstance(t, "source-key", "source-password")
	plain := archiveOfInstance(t, source)
	for name, c := range map[string]struct {
		group  string
		paired bool
	}{
		"in a group":     {`{"joinedAt":"2026-10-01T03:00:00Z"}`, true},
		"left its group": {`{"joinedAt":"0001-01-01T00:00:00Z"}`, false},
		"never paired":   {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			archive := plain
			if c.group != "" {
				archive = rebuildWith(t, appendEntry(t, plain, "group.json"), "group.json", []byte(c.group))
			}
			_, paired, err := Check(archive, "v1.2.3")
			if err != nil || paired != c.paired {
				t.Errorf("Check = paired %v, %v; want %v", paired, err, c.paired)
			}
		})
	}
}

// appendEntry returns archive with an empty entry called name added.
func appendEntry(t *testing.T, archive []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		mustWriteEntry(t, zw, f.Name, b)
	}
	mustWriteEntry(t, zw, name, nil)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
