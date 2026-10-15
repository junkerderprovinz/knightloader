package vpnconn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/netbind"
	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/nordvpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/pia"
	"github.com/junkerderprovinz/knightloader/internal/vpn/torguard"
	"github.com/junkerderprovinz/knightloader/internal/vpn/vpntest"
)

// memStore is a credential store that keeps the logins in memory.
type memStore struct {
	mu    sync.Mutex
	creds map[string]accounts.Credential
}

func (s *memStore) GetCredential(_, account string) (accounts.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.creds[account]
	if !ok {
		return c, errors.New("not stored")
	}
	return c, nil
}

func (s *memStore) SetCredential(_, account string, c accounts.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds == nil {
		s.creds = map[string]accounts.Credential{}
	}
	if c.IsZero() {
		delete(s.creds, account)
	} else {
		s.creds[account] = c
	}
	return nil
}

func (s *memStore) AccountIDs(string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id := range s.creds {
		ids = append(ids, id)
	}
	return ids
}

type harness struct {
	m       *Manager
	store   *memStore
	path    string
	changes atomic.Int32
	manual  atomic.Int32
}

func newHarness(t *testing.T, store *memStore, path string, tune ...func(*Options)) *harness {
	t.Helper()
	h := &harness{store: store, path: path}
	opts := Options{
		Store:          store,
		StatePath:      path,
		ManualPort:     func() int { return int(h.manual.Load()) },
		OnChange:       func() { h.changes.Add(1) },
		Logf:           t.Logf,
		ExitIPURL:      "http://" + vpntest.ServerAddr.String() + "/",
		ConnectTimeout: 5 * time.Second,
		Tunnel:         vpn.Options{Poll: 50 * time.Millisecond, HandshakeTimeout: 500 * time.Millisecond},
	}
	for _, f := range tune {
		f(&opts)
	}
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	h.m = m
	t.Cleanup(m.Close)
	return h
}

func (h *harness) provider(id string) ProviderState {
	for _, p := range h.m.State().Providers {
		if p.ID == id {
			return p
		}
	}
	return ProviderState{}
}

func (h *harness) waitFor(t *testing.T, id, state string) ProviderState {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		p := h.provider(id)
		if p.State == state {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is %s (%s %s), want %s", id, p.State, p.ErrorCode, p.Error, state)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTheCatalogueIsTheTilesList(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "vpnProviders.ts"))
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(src), "VPN_APP_ONLY")
	re := regexp.MustCompile(`id: '([^']+)',\s*name: '([^']*)',\s*fields: \[([^\]]*)\],\s*portForwarding: (true|false)`)
	var tiles []Provider
	for _, m := range re.FindAllStringSubmatch(head, -1) {
		var fields []string
		for _, f := range strings.Split(m[3], ",") {
			if f = strings.Trim(strings.TrimSpace(f), "'"); f != "" {
				fields = append(fields, f)
			}
		}
		tiles = append(tiles, Provider{ID: m[1], Name: m[2], Fields: fields, PortForwarding: m[4] == "true"})
	}
	ours := catalogue()
	if len(tiles) != len(ours) {
		t.Fatalf("the page lists %d providers, the catalogue %d", len(tiles), len(ours))
	}
	for i, p := range ours {
		w := tiles[i]
		if p.ID != w.ID || p.Name != w.Name || !slices.Equal(p.Fields, w.Fields) || p.PortForwarding != w.PortForwarding {
			t.Errorf("provider %d is %s %q %v %v here and %s %q %v %v on the page", i, p.ID, p.Name, p.Fields, p.PortForwarding, w.ID, w.Name, w.Fields, w.PortForwarding)
		}
		if p.PortForwarding && p.open != nil && p.port == noPort {
			t.Errorf("%s forwards a port and nothing follows it", p.ID)
		}
	}
}

