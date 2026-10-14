package vpn

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"time"
)

// NATPMPPort is the port a NAT-PMP gateway listens on (RFC 6886).
const NATPMPPort = 5351

const (
	opExternalAddress = 0
	opMapUDP          = 1
	opMapTCP          = 2
)

// ResultError is a gateway's refusal of a NAT-PMP request.
type ResultError uint16

func (e ResultError) Error() string {
	switch e {
	case 1:
		return "nat-pmp: the gateway does not support this version"
	case 2:
		return "nat-pmp: the gateway refused the mapping"
	case 3:
		return "nat-pmp: the gateway has no network to map on"
	case 4:
		return "nat-pmp: the gateway is out of ports"
	case 5:
		return "nat-pmp: the gateway does not support this request"
	}
	return "nat-pmp: result code " + strconv.Itoa(int(e))
}

// NATPMP talks NAT-PMP to one gateway. For a VPN provider's gateway, Dial is
// the tunnel's, since the gateway is only reachable inside it.
type NATPMP struct {
	Gateway netip.Addr
	Dial    func(ctx context.Context, network, address string) (net.Conn, error)
	// Attempts is how often a request is sent before giving up, 4 when 0.
	// The wait doubles from 250 ms each time, as the RFC has it; its nine
	// attempts would take over two minutes, far longer than a 60 second
	// mapping.
	Attempts int
}

// Mapping is a port the gateway forwards.
type Mapping struct {
	Protocol     string
	InternalPort uint16
	ExternalPort uint16
	Lifetime     time.Duration
	// Epoch is the gateway's seconds since its mappings were last lost.
	Epoch uint32
}

// ExternalAddress asks the gateway for its public address.
func (c *NATPMP) ExternalAddress(ctx context.Context) (netip.Addr, error) {
	resp, err := c.request(ctx, []byte{0, opExternalAddress}, 12)
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.AddrFrom4([4]byte(resp[8:12])), nil
}

// Map asks for the gateway's external port suggested to be forwarded to
// internal for protocol, "tcp" or "udp". A lifetime of 0 removes the mapping.
// The gateway may grant another external port than suggested, and a VPN's
// gateway may forward to that port rather than to internal.
func (c *NATPMP) Map(ctx context.Context, protocol string, internal, suggested uint16, lifetime time.Duration) (Mapping, error) {
	var op byte
	switch protocol {
	case "udp":
		op = opMapUDP
	case "tcp":
		op = opMapTCP
	default:
		return Mapping{}, fmt.Errorf("nat-pmp: unknown protocol %q", protocol)
	}
	req := make([]byte, 12)
	req[1] = op
	binary.BigEndian.PutUint16(req[4:], internal)
	binary.BigEndian.PutUint16(req[6:], suggested)
	binary.BigEndian.PutUint32(req[8:], uint32(lifetime/time.Second))
	resp, err := c.request(ctx, req, 16)
	if err != nil {
		return Mapping{}, err
	}
	return Mapping{
		Protocol:     protocol,
		InternalPort: binary.BigEndian.Uint16(resp[8:]),
		ExternalPort: binary.BigEndian.Uint16(resp[10:]),
		Lifetime:     time.Duration(binary.BigEndian.Uint32(resp[12:])) * time.Second,
		Epoch:        binary.BigEndian.Uint32(resp[4:]),
	}, nil
}

