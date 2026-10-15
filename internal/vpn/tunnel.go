package vpn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GopeedLab/gopeed/pkg/netbind"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
)

// Status is where a Tunnel stands.
type Status string

const (
	// Disconnected means there is no tunnel.
	Disconnected Status = "disconnected"
	// Connecting means the tunnel is set up and has not had a handshake yet.
	Connecting Status = "connecting"
	// Connected means the last handshake is recent enough for WireGuard to
	// send. Only then does traffic flow.
	Connected Status = "connected"
	// Reconnecting means the handshake went stale and the tunnel is trying
	// again.
	Reconnecting Status = "reconnecting"
)

// State is a snapshot of a Tunnel.
type State struct {
	Status Status `json:"status"`
	// Since is when Status last changed.
	Since         time.Time      `json:"since"`
	Endpoint      netip.AddrPort `json:"endpoint"`
	Addrs         []netip.Addr   `json:"addrs"`
	LastHandshake time.Time      `json:"lastHandshake"`
	RxBytes       uint64         `json:"rxBytes"`
	TxBytes       uint64         `json:"txBytes"`
	// Reconnects counts the attempts since the last handshake.
	Reconnects int `json:"reconnects"`
	// Err is the last error of setting the tunnel up or getting it back.
	Err string `json:"err,omitempty"`
}

// Options tune a Tunnel. The zero value is what a real connection wants.
type Options struct {
	// Refresh, when set, gets a fresh configuration before every reconnect,
	// for a provider whose sessions end, such as hide.me.
	Refresh func(ctx context.Context) (Config, error)
	// LookupNetIP resolves the endpoint's host, the system's resolver when
	// nil. It runs outside the tunnel, which is not there yet.
	LookupNetIP func(ctx context.Context, network, host string) ([]netip.Addr, error)
	// Logf receives WireGuard's errors and the tunnel's own events.
	Logf func(format string, args ...any)

	// Poll is how often the handshake is looked at.
	Poll time.Duration
	// StaleAfter is how old the last handshake may be before the tunnel
	// counts as down. WireGuard stops sending on a session after three
	// minutes, and a keepalive renews it every two.
	StaleAfter time.Duration
	// HandshakeTimeout is how long a new or renewed peer gets for its first
	// handshake before the tunnel tries again.
	HandshakeTimeout time.Duration
	// MaxBackoff caps the wait between reconnects.
	MaxBackoff time.Duration
}

func (o Options) withDefaults() Options {
	if o.Poll <= 0 {
		o.Poll = time.Second
	}
	if o.StaleAfter <= 0 {
		o.StaleAfter = 180 * time.Second
	}
	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = 20 * time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = time.Minute
	}
	if o.LookupNetIP == nil {
		o.LookupNetIP = net.DefaultResolver.LookupNetIP
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return o
}

// ErrDown is the error of a socket asked of a Tunnel that does not carry
// traffic. It is netbind's, so a torrent client bound to the tunnel treats it
// as it treats a missing interface.
var ErrDown = netbind.ErrDown

// ErrResolve is an endpoint whose name did not resolve, which a name server
// that is not reachable yet causes as often as a wrong name.
var ErrResolve = errors.New("vpn: resolve")

// Tunnel is one WireGuard connection inside the process. It is a
// netbind.Network: registered with netbind and named as the torrent client's
// interface, it carries every torrent socket and nothing else.
type Tunnel struct {
	opts Options

	mu       sync.Mutex
	cfg      Config
	dev      *device.Device
	ns       *netStack
	stack    uint64
	state    State
	attempt  time.Time
	backoff  time.Duration
	stop     context.CancelFunc
	stopped  chan struct{}
	watchers []func(State)
}

// New returns a disconnected Tunnel.
func New(opts Options) *Tunnel {
	return &Tunnel{opts: opts.withDefaults(), state: State{Status: Disconnected, Since: time.Now()}}
}

