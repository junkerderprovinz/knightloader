package testenv

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	anacrolix "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// SeedTorrent writes files, by their paths inside the torrent and their sizes,
// into a folder named name, builds the torrent for it and seeds it from a
// client that listens on loopback only. The magnet it returns names that
// client as a peer, so a download needs neither a tracker nor the DHT.
func SeedTorrent(t *testing.T, name string, files map[string]int) (hash, magnet string) {
	t.Helper()
	root, err := os.MkdirTemp("", "kl-seeder-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	for p, size := range files {
		full := filepath.Join(root, name, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, bytes.Repeat([]byte(p), size/len(p)+1)[:size], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib}

	cfg := anacrolix.NewDefaultClientConfig()
	cfg.DataDir = root
	cfg.Seed = true
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = true
	cfg.ListenHost = func(string) string { return "127.0.0.1" }
	// A test may seed more than one torrent, and the library's default port
	// is fixed.
	cfg.ListenPort = 0
	cl, err := anacrolix.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	tor, err := cl.AddTorrent(&mi)
	if err != nil {
		t.Fatal(err)
	}
	if err := tor.VerifyData(); err != nil {
		t.Fatal(err)
	}
	m := mi.Magnet(nil, &info)
	m.Params.Set("x.pe", fmt.Sprintf("127.0.0.1:%d", cl.LocalPort()))
	return mi.HashInfoBytes().HexString(), m.String()
}
