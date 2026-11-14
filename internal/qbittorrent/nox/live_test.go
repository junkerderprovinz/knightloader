package nox

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
)

// TestARealQBittorrentComesUpWithItsLoginAndGoesDownWithStop runs the
// qBittorrent named in KL_QBITTORRENT_NOX, which proves the settings file,
// the password hash and the shutdown against the real program.
// KL_QBITTORRENT_PROFILE keeps the profile for a look afterwards. The Windows
// build with a window asks about file associations on its first start and
// starts its WebUI only once that is answered, so its profile needs
// Win32\NeverCheckFileAssocation=true under [Preferences] ahead.
func TestARealQBittorrentComesUpWithItsLoginAndGoesDownWithStop(t *testing.T) {
	bin := os.Getenv("KL_QBITTORRENT_NOX")
	if bin == "" {
		t.Skip("KL_QBITTORRENT_NOX names no qBittorrent to run")
	}
	profile := os.Getenv("KL_QBITTORRENT_PROFILE")
	if profile == "" {
		profile = t.TempDir()
	}
	in := Start(Options{Binary: bin, Profile: profile, Logf: t.Logf})
	t.Cleanup(func() { _ = in.Stop() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := in.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st := in.Status()
	t.Logf("running qBittorrent %s on port %d", st.Version, st.Port)
	p, err := c.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range hardening {
		if p[k] != want {
			t.Errorf("%s = %v, want %v", k, p[k], want)
		}
	}
	if err := in.Stop(); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(st.Port), time.Second); err == nil {
		conn.Close()
		t.Fatal("qBittorrent still answers after Stop")
	}
}

// TestARealQBittorrentSeedsATorrentWhoseFilesAreInPlace adds a torrent made
// here from a file made here, with the check skipped, and drives it through
// the calls the torrent backend makes.
func TestARealQBittorrentSeedsATorrentWhoseFilesAreInPlace(t *testing.T) {
	bin := os.Getenv("KL_QBITTORRENT_NOX")
	if bin == "" {
		t.Skip("KL_QBITTORRENT_NOX names no qBittorrent to run")
	}
	profile := os.Getenv("KL_QBITTORRENT_PROFILE")
	if profile == "" {
		profile = t.TempDir()
	}
	in := Start(Options{Binary: bin, Profile: profile, Logf: t.Logf})
	t.Cleanup(func() { _ = in.Stop() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := in.Ready(ctx)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	content := bytes.Repeat([]byte("knightloader "), 5000)
	if err := os.WriteFile(filepath.Join(dir, "sample.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	const pieceLen = 16384
	var pieces []byte
	for off := 0; off < len(content); off += pieceLen {
		sum := sha1.Sum(content[off:min(off+pieceLen, len(content))])
		pieces = append(pieces, sum[:]...)
	}
	info := fmt.Sprintf("d6:lengthi%de4:name10:sample.bin12:piece lengthi%de6:pieces%d:%s7:privatei1ee", len(content), pieceLen, len(pieces), pieces)
	torrent := "d8:announce22:http://127.0.0.1:1/ann4:info" + info + "e"
	sum := sha1.Sum([]byte(info))
	hash := hex.EncodeToString(sum[:])

	err = c.Add(ctx, qbittorrent.Torrent{File: []byte(torrent), SavePath: dir, Tags: []string{"knightloader"}})
	if err != nil {
		t.Fatal(err)
	}
	var got qbittorrent.Info
	for deadline := time.Now().Add(30 * time.Second); ; {
		list, err := c.Torrents(ctx, qbittorrent.Filter{Tag: "knightloader"})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 && list[0].Progress == 1 {
			got = list[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("listed %+v", list)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("torrent %s is %s", got.Hash, got.State)
	if got.Hash != hash {
		t.Fatalf("qBittorrent reads hash %s, the torrent has %s", got.Hash, hash)
	}
	files, err := c.Files(ctx, hash)
	if err != nil || len(files) != 1 || files[0].Name != "sample.bin" || files[0].Size != int64(len(content)) {
		t.Fatalf("Files = %+v, %v", files, err)
	}
	states, err := c.PieceStates(ctx, hash)
	if err != nil || len(states) != len(pieces)/20 || slices.ContainsFunc(states, func(s int) bool { return s != 2 }) {
		t.Fatalf("PieceStates = %v, %v", states, err)
	}
	if err := c.Stop(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, false, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sample.bin")); err != nil {
		t.Fatalf("deleting the torrent without its files took the file: %v", err)
	}
}
