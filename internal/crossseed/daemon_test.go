package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeDaemon answers /api/webhook and /api/ping like cross-seed, and keeps
// the info hashes it was handed, each with the content path beside it.
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
		d.mu.Lock()
		d.hashes = append(d.hashes, r.PostFormValue("infoHash"))
		d.paths = append(d.paths, r.PostFormValue("path"))
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

func TestTheDaemonIsHandedTheInfoHashAndTheFilesWithItsKey(t *testing.T) {
	d := &fakeDaemon{}
	srv := httptest.NewServer(d)
	defer srv.Close()
	if err := NewDaemon(srv.URL+"/", "daemon-key").Announce(context.Background(), "abcd", "/data/Album"); err != nil {
		t.Fatal(err)
	}
	if got := d.handed(); len(got) != 1 || got[0] != "abcd" {
		t.Fatalf("the daemon was handed %v", got)
	}
	if d.paths[0] != "/data/Album" {
		t.Errorf("the daemon was handed the path %q", d.paths[0])
	}
}

func TestADaemonThatRefusesTheKeySaysSo(t *testing.T) {
	srv := httptest.NewServer(&fakeDaemon{})
	defer srv.Close()
	err := NewDaemon(srv.URL, "wrong").Announce(context.Background(), "abcd", "/data/Album")
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheDaemonAnswersAPing(t *testing.T) {
	srv := httptest.NewServer(&fakeDaemon{})
	defer srv.Close()
	if err := NewDaemon(srv.URL, "daemon-key").Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := NewDaemon(srv.URL+"/nothing", "daemon-key").Ping(context.Background()); err == nil {
		t.Fatal("an address without a daemon answered the ping")
	}
}
