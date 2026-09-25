// A palette colour edited while disco walks shows at once, not only once the
// picker closes.
//
// The walk (disco.js) builds its loop from the palette when applyDisco runs and
// paints from that loop on every frame after. A new palette written to the
// root alone lasts until the next frame, which paints the old loop's colours
// back, or under reduced motion until the next step. So a palette edit that
// does not go through renderAppearance has to apply disco again.
//
// The check runs appearance.js, the walk and editPaletteColour from options.js
// against a stand-in root and storage, gliding and stepping. It paints every
// position black, one edit at a time as the picker does, and then every colour
// on the root has to be black.
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
const edit = functionSource(read('src', 'options.js'), 'editPaletteColour');
if (!edit) fail('src/options.js: no editPaletteColour() - nothing stores a palette colour while the picker is open');

for (const steps of edit ? [false, true] : []) {
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
  vm.runInContext(edit, ctx, { filename: 'options.js' });

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
  frames(30);
  const positions = vm.runInContext('RAINBOW.length', ctx);
  for (let i = 0; i < positions; i++) await ctx.editPaletteColour(i, BLACK);
  // Longer than one of the walk's 2.4 second steps.
  frames(200);

  const shown = Array.from({ length: positions }, (_, i) => props[`--rb-${i}`]);
  if (shown.some((c) => c !== BLACK)) {
    fail(
      `${mode}: with every palette colour edited to ${BLACK} the walk still paints ${shown.join(' ')}, ` +
        'the palette it started with, until the picker closes',
    );
  }
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: a palette colour edited while disco walks reaches the walk at once, gliding and stepping');
