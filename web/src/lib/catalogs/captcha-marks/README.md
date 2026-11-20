# The captcha solvers' marks, and where they come from

`scripts/gen_captcha_marks.py` turns these files into `src/captchaMarks.ts`:
each mark as markup for `BrandMark`, with the colour its tile lights up in
("Brand tiles" in the design language). The captcha catalog in
`src/captchaProviders.ts` names the mark each solver wears, and an app reaches
them through `@glimstone/core/captchaProviders`.

The generator treats these files the way it treats the storage providers' marks
(see `../storage-marks/README.md`): gradients and groups stay as drawn, every
`id` gets a `glim-<id>-` prefix, the viewBox is tightened onto the ink, and
every colour is wrapped in `var(--mark-ink, ...)` or `var(--mark-cut, ...)`.

## Sources and licences

**The provider's own repository**: `capmonster.svg` is `static/img/logoNoText.svg`
from <https://github.com/ZennoLab/capmonster-captcha-solver-docs>, the
documentation of CapMonster Cloud, **MIT**, copyright CapMonster Cloud (text in
`LICENSE-capmonster-docs.txt`), unmodified. MIT may be carried into this
project's AGPL-3.0, so the combined work stays AGPL-3.0.

**The providers' own websites**, used under the trademark position below
rather than under a licence. Each file is the vector the provider's own page
serves:

- `capsolver.svg`: <https://www.capsolver.com/favicon.svg>, the icon of
  <https://www.capsolver.com/>. `capsolver-hover.svg` is the same drawing in
  its green alone and without the clip path, which cuts nothing, so the pin
  keeps its shape on the lit tile instead of falling into the tile colour.
- `solvecha.svg`: <https://solvecha.com/site/static/favicon.svg>, the icon of
  <https://solvecha.com/>. The generator lifts the teal square it stands on,
  and the tile lights in that teal, `#0e7c66`.
- `twocaptcha.svg`: the lock of
  <https://2captcha.com/dist/web/assets/twocaptcha-logo-CYCKxIN9.svg>, the
  header logo of <https://2captcha.com/>, without the word beside it. Its id
  is not `2captcha`, which a script cannot name a constant.
- `anticaptcha.svg`: <https://anti-captcha.com/_nuxt/logo.108c78f7.svg>, the
  header logo of <https://anti-captcha.com/>, the first letters of its name
  drawn in white. It lights in the `#111d35` the page sets it on and takes
  that navy on the light ground. Its letters run the full width of a mark's
  box, so the generator gives the box a quarter more room, half on each side,
  to keep them off the name.
- `deathbycaptcha.svg`: DeathByCaptcha's boxed name as an SVG, marked as a
  wordmark. Its near-black turns white on the dark ground.

## Left out

A solver whose own mark has no clean source wears the captcha glyph. Every
solver in the catalog has a mark.

## Trademarks

Every mark here is a trademark of its owner. It is used the one way a trademark
may be used without permission, to refer to the thing it names: a mark sits on
the tile that picks **that solver**, reproduced unmodified apart from its
colour, and nothing here claims endorsement by or affiliation with any of them.

## Adding one

Drop the SVG in here as its source has it, leaving out at most the word beside
the mark, add a line to `LOCAL` in the generator naming its source and licence,
and its id to `HOVER` with an `<id>-hover.svg` where its colours do not survive
one ink, and to `WORDMARK` when the mark is the brand's name written out, add
both to the section above and the licence's text beside this file
when it asks for one, then give the solver its `mark` in
`src/captchaProviders.ts` and run the generator:

    python scripts/gen_captcha_marks.py

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints. Should Simple Icons take one of these brands, give the generator a
`SIMPLE` list and the icons folder the way `gen_transfer_marks.py` has them.
