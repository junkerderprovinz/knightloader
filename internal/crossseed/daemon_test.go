package crossseed

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeDaemon answers /api/webhook and /api/ping like cross-seed, and keeps
// what it was handed: an info hash or a content path, never both, since
// cross-seed refuses a webhook that names both.
type fakeDaemon struct {
	mu     sync.Mutex
	hashes []string
	paths  []string
}

func (d *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/ping" && r.Method == http.MethodGet:
		w.Write([]byte("OK"))
	case r.URL.Path == "/api/webhook" && r.Method == http.MethodPost:
		if r.Header.Get("X-Api-Key") != "daemon-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		hash, path := r.PostFormValue("infoHash"), r.PostFormValue("path")
		if (hash == "") == (path == "") {
			http.Error(w, "A valid infoHash or an accessible path must be provided", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		d.hashes = append(d.hashes, hash)
		d.paths = append(d.paths, path)
		d.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (d *fakeDaemon) handed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.hashes...)
}

func (d *fakeDaemon) handedPaths() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.paths...)
}

func daemonAt(t *testing.T, folder string) (*fakeDaemon, *Daemon) {
	t.Helper()
	d := &fakeDaemon{}
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	return d, NewDaemon(srv.URL+"/", "daemon-key", folder)
}

func TestWithoutASharedFolderTheDaemonIsHandedThePathAlone(t *testing.T) {
	d, daemon := daemonAt(t, "")
	src := Source{Hash: "abcd", Name: "Album", Torrent: redTorrent("RED"), Dir: "/data"}
	if err := daemon.Hand(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if got := d.handedPaths(); len(got) != 1 || got[0] != filepath.Join("/data", "Album") || d.handed()[0] != "" {
		t.Fatalf("the daemon was handed the hashes %q and the paths %q", d.handed(), got)
	}
}

func TestWithASharedFolderTheDaemonFindsTheTorrentItselfByItsHash(t *testing.T) {
	folder := t.TempDir()
	d, daemon := daemonAt(t, folder)
	src := redSource(t)
	if err := daemon.Hand(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if got := d.handed(); len(got) != 1 || got[0] != src.Hash || d.handedPaths()[0] != "" {
		t.Fatalf("the daemon was handed the hashes %q and the paths %q", got, d.handedPaths())
	}
	kept := filepath.Join(folder, "torrents", src.Hash+".torrent")
	if b, err := os.ReadFile(kept); err != nil || !bytes.Equal(b, src.Torrent) {
		t.Fatalf("the torrent is not in the shared folder: %v", err)
	}
	if err := daemon.Forget(src.Hash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("the torrent stayed in the shared folder once forgotten: %v", err)
	}
}

func TestAMagnetGoesToTheDaemonByItsPathEvenWithASharedFolder(t *testing.T) {
	d, daemon := daemonAt(t, t.TempDir())
	src := Source{Hash: "abcd", Name: "Album", Files: []FileEntry{{"Album/01.flac", 3}}, Dir: "/data"}
	if err := daemon.Hand(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if got := d.handedPaths(); len(got) != 1 || got[0] != filepath.Join("/data", "Album") {
		t.Fatalf("the daemon was handed the paths %q", got)
	}
}

func TestADaemonThatRefusesTheKeySaysSo(t *testing.T) {
	srv := httptest.NewServer(&fakeDaemon{})
	defer srv.Close()
	err := NewDaemon(srv.URL, "wrong", "").Hand(context.Background(), Source{Hash: "abcd", Name: "Album", Dir: "/data"})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheDaemonAnswersAPing(t *testing.T) {
	srv := httptest.NewServer(&fakeDaemon{})
	defer srv.Close()
	if err := NewDaemon(srv.URL, "daemon-key", "").Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := NewDaemon(srv.URL+"/nothing", "daemon-key", "").Ping(context.Background()); err == nil {
		t.Fatal("an address without a daemon answered the ping")
	}
}