func TestProtonCarriesTorrentsAndFollowsItsForwardedPort(t *testing.T) {
	srv := vpntest.Start(t)
	srv.SetLifetime(2 * time.Second)
	h := newHarness(t, &memStore{}, filepath.Join(t.TempDir(), "vpn.json"))
	st, err := h.m.Connect("protonvpn", map[string]string{"config": srv.Config()})
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "protonvpn" {
		t.Fatalf("active %q after connect", st.Active)
	}
	p := h.waitFor(t, "protonvpn", "connected")
	deadline := time.Now().Add(10 * time.Second)
	for (p.ForwardedPort == 0 || p.ExitIP == "" || p.ForwardedAddress == "") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		p = h.provider("protonvpn")
	}
	if p.ForwardedPort != 51234 || h.m.ForwardedPort() != 51234 {
		t.Errorf("forwarded port %d (%d), want the gateway's 51234", p.ForwardedPort, h.m.ForwardedPort())
	}
	if p.ExitIP != vpntest.ExitAddr {
		t.Errorf("exit address %q, want %q", p.ExitIP, vpntest.ExitAddr)
	}
	if p.ForwardedAddress != vpntest.MappedAddr.String() {
		t.Errorf("forwarded port reached at %q, want the gateway's %v", p.ForwardedAddress, vpntest.MappedAddr)
	}
	if !p.Configured {
		t.Error("the login is not stored")
	}

	b := netbind.New()
	b.SetInterface(NetworkName)
	defer b.SetInterface("")
	c, err := b.DialContext(context.Background(), "tcp4", "10.2.0.1:7")
	if err != nil {
		t.Fatalf("a torrent socket cannot reach through the tunnel: %v", err)
	}
	c.Close()

	srv.SetMappedPort(40000)
	deadline = time.Now().Add(10 * time.Second)
	for h.m.ForwardedPort() != 40000 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if got := h.m.ForwardedPort(); got != 40000 {
		t.Errorf("forwarded port %d after the gateway moved it, want 40000", got)
	}

	before := h.changes.Load()
	st = h.m.Disconnect()
	if st.Active != "" || h.changes.Load() == before {
		t.Errorf("active %q after disconnect, changes heard %d", st.Active, h.changes.Load()-before)
	}
	if _, err := b.DialContext(context.Background(), "tcp4", "10.2.0.1:7"); !errors.Is(err, netbind.ErrDown) {
		t.Errorf("dial after disconnect = %v, want ErrDown", err)
	}
}

func TestAStoredLoginConnectsWithEmptyFields(t *testing.T) {
	srv := vpntest.Start(t)
	h := newHarness(t, &memStore{}, "")
	if _, err := h.m.Connect("custom", map[string]string{"config": srv.Config()}); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "custom", "connected")
	h.m.Disconnect()
	if _, err := h.m.Connect("custom", map[string]string{"config": ""}); err != nil {
		t.Fatalf("connect on the stored login: %v", err)
	}
	h.waitFor(t, "custom", "connected")
}

func TestAnOwnConfigReportsTheTorrentPortAsForwarded(t *testing.T) {
	srv := vpntest.Start(t)
	h := newHarness(t, &memStore{}, "")
	h.manual.Store(6881)
	h.m.Connect("custom", map[string]string{"config": srv.Config()})
	p := h.waitFor(t, "custom", "connected")
	if p.ForwardedPort != 6881 || h.m.ForwardedPort() != 0 {
		t.Errorf("forwarded port %d shown, %d for the torrent client; want 6881 and 0", p.ForwardedPort, h.m.ForwardedPort())
	}
	if p.ForwardedAddress != "" {
		t.Errorf("forwarded port reached at %q, want it left to the exit address", p.ForwardedAddress)
	}
	if srv.Mappings() != 0 {
		t.Error("an own config was asked about NAT-PMP")
	}
}

