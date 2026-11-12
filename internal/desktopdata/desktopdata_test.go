package desktopdata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/jre"
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
)

func TestKLDataNamesTheDataFolder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KL_DATA", dir)
	if got := Dir("KnightLoader Test"); got != dir {
		t.Fatalf("Dir = %q, want %q", got, dir)
	}
}

func TestTheDataFolderIsNamedAfterTheProduct(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no config folder here: %v", err)
	}
	t.Setenv("KL_DATA", "")
	if got, want := Dir("KnightLoader Test"), filepath.Join(base, "KnightLoader Test"); got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
}

func TestEachComponentLandsWhereTheAppLooks(t *testing.T) {
	data := t.TempDir()
	want := map[string]string{
		"ffmpeg":      mediatools.FFmpegPath(data),
		"java":        jre.Java(data),
		"jdownloader": filepath.Join(data, "jd", "JDownloader.jar"),
	}
	got := map[string]string{}
	for _, c := range Components(data) {
		got[c.ID] = c.Target
	}
	for id, path := range want {
		if got[id] != path {
			t.Errorf("%s lands at %q, want %q", id, got[id], path)
		}
	}
}

func TestAnFFmpegOnPathIsLeftToItsOwner(t *testing.T) {
	bin := t.TempDir()
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	data := t.TempDir()

	for _, c := range Components(data) {
		if c.ID != "ffmpeg" {
			continue
		}
		if err := c.Fetch(context.Background(), nil); err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if _, err := os.Stat(c.Target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("fetched a copy beside the one on PATH: %v", err)
		}
	}
}
