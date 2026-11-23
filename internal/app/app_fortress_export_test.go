package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/lan"
	"github.com/junkerderprovinz/parleyport/kit/member"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

var vault = kitfortress.Caller{ID: "m-vault", App: "bombvault", Device: "Tower"}

// readExport reads a whole export and checks it is as long as announced.
func readExport(t *testing.T, a *App) []byte {
	t.Helper()
	r, n, err := a.FortressExport(context.Background(), vault)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if int64(len(raw)) != n {
		t.Fatalf("the export announced %d bytes and held %d", n, len(raw))
	}
	return raw
}

func TestTheFortressExportIsTheBackupARestoreAccepts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	raw := readExport(t, a)
	if _, err := backup.Stage(t.TempDir(), raw, buildinfo.Version, nil); err != nil {
		t.Errorf("a restore refuses the export: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, backupSnapshotPrefix+"*")); len(left) != 0 {
		t.Errorf("the export left %v in the data directory", left)
	}
}

func TestWithAnAppKeyTheFortressExportIsSealed(t *testing.T) {
	t.Parallel()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	key, err := appkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	a.AppKey = key

	raw := readExport(t, a)
	if _, err := backup.Unseal(raw, nil); err == nil {
		t.Fatal("the export opens without the app key")
	}
	archive, err := backup.Unseal(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(t.TempDir(), archive, buildinfo.Version, nil); err != nil {
		t.Errorf("a restore with the key refuses the export: %v", err)
	}
}

// An export nobody reads to the end still goes once it is closed.
func TestAnExportClosedHalfwayLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	r, _, err := a.FortressExport(context.Background(), vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(r, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, backupSnapshotPrefix+"*")); len(left) != 0 {
		t.Errorf("the export left %v in the data directory", left)
	}
}

type memStore struct {
	mu   sync.Mutex
	data []byte
}

func (s *memStore) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.data), nil
}

func (s *memStore) Save(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = bytes.Clone(b)
	return nil
}

// onTheNetwork is a network announce service on a multicast group of the
// test's own, or a skip on a host that cannot join one.
func onTheNetwork(t *testing.T, port int) *lan.Service {
	t.Helper()
	addr := netip.AddrPortFrom(netip.MustParseAddr("239.255.77.251"), uint16(port))
	probe, err := net.ListenMulticastUDP("udp4", nil, net.UDPAddrFromAddrPort(addr))
	if err != nil {
		t.Skipf("this host cannot join a multicast group: %v", err)
	}
	_ = probe.Close()
	s := lan.New(lan.Config{Group: addr})
	s.Start()
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// openFortress opens app on device in a fortress of the test's phrase, met
// only on the network the service announces on.
func openFortress(t *testing.T, network *lan.Service, app, device string, edit func(*kitfortress.Config)) (*kitfortress.Fortress, string) {
	t.Helper()
	seed := sha256.Sum256([]byte(device))
	id, err := member.FromSeed(member.KindInstance, seed[:])
	if err != nil {
		t.Fatal(err)
	}
	var current atomic.Pointer[kitfortress.Fortress]
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f := current.Load(); f != nil && r.URL.Path == group.DirectPath {
			f.DirectHandler().ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(direct.Close)
	cfg := kitfortress.Config{
		App:      kitfortress.App{ID: app, Name: app, Version: "1.0.0"},
		Secret:   []byte("a phrase both hold"),
		Identity: id,
		Self:     group.Self{Name: device, Deployment: "container", DirectURL: direct.URL},
		Relay:    group.RelayOff,
		LAN:      network,
		Store:    &memStore{},
	}
	edit(&cfg)
	f, err := kitfortress.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current.Store(f)
	t.Cleanup(func() { _ = f.Close() })
	return f, id.ID()
}

func TestABackupAppOnTheSameNetworkFetchesTheExport(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	port := freeUDPPort(t)
	_, loaderID := openFortress(t, onTheNetwork(t, port), "knightloader", "Attic", func(c *kitfortress.Config) { c.Export = a.FortressExport })
	keeper, _ := openFortress(t, onTheNetwork(t, port), "bombvault", "Tower", func(c *kitfortress.Config) { c.Uses = []string{kitfortress.ExportAction} })

	deadline := time.Now().Add(20 * time.Second)
	for !knowsApp(keeper, loaderID, "knightloader") {
		if time.Now().After(deadline) {
			t.Fatal("the backup app never learned that the downloader runs knightloader")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := keeper.Export(ctx, loaderID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Stage(t.TempDir(), raw, buildinfo.Version, nil); err != nil {
		t.Errorf("a restore refuses what the backup app fetched: %v", err)
	}
}

func knowsApp(f *kitfortress.Fortress, id, app string) bool {
	for _, m := range f.Members() {
		if m.ID == id && m.App.ID == app {
			return true
		}
	}
	return false
}
