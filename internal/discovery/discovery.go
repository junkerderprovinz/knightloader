// Package discovery lets KnightLoader instances on one network find each
// other with nothing configured. The relay (internal/relay) handles instances
// that cannot reach each other; on a home network only the address is
// missing.
//
// The protocol is a periodic JSON announce to a fixed UDP multicast group.
// mDNS would need a library and a second resolver stack to carry four
// fields.
//
// Discovery only makes an instance visible. Being on the same network is not
// consent, so adding a discovered instance stores an address and nothing
// else, and no credential travels either way. A peer with a password refuses
// calls until the two are paired with a code (internal/api/routes_pairing.go).
package discovery

import (
	"encoding/json"
	"net"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// group is in the administratively scoped 239.255/16 range (RFC 2365), which
// routers do not forward beyond the local network.
const (
	group = "239.255.77.49"
	port  = 8750
)

const (
	// announceEvery is how often an instance announces itself: one small
	// datagram per instance per interval.
	announceEvery = 5 * time.Second
	// peerTTL keeps a peer listed through a couple of dropped datagrams, so
	// it does not flicker in and out.
	peerTTL = 3*announceEvery + 2*time.Second
	// readLimit bounds one datagram.
	readLimit = 8 << 10
	// fieldLimit bounds one announced string. Anything on the network can
	// send to the group, so what is kept stays proportional to what is shown.
	fieldLimit = 128
	// maxPeers bounds how many instances are tracked. Ids are chosen by the
	// sender, and an instance nobody looks at never prunes on read, so a
	// device announcing fresh ids would otherwise grow the map without limit.
	maxPeers = 256
)

// Peer is one instance seen on the local network.
type Peer struct {
	// ID is the announcing instance's InstanceID, the same one the relay
	// uses.
	ID string `json:"id"`
	// Name is what to show; it falls back to the ID.
	Name string `json:"name"`
	// URL is the address to reach it on, as the announcer computed it.
	URL string `json:"url"`
	// Deployment is "container" or "desktop" (buildinfo.Deployment).
	Deployment string `json:"deployment"`
	// LastSeen is set by the receiver, never sent.
	LastSeen time.Time `json:"lastSeen"`
}

// Service announces this instance and tracks the others. A host without
// multicast simply sees no peers; nothing here reports an error after Start,
// so discovery failing never stops anything else.
type Service struct {
	self Peer

	mu    sync.Mutex
	peers map[string]Peer

	conn *ipv4.PacketConn
	// started means somebody will close done: Start either closes it or
	// hands that to announceLoop. Close waits on done only when this is set.
	started bool
	// closing makes a Start that arrives after Close return without opening
	// a socket that nothing would close.
	closing bool

	quit chan struct{}
	done chan struct{}
	once sync.Once
}

// SetSelf replaces what this instance announces from the next tick on, so a
// rename or a new address reaches the other machines without a restart.
func (s *Service) SetSelf(self Peer) {
	s.mu.Lock()
	s.self = self
	s.mu.Unlock()
}

func (s *Service) currentSelf() Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.self
}

