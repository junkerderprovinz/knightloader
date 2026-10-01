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
// else, and no credential travels either way.
//
// An instance in a phrase group also tags its announce with a MAC only
// members can compute. A member that recognises the tag files the announcer
// as a member it can call directly (internal/federation's direct transport);
// the tag says nothing about the phrase to anyone else.
package discovery

import (
	"encoding/json"
	"net"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/ipv4"

	"github.com/junkerderprovinz/knightloader/internal/relay"
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
	// addressLimit is relay.MaxAddressBytes, so a member announces the same
	// address on both paths.
	addressLimit = 200
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
	// Address is where a group member's web interface is, as
	// relay.Announce.Address. Only a tagged announce carries it, since a
	// known domain is nothing to tell every device on the network.
	Address string `json:"address,omitempty"`
	// Sent is the Unix time the announce went out, and Tag the group MAC over
	// it and the fields above, empty outside a group. The tag covers the time,
	// so a captured announce stops passing once it is old.
	Sent int64  `json:"sent,omitempty"`
	Tag  string `json:"tag,omitempty"`
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
	// members holds the announces that passed isMember, apart from peers, so
	// a stranger announcing a member's id cannot replace its address.
	members  map[string]Peer
	sign     func(Peer) string
	isMember func(Peer) bool

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

// SetGroup makes every announce carry the tag sign computes for it, and
// files an announce that isMember accepts as a member. nil for both leaves
// the group, which forgets every member at once.
func (s *Service) SetGroup(sign func(Peer) string, isMember func(Peer) bool) {
	s.mu.Lock()
	s.sign, s.isMember = sign, isMember
	s.members = map[string]Peer{}
	s.mu.Unlock()
}

func (s *Service) currentSelf() (Peer, func(Peer) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.self, s.sign
}

// New builds a Service that will announce self. With an empty URL, as on the
// desktop build, it listens without announcing.
func New(self Peer) *Service {
	return &Service{
		self:    self,
		peers:   map[string]Peer{},
		members: map[string]Peer{},
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
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

// Peers is every instance seen recently that is not a member of this
// instance's group, this one excluded, sorted by name so a UI does not
// reshuffle on every poll.
func (s *Service) Peers() []Peer {
	s.mu.Lock()
	s.pruneLocked(time.Now().Add(-peerTTL))
	out := sorted(s.peers)
	s.mu.Unlock()
	return out
}

// Members is every member of this instance's group announcing on this
// network, sorted like Peers.
func (s *Service) Members() []Peer {
	s.mu.Lock()
	s.pruneLocked(time.Now().Add(-peerTTL))
	out := sorted(s.members)
	s.mu.Unlock()
	return out
}

func sorted(m map[string]Peer) []Peer {
	out := make([]Peer, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *Service) pruneLocked(cutoff time.Time) {
	for _, m := range []map[string]Peer{s.peers, s.members} {
		for id, p := range m {
			if p.LastSeen.Before(cutoff) {
				delete(m, id)
			}
		}
	}
}

func (s *Service) announceLoop(conn *ipv4.PacketConn, addr *net.UDPAddr) {
	defer close(s.done)

	// The announce is rebuilt every tick, since the name and address can
	// change, and an instance that gains an address starts announcing.
	send := func() {
		self, sign := s.currentSelf()
		if self.URL == "" || self.ID == "" {
			return
		}
		self.Sent = time.Now().Unix()
		if sign != nil {
			self.Tag = sign(self)
		} else {
			self.Address = ""
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
		if self, _ := s.currentSelf(); p.ID == "" || p.ID == self.ID || p.URL == "" {
			continue
		}
		s.absorb(p)
	}
}

// absorb files one announce. It is separate from readLoop so tests can drive
// it without real datagrams.
func (s *Service) absorb(p Peer) {
	// A member is listed under its id beside stored peers, so an id of any
	// other shape could take a stored peer's name.
	if !relay.ValidInstanceID(p.ID) {
		return
	}
	p.Name = clip(p.Name)
	p.URL = clip(p.URL)
	p.Deployment = clip(p.Deployment)
	p.Tag = clip(p.Tag)
	p.LastSeen = time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	// The tag covers the address as sent, so it is checked first. Only a
	// member's address is kept, and a long one is dropped rather than clipped,
	// since a clipped address leads somewhere else.
	member := s.isMember != nil && p.Tag != "" && s.isMember(p)
	if !member || len(p.Address) > addressLimit {
		p.Address = ""
	}
	if p.Name == "" {
		p.Name = p.ID
	}
	into := s.peers
	if member {
		into = s.members
		// Datagrams can arrive out of order, and an older announce played
		// back must not bring back an address the member has since left.
		if known, ok := into[p.ID]; ok && p.Sent < known.Sent {
			return
		}
		delete(s.peers, p.ID)
	}
	if _, known := into[p.ID]; !known && len(into) >= maxPeers {
		// Sweep expired peers first, so the cap does not freeze the list at
		// whatever was seen first.
		s.pruneLocked(time.Now().Add(-peerTTL))
		if len(into) >= maxPeers {
			return
		}
	}
	into[p.ID] = p
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
