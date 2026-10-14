// Package vpntest runs a WireGuard server in the test process, on a userspace
// stack of its own, for tests of what connects to one. It answers TCP echo on
// port 7 and NAT-PMP at its tunnel address, the way a provider's gateway
// sits at the first address of the tunnel's network, and on port 80 it answers
// with ExitAddr, the way an address lookup through a VPN does.
package vpntest

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
)

// Addresses inside the tunnel: the server's, which is also the gateway, and
// the client's.
var (
	ServerAddr = netip.MustParseAddr("10.2.0.1")
	ClientAddr = netip.MustParseAddr("10.2.0.2")
)

// ExitAddr is what the server's port 80 answers.
const ExitAddr = "203.0.113.7"

// Server is a running WireGuard server and the client key it lets in.
type Server struct {
	Key       vpn.Key
	ClientKey vpn.Key
	Port      uint16

	dev *device.Device

	mu       sync.Mutex
	mapped   uint16
	lifetime uint32
	maps     int
}

// Start runs a server on a free UDP port of loopback and closes it when t
// ends.
func Start(t testing.TB) *Server {
	t.Helper()
	sk, err := vpn.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	ck, err := vpn.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Key: sk, ClientKey: ck, mapped: 51234, lifetime: 60}
	tdev, tnet, err := netstack.CreateNetTUN([]netip.Addr{ServerAddr}, nil, vpn.DefaultMTU)
	if err != nil {
		t.Fatal(err)
	}
	s.dev = device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
	pub := ck.PublicKey()
	uapi := fmt.Sprintf("private_key=%x\nlisten_port=0\npublic_key=%x\nallowed_ip=%s/32\n", sk[:], pub[:], ClientAddr)
	if err := s.dev.IpcSet(uapi); err != nil {
		t.Fatal(err)
	}
	if err := s.dev.Up(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.dev.Close)
	got, _ := s.dev.IpcGet()
	for _, line := range strings.Split(got, "\n") {
		if v, ok := strings.CutPrefix(line, "listen_port="); ok {
			fmt.Sscan(v, &s.Port)
		}
	}
	ln, err := tnet.ListenTCPAddrPort(netip.AddrPortFrom(ServerAddr, 7))
	if err != nil {
		t.Fatal(err)
	}
	go echo(ln)
	gw, err := tnet.ListenUDPAddrPort(netip.AddrPortFrom(ServerAddr, vpn.NATPMPPort))
	if err != nil {
		t.Fatal(err)
	}
	go s.natpmp(gw)
	web, err := tnet.ListenTCPAddrPort(netip.AddrPortFrom(ServerAddr, 80))
	if err != nil {
		t.Fatal(err)
	}
	go http.Serve(web, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, ExitAddr) }))
	return s
}

// Config is the client's configuration, as a provider hands it out.
func (s *Server) Config() string {
	return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\n\n[Peer]\nPublicKey = %s\nAllowedIPs = 0.0.0.0/0\nEndpoint = 127.0.0.1:%d\n",
		s.ClientKey, ClientAddr, s.Key.PublicKey(), s.Port)
}

// SetMappedPort sets the external port the gateway grants every mapping,
// whatever the client asks for, as Proton's does.
func (s *Server) SetMappedPort(p uint16) {
	s.mu.Lock()
	s.mapped = p
	s.mu.Unlock()
}

// SetLifetime caps how long a mapping lasts, which is how soon the client
// renews it.
func (s *Server) SetLifetime(d time.Duration) {
	s.mu.Lock()
	s.lifetime = uint32(d / time.Second)
	s.mu.Unlock()
}

// Mappings counts the mapping requests the gateway answered.
func (s *Server) Mappings() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maps
}

// Close stops the server, as a provider's server that goes away.
func (s *Server) Close() { s.dev.Close() }

func echo(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			io.Copy(c, c)
		}()
	}
}

func (s *Server) natpmp(pc net.PacketConn) {
	buf := make([]byte, 64)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if n < 2 || buf[0] != 0 {
			continue
		}
		epoch := uint32(time.Now().Unix())
		if buf[1] == 0 {
			resp := make([]byte, 12)
			resp[1] = 128
			binary.BigEndian.PutUint32(resp[4:], epoch)
			copy(resp[8:], []byte{203, 0, 113, 7})
			pc.WriteTo(resp, from)
			continue
		}
		if n < 12 {
			continue
		}
		resp := make([]byte, 16)
		resp[1] = 128 + buf[1]
		binary.BigEndian.PutUint32(resp[4:], epoch)
		copy(resp[8:10], buf[4:6])
		lifetime := binary.BigEndian.Uint32(buf[8:])
		if lifetime > 0 {
			s.mu.Lock()
			binary.BigEndian.PutUint16(resp[10:], s.mapped)
			binary.BigEndian.PutUint32(resp[12:], min(lifetime, s.lifetime))
			s.maps++
			s.mu.Unlock()
		}
		pc.WriteTo(resp, from)
	}
}
