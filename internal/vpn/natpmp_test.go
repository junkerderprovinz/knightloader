package vpn

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"
)

// natpmpRequest is one request a fakeGateway saw.
type natpmpRequest struct {
	op                  byte
	internal, suggested uint16
	lifetime            uint32
}

// fakeGateway answers NAT-PMP on a packet socket. assign picks the external
// port of a mapping, or a result code to refuse it with.
type fakeGateway struct {
	public   netip.Addr
	lifetime uint32
	assign   func(op byte, internal, suggested uint16) (port uint16, code uint16)

	mu       sync.Mutex
	silent   bool
	drop     int
	requests []natpmpRequest
	start    time.Time
}

func (g *fakeGateway) serve(pc net.PacketConn) {
	g.start = time.Now()
	buf := make([]byte, 64)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if n < 2 || buf[0] != 0 {
			continue
		}
		req := natpmpRequest{op: buf[1]}
		if n >= 12 {
			req.internal = binary.BigEndian.Uint16(buf[4:])
			req.suggested = binary.BigEndian.Uint16(buf[6:])
			req.lifetime = binary.BigEndian.Uint32(buf[8:])
		}
		g.mu.Lock()
		g.requests = append(g.requests, req)
		skip := g.silent || g.drop > 0
		if g.drop > 0 {
			g.drop--
		}
		g.mu.Unlock()
		if skip {
			continue
		}
		pc.WriteTo(g.answer(req), from)
	}
}

func (g *fakeGateway) answer(req natpmpRequest) []byte {
	epoch := uint32(time.Since(g.start).Seconds())
	if req.op == opExternalAddress {
		resp := make([]byte, 12)
		resp[1] = 128
		binary.BigEndian.PutUint32(resp[4:], epoch)
		copy(resp[8:], g.public.AsSlice())
		return resp
	}
	resp := make([]byte, 16)
	resp[1] = 128 + req.op
	binary.BigEndian.PutUint32(resp[4:], epoch)
	if req.lifetime == 0 {
		binary.BigEndian.PutUint16(resp[8:], req.internal)
		return resp
	}
	port, code := g.assign(req.op, req.internal, req.suggested)
	binary.BigEndian.PutUint16(resp[2:], code)
	binary.BigEndian.PutUint16(resp[8:], req.internal)
	binary.BigEndian.PutUint16(resp[10:], port)
	binary.BigEndian.PutUint32(resp[12:], min(req.lifetime, g.lifetime))
	return resp
}

func (g *fakeGateway) seen() []natpmpRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.requests)
}

func (g *fakeGateway) set(f func(g *fakeGateway)) {
	g.mu.Lock()
	f(g)
	g.mu.Unlock()
}

// newLoopGateway runs g on loopback and returns a client whose requests reach
// it there rather than at the real NAT-PMP port.
func newLoopGateway(t *testing.T, g *fakeGateway) *NATPMP {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go g.serve(pc)
	at := pc.LocalAddr().String()
	return &NATPMP{
		Gateway: netip.MustParseAddr("10.2.0.1"),
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, at)
		},
	}
}

func protonLike(port *uint16, mu *sync.Mutex) func(byte, uint16, uint16) (uint16, uint16) {
	return func(byte, uint16, uint16) (uint16, uint16) {
		mu.Lock()
		defer mu.Unlock()
		return *port, 0
	}
}

