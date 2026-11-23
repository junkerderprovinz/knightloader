package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/backup"
)

// restoreWaits reports whether dir holds a restore waiting for the next start, or
// the remains of one being put together.
func restoreWaits(t *testing.T, dir string) bool {
	t.Helper()
	for _, name := range []string{"restore-pending", "restore-pending.staging"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func newImportApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, dir
}

func refusalCode(err error) string {
	var r *kitfortress.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// brokenAfter reads half of a backup and then fails the way a damaged
// stream does.
type brokenAfter struct {
	r    io.Reader
	left int
}

func (b *brokenAfter) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, group.ErrStreamBroken
	}
	if len(p) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= n
	return n, err
}

func TestABackupReadToTheEndIsStagedForTheNextStart(t *testing.T) {
	t.Parallel()
	source, _ := newImportApp(t)
	raw := readExport(t, source)
	a, dir := newImportApp(t)

	if err := a.FortressImport(context.Background(), vault, bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if !restoreWaits(t, dir) {
		t.Error("the backup was read in full and nothing waits for the next start")
	}
}

func TestABackupThatBreaksOnTheWayChangesNothing(t *testing.T) {
	t.Parallel()
	source, _ := newImportApp(t)
	raw := readExport(t, source)
	a, dir := newImportApp(t)

	err := a.FortressImport(context.Background(), vault, &brokenAfter{r: bytes.NewReader(raw), left: len(raw) / 2}, int64(len(raw)))
	if !errors.Is(err, group.ErrStreamBroken) {
		t.Errorf("FortressImport = %v, want the broken stream", err)
	}
	if restoreWaits(t, dir) {
		t.Error("a backup that broke halfway was staged")
	}
}

func TestAnImportIsRefusedWithAReasonAndChangesNothing(t *testing.T) {
	t.Parallel()
	source, _ := newImportApp(t)
	key, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	source.AppKey = key
	sealed := readExport(t, source)

	for _, c := range []struct {
		name string
		body []byte
		n    int64
		want string
	}{
		{"not a backup", []byte("this is no archive"), 18, "invalid_backup"},
		{"sealed with another app key", sealed, int64(len(sealed)), "wrong_app_key"},
		{"larger than any backup", nil, backup.MaxUploadBytes + 1, "too_large"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, dir := newImportApp(t)
			other, err := appkey.Generate()
			if err != nil {
				t.Fatal(err)
			}
			a.AppKey = other
			err = a.FortressImport(context.Background(), vault, bytes.NewReader(c.body), c.n)
			if refusalCode(err) != c.want {
				t.Errorf("FortressImport = %v, want the refusal %s", err, c.want)
			}
			if restoreWaits(t, dir) {
				t.Error("a refused import was staged")
			}
		})
	}
}

func TestABackupAppOnTheSameNetworkHandsTheBackupBack(t *testing.T) {
	source, _ := newImportApp(t)
	raw := readExport(t, source)
	a, dir := newImportApp(t)

	port := freeUDPPort(t)
	loader, loaderID := openFortress(t, onTheNetwork(t, port), "knightloader", "Attic", func(c *kitfortress.Config) { c.Import = a.FortressImport })
	keeper, _ := openFortress(t, onTheNetwork(t, port), "bombvault", "Tower", func(c *kitfortress.Config) {
		c.Uses = []string{kitfortress.ImportAction}
	})
	if err := loader.SetPermission("bombvault", "knightloader", kitfortress.ImportAction, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for !knowsApp(keeper, loaderID, "knightloader") {
		if time.Now().After(deadline) {
			t.Fatal("the backup app never learned that the downloader runs knightloader")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := keeper.Import(ctx, loaderID, bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if !restoreWaits(t, dir) {
		t.Error("the backup handed back over the network was not staged")
	}
}