// request sends req until an answer to it comes back, and checks the answer's
// opcode, length and result code.
func (c *NATPMP) request(ctx context.Context, req []byte, size int) ([]byte, error) {
	dial := c.Dial
	if dial == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	conn, err := dial(ctx, "udp4", netip.AddrPortFrom(c.Gateway, NATPMPPort).String())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	attempts := c.Attempts
	if attempts <= 0 {
		attempts = 4
	}
	wait := 250 * time.Millisecond
	buf := make([]byte, 64)
	for range attempts {
		if _, err := conn.Write(req); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(wait)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		conn.SetReadDeadline(deadline)
		for {
			n, err := conn.Read(buf)
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			if err != nil {
				return nil, err
			}
			// Anything else on the socket is stale or not ours.
			if n < 4 || buf[0] != 0 || buf[1] != 128+req[1] {
				continue
			}
			if code := binary.BigEndian.Uint16(buf[2:]); code != 0 {
				return nil, ResultError(code)
			}
			if n < size {
				return nil, fmt.Errorf("nat-pmp: answer of %d bytes, want %d", n, size)
			}
			return buf[:n], nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wait *= 2
	}
	return nil, fmt.Errorf("nat-pmp: no answer from %s", c.Gateway)
}

// PortForward keeps one port forwarded for TCP and UDP and follows the port
// the gateway hands out. Proton's gateway, for one, forwards its external
// port to the same port inside the tunnel, so whatever listens has to move to
// that port; a gateway that does as the RFC says ends up the same way, since
// the port is asked for internal and external alike.
type PortForward struct {
	Client *NATPMP
	// Lifetime is what each mapping asks for, 60 seconds when 0.
	Lifetime time.Duration
	// Renew is how long before a mapping runs out it is renewed, a quarter
	// of the lifetime when 0, which renews a 60 second mapping every 45.
	Renew time.Duration
	// Prefer is the port asked for first, such as the one the torrent client
	// listens on already.
	Prefer uint16
	// OnChange hears the forwarded port each time it changes, and 0 once
	// the mapping has run out without being renewed.
	OnChange func(port uint16)
	Logf     func(format string, args ...any)

	mu   sync.Mutex
	port uint16
}

// Port is the forwarded port, 0 while there is none.
func (pf *PortForward) Port() uint16 {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	return pf.port
}

// Run maps the port and renews it until ctx ends, then asks the gateway to
// drop it.
func (pf *PortForward) Run(ctx context.Context) error {
	lifetime := cmp.Or(pf.Lifetime, 60*time.Second)
	renewBefore := cmp.Or(pf.Renew, lifetime/4)
	want := pf.Prefer
	var expires time.Time
	retry := time.Second
	for {
		port, granted, err := pf.mapBoth(ctx, want, lifetime)
		var next time.Duration
		switch {
		case ctx.Err() != nil:
			pf.drop()
			return ctx.Err()
		case err != nil:
			pf.logf("vpn: port forwarding: %v", err)
			if !expires.IsZero() && time.Now().After(expires) {
				pf.set(0)
				expires = time.Time{}
			}
			next = retry
			retry = min(2*retry, renewBefore)
		default:
			retry = time.Second
			want = port
			expires = time.Now().Add(granted)
			pf.set(port)
			next = max(granted-renewBefore, time.Second)
		}
		select {
		case <-ctx.Done():
			pf.drop()
			return ctx.Err()
		case <-time.After(next):
		}
	}
}

func (pf *PortForward) drop() {
	p := pf.Port()
	if p == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pf.Client.Map(ctx, "tcp", p, 0, 0)
	pf.Client.Map(ctx, "udp", p, 0, 0)
	pf.set(0)
}

// mapBoth maps want for TCP and then the port TCP got for UDP, asking once
// more when the gateway hands out a port other than the one asked for, so
// inside and outside end on one port. A gateway that puts UDP elsewhere
// leaves the port forwarded for TCP only.
func (pf *PortForward) mapBoth(ctx context.Context, want uint16, lifetime time.Duration) (uint16, time.Duration, error) {
	internal := want
	if internal == 0 {
		// Port 0 inside asks to drop every mapping; Proton's own guide asks with 1.
		internal = 1
	}
	tcp, err := pf.Client.Map(ctx, "tcp", internal, want, lifetime)
	if err != nil {
		return 0, 0, err
	}
	if tcp.ExternalPort != internal {
		if tcp, err = pf.Client.Map(ctx, "tcp", tcp.ExternalPort, tcp.ExternalPort, lifetime); err != nil {
			return 0, 0, err
		}
	}
	port := tcp.ExternalPort
	udp, err := pf.Client.Map(ctx, "udp", port, port, lifetime)
	if err != nil {
		return 0, 0, err
	}
	if udp.ExternalPort != port {
		pf.logf("vpn: the gateway forwards TCP on %d and UDP on %d; UDP stays unreachable", port, udp.ExternalPort)
		pf.Client.Map(ctx, "udp", port, 0, 0)
		return port, tcp.Lifetime, nil
	}
	return port, min(tcp.Lifetime, udp.Lifetime), nil
}

func (pf *PortForward) set(port uint16) {
	pf.mu.Lock()
	changed := pf.port != port
	pf.port = port
	pf.mu.Unlock()
	if changed && pf.OnChange != nil {
		pf.OnChange(port)
	}
}

func (pf *PortForward) logf(format string, args ...any) {
	if pf.Logf != nil {
		pf.Logf(format, args...)
	}
}
