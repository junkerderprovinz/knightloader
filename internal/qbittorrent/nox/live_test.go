package nox

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
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
