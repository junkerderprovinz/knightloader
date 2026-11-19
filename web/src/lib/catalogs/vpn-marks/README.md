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

**Wikimedia Commons**: `kaspersky.svg` is
<https://commons.wikimedia.org/wiki/File:Kaspersky_icon.svg>, unmodified, which
Commons holds as **public domain** (PD-textlogo, a mark too simple for
copyright). It is still Kaspersky's trademark, used under the position below.

**The brands' own press material**, used under the trademark position below
rather than under a licence. Each press page hands the files out for others to
show the brand and names no further terms:

- `fsecure.svg`: the F of `2023_Logo/VERTICAL/RGB/POSITIVE/F-SECURE_LOGO_VERTICAL_POSITIVE_RGB.svg`
  in <https://assets.f-secure.com/z/for-media/f-secure_2023_logo.zip>, the logo
  download on <https://company.f-secure.com/en/for-media>, without the word
  under it.
- `purevpn.svg` and `purevpn-hover.svg`: the disc of `PureVPN/vpn original.svg`
  and of `PureVPN/vpn all dark.svg` in
  <https://images.purevpn-tools.com/press-room/PureVPN.zip>, the media kit on
  <https://www.purevpn.com/press-enquiry.php>, each without the word beside it.
  The single-colour disc is the lit tile's drawing, where the rings of the
  colour logo would melt into one.

**selfh.st icons** (`selfhst/icons`, <https://selfh.st/icons>,
<https://github.com/selfhst/icons>), by selfh.st, under **CC BY 4.0** (text in
`LICENSE-cc-by-4.0.txt`, <https://creativecommons.org/licenses/by/4.0/>):
`azirevpn.svg`, `ipvanish.svg`, `njalla.svg`, `ovpn.svg` and `privadovpn.svg`
are the files of those names in `svg/`, and `ipvanish-hover.svg` and
`privadovpn-hover.svg` are `svg/ipvanish-dark.svg` and `svg/privadovpn-dark.svg`,
their single-colour versions, all unmodified apart from the colour the
generator gives them. CC BY 4.0 may be carried into this project's AGPL-3.0
with this attribution.

**The providers' own websites**, used under the trademark position below
rather than under a licence. Each file is the vector the provider's own page
serves, taken from the page named, with at most the word beside the symbol
left out:

- `cactusvpn.svg`: the cactus of
  <https://www.cactusvpn.com/wp-content/themes/cactusvpn/images/logo.svg>, the
  header logo of <https://www.cactusvpn.com/>.
- `cyberghost.svg`: the ghost of
  <https://assets.cyberghostvpn.com/img/frontend/cg/navigation/icons/logo-cyberghost.svg>,
  the header logo of <https://www.cyberghostvpn.com/>.
- `hideme.svg`: <https://hide.me/favicon.svg>, from <https://hide.me/en/>.
- `hma.svg`: the `hma-logo` symbol the header of <https://www.hidemyass.com/>
  draws inline, written out as a whole SVG. HMA's mark is its three letters,
  which still read on a tile.
- `hotspotshield.svg`: the shield of
  <https://www.hotspotshield.com/imgs/hotspot-shield-logo.1.0.0.svg>, the header
  logo of <https://www.hotspotshield.com/>.
- `levpn.svg`: <https://cdn.le-vpn.com/icons/favicon.svg>, from
  <https://www.le-vpn.com/>, without the layer name on its root, which a wrong
  encoding garbled. `levpn-hover.svg` is its ring and its shield in one
  colour, the crest cut out of the shield, since the white discs between them
  would swallow the shield on the lit tile.
- `obscura.svg`: <https://obscura.com/favicon.svg>, from <https://obscura.com/>,
  where Obscura VPN lives.
- `privatevpn.svg`: the shield and cloud of
  <https://privatevpn.com/_next/static/media/logotype_light.2hh1ct2vsh7pm.svg>,
  the header logo of <https://privatevpn.com/>.
- `strongvpn.svg`: the shield of
  <https://strongvpn.com/wp-content/uploads/2023/01/logo.svg>, the header logo
  of <https://strongvpn.com/>.
- `trustzone.svg`: <https://trust.zone/images/favicon/safari-pinned-tab.svg>,
  the mask icon of <https://trust.zone/>, lit in the `#005a88` the page gives it.
- `unlocator.svg`: the pin of <https://unlocator.com/logo.svg>, the logo of
  <https://unlocator.com/>, without the ids the drawing program gave several
  groups twice, which an SVG may give only one element.
- `vpnunlimited.svg`: <https://www.vpnunlimited.com/svg/vpnu_mark.svg>, from
  <https://www.vpnunlimited.com/>.
- `vyprvpn.svg`: the viper of
  <https://www.vyprvpn.com/site/templates/images/vyprvpn_logo.svg>, the header
  logo of <https://www.vyprvpn.com/>.
- `windscribe.svg`: <https://windscribe.com/images/badge-logo.svg>, from
  <https://windscribe.com/>, lit in the `#1c2332` the page sets it on.
- `xvpn.svg`: <https://xvpn.io/assetsV2/static/safari-pinned-tab.CZYJvw4n.svg>,
  the mask icon of <https://xvpn.io/>, lit on black.
- `zoogvpn.svg`: the lock of
  <https://zoogvpn.com/wp-content/uploads/2026/05/ZoogVPN-darck-logo.svg>, the
  header logo of <https://zoogvpn.com/>.

HMA VPN lights in the `#0059a5` its page names as its tile colour.

## Owners' marks for a narrower product

A few providers wear the mark of the company that runs them, because the VPN
has no mark of its own that could be sourced cleanly: Avast SecureLine VPN,
Avira Phantom VPN, Bitdefender VPN, F-Secure VPN, Kaspersky VPN, McAfee VPN,
Mozilla VPN and Norton VPN. The own WireGuard configuration wears WireGuard's
mark, since that is what it is.

A provider whose own mark has no clean source wears the glyph of its kind.
None of the icon sets above carries Anonine, Astrill VPN, cryptostorm,
FastestVPN, Ivacy VPN, TorGuard, TunnelBear, Turbo VPN, VPN.ac or Xeovo, and
their own sites have no vector of the symbol on its own. Anonine's logo is a
picture inside an SVG, and so is Xeovo's favicon, whose wordmark is too wide
for a tile. Astrill draws its star and its name as one shape, TorGuard's
symbol is a speck beside its name, and VPN.ac's sits inside its name and
tagline. cryptostorm and FastestVPN serve their logos only as pictures, Turbo
VPN's mask icon is a plain square, and the sites of Ivacy VPN and TunnelBear
turn away a plain download.

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
single-colour version under `HOVER`: a Simple Icons slug, or `<id>-hover.svg`
beside the logo. Then give the provider its `mark` in `src/vpnProviders.ts`
and run the generator:

    npm pack simple-icons@16 && tar xzf simple-icons-*.tgz
    python scripts/gen_vpn_marks.py package/icons

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints.