// Connect sets the tunnel up with cfg, replacing a tunnel already up, and
// returns once WireGuard runs; the handshake follows in the background, see
// WaitConnected. A tunnel that cannot get or keep a handshake keeps trying
// until Disconnect.
func (t *Tunnel) Connect(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	t.Disconnect()
	endpoint, err := t.resolve(ctx, cfg.Peer.Endpoint, 0)
	if err != nil {
		t.fail(err)
		return err
	}
	t.mu.Lock()
	err = t.build(cfg, endpoint)
	if err != nil {
		t.mu.Unlock()
		t.fail(err)
		return err
	}
	runCtx, stop := context.WithCancel(context.Background())
	t.stop = stop
	t.stopped = make(chan struct{})
	go t.monitor(runCtx, t.stopped)
	st := t.setStatusLocked(Connecting, "")
	t.mu.Unlock()
	t.notify(st)
	return nil
}

// build brings up a new device on a new stack. It must be called with mu held.
func (t *Tunnel) build(cfg Config, endpoint netip.AddrPort) error {
	// Up sends the first handshake initiation, and the answer can arrive before
	// Up returns.
	attempt := time.Now()
	ns, err := newNetStack(cfg.addrs(), cfg.DNS, cfg.mtu(), true)
	if err != nil {
		return err
	}
	logf := t.opts.Logf
	dev := device.NewDevice(ns, conn.NewDefaultBind(), &device.Logger{
		Verbosef: func(string, ...any) {},
		Errorf:   func(format string, args ...any) { logf("wireguard: "+format, args...) },
	})
	if err := dev.IpcSet(cfg.uapi(endpoint)); err != nil {
		dev.Close()
		return fmt.Errorf("vpn: configure WireGuard: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return fmt.Errorf("vpn: bring WireGuard up: %w", err)
	}
	if t.dev != nil {
		t.dev.Close()
	}
	t.cfg, t.dev, t.ns = cfg, dev, ns
	t.stack++
	t.state.Endpoint = endpoint
	t.state.Addrs = cfg.addrs()
	t.state.LastHandshake = time.Time{}
	t.attempt = attempt
	return nil
}

// Disconnect takes the tunnel down. Every socket of it closes, and nothing
// can be sent through it until the next Connect.
func (t *Tunnel) Disconnect() {
	t.mu.Lock()
	stop, stopped := t.stop, t.stopped
	t.stop, t.stopped = nil, nil
	t.mu.Unlock()
	if stop != nil {
		stop()
		<-stopped
	}
	t.mu.Lock()
	dev := t.dev
	if dev == nil && t.state.Status == Disconnected {
		t.mu.Unlock()
		return
	}
	t.dev, t.ns = nil, nil
	t.stack++
	t.state.Addrs = nil
	t.state.Reconnects = 0
	t.backoff = 0
	st := t.setStatusLocked(Disconnected, "")
	t.mu.Unlock()
	// Watchers such as a Binder close their sockets while the stack still
	// stands, so none of them sees it fail.
	t.notify(st)
	if dev != nil {
		dev.Close()
	}
}

// State reports where the tunnel stands.
func (t *Tunnel) State() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state
	st.Addrs = slices.Clone(st.Addrs)
	return st
}

// Watch has f called with the new state whenever the status changes.
func (t *Tunnel) Watch(f func(State)) {
	t.mu.Lock()
	t.watchers = append(t.watchers, f)
	t.mu.Unlock()
}

