package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

// denoApp builds a desktop-like App with the automatic update on, no Deno
// anywhere, and a Deno update that only counts.
func denoApp(t *testing.T) (*App, *atomic.Int32) {
	t.Helper()
	if err := mediatools.CanInstallDeno(t.TempDir()); err != nil {
		t.Skip(err)
	}
	a := autoUpdateApp(t, true)
	a.InstallMissingTools = true
	t.Setenv("PATH", t.TempDir())
	t.Setenv("KL_DENO", "")
	var fetched atomic.Int32
	a.updateDeno = func(context.Context, string) (mediatools.DenoRecord, bool, error) {
		fetched.Add(1)
		return mediatools.DenoRecord{Version: "2.9.8", Asset: "deno.zip"}, false, nil
	}
	return a, &fetched
}

// fetchedDeno puts a fetched Deno in the data folder, fetched at.
func fetchedDeno(t *testing.T, a *App, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(mediatools.ToolsDir(a.DataDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediatools.DenoPath(a.DataDir), []byte("deno"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(mediatools.DenoRecord{Version: "2.9.8", FetchedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediatools.ToolsDir(a.DataDir), "deno.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheDesktopFetchesDenoWhereNoneIsFound(t *testing.T) {
	a, fetched := denoApp(t)
	a.autoUpdateDeno()
	if fetched.Load() != 1 {
		t.Fatalf("deno was fetched %d times, want once", fetched.Load())
	}
}

func TestTheContainerFetchesNoDeno(t *testing.T) {
	a, fetched := denoApp(t)
	a.InstallMissingTools = false
	a.autoUpdateDeno()
	if fetched.Load() != 0 {
		t.Fatalf("deno was fetched %d times without the desktop's say-so", fetched.Load())
	}
}

func TestADenoOfTheOwnersIsLeftAlone(t *testing.T) {
	for _, where := range []string{"PATH", "KL_DENO"} {
		t.Run(where, func(t *testing.T) {
			a, fetched := denoApp(t)
			bin := t.TempDir()
			own := filepath.Join(bin, filepath.Base(mediatools.DenoPath("")))
			if err := os.WriteFile(own, []byte("deno"), 0o755); err != nil {
				t.Fatal(err)
			}
			if where == "PATH" {
				t.Setenv("PATH", bin)
			} else {
				t.Setenv("KL_DENO", own)
			}
			a.autoUpdateDeno()
			if fetched.Load() != 0 {
				t.Fatalf("deno was fetched %d times over the one in %s", fetched.Load(), where)
			}
		})
	}
}

func TestNoDenoIsFetchedWhileTheUpdateIsOff(t *testing.T) {
	a, fetched := denoApp(t)
	s := a.Settings.Get()
	s.ComponentsAutoUpdate = false
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	a.autoUpdateDeno()
	if fetched.Load() != 0 {
		t.Fatalf("switched off, deno was fetched %d times", fetched.Load())
	}
}
