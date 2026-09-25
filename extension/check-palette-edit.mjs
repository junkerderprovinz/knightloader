// A palette colour edited while disco walks shows at once, not only once the
// picker closes.
//
// The walk (disco.js) builds its loop from the palette when applyDisco runs and
// paints from that loop on every frame after. A new palette written to the
// root alone lasts until the next frame, which paints the old loop's colours
// back, or under reduced motion until the next step. So a palette edit that
// does not go through renderAppearance has to apply disco again.
//
// The picker also sends one edit per drag frame, and the frames overlap. An
// edit that read the palette back from storage could miss the frame before it
// and write that frame's colour back over it, so the edits start from the
// palette the page keeps in livePalette, as the accent swatches do from
// liveAccentCustoms.
//
// The check runs appearance.js, the walk and editPaletteColour from options.js
// against a stand-in root and storage, gliding and stepping. It paints every
// position black, one edit per frame with none waiting for the one before, and
// then every colour on the root and in storage has to be black.
//
// Run by CI and by hand: `node extension/check-palette-edit.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];
const fail = (why) => problems.push(why);

/** The source of one top-level function of a page script, keyword to closing brace. */
function functionSource(text, name) {
  const start = text.search(new RegExp(`^(async )?function ${name}\\(`, 'm'));
  if (start < 0) return null;
  let depth = 0;
  for (let i = text.indexOf('{', text.indexOf(')', start)); i < text.length; i++) {
    if (text[i] === '{') depth++;
    else if (text[i] === '}' && --depth === 0) return text.slice(start, i + 1);
  }
  return null;
}

const BLACK = '#000000';
const options = read('src', 'options.js');
const edit = functionSource(options, 'editPaletteColour');
if (!edit) fail('src/options.js: no editPaletteColour() - nothing stores a palette colour while the picker is open');
const live = options.match(/^let livePalette = .*;$/m)?.[0];
if (!live) fail('src/options.js: no livePalette - every drag frame reads the palette back from storage');

for (const steps of edit && live ? [false, true] : []) {
  const mode = steps ? 'stepping (reduced motion)' : 'gliding';
  const props = {};
  const storage = { rainbow: true, rainbowDisco: true };
  let due = [];
  const ctx = vm.createContext({
    document: {
      documentElement: {
        style: { setProperty: (k, v) => (props[k] = v), removeProperty: (k) => delete props[k] },
        setAttribute() {},
        removeAttribute() {},
      },
    },
    window: { matchMedia: () => ({ matches: steps }) },
    requestAnimationFrame: (f) => due.push(f),
    cancelAnimationFrame: () => {},
    chrome: {
      storage: {
        local: {
          get: async (keys) => Object.fromEntries(keys.filter((k) => k in storage).map((k) => [k, storage[k]])),
          set: async (patch) => Object.assign(storage, structuredClone(patch)),
        },
      },
    },
  });
  for (const f of ['appearance.js', 'discoLoop.js', 'disco.js']) {
    vm.runInContext(read('src', f), ctx, { filename: f });
  }
  vm.runInContext(`${live}\n${edit}`, ctx, { filename: 'options.js' });

  let now = 0;
  const frames = (count) => {
    for (let i = 0; i < count; i++) {
      const run = due;
      due = [];
      now += 16;
      for (const f of run) f(now);
    }
  };

  const a = await ctx.applyAppearance();
  ctx.applyDisco(a.disco);
  // renderAppearance seeds the palette the swatches edit from.
  ctx.seed = a.rainbow.palette;
  vm.runInContext('livePalette = seed.slice()', ctx);
  frames(30);
  const positions = vm.runInContext('RAINBOW.length', ctx);
  await Promise.all(Array.from({ length: positions }, (_, i) => ctx.editPaletteColour(i, BLACK)));
  // Longer than one of the walk's 2.4 second steps.
  frames(200);

  const stored = storage.rainbowPalette ?? [];
  if (stored.length !== positions || stored.some((c) => c !== BLACK)) {
    fail(
      `${mode}: with every palette colour edited to ${BLACK}, one frame each, storage keeps ${stored.join(' ')}: ` +
        'a frame wrote back the colour an earlier frame had just changed',
    );
  }
  const shown = Array.from({ length: positions }, (_, i) => props[`--rb-${i}`]);
  if (shown.some((c) => c !== BLACK)) {
    fail(
      `${mode}: with every palette colour edited to ${BLACK} the walk still paints ${shown.join(' ')}, ` +
        'not the palette that was edited, until the picker closes',
    );
  }
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: palette colours edited one per drag frame all reach storage and the walk at once, gliding and stepping');
