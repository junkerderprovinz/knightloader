package vpnconn

import "net/netip"

// Provider is one tile of the VPN page: what its connect form asks for, and
// how KnightLoader connects to it, where it can.
type Provider struct {
	ID             string
	Name           string
	Fields         []string
	PortForwarding bool

	open opener
	port portKind
	// gateway answers NAT-PMP inside the tunnel, for a provider whose
	// configuration does not say.
	gateway netip.Addr
}

type portKind int

const (
	noPort portKind = iota
	// natpmpPort asks the gateway for a port and follows the one it grants.
	natpmpPort
	// manualPort is a port the user forwards in the provider's account, which
	// the torrent client then has to listen on: the torrent settings' port.
	manualPort
)

// protonGateway is where Proton VPN answers NAT-PMP on every server.
var protonGateway = netip.MustParseAddr("10.2.0.1")

// catalogue is the provider list of web/src/lib/vpnProviders.ts, in its order;
// a test holds the two together. A provider without open is listed so its
// tile shows, and refuses to connect until its way in is built.
func catalogue() []Provider {
	conf, hide := opener(openConfig), opener(openHideMe)
	return []Provider{
		{ID: "nordvpn", Name: "NordVPN", Fields: []string{"token"}},
		{ID: "protonvpn", Name: "Proton VPN", Fields: []string{"config"}, PortForwarding: true, open: conf, port: natpmpPort, gateway: protonGateway},
		{ID: "surfshark", Name: "Surfshark", Fields: []string{"config"}, open: conf},
		{ID: "pia", Name: "Private Internet Access", Fields: []string{"username", "password"}, PortForwarding: true},
		{ID: "mullvad", Name: "Mullvad VPN", Fields: []string{"token"}},
		{ID: "windscribe", Name: "Windscribe", Fields: []string{"config"}, PortForwarding: true, open: conf, port: manualPort},
		{ID: "ipvanish", Name: "IPVanish", Fields: []string{"config"}, open: conf},
		{ID: "purevpn", Name: "PureVPN", Fields: []string{"config"}, PortForwarding: true, open: conf, port: manualPort},
		{ID: "hideme", Name: "hide.me", Fields: []string{"username", "password", "location"}, PortForwarding: true, open: hide, port: natpmpPort},
		{ID: "airvpn", Name: "AirVPN", Fields: []string{"config"}, PortForwarding: true, open: conf, port: manualPort},
		{ID: "ivpn", Name: "IVPN", Fields: []string{"config"}, open: conf},
		{ID: "torguard", Name: "TorGuard", Fields: []string{"username", "password", "location"}, PortForwarding: true},
		{ID: "privadovpn", Name: "PrivadoVPN", Fields: []string{"config"}, open: conf},
		{ID: "vyprvpn", Name: "VyprVPN", Fields: []string{"username", "password", "location"}, PortForwarding: true},
		{ID: "ovpn", Name: "OVPN", Fields: []string{"config"}, PortForwarding: true, open: conf, port: manualPort},
		{ID: "azirevpn", Name: "AzireVPN", Fields: []string{"username", "password"}, PortForwarding: true},
		{ID: "fastestvpn", Name: "FastestVPN", Fields: []string{"config"}, open: conf},
		{ID: "ivacy", Name: "Ivacy VPN", Fields: []string{"config"}, PortForwarding: true, open: conf, port: manualPort},
		{ID: "privatevpn", Name: "PrivateVPN", Fields: []string{"config"}, open: conf},
		{ID: "astrill", Name: "Astrill VPN", Fields: []string{"config"}, PortForwarding: true, open: conf, port: manualPort},
		{ID: "zoogvpn", Name: "ZoogVPN", Fields: []string{"config"}, open: conf},
		{ID: "custom", Name: "Own WireGuard config", Fields: []string{"config"}, PortForwarding: true, open: conf, port: manualPort},
	}
}