func TestRefusals(t *testing.T) {
	h := newHarness(t, &memStore{}, "")
	h.m.providers = append(h.m.providers, Provider{ID: "soon", Name: "Soon", Fields: []string{"token"}})
	cases := []struct {
		id     string
		fields map[string]string
		status int
		code   string
		field  string
	}{
		{"nowhere", nil, http.StatusNotFound, "vpn-unknown", ""},
		{"soon", map[string]string{"token": "x"}, http.StatusNotImplemented, "vpn-unsupported", ""},
		{"protonvpn", nil, http.StatusBadRequest, "vpn-field-missing", "config"},
		{"hideme", map[string]string{"username": "knight"}, http.StatusBadRequest, "vpn-field-missing", "password"},
		{"protonvpn", map[string]string{"password": "x"}, http.StatusBadRequest, "vpn-field-unknown", "password"},
	}
	for _, c := range cases {
		_, err := h.m.Connect(c.id, c.fields)
		var r *Refusal
		if !errors.As(err, &r) || r.Status != c.status || r.Code != c.code || r.Field != c.field {
			t.Errorf("connect %s %v = %v, want %d %s %s", c.id, c.fields, err, c.status, c.code, c.field)
		}
	}
	if h.m.Active() != "" {
		t.Errorf("a refused connect left %q in use", h.m.Active())
	}
}

func TestFailuresCarryTheirCode(t *testing.T) {
	srv := vpntest.Start(t)
	h := newHarness(t, &memStore{}, "")
	h.m.Connect("custom", map[string]string{"config": "not a config"})
	if p := h.waitFor(t, "custom", "failed"); p.ErrorCode != CodeConfigInvalid {
		t.Errorf("a broken config failed with %q", p.ErrorCode)
	}

	srv.Close()
	h.m.Connect("custom", map[string]string{"config": srv.Config()})
	if p := h.waitFor(t, "custom", "failed"); p.ErrorCode != CodeTimeout {
		t.Errorf("a server that does not answer failed with %q", p.ErrorCode)
	}
}

