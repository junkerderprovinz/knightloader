// The VPN catalog: the providers an app can route its traffic through, each
// with its mark. An app hands catalogPicker the ids it can connect to and keeps
// what it needs to sign in (the fields its form asks for, whether the provider
// forwards a port) beside them by id.
//
// A provider that hands out WireGuard, as a configuration file or through a
// login, stands under providers. One that keeps WireGuard inside its own app
// stands apart, so an app can show it as a tile that waits rather than leave
// somebody searching for it. The own configuration takes any WireGuard server
// and is the tile an app marks last.

import type { Catalog, CatalogEntry } from "./catalog";
import { VPN_MARKS as M } from "./vpnMarks";

export type VpnGroup = "provider" | "appOnly";

const VPN = "IconShieldOn";

const ENTRIES = [
  { id: "airvpn", name: "AirVPN", group: "provider", mark: M.airvpn },
  { id: "anonine", name: "Anonine", group: "provider", glyph: VPN },
  { id: "astrill", name: "Astrill VPN", group: "provider", glyph: VPN },
  { id: "azirevpn", name: "AzireVPN", group: "provider", glyph: VPN },
  { id: "cactusvpn", name: "CactusVPN", group: "provider", glyph: VPN },
  { id: "cryptostorm", name: "cryptostorm", group: "provider", glyph: VPN },
  { id: "custom", name: "Own WireGuard config", group: "provider", terms: [".conf"], mark: M.wireguard, word: true },
  { id: "fastestvpn", name: "FastestVPN", group: "provider", glyph: VPN },
  { id: "hideme", name: "hide.me", group: "provider", glyph: VPN },
  { id: "ipvanish", name: "IPVanish", group: "provider", glyph: VPN },
  { id: "ivacy", name: "Ivacy VPN", group: "provider", glyph: VPN },
  { id: "ivpn", name: "IVPN", group: "provider", mark: M.ivpn },
  { id: "kaspersky", name: "Kaspersky VPN", group: "provider", glyph: VPN },
  { id: "levpn", name: "Le VPN", group: "provider", glyph: VPN },
  { id: "mullvad", name: "Mullvad VPN", group: "provider", mark: M.mullvad },
  { id: "njalla", name: "Njalla VPN", group: "provider", glyph: VPN },
  { id: "nordvpn", name: "NordVPN", group: "provider", terms: ["NordLynx"], mark: M.nordvpn },
  { id: "obscura", name: "Obscura VPN", group: "provider", glyph: VPN },
  { id: "ovpn", name: "OVPN", group: "provider", glyph: VPN },
  { id: "pia", name: "Private Internet Access", group: "provider", mark: M.pia },
  { id: "privadovpn", name: "PrivadoVPN", group: "provider", glyph: VPN },
  { id: "privatevpn", name: "PrivateVPN", group: "provider", glyph: VPN },
  { id: "protonvpn", name: "Proton VPN", group: "provider", mark: M.protonvpn },
  { id: "purevpn", name: "PureVPN", group: "provider", glyph: VPN },
  { id: "strongvpn", name: "StrongVPN", group: "provider", glyph: VPN },
  { id: "surfshark", name: "Surfshark", group: "provider", mark: M.surfshark },
  { id: "torguard", name: "TorGuard", group: "provider", glyph: VPN },
  { id: "trustzone", name: "Trust.Zone", group: "provider", glyph: VPN },
  { id: "unlocator", name: "Unlocator VPN", group: "provider", glyph: VPN },
  { id: "vpnac", name: "VPN.ac", group: "provider", glyph: VPN },
  { id: "vpnunlimited", name: "VPN Unlimited", group: "provider", terms: ["KeepSolid"], glyph: VPN },
  { id: "windscribe", name: "Windscribe", group: "provider", glyph: VPN },
  { id: "xeovo", name: "Xeovo", group: "provider", glyph: VPN },
  { id: "zoogvpn", name: "ZoogVPN", group: "provider", glyph: VPN },

  { id: "avast", name: "Avast SecureLine VPN", group: "appOnly", mark: M.avast },
  { id: "avira", name: "Avira Phantom VPN", group: "appOnly", mark: M.avira },
  { id: "bitdefender", name: "Bitdefender VPN", group: "appOnly", mark: M.bitdefender },
  { id: "cyberghost", name: "CyberGhost", group: "appOnly", glyph: VPN },
  { id: "expressvpn", name: "ExpressVPN", group: "appOnly", mark: M.expressvpn },
  { id: "fsecure", name: "F-Secure VPN", group: "appOnly", glyph: VPN },
  { id: "hma", name: "HMA VPN", group: "appOnly", terms: ["Hide My Ass"], glyph: VPN },
  { id: "hotspotshield", name: "Hotspot Shield", group: "appOnly", glyph: VPN },
  { id: "mcafee", name: "McAfee VPN", group: "appOnly", mark: M.mcafee },
  { id: "mozillavpn", name: "Mozilla VPN", group: "appOnly", mark: M.mozilla },
  { id: "norton", name: "Norton VPN", group: "appOnly", mark: M.norton },
  { id: "tunnelbear", name: "TunnelBear", group: "appOnly", glyph: VPN },
  { id: "turbovpn", name: "Turbo VPN", group: "appOnly", glyph: VPN },
  { id: "vyprvpn", name: "VyprVPN", group: "appOnly", glyph: VPN },
  { id: "xvpn", name: "X-VPN", group: "appOnly", glyph: VPN },
] as const satisfies readonly CatalogEntry<VpnGroup>[];

export type VpnProviderId = (typeof ENTRIES)[number]["id"];

/** Every VPN provider, with its group's place in the picker. */
export const VPN_PROVIDERS: Catalog<VpnGroup, VpnProviderId> = {
  groups: ["provider", "appOnly"],
  entries: ENTRIES,
};
