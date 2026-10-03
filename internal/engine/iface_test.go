package engine

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/netbind"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestAMagnetWaitsOutAnInterfaceThatIsDownAndThenGetsItsWholeTimeout(t *testing.T) {
	const timeout = 2 * time.Minute
	now := time.Now()
	cases := []struct {
		name string
		st   netbind.State
		want time.Duration
	}{
		{"any interface", netbind.State{Up: true, Since: now.Add(-time.Second)}, 0},
		{"interface down", netbind.State{Interface: "wg0", Since: now.Add(-time.Hour)}, timeout},
		{"interface up for long", netbind.State{Interface: "wg0", Up: true, Since: now.Add(-time.Hour)}, 0},
		{"interface back a minute ago", netbind.State{Interface: "wg0", Up: true, Since: now.Add(-time.Minute)}, time.Minute},
	}
	for _, c := range cases {
		if got := metadataWaitLeft(c.st, timeout, now); got != c.want {
			t.Errorf("%s: waits %s more, want %s", c.name, got, c.want)
		}
	}
}

func TestAnInterfaceThatIsNotThereIsDownAndAnyIsUp(t *testing.T) {
	if InterfaceUp("kl-no-such-interface0") {
		t.Error("an interface the system does not have is up")
	}
	if !InterfaceUp("") {
		t.Error("any interface is down")
	}
	if name := loopbackName(t); !InterfaceUp(name) {
		t.Errorf("loopback %s is down", name)
	}
}

func loopbackName(t *testing.T) string {
	t.Helper()
	ifs, err := NetInterfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ni := range ifs {
		for _, a := range ni.Addrs {
			if ip := net.ParseIP(a); ip != nil && ip.Equal(net.IPv4(127, 0, 0, 1)) {
				return ni.Name
			}
		}
	}
	t.Skip("no interface holds 127.0.0.1")
	return ""
}

// loaded is the latest byte count the task reported.
func (b *byTask) loaded(id string) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var n int64
	for _, u := range b.m[id] {
		if u.Loaded > 0 {
			n = u.Loaded
		}
	}
	return n
}

// A torrent tied to loopback, where its seeder is, stops moving the moment the
// interface it is tied to is missing, without failing, and finishes once the
// interface is there again.
func TestATorrentStopsWhileItsInterfaceIsMissingAndFinishesOnceItIsBack(t *testing.T) {
	requireTorrentClient(t)
	lo := loopbackName(t)
	_, magnet := seedTorrentAt(t, "Film", []seedFile{{"Film.mkv", 384 << 10}}, 32<<10)
	dir, err := os.MkdirTemp("", "kl-bt-iface-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b := &byTask{}
	e, err := New(dir, b.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	tie := func(name string) {
		t.Helper()
		if err := e.SetTorrentConfig(TorrentConfig{Interface: name}); err != nil {
			t.Fatal(err)
		}
	}
	tie(lo)
	t.Cleanup(func() { tie("") })
	e.SetMetadataTimeout(30 * time.Second)

	e.Start(Job{TaskID: "film", URL: magnet, Dir: dir})
	deadline := time.Now().Add(30 * time.Second)
	for b.loaded("film") == 0 {
		if status, _, errText := b.last("film"); status == core.StatusError || status == core.StatusDone || time.Now().After(deadline) {
			t.Fatalf("the torrent is %q (%s) before the cut, want it running", status, errText)
		}
		time.Sleep(100 * time.Millisecond)
	}

	tie("kl-no-such-interface0")
	if st := netbind.Default.State(); st.Up {
		t.Fatalf("the client's binding is %+v with the interface missing, want down", st)
	}
	// What was in flight lands within a progress tick or two.
	time.Sleep(time.Second)
	held := b.loaded("film")
	time.Sleep(3 * time.Second)
	if now := b.loaded("film"); now != held {
		t.Fatalf("the torrent went from %d to %d bytes with its interface missing", held, now)
	}
	if status, _, errText := b.last("film"); status != core.StatusRunning {
		t.Fatalf("the torrent is %q (%s) with its interface missing, want it waiting as running", status, errText)
	}

	tie(lo)
	waitDone(t, b, "film")
}

// A magnet started while its interface is missing reaches no peer, and waits
// rather than failing when its metadata timeout runs out, with a UDP tracker
// as without one.
func TestAMagnetDoesNotFailWhileItsInterfaceIsMissing(t *testing.T) {
	requireTorrentClient(t)
	_, magnet := seedTorrent(t, "Film", []seedFile{{"Film.mkv", 48 << 10}})
	_, tracked := seedTorrent(t, "Clip", []seedFile{{"Clip.mkv", 48 << 10}})
	dir, err := os.MkdirTemp("", "kl-bt-iface-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b := &byTask{}
	e, err := New(dir, b.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	if err := e.SetTorrentConfig(TorrentConfig{Interface: "kl-no-such-interface0"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.SetTorrentConfig(TorrentConfig{}) })
	e.SetMetadataTimeout(time.Second)

	e.Start(Job{TaskID: "held", URL: magnet, Dir: dir})
	e.Start(Job{TaskID: "udp", URL: tracked + "&tr=udp%3A%2F%2F127.0.0.1%3A1%2Fannounce", Dir: dir})
	time.Sleep(4 * time.Second)
	for _, id := range []string{"held", "udp"} {
		switch status, _, errText := b.last(id); status {
		case core.StatusError:
			t.Fatalf("%s: the magnet failed while its interface was missing: %s", id, errText)
		case core.StatusRunning, core.StatusDone:
			t.Fatalf("%s: the magnet is %q with its interface missing; a peer sent its file list", id, status)
		}
	}
}
