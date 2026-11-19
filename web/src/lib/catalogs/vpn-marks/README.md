# The VPN providers' marks, and where they come from

`scripts/gen_vpn_marks.py` turns these files, and the marks it takes from
Simple Icons, into `src/vpnMarks.ts`: each mark as markup for `BrandMark`, with
the colour its tile lights up in ("Brand tiles" in the design language). The
VPN catalog in `src/vpnProviders.ts` names the mark each provider wears, and an
app reaches them through `@glimstone/core/vpnProviders`.

The generator treats these files the way it treats the storage providers' marks
(see `../storage-marks/README.md`): gradients and groups stay as drawn, every
`id` gets a `glim-<id>-` prefix, the viewBox is tightened onto the ink, and
every colour is wrapped in `var(--mark-ink, ...)` or `var(--mark-cut, ...)`.
A logo of several colours that would melt into one blot on its lit tile also
brings a `hover` drawing, the brand's single-colour version with its holes,
which the lit tile shows instead.

## Sources and licences

**Simple Icons** (<https://simpleicons.org>) for `avast`, `avira`,
`bitdefender`, `expressvpn`, `mcafee`, `mozilla`, `mullvad`, `nordvpn`,
`norton`, `pia` (the set's `privateinternetaccess`), `surfshark` and
`wireguard`, and for the lit tile's drawing of `protonvpn`, whose gradient
would otherwise flatten into a single wedge there. The set is **CC0 1.0** and
asks for nothing, and none of these icons names a licence of its own.

**Dashboard Icons** (`homarr-labs/dashboard-icons`, <https://dashboardicons.com>),
under **Apache-2.0** (text in `LICENSE-apache-2.0.txt`), copyright the Homarr
Labs team and contributors: `airvpn.svg` is `svg/airvpn.svg` and
`protonvpn.svg` is `svg/proton-vpn.svg`, both unmodified. Proton VPN's tile
lights in Proton's violet, `#6d4aff`, rather than the pale lilac its gradient
covers most with.

**The provider's own repository**: `ivpn.svg` is
`ui/References/Linux/ui/ivpnicon.svg` from <https://github.com/ivpn/desktop-app>,
**GPL-3.0**, unmodified. The GPL-3.0 may be carried into this project's
AGPL-3.0, and Apache-2.0 may too, so the combined work stays AGPL-3.0.

## Owners' marks for a narrower product

A few providers wear the mark of the company that runs them, because the VPN
has no mark of its own that could be sourced cleanly: Avast SecureLine VPN,
Avira Phantom VPN, Bitdefender VPN, McAfee VPN, Mozilla VPN and Norton VPN. The
own WireGuard configuration wears WireGuard's mark, since that is what it is.

A provider whose own mark has no clean source wears the glyph of its kind. No
icon set carries Anonine, Astrill VPN, AzireVPN, CactusVPN, cryptostorm,
CyberGhost, FastestVPN, F-Secure VPN, hide.me, HMA VPN, Hotspot Shield,
IPVanish, Ivacy VPN, Le VPN, Njalla VPN, OVPN, PrivadoVPN, PrivateVPN, PureVPN,
StrongVPN, TorGuard, Trust.Zone, TunnelBear, Turbo VPN, Unlocator VPN, VPN.ac,
VPN Unlimited, VyprVPN, Xeovo, X-VPN or ZoogVPN, and their sites hand out
their logos as pictures or under no stated terms. Windscribe's own repository
carries only its wordmark, Obscura VPN's client names no licence the mark could
be used under, and the Kaspersky mark in Simple Icons is a wordmark five times
as wide as it is tall, which a tile shrinks past reading.

## Trademarks

Every mark here is a trademark of its owner. It is used the one way a trademark
may be used without permission, to refer to the thing it names: a mark sits on
the tile that picks **that provider**, reproduced unmodified apart from its
colour, and nothing here claims endorsement by or affiliation with any of them.

## Adding one

For a mark Simple Icons carries, add a line to `SIMPLE` in the generator. For
any other, drop the SVG in here as its source has it, leaving out at most the
word beside the mark, add a line to `LOCAL` naming its source and licence, add
both to the section above and the licence's text beside this file when it asks
for one. A logo whose colours do not survive one ink gets its brand's
single-colour version from Simple Icons under `HOVER`. Then give the provider
its `mark` in `src/vpnProviders.ts` and run the generator:

    npm pack simple-icons@16 && tar xzf simple-icons-*.tgz
    python scripts/gen_vpn_marks.py package/icons

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints.
