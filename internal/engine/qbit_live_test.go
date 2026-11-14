package engine

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/nox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
)

// liveQBit runs the qBittorrent named in KL_QBITTORRENT_NOX on loopback, with
// nothing that reaches past this machine, and returns its client and the
// port it listens on for peers.
func liveQBit(t *testing.T) (*qbittorrent.Client, int) {
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
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	err = c.SetPreferences(ctx, map[string]any{
		"listen_port": port, "dht": false, "pex": false, "lsd": false, "upnp": false,
		"current_network_interface": "", "announce_ip": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, port
}

func TestARealQBittorrentFetchesAMagnetFromAnotherAndSeedsIt(t *testing.T) {
	seeder, port := liveQBit(t)
	leecher, _ := liveQBit(t)
	ctx := context.Background()

	src := t.TempDir()
	raw, hash := testenv.MakeTorrent(t, src, "Pack", map[string]int{"a.bin": 300 << 10, "sub/b.bin": 5000, "skip.nfo": 100})
	if err := seeder.Add(ctx, qbittorrent.Torrent{File: raw, SavePath: src, Folder: true}); err != nil {
		t.Fatal(err)
	}

	log := &updateLog{}
	e, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	e.UseTorrents(torrents.QBit(func(context.Context) (*qbittorrent.Client, error) { return leecher, nil }))
	dir := t.TempDir()
	magnet := "magnet:?xt=urn:btih:" + hash + "&dn=Pack&x.pe=127.0.0.1:" + strconv.Itoa(port)
	e.Start(Job{TaskID: "t", URL: magnet, Dir: dir, FileRules: torrent.FileRules{Exclude: []string{`\.nfo$`}}})

	deadline := time.Now().Add(2 * time.Minute)
	var done core.Update
	for done.Status != core.StatusDone {
		if time.Now().After(deadline) {
			t.Fatalf("not done: %+v", log.snapshot())
		}
		for _, u := range log.snapshot() {
			if u.Status == core.StatusError {
				t.Fatalf("failed: %s", u.Err)
			}
			if u.Status == core.StatusDone {
				done = u
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if done.File != filepath.Join(dir, "Pack") || done.Torrent == nil || !done.Torrent.Seeding {
		t.Errorf("done = %+v, torrent %+v", done, done.Torrent)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Pack", "sub", "b.bin"))
	if err != nil || string(got) != string(testenv.Content("sub/b.bin", 5000)) {
		t.Errorf("b.bin: %d bytes, %v", len(got), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Pack", "skip.nfo")); err == nil {
		t.Error("the file the rules left out was fetched")
	}
	list, err := leecher.Torrents(ctx, qbittorrent.Filter{Tag: torrents.Tag})
	if err != nil || len(list) != 1 || list[0].Hash != hash {
		t.Fatalf("tagged torrents %+v, %v", list, err)
	}

	e.Remove("t", true)
	if _, err := os.Stat(filepath.Join(dir, "Pack", "a.bin")); err == nil {
		t.Error("a.bin is still there after a removal with its files")
	}
	if list, _ := leecher.Torrents(ctx, qbittorrent.Filter{Hashes: []string{hash}}); len(list) != 0 {
		t.Errorf("qBittorrent still holds it: %+v", list)
	}
}

func TestARealQBittorrentSeedsAnUploadWhoseFilesAreInPlace(t *testing.T) {
	c, _ := liveQBit(t)
	log := &updateLog{}
	e, err := New(t.TempDir(), log.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	e.UseTorrents(torrents.QBit(func(context.Context) (*qbittorrent.Client, error) { return c, nil }))
	dir := t.TempDir()
	uri, root, _ := finishedTorrent(t, dir)
	e.Start(Job{TaskID: "t", URL: uri, Dir: dir, TorrentRoot: root, Seed: true, Verify: true, SeedFrom: core.TorrentStats{Uploaded: 5}})
	u := log.waitFor(t, "seeding", func(u core.Update) bool { return u.Torrent != nil && u.Torrent.Seeding })
	if u.Torrent.Uploaded != 5 {
		t.Errorf("torrent %+v", u.Torrent)
	}
}
