# The download services' marks, and where they come from

`scripts/gen_download_marks.py` turns these files, and the marks it takes from
Simple Icons, into `src/downloadMarks.ts`: each mark as markup for `BrandMark`,
with the colour its tile lights up in ("Brand tiles" in the design language).
The download catalog in `src/downloadProviders.ts` names the mark each debrid
service and file hoster wears, and an app reaches them through
`@glimstone/core/downloadProviders`.

The generator treats these files the way it treats the storage providers' marks
(see `../storage-marks/README.md`): gradients and groups stay as drawn, every
`id` gets a `glim-<id>-` prefix, the viewBox is tightened onto the ink, and
every colour is wrapped in `var(--mark-ink, ...)` or `var(--mark-cut, ...)`.
Four of the files are the storage providers' own, copied here so that each
catalog stays in an entry of its own, and they come out the same.

## Sources and licences

**Simple Icons** (<https://simpleicons.org>) for `debridlink` and `mediafire`.
The set is **CC0 1.0** and asks for nothing, and neither icon names a licence
of its own.

**Dashboard Icons** (`homarr-labs/dashboard-icons`, <https://dashboardicons.com>),
under **Apache-2.0** (text in `LICENSE-apache-2.0.txt`), copyright the Homarr
Labs team and contributors: `mega.svg` is `svg/mega-nz.svg`, `premiumize.svg`
is `svg/premiumize.svg` and `realdebrid.svg` is `svg/real-debrid.svg`, all
unmodified.

**The project's own repository**: `pixeldrain.svg` is
`res/static/img/pixeldrain.svg` from <https://github.com/Fornaxian/pixeldrain_web>,
**AGPL-3.0**, unmodified.

**The brand's own material**: `fichier.svg` is the cube traced from 1Fichier's
`logo.png` on <https://img.1fichier.com>, as `../storage-marks/README.md`
describes. It is 1Fichier's, used under the trademark position below rather
than under a licence.

Apache-2.0 may be carried into this project's AGPL-3.0, and the AGPL-3.0 file
is under this project's own licence, so the combined work stays AGPL-3.0.

## Left out

A provider whose own mark has no clean source wears the glyph of its kind. No
icon set carries AllDebrid, BestDebrid, CocoLeech, CoolDebrid, DebridItalia,
Deepbrid, FakirDebrid, LinkSnappy, Mega-Debrid, MultiUp, MyDebrid, NeoDebrid,
Offcloud, ProLeech, RPNet, TorBox or Zevera, nor Clicknupload, ddownload,
FileBoom, filer.net, FileSpace, Gofile, KatFile, Keep2Share, KrakenFiles,
NitroFlare, Rapidgator, Send.now, TezFiles, Uploadrar or Workupload, and their
sites hand out their logos as pictures and favicons under no stated terms.

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
for one, then give the provider its `mark` in `src/downloadProviders.ts` and
run the generator:

    npm pack simple-icons@16 && tar xzf simple-icons-*.tgz
    python scripts/gen_download_marks.py package/icons

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints.
