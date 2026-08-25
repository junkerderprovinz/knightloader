// Package discovery makes KnightLoader instances on one network find each
// other with nothing configured at all.
//
// The relay already solves "two instances that cannot reach each other"
// (internal/relay), but it needs a third host somebody runs and a key typed
// into both ends. The overwhelmingly common case needs neither: a server, a
// desktop and a phone on one home network, where the only thing missing was
// ever the address. This fills that in.
//
// UDP multicast, hand-rolled, no dependency. mDNS/DNS-SD would be the
// textbook answer, but it drags in a library, a second name-resolution stack
// and its own failure modes, to carry four fields between processes that
// already speak JSON over HTTP. A periodic announce to a fixed group is the
// whole protocol.
//
// WHAT THIS DELIBERATELY DOES NOT DO: it does not pair anything, and adding a
// discovered instance does not either. Being on the same network is not
// consent - a guest laptop or an IoT device sits on that network too - so
// discovery only makes an instance VISIBLE, and adding it is an explicit act
// that stores an address and nothing else.
//
// No credential travels in either direction. That is the point rather than a
// gap: a credential exchange driven by an announce anything on the LAN can
// send would mean any device there could help itself to a token. It also has a
// consequence worth stating plainly wherever this is described to a person: a
// peer with a password set, added this way, will refuse the calls that follow
// until the two are paired with a code (internal/api/routes_pairing.go), which
// is the only path that hands credentials over.
package discovery

