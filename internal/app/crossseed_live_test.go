package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/nox"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// liveQBit runs the qBittorrent named in KL_QBITTORRENT_NOX on loopback with
// peer discovery off, so nothing reaches past this machine.
func liveQBit(t *testing.T) *qbittorrent.Client {
	t.Helper()
	bin := os.Getenv("KL_QBITTORRENT_NOX")
	if bin == "" {
		t.Skip("KL_QBITTORRENT_NOX names no qBittorrent to run")
	}
	profile := t.TempDir()
	if runtime.GOOS == "windows" {
		// The build with a window asks about file associations before its
		// WebUI comes up.
		conf := filepath.Join(profile, "qBittorrent", "config")
		if err := os.MkdirAll(conf, 0o755); err != nil {
			t.Fatal(err)
		}
		ini := "[Preferences]\r\nWin32\\NeverCheckFileAssocation=true\r\nGeneral\\StartMinimized=true\r\nGeneral\\NoSplashScreen=true\r\n"
		if err := os.WriteFile(filepath.Join(conf, "qBittorrent.ini"), []byte(ini), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := nox.Start(nox.Options{Binary: bin, Profile: profile, Logf: t.Logf})
	t.Cleanup(func() { _ = in.Stop() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := in.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = c.SetPreferences(ctx, map[string]any{"dht": false, "pex": false, "lsd": false, "upnp": false})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The twin of a RED album, found on a fake OPS, lands in a real qBittorrent
// seeding from the album's files or from hardlinks to them, tagged and filed
// as the settings say, and leaves with its task.
func TestARealQBittorrentSeedsTheTwin(t *testing.T) {
	for _, mode := range []string{settings.LinkNone, settings.LinkHardlink} {
		t.Run(mode, func(t *testing.T) { seedTwinLive(t, mode) })
	}
}

func seedTwinLive(t *testing.T, mode string) {
	c := liveQBit(t)
	r := crossSeedApp(t)
	r.a.UseTorrents(torrents.QBit(func(context.Context) (*qbittorrent.Client, error) { return c, nil }))
	links := filepath.Join(r.downloads, "cross-seeds")
	r.switchOn(t, func(cs *settings.CrossSeed) {
		cs.Category = "cross-seed music"
		cs.LinkMode, cs.LinkDir = mode, links
	})
	from := r.downloads
	if mode != settings.LinkNone {
		from = filepath.Join(links, "OPS")
	}
	if entries, _ := r.a.CrossSeedLog(5); len(entries) != 1 || entries[0].Result != "injected" {
		t.Fatalf("the log says %+v", entries)
	}

	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for {
		list, err := c.Torrents(ctx, qbittorrent.Filter{Hashes: []string{r.twin}})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 && strings.HasSuffix(list[0].State, "UP") && list[0].Progress == 1 {
			got := list[0]
			tags := strings.Split(strings.ReplaceAll(got.Tags, " ", ""), ",")
			if !slices.Contains(tags, torrents.Tag) || !slices.Contains(tags, CrossSeedTag) {
				t.Errorf("the twin carries the tags %q", got.Tags)
			}
			if got.Category != "cross-seed music" {
				t.Errorf("the twin is filed under %q", got.Category)
			}
			if filepath.Clean(filepath.FromSlash(got.SavePath)) != from {
				t.Errorf("the twin seeds from %s", got.SavePath)
			}
			t.Logf("the twin %s is %s from %s", got.Hash, got.State, got.SavePath)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the twin did not come to seed: %+v", list)
		}
		time.Sleep(250 * time.Millisecond)
	}

	r.a.Remove(r.task.ID, true)
	deadline = time.Now().Add(30 * time.Second)
	for {
		list, _ := c.Torrents(ctx, qbittorrent.Filter{Hashes: []string{r.twin}})
		if len(list) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the twin stayed in qBittorrent after its task was removed")
		}
		time.Sleep(250 * time.Millisecond)
	}
	if mode == settings.LinkNone {
		return
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		_, err := os.Stat(filepath.Join(links, "OPS", "Album", "01.flac"))
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the twin's hardlinks stayed after its task was removed with its files")
		}
		time.Sleep(250 * time.Millisecond)
	}
}