func TestMapSendsTheRequestAndReadsTheGrant(t *testing.T) {
	g := &fakeGateway{lifetime: 60, assign: func(_ byte, _, suggested uint16) (uint16, uint16) { return suggested + 1, 0 }}
	c := newLoopGateway(t, g)
	m, err := c.Map(context.Background(), "udp", 6881, 6881, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := Mapping{Protocol: "udp", InternalPort: 6881, ExternalPort: 6882, Lifetime: 60 * time.Second, Epoch: m.Epoch}
	if m != want {
		t.Errorf("Map = %+v, want %+v", m, want)
	}
	if got := g.seen(); !slices.Equal(got, []natpmpRequest{{op: opMapUDP, internal: 6881, suggested: 6881, lifetime: 60}}) {
		t.Errorf("gateway saw %+v", got)
	}
}

func TestExternalAddressIsTheGatewaysPublicOne(t *testing.T) {
	g := &fakeGateway{public: netip.MustParseAddr("185.159.157.1")}
	c := newLoopGateway(t, g)
	a, err := c.ExternalAddress(context.Background())
	if err != nil || a != g.public {
		t.Errorf("ExternalAddress = %v, %v; want %v", a, err, g.public)
	}
}

func TestARefusedMappingIsAResultError(t *testing.T) {
	g := &fakeGateway{lifetime: 60, assign: func(byte, uint16, uint16) (uint16, uint16) { return 0, 2 }}
	c := newLoopGateway(t, g)
	_, err := c.Map(context.Background(), "tcp", 6881, 6881, time.Minute)
	var re ResultError
	if !errors.As(err, &re) || re != 2 {
		t.Errorf("Map = %v, want result code 2", err)
	}
}

func TestALostRequestIsSentAgain(t *testing.T) {
	g := &fakeGateway{lifetime: 60, drop: 2, assign: func(_ byte, _, s uint16) (uint16, uint16) { return s, 0 }}
	c := newLoopGateway(t, g)
	if _, err := c.Map(context.Background(), "tcp", 6881, 6881, time.Minute); err != nil {
		t.Fatal(err)
	}
	if n := len(g.seen()); n != 3 {
		t.Errorf("gateway saw %d requests, want 3", n)
	}
}

func TestASilentGatewayIsGivenUpOn(t *testing.T) {
	g := &fakeGateway{silent: true}
	c := newLoopGateway(t, g)
	c.Attempts = 2
	if _, err := c.Map(context.Background(), "tcp", 6881, 6881, time.Minute); err == nil {
		t.Fatal("Map succeeded without an answer")
	}
	if n := len(g.seen()); n != 2 {
		t.Errorf("gateway saw %d requests, want 2", n)
	}
}

// portLog collects what a PortForward reports.
type portLog struct {
	mu    sync.Mutex
	ports []uint16
}

func (l *portLog) add(p uint16) {
	l.mu.Lock()
	l.ports = append(l.ports, p)
	l.mu.Unlock()
}

func (l *portLog) get() []uint16 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.ports)
}

func waitUntil(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(d); !ok(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPortForwardFollowsAGatewayThatPicksThePort(t *testing.T) {
	var mu sync.Mutex
	port := uint16(51234)
	g := &fakeGateway{lifetime: 2, assign: protonLike(&port, &mu)}
	log := &portLog{}
	pf := &PortForward{Client: newLoopGateway(t, g), Lifetime: 2 * time.Second, OnChange: log.add}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pf.Run(ctx) }()

	waitUntil(t, "the first port", 5*time.Second, func() bool { return pf.Port() == 51234 })
	first := g.seen()
	if first[0].internal != 1 || first[0].op != opMapTCP {
		t.Errorf("first request %+v, want TCP with internal port 1", first[0])
	}
	waitUntil(t, "a renewal", 5*time.Second, func() bool {
		return slices.ContainsFunc(g.seen()[len(first):], func(r natpmpRequest) bool {
			return r.op == opMapUDP && r.internal == 51234 && r.suggested == 51234
		})
	})

	mu.Lock()
	port = 40000
	mu.Unlock()
	waitUntil(t, "the new port", 5*time.Second, func() bool { return pf.Port() == 40000 })

	g.set(func(g *fakeGateway) { g.silent = true })
	waitUntil(t, "the port to run out", 10*time.Second, func() bool { return pf.Port() == 0 })
	if got := log.get(); !slices.Equal(got, []uint16{51234, 40000, 0}) {
		t.Errorf("reported ports %v, want 51234, 40000, 0", got)
	}
	cancel()
	<-done
}

func TestPortForwardKeepsThePortAnRFCGatewayGrants(t *testing.T) {
	g := &fakeGateway{lifetime: 60, assign: func(_ byte, _, s uint16) (uint16, uint16) { return s, 0 }}
	log := &portLog{}
	pf := &PortForward{Client: newLoopGateway(t, g), Prefer: 6881, OnChange: log.add}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pf.Run(ctx) }()
	waitUntil(t, "the port", 5*time.Second, func() bool { return pf.Port() == 6881 })
	cancel()
	<-done
	reqs := g.seen()
	if len(reqs) != 4 {
		t.Fatalf("gateway saw %+v, want a TCP and a UDP mapping and both dropped", reqs)
	}
	for _, r := range reqs[:2] {
		if r.internal != 6881 || r.suggested != 6881 || r.lifetime != 60 {
			t.Errorf("request %+v, want 6881 inside and out for 60 s", r)
		}
	}
	for _, r := range reqs[2:] {
		if r.lifetime != 0 {
			t.Errorf("request %+v after Run ended, want the mapping dropped", r)
		}
	}
	if got := log.get(); !slices.Equal(got, []uint16{6881, 0}) {
		t.Errorf("reported ports %v, want 6881 then 0", got)
	}
}
