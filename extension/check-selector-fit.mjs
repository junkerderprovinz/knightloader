// A well selector keeps to its one row (GlimStone, "It keeps to one row").
//
// The options page and the popup draw their wells with a glyph on every
// segment. While the labels fit beside their glyphs the well shows both, and a
// hugging well is as wide as its segments. Where the row is too short for the
// labels the glyphs stand alone, each label kept as the segment's name and
// bubble, and once the row has room again the labels come back.
//
// The check runs selectorFit.js against a stand-in well whose box and segment
// widths it sets itself, and resizes the box through the ResizeObserver the
// script watches with.
//
// Run by CI and by hand: `node extension/check-selector-fit.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];
const fail = (why) => problems.push(why);

function classes() {
  const set = new Set();
  return {
    add: (...c) => c.forEach((x) => set.add(x)),
    remove: (...c) => c.forEach((x) => set.delete(x)),
    toggle: (c, on) => (on ? set.add(c) : set.delete(c)),
    contains: (c) => set.has(c),
  };
}

function segment(label, width) {
  const words = { textContent: label };
  return {
    width,
    style: { cssText: '' },
    dataset: {},
    querySelector: () => words,
  };
}

const watchers = [];
const segs = [segment('Round', 70), segment('Slightly rounded', 130), segment('Square', 72)];
const track = {
  room: 400,
  classList: classes(),
  style: { width: '', minWidth: '' },
  parentElement: {},
  get clientWidth() {
    return this.room;
  },
  querySelectorAll: () => segs,
};
track.classList.add('glim-seg', 'glim-seg--hug');

const ctx = vm.createContext({
  ResizeObserver: class {
    constructor(cb) {
      watchers.push(cb);
    }
    observe() {}
    disconnect() {}
  },
  getComputedStyle: (el) =>
    el === track
      ? { paddingLeft: '3px', paddingRight: '3px', columnGap: '2px' }
      : { width: el.style.width === 'max-content' ? `${el.width}px` : '0px' },
  document: { fonts: undefined },
  WeakMap,
});
vm.runInContext(readFileSync(join(here, 'src', 'selectorFit.js'), 'utf8'), ctx, { filename: 'selectorFit.js' });

const resize = (room) => {
  track.room = room;
  for (const cb of watchers) cb();
};
const squeezed = () => track.classList.contains('glim-seg--squeezed');

ctx.fitSegments(track);
if (squeezed()) fail('a well with room for its labels showed its glyphs alone');
const hug = 70 + 130 + 72 + 2 * 2 + 6 + 1;
if (track.style.width !== `${hug}px`) {
  fail(`a hugging well is ${track.style.width || 'unsized'}, not as wide as its segments (${hug}px)`);
}

resize(200);
if (!squeezed()) fail('a well too narrow for its labels kept them, so its row overflows or wraps');
if (segs.some((s) => !s.dataset.tip)) fail('a squeezed segment lost its label as its bubble');
if (track.style.width !== '100%') fail('a squeezed hugging well does not take its whole box');

resize(400);
if (squeezed()) fail('a well that has room again still shows its glyphs alone');
if (segs.some((s) => s.dataset.tip)) fail('a segment that shows its label again still carries it as a bubble');

if (problems.length > 0) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: a well keeps to its one row and shows its glyphs alone where its labels do not fit');
