// A well selector keeps to one row at every width and font scale (GlimStone,
// "It keeps to one row").
//
// Its segments share the groove equally while every name fits, take the
// width of their names after that, and show their glyphs alone where every
// segment has one. A well without a glyph on every segment turns into the
// dropdown instead. It never stands two to a row.
//
// The label widths are what the phone measures at 14 points in German, in the
// 312-point groove of a 360-point screen, and at a large font scale.
//
// Run by CI and by hand, from mobile/: `node check-well-selector.mjs`.
import { registerHooks } from 'node:module';

// The app imports its own modules without an extension, which Metro resolves
// and Node does not, so a relative import without one is tried as a .ts file.
const src = new URL('./src/', import.meta.url).href;
registerHooks({
  resolve(specifier, context, next) {
    const ours = context.parentURL?.startsWith(src);
    if (ours && /^\.\.?\//.test(specifier) && !/\.\w+$/.test(specifier)) return next(`${specifier}.ts`, context);
    return next(specifier, context);
  },
});

const { segmentFlex, wellForm } = await import(new URL('./src/components/wellForm.ts', import.meta.url).href);
const { readFileSync } = await import('node:fs');

const problems = [];
const check = (what, got, want) => {
  if (got !== want) problems.push(`${what}: got ${got}, want ${want}`);
};

const GROOVE = 312;
// A 16-point glyph and the 5 points before its name.
const GLYPH = 21;
const scaled = (widths, by) => widths.map((w) => w * by);

const theme = [25, 40, 42];
const corners = [30, 95, 35];
const tabs = [70, 130];

check('short names share the groove equally', wellForm(GROOVE, theme, GLYPH), 'even');
check('a long name takes its own width', wellForm(GROOVE, corners, GLYPH), 'content');
check('names too long for the row leave the glyphs alone', wellForm(GROOVE, scaled(corners, 1.6), GLYPH), 'squeezed');
check('a well without glyphs keeps its names while they fit', wellForm(GROOVE, tabs, null), 'even');
check('and turns into the dropdown once they do not', wellForm(GROOVE, scaled(tabs, 2), null), 'list');

// Android cuts a one-line name that overflows its segment by any fraction of
// a point, so a name only counts as fitting with room to spare.
const share = (GROOVE - 6 - 2 * 2) / 3;
check('a name that fills its share to the point takes its own width', wellForm(GROOVE, [share - 12, 40, 40], null), 'content');
check('a name with room to spare still shares the groove equally', wellForm(GROOVE, [share - 16, 40, 40], null), 'even');

// Yoga reads `flex: 1` as a basis of 0 whenever flexBasis is auto, so a
// segment laid out by its name has to say grow, shrink and basis apart.
check('a segment laid out by its name starts from its name', JSON.stringify(segmentFlex('content')), JSON.stringify({ flexGrow: 1, flexShrink: 0, flexBasis: 'auto' }));
check('an even segment starts from nothing', JSON.stringify(segmentFlex('even')), JSON.stringify({ flexGrow: 1, flexShrink: 1, flexBasis: 0 }));
const glim = readFileSync(new URL('./src/components/glim.tsx', import.meta.url), 'utf8');
check('the segment style leaves the flex shorthand out', /^\s*segment: \{[^}]*\bflex: /m.test(glim), false);

if (problems.length > 0) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: a well keeps to one row at every width and font scale');
