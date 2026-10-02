package engine

import (
	"slices"
	"strings"

	"github.com/GopeedLab/gopeed/pkg/netbind"
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
// beyond its own link.
func NetInterfaces() ([]NetInterface, error) {
	ifs, err := netbind.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetInterface, 0, len(ifs))
	for _, ifc := range ifs {
		ni := NetInterface{Name: ifc.Name, Up: ifc.Up, Addrs: []string{}}
		for _, a := range ifc.Addrs {
			ni.Addrs = append(ni.Addrs, a.String())
		}
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