func TestAnEndpointThatDoesNotResolveYetIsTriedAgain(t *testing.T) {
	srv := vpntest.Start(t)
	var resolves atomic.Bool
	h := newHarness(t, &memStore{}, "", func(o *Options) {
		o.RenewAfter = 200 * time.Millisecond
		o.Tunnel.LookupNetIP = func(_ context.Context, _, host string) ([]netip.Addr, error) {
			if !resolves.Load() {
				return nil, &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
	})
	cfg := strings.Replace(srv.Config(), "Endpoint = 127.0.0.1:", "Endpoint = vpn.example.test:", 1)
	if _, err := h.m.Connect("custom", map[string]string{"config": cfg}); err != nil {
		t.Fatal(err)
	}
	if p := h.waitFor(t, "custom", "failed"); p.ErrorCode != CodeTimeout {
		t.Errorf("a name that did not resolve failed with %q", p.ErrorCode)
	}
	resolves.Store(true)
	h.waitFor(t, "custom", "connected")
}

func TestForgetDisconnectsAndDropsTheLogin(t *testing.T) {
	srv := vpntest.Start(t)
	store := &memStore{}
	h := newHarness(t, store, "")
	h.m.Connect("custom", map[string]string{"config": srv.Config()})
	h.waitFor(t, "custom", "connecting")
	st, err := h.m.Forget("custom")
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "" || h.provider("custom").Configured || len(store.AccountIDs(Service)) != 0 {
		t.Errorf("after forget: active %q, stored %v", st.Active, store.AccountIDs(Service))
	}
}

func TestResumeTakesUpTheProviderInUse(t *testing.T) {
	srv := vpntest.Start(t)
	store := &memStore{}
	path := filepath.Join(t.TempDir(), "vpn.json")
	first := newHarness(t, store, path)
	first.m.Connect("custom", map[string]string{"config": srv.Config()})
	first.waitFor(t, "custom", "connected")
	first.m.Close()

	var kept struct{ Active string }
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &kept)
	if kept.Active != "custom" {
		t.Fatalf("kept %q", b)
	}
	second := newHarness(t, store, path)
	second.m.Resume()
	second.waitFor(t, "custom", "connected")
}

func TestRoutesAreKeptAndCountOnlyWhileAProviderIsInUse(t *testing.T) {
	srv := vpntest.Start(t)
	store := &memStore{}
	path := filepath.Join(t.TempDir(), "vpn.json")
	h := newHarness(t, store, path)
	if st := h.m.State(); st.Routes != DefaultRoutes {
		t.Fatalf("routes %+v at first, want torrents only", st.Routes)
	}
	h.m.SetRoutes(Routes{Torrents: true, Usenet: true})
	if h.m.Through(Usenet) {
		t.Error("Usenet goes through the VPN with no provider in use")
	}
	h.m.Connect("custom", map[string]string{"config": srv.Config()})
	if !h.m.Through(Usenet) || h.m.Through(Debrid) {
		t.Error("the switches do not hold while a provider is in use")
	}
	h.m.Close()

	again := newHarness(t, store, path)
	if r := again.m.State().Routes; r != (Routes{Torrents: true, Usenet: true}) {
		t.Errorf("routes %+v after a restart", r)
	}
}

func TestADialWaitsWhileTheTunnelIsDown(t *testing.T) {
	h := newHarness(t, &memStore{}, "")
	if _, err := h.m.DialContext(context.Background(), "tcp", "10.2.0.1:7"); !errors.Is(err, ErrNoVPN) {
		t.Errorf("dial with no provider = %v, want ErrNoVPN", err)
	}
	srv := vpntest.Start(t)
	srv.Close()
	h.m.Connect("custom", map[string]string{"config": srv.Config()})
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	if _, err := h.m.DialContext(ctx, "tcp", "10.2.0.1:7"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("dial with the tunnel down = %v, want it held until the deadline", err)
	}
}

func TestCryptostormIsAskedOnceForTheTorrentPort(t *testing.T) {
	srv := vpntest.Start(t)
	defer func(u string) { fwdURL = u }(fwdURL)
	fwdURL = "http://" + vpntest.ServerAddr.String() + "/fwd"
	h := newHarness(t, &memStore{}, "")
	h.manual.Store(31337)
	h.m.Connect("cryptostorm", map[string]string{"config": srv.Config()})
	h.waitFor(t, "cryptostorm", "connected")
	waitUntil := time.Now().Add(10 * time.Second)
	for h.m.ForwardedPort() == 0 && time.Now().Before(waitUntil) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := h.m.ForwardedPort(); got != 31337 {
		t.Errorf("forwarded port %d, want the torrent port 31337", got)
	}
	if f := srv.Forwards(); !slices.Equal(f, []string{"31337"}) {
		t.Errorf("the server was asked for %v", f)
	}
}

func TestAPortOutsideCryptostormsRangeGetsOneInIt(t *testing.T) {
	for _, manual := range []int{0, 6881, 70000} {
		if p := fwdPortFor(manual); p < fwdLow {
			t.Errorf("port %d for %d", p, manual)
		}
	}
	if p := fwdPortFor(40000); p != 40000 {
		t.Errorf("port %d for 40000", p)
	}
}

func TestASessionsKeepForwardsThePortWhileTheTunnelStands(t *testing.T) {
	srv := vpntest.Start(t)
	h := newHarness(t, &memStore{}, "")
	var kept atomic.Int32
	open := func(context.Context, Login) (*session, error) {
		cfg, err := vpn.ParseConfig(srv.Config())
		if err != nil {
			return nil, err
		}
		return &session{cfg: cfg, keep: func(ctx context.Context, k keeper) {
			kept.Add(1)
			k.setPort(42424)
			<-ctx.Done()
		}}, nil
	}
	h.m.providers = append(h.m.providers, Provider{ID: "keeper", Name: "Keeper", Fields: []string{"token"}, PortForwarding: true, open: open, port: apiPort})
	h.m.Connect("keeper", map[string]string{"token": "x"})
	p := h.waitFor(t, "keeper", "connected")
	for deadline := time.Now().Add(5 * time.Second); p.ForwardedPort == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		p = h.provider("keeper")
	}
	if p.ForwardedPort != 42424 || h.m.ForwardedPort() != 42424 {
		t.Errorf("forwarded port %d (%d), want the one keep set", p.ForwardedPort, h.m.ForwardedPort())
	}
	h.m.Disconnect()
	if h.m.ForwardedPort() != 0 || kept.Load() != 1 {
		t.Errorf("after disconnect: port %d, keep ran %d times", h.m.ForwardedPort(), kept.Load())
	}
}

