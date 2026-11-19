# The notification services' marks, and where they come from

`scripts/gen_service_marks.py` turns these files, and four marks from Simple
Icons, into `src/serviceMarks.ts`: each mark as markup for `BrandMark`, with the
colour its tile lights up in ("Brand tiles" in the design language). The
notification catalog in `src/notifyProviders.ts` names the mark each service
wears, and an app reaches it through `@glimstone/core/notifyProviders`. An app
that draws its own tiles imports the marks from `@glimstone/core/serviceMarks`
and passes the one it means by id, `SERVICE_MARKS.gotify`. The picker never
chooses a mark by a service's name.

The files are reproduced as drawn, gradients, groups and all, because a mark
drawn in several tones is several tones, and flattening one would be redrawing
somebody's logo. What the generator does to a file is mechanical: it prefixes
every `id` with `glim-<id>-` so two marks on one page cannot swap gradients,
folds an Illustrator `<style>` block into the elements it paints, turns an
Illustrator `<switch>` into the group a browser draws, tightens the viewBox onto
the ink, and wraps each colour in `var(--mark-ink, ...)` or `var(--mark-cut, ...)`
so a lit tile can turn the mark into its ink. A colour one theme's tile would
swallow becomes a `light-dark()` pair, same hue, other lightness.

## Sources and licences

**Simple Icons** (<https://simpleicons.org>, **CC0 1.0**) for `discord`, `ntfy`,
`matrix` and `unraid`. CC0 asks for nothing, so they need no file here.

**Dashboard Icons** (`homarr-labs/dashboard-icons`, <https://dashboardicons.com>)
for `slack.svg` and `gotify.svg`. They are under the **Apache-2.0** licence, whose
text is beside this file as `LICENSE-dashboard-icons.txt`. Copyright the Homarr
Labs team and contributors. Apache-2.0 asks for attribution, which is why it is
named here, in the generator and above every mark built from it. Gotify's gopher
is the Gotify project's own drawing, published under **CC BY 4.0** in
<https://github.com/gotify/logo>; the file here is Dashboard Icons' cleaned copy
of it.

**Shoutrrr's repository**, <https://github.com/nicholas-fedor/shoutrrr>, for
`shoutrrr.svg`: `docs/assets/media/shoutrrr-logotype.svg`, the gopher with the
megaphone in its speech bubble. The repository is under the **MIT** licence,
whose text is beside this file as `LICENSE-shoutrrr.txt`; it asks for the
copyright notice to travel with the file. Copyright Nicholas Fedor. The file is
the Inkscape original with what draws nothing taken out: the JPEG it was traced
over, which lies outside the drawing, the hidden elements, Inkscape's own
attributes and metadata, and a `paint-order:normal`, the default. Every path,
fill and transform that draws is as drawn.

**The brands' own material**, for the two that no icon set carries. Each is its
owner's, used under the trademark position below.

- `healthchecks.svg`: `static/img/favicon.svg` from Healthchecks.io's own
  repository <https://github.com/healthchecks/healthchecks>, unmodified.
- `apprise.svg`: Apprise has its logo only as PNG,
  `apprise/assets/themes/default/apprise-logo.png` in
  <https://github.com/caronc/apprise>, the mark and the word APPRISE side by
  side. The mark alone was traced from it: the megaphone and its A in the two
  teals sampled from the image, and the arc behind them as the two circles that
  fit its edges, since the word's first P covers part of it. The original fades
  the arc out at both ends, and the trace cuts it where the fade begins.

Apache-2.0 and MIT may both be carried into this project's AGPL-3.0, and the
combined work stays AGPL-3.0.

## Trademarks

Every mark here is a trademark of its owner. It is used the one way a trademark
may be used without permission, to refer to the thing it names: a mark sits on
the tile that picks **that service**, reproduced unmodified apart from its
colour, and nothing here claims endorsement by or affiliation with any of them.

## Adding one

For a mark Simple Icons carries, add a line to `SIMPLE` in the generator. For
any other, drop the SVG in here unchanged, add a line to `LOCAL` naming its
source and licence, add the licence to the section above and its text beside
this file when it asks for one, and run the generator:

    npm pack simple-icons@16 && tar xzf simple-icons-*.tgz
    python scripts/gen_service_marks.py package/icons

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints. It refuses a file whose text it would otherwise drop silently.