// WaitConnected blocks until the tunnel has a handshake or ctx ends.
func (t *Tunnel) WaitConnected(ctx context.Context) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		st := t.State()
		switch st.Status {
		case Connected:
			return nil
		case Disconnected:
			return errors.New("vpn: the tunnel is disconnected")
		}
		select {
		case <-ctx.Done():
			if st.Err != "" {
				return fmt.Errorf("vpn: no handshake: %s", st.Err)
			}
			return fmt.Errorf("vpn: no handshake: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

func (t *Tunnel) notify(st State) {
	t.mu.Lock()
	ws := slices.Clone(t.watchers)
	t.mu.Unlock()
	for _, w := range ws {
		w(st)
	}
}

// setStatusLocked must be called with mu held; the caller notifies with the
// returned state once it has let go of mu.
func (t *Tunnel) setStatusLocked(s Status, errText string) State {
	if t.state.Status != s {
		t.state.Since = time.Now()
	}
	t.state.Status = s
	t.state.Err = errText
	st := t.state
	st.Addrs = slices.Clone(st.Addrs)
	return st
}

func (t *Tunnel) fail(err error) {
	t.mu.Lock()
	t.state.Err = err.Error()
	t.mu.Unlock()
	t.opts.Logf("vpn: %v", err)
}

func (t *Tunnel) resolve(ctx context.Context, endpoint string, attempt int) (netip.AddrPort, error) {
	host, port, err := splitEndpoint(endpoint)
	if err != nil {
		return netip.AddrPort{}, err
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return netip.AddrPortFrom(ip.Unmap(), port), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ips, err := t.opts.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("%w %s: %w", ErrResolve, host, err)
	}
	if len(ips) == 0 {
		return netip.AddrPort{}, fmt.Errorf("%w %s: no address", ErrResolve, host)
	}
	// IPv4 first: a provider's IPv6 is often the one that is broken.
	slices.SortStableFunc(ips, func(a, b netip.Addr) int {
		switch {
		case a.Is4() && !b.Is4():
			return -1
		case b.Is4() && !a.Is4():
			return 1
		}
		return 0
	})
	return netip.AddrPortFrom(ips[attempt%len(ips)].Unmap(), port), nil
}

// monitor watches the handshake and gets the tunnel back when it goes stale.
func (t *Tunnel) monitor(ctx context.Context, done chan struct{}) {
	defer close(done)
	tick := time.NewTicker(t.opts.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		t.check(ctx)
	}
}

func (t *Tunnel) check(ctx context.Context) {
	t.mu.Lock()
	if t.dev == nil {
		t.mu.Unlock()
		return
	}
	stats, err := readStats(t.dev)
	if err != nil {
		t.mu.Unlock()
		t.opts.Logf("vpn: read WireGuard state: %v", err)
		return
	}
	t.state.LastHandshake = stats.lastHandshake
	t.state.RxBytes, t.state.TxBytes = stats.rx, stats.tx
	status, due := t.opts.judge(t.state.Status, stats.lastHandshake, t.attempt, t.backoff, time.Now())
	var st State
	changed := status != t.state.Status
	if changed {
		if status == Connected {
			t.backoff = 0
			t.state.Reconnects = 0
		}
		st = t.setStatusLocked(status, "")
	}
	t.mu.Unlock()
	if changed {
		t.opts.Logf("vpn: tunnel %s", st.Status)
		t.notify(st)
	}
	if due {
		t.reconnect(ctx)
	}
}

// judge is the status a tunnel moves to given its last handshake, and whether
// it is time to try again: the handshake must be younger than StaleAfter and
// newer than the last attempt, which set up a peer that has not had one yet.
func (o Options) judge(status Status, handshake, attempt time.Time, backoff time.Duration, now time.Time) (Status, bool) {
	fresh := !handshake.IsZero() && now.Sub(handshake) < o.StaleAfter && !handshake.Before(attempt)
	switch {
	case fresh:
		return Connected, false
	case status == Connected:
		return Reconnecting, true
	}
	return status, now.Sub(attempt) >= o.HandshakeTimeout+backoff
}

// reconnect sets the peer up again with a freshly resolved endpoint, or with
// a fresh configuration when the provider hands one out. A configuration with
// other addresses needs a new stack, which moves every socket.
func (t *Tunnel) reconnect(ctx context.Context) {
	t.mu.Lock()
	cfg := t.cfg
	n := t.state.Reconnects
	t.state.Reconnects++
	t.attempt = time.Now()
	t.backoff = min(max(2*t.backoff, t.opts.Poll), t.opts.MaxBackoff)
	t.mu.Unlock()

	var err error
	if t.opts.Refresh != nil {
		var fresh Config
		if fresh, err = t.opts.Refresh(ctx); err == nil {
			err = fresh.Validate()
		}
		if err == nil {
			cfg = fresh
		}
	}
	var endpoint netip.AddrPort
	if err == nil {
		endpoint, err = t.resolve(ctx, cfg.Peer.Endpoint, n)
	}
	if err != nil {
		t.fail(err)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dev == nil || ctx.Err() != nil {
		return
	}
	t.opts.Logf("vpn: reconnecting to %s, attempt %d", endpoint, n+1)
	if slices.Equal(cfg.Addresses, t.cfg.Addresses) && cfg.mtu() == t.cfg.mtu() {
		// On Windows an ICMP port unreachable from a server that is away
		// ends WireGuard's receive loop for good; a new socket starts it
		// again, and also follows a network the system moved to.
		err = t.dev.BindUpdate()
		if err == nil {
			err = t.dev.IpcSet(cfg.uapi(endpoint))
		}
		if err == nil {
			t.cfg = cfg
			t.state.Endpoint = endpoint
		}
	} else {
		err = t.build(cfg, endpoint)
	}
	if err != nil {
		t.state.Err = err.Error()
		t.opts.Logf("vpn: reconnect: %v", err)
	}
}

type stats struct {
	lastHandshake time.Time
	rx, tx        uint64
}

func readStats(dev *device.Device) (stats, error) {
	var s stats
	out, err := dev.IpcGet()
	if err != nil {
		return s, err
	}
	var sec, nsec int64
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "last_handshake_time_sec":
			sec, _ = strconv.ParseInt(v, 10, 64)
		case "last_handshake_time_nsec":
			nsec, _ = strconv.ParseInt(v, 10, 64)
		case "rx_bytes":
			s.rx, _ = strconv.ParseUint(v, 10, 64)
		case "tx_bytes":
			s.tx, _ = strconv.ParseUint(v, 10, 64)
		}
	}
	if sec != 0 || nsec != 0 {
		s.lastHandshake = time.Unix(sec, nsec)
	}
	return s, nil
}

// current is the stack while the tunnel carries traffic.
func (t *Tunnel) current() (*netStack, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ns == nil || t.state.Status != Connected {
		return nil, ErrDown
	}
	return t.ns, nil
}

// Status implements netbind.Network.
func (t *Tunnel) Status() netbind.NetworkStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return netbind.NetworkStatus{
		Up:    t.ns != nil && t.state.Status == Connected,
		Addrs: slices.Clone(t.state.Addrs),
		Stack: t.stack,
	}
}

// DialContext dials through the tunnel, resolving a name with the tunnel's
// name servers. It fails with ErrDown unless the tunnel is connected.
func (t *Tunnel) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	ns, err := t.current()
	if err != nil {
		return nil, err
	}
	return ns.DialContext(ctx, network, address)
}

// ListenPacket opens a UDP socket inside the tunnel.
func (t *Tunnel) ListenPacket(network string, at netip.AddrPort) (net.PacketConn, error) {
	ns, err := t.current()
	if err != nil {
		return nil, err
	}
	return ns.ListenPacket(network, at)
}

// Listen opens a TCP listener inside the tunnel, which is where a port the
// provider forwards arrives.
func (t *Tunnel) Listen(network string, at netip.AddrPort) (net.Listener, error) {
	ns, err := t.current()
	if err != nil {
		return nil, err
	}
	return ns.Listen(network, at)
}

// LookupNetIP resolves host with the tunnel's name servers.
func (t *Tunnel) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	ns, err := t.current()
	if err != nil {
		return nil, err
	}
	return ns.LookupNetIP(ctx, network, host)
}