func TestConnectsAtOnceLeaveNoRunBehindADisconnect(t *testing.T) {
	srv := vpntest.Start(t)
	h := newHarness(t, &memStore{}, "")
	var opened atomic.Int32
	open := func(context.Context, Login) (*session, error) {
		opened.Add(1)
		cfg, err := vpn.ParseConfig(srv.Config())
		return &session{cfg: cfg}, err
	}
	h.m.providers = append(h.m.providers, Provider{ID: "twice", Name: "Twice", Fields: []string{"token"}, open: open})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.m.Connect("twice", map[string]string{"token": "x"})
		}()
	}
	wg.Wait()
	h.m.Disconnect()
	before := opened.Load()
	time.Sleep(time.Second)
	if st := h.m.tun.State().Status; st != vpn.Disconnected || opened.Load() != before {
		t.Errorf("after the disconnect the tunnel is %s and %d sessions were opened", st, opened.Load()-before)
	}
}

func TestAPortRequestThatFailedIsSentAgain(t *testing.T) {
	defer func(d time.Duration) { portRetry = d }(portRetry)
	portRetry = 50 * time.Millisecond
	srv := vpntest.Start(t)
	h := newHarness(t, &memStore{}, "")
	var asked atomic.Int32
	open := func(context.Context, Login) (*session, error) {
		cfg, err := vpn.ParseConfig(srv.Config())
		if err != nil {
			return nil, err
		}
		return &session{cfg: cfg, askPort: func(context.Context, dialFunc) (uint16, error) {
			if asked.Add(1) == 1 {
				return 0, errors.New("the API did not answer")
			}
			return 42424, nil
		}}, nil
	}
	h.m.providers = append(h.m.providers, Provider{ID: "asker", Name: "Asker", Fields: []string{"token"}, PortForwarding: true, open: open, port: apiPort})
	h.m.Connect("asker", map[string]string{"token": "x"})
	h.waitFor(t, "asker", "connected")
	for deadline := time.Now().Add(5 * time.Second); h.m.ForwardedPort() == 0 && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.m.ForwardedPort(); got != 42424 || asked.Load() != 2 {
		t.Errorf("forwarded port %d after %d requests, want 42424 after 2", got, asked.Load())
	}
}

func TestAProvidersAPIErrorsCarryTheirCode(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{fmt.Errorf("pia: token: %w", pia.ErrRefused), CodeLoginRefused},
		{nordvpn.ErrRefused, CodeLoginRefused},
		{torguard.ErrRefused, CodeLoginRefused},
		{fmt.Errorf("nordvpn: %q: %w", "atlantis", vpn.ErrUnknownLocation), CodeLocationUnknown},
	}
	for _, c := range cases {
		var f *failure
		if !errors.As(apiFailure(c.err), &f) || f.code != c.code {
			t.Errorf("%v has code %v, want %s", c.err, f, c.code)
		}
	}
	if err := errors.New("network down"); apiFailure(err) != err {
		t.Error("an error of the network got a code")
	}
}

func TestTorGuardLocations(t *testing.T) {
	for loc, want := range map[string]string{
		"":                   "nl.secureconnect.me",
		"ger":                "ger.secureconnect.me",
		"US-NY":              "us-ny.secureconnect.me",
		"swiss.torguard.org": "swiss.torguard.org",
		"146.70.83.218":      "146.70.83.218",
	} {
		if got := torGuardHost(loc); got != want {
			t.Errorf("%q is %q, want %q", loc, got, want)
		}
	}
}
