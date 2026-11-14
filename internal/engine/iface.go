package engine

import (
	"net"
	"net/netip"
	"slices"
	"strings"
)

// NetInterface is a network interface the torrent client can be tied to, with
// the addresses it would send from there.
type NetInterface struct {
	Name  string   `json:"name"`
	Up    bool     `json:"up"`
	Addrs []string `json:"addrs"`
}

// NetInterfaces lists the system's network interfaces by name. Up means the
// torrent client can use one: the system has it up and it has an address
// beyond its own link. A network inside this process, such as the built-in
// VPN's, is not one of them, since the client is another program.
func NetInterfaces() ([]NetInterface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetInterface, 0, len(ifs))
	for _, ifc := range ifs {
		ni := NetInterface{Name: ifc.Name, Addrs: []string{}}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			p, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := p.Addr()
			if ip.IsLinkLocalUnicast() {
				continue
			}
			ni.Addrs = append(ni.Addrs, ip.String())
		}
		ni.Up = ifc.Flags&net.FlagUp != 0 && len(ni.Addrs) > 0
		out = append(out, ni)
	}
	slices.SortFunc(out, func(a, b NetInterface) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// InterfaceUp reports whether torrent traffic can pass through the named
// interface, by the same rule as NetInterfaces. Any interface, the empty name,
// always can.
func InterfaceUp(name string) bool {
	if name == "" {
		return true
	}
	ifs, err := NetInterfaces()
	if err != nil {
		return false
	}
	i := slices.IndexFunc(ifs, func(ni NetInterface) bool { return ni.Name == name })
	return i >= 0 && ifs[i].Up
}
