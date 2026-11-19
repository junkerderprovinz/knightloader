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

## Left out

A solver whose own mark has no clean source wears the captcha glyph. Neither
Simple Icons nor Dashboard Icons carries 2Captcha, 9kw.eu, Anti-Captcha,
CapSolver, DeathByCaptcha or Solvecha. Their client libraries on GitHub hold
no logo, DeathByCaptcha's browser extensions hold it only as a picture, and
their sites hand it out under no stated terms.

## Trademarks

Every mark here is a trademark of its owner. It is used the one way a trademark
may be used without permission, to refer to the thing it names: a mark sits on
the tile that picks **that solver**, reproduced unmodified apart from its
colour, and nothing here claims endorsement by or affiliation with any of them.

## Adding one

Drop the SVG in here as its source has it, leaving out at most the word beside
the mark, add a line to `LOCAL` in the generator naming its source and licence,
add both to the section above and the licence's text beside this file when it
asks for one, then give the solver its `mark` in `src/captchaProviders.ts` and
run the generator:

    python scripts/gen_captcha_marks.py

It needs `svgelements` (`pip install svgelements`) to measure where a mark
paints. Should Simple Icons take one of these brands, give the generator a
`SIMPLE` list and the icons folder the way `gen_transfer_marks.py` has them.
