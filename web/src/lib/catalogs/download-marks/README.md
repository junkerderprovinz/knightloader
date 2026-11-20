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
unmodified. `premiumize-hover.svg`, the lit tile's drawing, is the same
figure and check in one colour, with a gap cut into the figure along the
check so the two stay apart in a single ink.

**The project's own repository**: `pixeldrain.svg` is
`res/static/img/pixeldrain.svg` from <https://github.com/Fornaxian/pixeldrain_web>,
**AGPL-3.0**, unmodified.

**The brand's own material**: `fichier.svg` is the cube traced from 1Fichier's
`logo.png` on <https://img.1fichier.com>, as `../storage-marks/README.md`
describes. It is 1Fichier's, used under the trademark position below rather
than under a licence.

**The providers' own websites**, used under the same trademark position. Each
file is the vector the provider's own page serves:

- `torbox.svg`: <https://torbox.app/logo.svg>, the logo of <https://torbox.app/>.
- `workupload.svg`: the rocket of
  <https://workupload.com/bundles/app/img/workupload_logo_medium.svg>, the
  header logo of <https://workupload.com/>, without the word beside it.
- `tezfiles.svg`: the logo the header of <https://tezfiles.com/> draws inline,
  its name with the share symbol in it, each inner `svg` turned into the group
  it draws as. It is marked as a wordmark, and on a lit tile its grey and
  orange letters all turn ink.

**The brands' own logos as SVG files**, under the same trademark position,
each as the brand draws it:

- `deepbrid.svg`: the D of Deepbrid's logo.
- `ddownload.svg`: ddownload's name, marked as a wordmark.
- `fileboom.svg`: FileBoom's name with the bomb in it, marked as a wordmark.
- `filer.svg`: the F in the green square of filer.net's logo, without the rest
  of its name.
- `gofile.svg`: Gofile's folder with its speed lines.
- `keep2share.svg`: Keep2Share's name, marked as a wordmark, without the slate
  plate it sits on, which is the colour its tile lights in. Off the plate its
  2 is cut out of a box of its blue, and on the light ground the white
  letters take that slate.
- `mydebrid.svg`: the M of MyDebrid's logo.
- `rapidgator.svg`: Rapidgator's rg on its orange plate.
- `zevera.svg`: Zevera's V in its ring, lit in its yellow, `#ffce32`, with the
  ring drawn a little heavier so it holds at a tile's size. The white spot in
  the notch of the V keeps its white on the lit tile.

**The brands' own pictures, traced**, for the providers whose logo has no
vector anywhere, under the same trademark position. Each was traced with
potrace from the largest clean picture, its colours snapped to the few the
picture is drawn in and compared with it side by side:

- `alldebrid.svg`: AllDebrid's name in its black outline, from a PNG of the
  logo its site serves only as a picture
  (<https://cdn.alldebrid.com/lib/images/default/logo_alldebrid.png>), in its
  black, yellow and white, marked as a wordmark and lit in its yellow,
  `#fbc624`.
- `cocoleech.svg`: <https://cocoleech.com/images/coco2.png>, its name in brown
  with the green swoosh, marked as a wordmark. The brown turns the lighter
  brown of its own highlight on the dark ground.
- `cooldebrid.svg`: the plug of <https://cooldebrid.com/img/logo1.png>,
  without the word beside it.
- `fakirdebrid.svg`: the stopwatch of <https://fakirdebrid.net/images/logo2.png>,
  without the word beside it. The site turns away a plain download, so the
  file came from the Internet Archive's copy of 5 August 2025.
- `katfile.svg`: the round FILE badge of <https://katfile.com/images/logo.png>,
  without the K and the T beside it, lit in the `#3074b5` of its page.
- `krakenfiles.svg`: the KF of <https://krakenfiles.com/images/kf_logo_dark.png>.
- `megadebrid.svg`: <https://cdn.mega-debrid.eu/images/publicHome/logo.png>, its
  white name, marked as a wordmark and lit in the `#239f85` of its page.
- `nitroflare.svg`: the flame of <https://nitroflare.com/img/symbol.png>.
- `offcloud.svg`: <https://offcloud.com/images/logo-blue-short-lg.png>.
- `rpnet.svg`: the white flash of <https://premium.rpnet.biz/img/rpnet-logo.svg>,
  which holds it as a picture, lit in the `#232838` of its page.
- `sendnow.svg`: the ribbon of <https://send.now/static/img/logo.png> in two of
  its blues. Its folds lie beside each other, so on a lit tile they all turn
  ink.
- `uploadrar.svg`: Uploadrar's name, from the PNG of the logo its site serves
  only as a picture, marked as a wordmark and lit in its teal, `#10bbb8`. Its
  black turns white on the dark ground.

The white logos take their tile colour on the light ground, where white would
vanish.

Apache-2.0 may be carried into this project's AGPL-3.0, and the AGPL-3.0 file
is under this project's own licence, so the combined work stays AGPL-3.0.

## Left out

A provider whose own mark has no clean source wears the glyph of its kind.
That is BestDebrid alone: its header shows a stock download icon beside its
name, and its only other picture is a glowing banner for social media.

## Trademarks

Every mark here is a trademark of its owner. It is used the one way a trademark
may be used without permission, to refer to the thing it names: a mark sits on
the tile that picks **that provider**, reproduced unmodified apart from its
colour, and nothing here claims endorsement by or affiliation with any of them.

## Adding one

For a mark Simple Icons carries, add a line to `SIMPLE` in the generator. For
any other, drop the SVG in here as its source has it, leaving out at most the
word beside the mark, add a line to `LOCAL` naming its source and licence, and
its id to `WORDMARK` when the mark is the brand's name written out, add both to
the section above and the licence's text beside this file when it asks
for one, then give the provider its `mark` in `src/downloadProviders.ts` and
run the generator:

    npm pack simple-icons@16 && tar xzf simple-icons-*.tgz
    python scripts/gen_download_marks.py package/icons

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints.
