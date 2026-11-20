// The VPN catalog: the providers an app can route its traffic through, each
// with its mark. An app hands catalogPicker the ids it can connect to and keeps
// what it needs to sign in (the fields its form asks for, whether the provider
// forwards a port) beside them by id.
//
// A provider that hands out WireGuard, as a configuration file or through a
// login, stands under providers. One that keeps WireGuard inside its own app
// stands apart, so an app can show it as a tile that waits rather than leave
// somebody searching for it. The own configuration takes any WireGuard server
// and stands last.

import type { Catalog, CatalogEntry } from "./catalog";
import { VPN_MARKS as M } from "./vpnMarks";

export type VpnGroup = "provider" | "appOnly";

const ENTRIES = [
  { id: "airvpn", name: "AirVPN", group: "provider", mark: M.airvpn },
  { id: "anonine", name: "Anonine", group: "provider", mark: M.anonine },
  { id: "astrill", name: "Astrill VPN", group: "provider", mark: M.astrill },
  { id: "azirevpn", name: "AzireVPN", group: "provider", mark: M.azirevpn },
  { id: "cactusvpn", name: "CactusVPN", group: "provider", mark: M.cactusvpn },
  { id: "cryptostorm", name: "cryptostorm", group: "provider", mark: M.cryptostorm },
  { id: "custom", name: "WireGuard", group: "provider", terms: [".conf"], mark: M.wireguard, last: true },
  { id: "fastestvpn", name: "FastestVPN", group: "provider", mark: M.fastestvpn },
  { id: "hideme", name: "hide.me", group: "provider", mark: M.hideme },
  { id: "ipvanish", name: "IPVanish", group: "provider", mark: M.ipvanish },
  { id: "ivacy", name: "Ivacy VPN", group: "provider", mark: M.ivacy },
  { id: "ivpn", name: "IVPN", group: "provider", mark: M.ivpn },
  { id: "kaspersky", name: "Kaspersky VPN", group: "provider", mark: M.kaspersky },
  { id: "levpn", name: "Le VPN", group: "provider", mark: M.levpn },
  { id: "mullvad", name: "Mullvad VPN", group: "provider", mark: M.mullvad },
  { id: "njalla", name: "Njalla VPN", group: "provider", mark: M.njalla },
  { id: "nordvpn", name: "NordVPN", group: "provider", terms: ["NordLynx"], mark: M.nordvpn },
  { id: "obscura", name: "Obscura VPN", group: "provider", mark: M.obscura },
  { id: "ovpn", name: "OVPN", group: "provider", mark: M.ovpn },
  { id: "pia", name: "Private Internet Access", group: "provider", mark: M.pia },
  { id: "privadovpn", name: "PrivadoVPN", group: "provider", mark: M.privadovpn },
  { id: "privatevpn", name: "PrivateVPN", group: "provider", mark: M.privatevpn },
  { id: "protonvpn", name: "Proton VPN", group: "provider", mark: M.protonvpn },
  { id: "purevpn", name: "PureVPN", group: "provider", mark: M.purevpn },
  { id: "strongvpn", name: "StrongVPN", group: "provider", mark: M.strongvpn },
  { id: "surfshark", name: "Surfshark", group: "provider", mark: M.surfshark },
  { id: "torguard", name: "TorGuard", group: "provider", mark: M.torguard },
  { id: "trustzone", name: "Trust.Zone", group: "provider", mark: M.trustzone },
  { id: "unlocator", name: "Unlocator VPN", group: "provider", mark: M.unlocator },
  { id: "vpnac", name: "VPN.ac", group: "provider", mark: M.vpnac },
  { id: "vpnunlimited", name: "VPN Unlimited", group: "provider", terms: ["KeepSolid"], mark: M.vpnunlimited },
  { id: "windscribe", name: "Windscribe", group: "provider", mark: M.windscribe },
  { id: "xeovo", name: "Xeovo", group: "provider", mark: M.xeovo },
  { id: "zoogvpn", name: "ZoogVPN", group: "provider", mark: M.zoogvpn },

  { id: "avast", name: "Avast SecureLine VPN", group: "appOnly", mark: M.avast },
  { id: "avira", name: "Avira Phantom VPN", group: "appOnly", mark: M.avira },
  { id: "bitdefender", name: "Bitdefender VPN", group: "appOnly", mark: M.bitdefender },
  { id: "cyberghost", name: "CyberGhost", group: "appOnly", mark: M.cyberghost },
  { id: "expressvpn", name: "ExpressVPN", group: "appOnly", mark: M.expressvpn },
  { id: "fsecure", name: "F-Secure VPN", group: "appOnly", mark: M.fsecure },
  { id: "hma", name: "HMA VPN", group: "appOnly", terms: ["Hide My Ass"], mark: M.hma },
  { id: "hotspotshield", name: "Hotspot Shield", group: "appOnly", mark: M.hotspotshield },
  { id: "mcafee", name: "McAfee VPN", group: "appOnly", mark: M.mcafee },
  { id: "mozillavpn", name: "Mozilla VPN", group: "appOnly", mark: M.mozilla },
  { id: "norton", name: "Norton VPN", group: "appOnly", mark: M.norton },
  { id: "tunnelbear", name: "TunnelBear", group: "appOnly", mark: M.tunnelbear },
  { id: "turbovpn", name: "Turbo VPN", group: "appOnly", mark: M.turbovpn },
  { id: "vyprvpn", name: "VyprVPN", group: "appOnly", mark: M.vyprvpn },
  { id: "xvpn", name: "X-VPN", group: "appOnly", mark: M.xvpn },
] as const satisfies readonly CatalogEntry<VpnGroup>[];

export type VpnProviderId = (typeof ENTRIES)[number]["id"];

/** Every VPN provider, with its group's place in the picker. */
export const VPN_PROVIDERS: Catalog<VpnGroup, VpnProviderId> = {
  groups: ["provider", "appOnly"],
  entries: ENTRIES,
};