import (
	"encoding/json"
	"net"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// group is an administratively-scoped multicast address (RFC 2365, the
// 239.255/16 "IPv4 Local Scope"): routers do not forward it beyond the local
// network, which is exactly the reach this is meant to have.
const (
	group = "239.255.77.49"
	port  = 8750
)

const (
	// announceEvery is how often an instance says it is here. Short enough
	// that a newly started instance shows up while somebody is still looking
	// at the page, long enough that it is nothing on a network's budget: one
	// ~200 byte datagram per instance per interval.
	announceEvery = 5 * time.Second
	// peerTTL is how long a peer stays listed after its last announce. Three
	// missed announces rather than one, so a single dropped datagram - which
	// UDP multicast does, routinely - does not make an instance flicker out
	// of the list and back in.
	peerTTL = 3*announceEvery + 2*time.Second
	// readLimit bounds one datagram. An announce is a handful of short
	// strings; anything larger is not one.
	readLimit = 8 << 10
	// fieldLimit bounds ONE announced string. Everything on the wire here is
	// attacker-controlled - anything on the network can send whatever it likes
	// to this group - and a datagram under readLimit can still carry several
	// kilobytes in a single field. Truncating on arrival keeps what is
	// retained proportional to what is displayable.
	fieldLimit = 128
	// maxPeers bounds how many instances are tracked at once.
	//
	// The map is keyed by an id the sender chooses, so without a cap a single
	// device announcing a fresh random id in a loop grows it for as long as it
	// likes. Pruning happens on read, and a headless instance nobody opens the
	// Instances page on never reads. A home network does not have 256
	// KnightLoaders; something sending more than that is not a network to
	// enumerate, so the newest are simply not admitted once the expired ones
	// have been swept.
	maxPeers = 256
)

// Peer is one instance seen on the local network.
type Peer struct {
	// ID is the announcing instance's own stable identifier - the same
	// InstanceID the relay addresses a sibling by, so the two mechanisms
	// never disagree about who is who.
	ID string `json:"id"`
	// Name is what to show. Falls back to the ID when an instance has no
	// configured name, the same precedence pairing already uses.
	Name string `json:"name"`
	// URL is the address to reach it on, as the announcer computed it.
	URL string `json:"url"`
	// Deployment is "container" or "desktop" (buildinfo.Deployment).
	Deployment string `json:"deployment"`
	// LastSeen is filled in by the receiver, never sent.
	LastSeen time.Time `json:"lastSeen"`
}

// Service announces this instance and tracks the others.
//
// Every method is safe on a zero network: an instance on a host with
// multicast blocked, or no network at all, simply sees no peers and is seen
// by none. Discovery failing must never keep anything else from working,
// which is why nothing here returns an error to the caller after Start.
type Service struct {
	self Peer

	mu    sync.Mutex
	peers map[string]Peer

	conn *ipv4.PacketConn
	// started records that Start ran, which is the same thing as "done will be
	// closed by somebody": every path out of Start either closes it directly
	// or hands that job to announceLoop's defer. Close waits on done only when
	// this is set, because waiting on a channel nothing will ever close is a
	// shutdown that hangs forever rather than an error anyone can see.
	started bool
	// closing records that Close already ran, so a Start that arrives after it
	// bails instead of opening a socket and two goroutines nothing will ever
	// shut down. No caller does that today - startDiscovery calls Start
	// synchronously before anything can reach the Service - but the socket, the
	// group membership and readLoop would all leak silently, and the whole
	// point of holding this lifecycle in one place is that it does not depend
	// on every future caller getting the order right.
	closing bool

	quit chan struct{}
	done chan struct{}
	once sync.Once
}

// SetSelf replaces what this instance announces from the next tick on.
//
// The announce used to be marshalled once, before the loop, and re-sent
// unchanged forever - so renaming an instance left every other machine's
// "Found on your network" card showing the old name until the process
// restarted, and one click there stored the peer under a name that no longer
// existed. The same staleness applies to the address after a DHCP move.
//
// This is the same problem the relay announce already solves by calling
// applyRelay whenever InstanceName changes (see routes_settings.go), and it is
// solved the same way rather than a second way.
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

// New builds a Service that will announce self. url may be empty for a build
// with no address to offer (the desktop): it then listens without announcing,
// which is the honest shape - it can find others, nothing can reach it.
func New(self Peer) *Service {
	return &Service{
		self:  self,
		peers: map[string]Peer{},
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// Start joins the group and begins announcing. It never blocks and never
// fails loudly: a host that cannot do multicast is a host with no peers, not
// a broken instance.
func (s *Service) Start() {
	s.mu.Lock()
	if s.closing {
		// Already closed. done was closed by Close in that case, so there is
		// nothing to arrange and nothing to wait on.
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	addr := &net.UDPAddr{IP: net.ParseIP(group), Port: port}
	// ListenMulticastUDP rather than ListenPacket, because it sets
	// SO_REUSEADDR: a container and a desktop build on ONE machine both have
	// to be able to listen, and that is a case this project explicitly
	// supports (pairingSelf's own comment calls it out).
	c, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		close(s.done)
		return
	}
	// ...but its defaults do not actually deliver on a multi-homed host:
	// loopback is off, so two instances on one machine never see each other,
	// and it joins the group on ONE interface, which on a box with several
	// (a NAS with a bridge, a laptop on wifi and ethernet) is routinely the
	// wrong one. Verified on this project's own machine: with the plain
	// socket, nothing arrived at all - not even a packet the process sent
	// itself. x/net/ipv4 is what can say otherwise, and is already a direct
	// dependency.
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
	// Under the lock, because Close reads it: an unsynchronised pair here is a
	// data race the -race build would fail on the day anything closes a
	// service while it is still coming up.
	s.mu.Lock()
	if s.closing {
		// Close landed between the guard at the top of this function and here,
		// so it snapshotted a nil conn and will not close this socket. Nobody
		// else will either, and readLoop would sit in ReadFrom on it forever -
		// so it is closed here, before the goroutines that would use it are
		// ever started.
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

// Close stops announcing and listening. Safe to call more than once, and safe
// on a Service that was never started - which is not a case any caller reaches
// today, but the alternative is a Close that blocks forever on a channel
// nothing will close, and a shutdown that hangs is far harder to diagnose than
// one that returns.
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
	// Never started, and now never will be - Start's own guard sees closing.
	// done is closed here so a second Close, or one racing a Start that has
	// already passed that guard, still has something to wait on.
	if first {
		close(s.done)
	}
	return nil
}

// listening reports whether Start managed to join the group. Exists for the
// tests, which have to skip on a host where multicast is blocked and must not
// reach into the field to find out - Close reads the same one, so an
// unsynchronised peek is a data race the -race build would fail on.
func (s *Service) listening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

// isClosing reports whether Close has run. For the tests, same reasoning as
// listening: the field is shared, so nothing may read it without the lock.
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

// pruneLocked drops everything last seen before cutoff. The caller holds mu.
func (s *Service) pruneLocked(cutoff time.Time) {
	for id, p := range s.peers {
		if p.LastSeen.Before(cutoff) {
			delete(s.peers, id)
		}
	}
}

func (s *Service) announceLoop(conn *ipv4.PacketConn, addr *net.UDPAddr) {
	defer close(s.done)

	// Rebuilt every time rather than marshalled once outside the loop: what
	// this instance calls itself, and the address it is reachable at, can both
	// change while it runs. See SetSelf.
	//
	// An instance with no address to announce still listens - see New - and
	// that too is re-checked per tick, so one that gains an address later
	// starts announcing without a restart.
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

	// Announce at once rather than after the first tick: an instance that
	// just booted should appear now, not in five seconds.
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

// broadcast sends one announce out of EVERY multicast-capable interface,
// rather than trusting the routing table to pick. A NAS with a Docker bridge
// and a LAN NIC has more than one plausible answer, and the wrong one reaches
// nobody - sending on all of them costs a handful of datagrams.
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
			continue // not one of ours; anything may share a multicast group
		}
		// Ignore our own announce, which multicast loops back by default, and
		// anything that did not identify itself.
		if p.ID == "" || p.ID == s.currentSelf().ID || p.URL == "" {
			continue
		}
		s.absorb(p)
	}
}

// absorb files one announce. Split out of readLoop so the flood test can drive
// the insert path without 2000 real datagrams - the question there is what the
// map does with announces, not whether multicast delivers them.
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
		// Sweep first, then decide: the cap is there to stop unbounded growth,
		// not to freeze the list at whatever 256 instances happened to be seen
		// first. An ordinary network never reaches this at all.
		s.pruneLocked(time.Now().Add(-peerTTL))
		if len(s.peers) >= maxPeers {
			return
		}
	}
	s.peers[p.ID] = p
}

// clip bounds one announced string. See fieldLimit.
func clip(s string) string {
	if len(s) > fieldLimit {
		return s[:fieldLimit]
	}
	return s
}

// LocalIPv4 is the address to announce: the first non-loopback,
// non-link-local IPv4 bound to a local interface.
//
// Link-local (169.254/16) is excluded for the same reason routes_remote.go
// excludes it from the addresses it reports - it is what a NIC gives itself
// when DHCP fails, reachable only by another interface in the identical
// broken state, and it sorts ahead of a real "192.168." address.
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
