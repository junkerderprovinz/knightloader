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

// ffmpegApp builds a desktop-like App with the automatic update on, no ffmpeg
// on PATH, and an ffmpeg update that only counts.
func ffmpegApp(t *testing.T) (*App, *atomic.Int32) {
	t.Helper()
	if err := mediatools.CanInstallFFmpeg(t.TempDir()); err != nil {
		t.Skip(err)
	}
	a := autoUpdateApp(t, true)
	a.InstallMissingTools = true
	t.Setenv("PATH", t.TempDir())
	var fetched atomic.Int32
	a.updateFFmpeg = func(context.Context, string) (mediatools.FFmpegRecord, bool, error) {
		fetched.Add(1)
		return mediatools.FFmpegRecord{Version: "N-1", Asset: "ffmpeg.zip"}, false, nil
	}
	return a, &fetched
}

// fetchedFFmpeg puts a fetched ffmpeg in the data folder, fetched at.
func fetchedFFmpeg(t *testing.T, a *App, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(mediatools.ToolsDir(a.DataDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediatools.FFmpegPath(a.DataDir), []byte("ffmpeg"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(mediatools.FFmpegRecord{Version: "N-1", FetchedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediatools.ToolsDir(a.DataDir), "ffmpeg.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheDesktopFetchesFFmpegWhereNoneIsFound(t *testing.T) {
	a, fetched := ffmpegApp(t)
	a.autoUpdateFFmpeg()
	if fetched.Load() != 1 {
		t.Fatalf("ffmpeg was fetched %d times, want once", fetched.Load())
	}
}

func TestTheContainerFetchesNoFFmpeg(t *testing.T) {
	a, fetched := ffmpegApp(t)
	a.InstallMissingTools = false
	a.autoUpdateFFmpeg()
	if fetched.Load() != 0 {
		t.Fatalf("ffmpeg was fetched %d times without the desktop's say-so", fetched.Load())
	}
}

func TestAnFFmpegOnPathIsLeftAlone(t *testing.T) {
	a, fetched := ffmpegApp(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, filepath.Base(mediatools.FFmpegPath(""))), []byte("ffmpeg"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	a.autoUpdateFFmpeg()
	if fetched.Load() != 0 {
		t.Fatalf("ffmpeg was fetched %d times over the one on PATH", fetched.Load())
	}
}

func TestNoFFmpegIsFetchedWhileTheUpdateIsOff(t *testing.T) {
	a, fetched := ffmpegApp(t)
	s := a.Settings.Get()
	s.ComponentsAutoUpdate = false
	if _, err := a.Settings.Set(s); err != nil {
		t.Fatal(err)
	}
	a.autoUpdateFFmpeg()
	if fetched.Load() != 0 {
		t.Fatalf("switched off, ffmpeg was fetched %d times", fetched.Load())
	}
}