// New builds a Service that will announce self. With an empty URL, as on the
// desktop build, it listens without announcing.
func New(self Peer) *Service {
	return &Service{
		self:  self,
		peers: map[string]Peer{},
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// Start joins the group and begins announcing. It never blocks, and a host
// that cannot do multicast just ends up with no peers.
func (s *Service) Start() {
	s.mu.Lock()
	if s.closing {
		// Close already closed done.
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	addr := &net.UDPAddr{IP: net.ParseIP(group), Port: port}
	// ListenMulticastUDP sets SO_REUSEADDR, so a container and a desktop build
	// on one machine can both listen.
	c, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		close(s.done)
		return
	}
	// Its defaults turn loopback off and join on one interface only, which on
	// a multi-homed host delivered nothing at all. x/net/ipv4 fixes both.
	p := ipv4.NewPacketConn(c)
	_ = p.SetMulticastLoopback(true)
	ifs, _ := net.Interfaces()
	joined := 0
	for i := range ifs {
		if ifs[i].Flags&net.FlagUp == 0 || ifs[i].Flags&net.FlagMulticast == 0 {
			continue
		}
		if p.JoinGroup(&ifs[i], addr) == nil {
			joined++
		}
	}
	if joined == 0 {
		_ = p.Close()
		close(s.done)
		return
	}
	s.mu.Lock()
	if s.closing {
		// Close ran after the check above and saw no conn, so this socket is
		// closed here before any goroutine uses it.
		s.mu.Unlock()
		_ = p.Close()
		close(s.done)
		return
	}
	s.conn = p
	s.mu.Unlock()
	go s.readLoop(p)
	go s.announceLoop(p, addr)
}

// Close stops announcing and listening. It is safe to call more than once and
// on a Service that was never started.
func (s *Service) Close() error {
	s.mu.Lock()
	conn, started := s.conn, s.started
	first := !s.closing
	s.closing = true
	s.mu.Unlock()

	s.once.Do(func() {
		close(s.quit)
		if conn != nil {
			_ = conn.Close()
		}
	})
	if started {
		<-s.done
		return nil
	}
	// Never started, and Start will now refuse to, so done is closed here.
	if first {
		close(s.done)
	}
	return nil
}

// listening reports whether Start joined the group, for tests that must skip
// where multicast is blocked.
func (s *Service) listening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

func (s *Service) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

// Peers is every instance seen recently, this one excluded, sorted by name so
// a UI does not reshuffle on every poll.
func (s *Service) Peers() []Peer {
	s.mu.Lock()
	s.pruneLocked(time.Now().Add(-peerTTL))
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *Service) pruneLocked(cutoff time.Time) {
	for id, p := range s.peers {
		if p.LastSeen.Before(cutoff) {
			delete(s.peers, id)
		}
	}
}

func (s *Service) announceLoop(conn *ipv4.PacketConn, addr *net.UDPAddr) {
	defer close(s.done)

	// The announce is rebuilt every tick, since the name and address can
	// change, and an instance that gains an address starts announcing.
	send := func() {
		self := s.currentSelf()
		if self.URL == "" || self.ID == "" {
			return
		}
		payload, err := json.Marshal(self)
		if err != nil {
			return
		}
		s.broadcast(conn, payload, addr)
	}

	send()

	t := time.NewTicker(announceEvery)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
			send()
		}
	}
}

// broadcast sends one announce out of every multicast-capable interface,
// since on a NAS with a Docker bridge the routing table can pick the wrong
// one.
func (s *Service) broadcast(conn *ipv4.PacketConn, payload []byte, addr *net.UDPAddr) {
	ifs, err := net.Interfaces()
	if err != nil {
		return
	}
	for i := range ifs {
		if ifs[i].Flags&net.FlagUp == 0 || ifs[i].Flags&net.FlagMulticast == 0 {
			continue
		}
		if conn.SetMulticastInterface(&ifs[i]) != nil {
			continue
		}
		_, _ = conn.WriteTo(payload, nil, addr)
	}
}

func (s *Service) readLoop(conn *ipv4.PacketConn) {
	buf := make([]byte, readLimit)
	for {
		n, _, _, err := conn.ReadFrom(buf)
		if err != nil {
			return // the socket was closed, or the network went away
		}
		var p Peer
		if json.Unmarshal(buf[:n], &p) != nil {
			continue // anything may share a multicast group
		}
		// Skip our own looped-back announce and anything unidentified.
		if p.ID == "" || p.ID == s.currentSelf().ID || p.URL == "" {
			continue
		}
		s.absorb(p)
	}
}

// absorb files one announce. It is separate from readLoop so tests can drive
// it without real datagrams.
func (s *Service) absorb(p Peer) {
	p.ID = clip(p.ID)
	p.Name = clip(p.Name)
	p.URL = clip(p.URL)
	p.Deployment = clip(p.Deployment)
	if p.Name == "" {
		p.Name = p.ID
	}
	p.LastSeen = time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, known := s.peers[p.ID]; !known && len(s.peers) >= maxPeers {
		// Sweep expired peers first, so the cap does not freeze the list at
		// whatever was seen first.
		s.pruneLocked(time.Now().Add(-peerTTL))
		if len(s.peers) >= maxPeers {
			return
		}
	}
	s.peers[p.ID] = p
}

func clip(s string) string {
	if len(s) > fieldLimit {
		return s[:fieldLimit]
	}
	return s
}

// LocalIPv4 is the address to announce: the first non-loopback,
// non-link-local IPv4 on a local interface. A 169.254 address is what a NIC
// gives itself when DHCP fails, and nothing else can reach it.
func LocalIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}
