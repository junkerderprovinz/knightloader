// Whatever the pointer is over lights up in one colour, the accent's hue on
// surface3's lightness (GlimStone rule 21): a button, a row, a menu entry and a
// tile alike. check-hover-ramp.mjs holds the components to it.
import { describe, expect, it } from 'vitest';

import { ACCENTS, RAINBOW } from '../lib/appearance';
import { MIN_TEXT_CONTRAST, contrastRatio, oklabOf, srgbOf } from '../lib/contrast';

/** The one call of node:fs used here; the web build carries no Node types. */
interface NodeFs {
  readFileSync(path: string, encoding: 'utf8'): string;
}

// Vitest hands a test every stylesheet empty, even as ?raw, so the sources are
// read off the disk, from web/ where it runs.
const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule('node:fs');
const read = (file: string) => fs.readFileSync(`src/${file}`, 'utf8');
const CSS = read('index.css').replace(/\/\*[\s\S]*?\*\//g, '');

// A selector starts after the last at-rule statement before it, such as @import.
const rules = [...CSS.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selector, body]) => ({
  selector: selector.split(';').pop()!.trim(),
  body,
}));

/** The custom properties of the first rule whose selector is exactly `selector`. */
function block(selector: string): Record<string, string> {
  const rule = rules.find((r) => r.selector === selector);
  expect(rule, `no rule ${selector}`).toBeTruthy();
  return Object.fromEntries([...rule!.body.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]));
}

const tint = (l: number) => `oklab(from var(--accent) ${l} calc(a * var(--hover-chroma)) calc(b * var(--hover-chroma)))`;
const both = (light: number, dark: number) => `light-dark(${tint(light)}, ${tint(dark)})`;

// GlimStone's lightness for the hover: light, dark and OLED.
const STEP = [0.861, 0.432, 0.338] as const;
// One Carbon step further, for a control that rests on surface3 already.
const STEP_UP = [0.827, 0.532, 0.438] as const;

// What the colour picker can hand in: twelve hues at low, mid and high chroma
// on four lightness steps, and a run of greys.
const SWEEP = [
  ...[0.45, 0.6, 0.75, 0.9].flatMap((l) =>
    [0.02, 0.08, 0.2].flatMap((c) =>
      Array.from({ length: 12 }, (_, i) => srgbOf([l, c * Math.cos((i * Math.PI) / 6), c * Math.sin((i * Math.PI) / 6)])),
    ),
  ),
  ...['#222222', '#555555', '#6B6B6B', '#777777', '#7B7B7B', '#888888', '#999999', '#BBBBBB', '#DDDDDD'],
];

describe('the hover', () => {
  it("is GlimStone's one colour, declared again inside a rainbow position", () => {
    const own = block(':root,\n[data-theme],\n[data-rainbow] .glim-hue');
    expect(own['--hover-chroma']).toBe('0.3');
    expect(own['--hover']).toBe(both(STEP[0], STEP[1]));
  });

  it('moves one step down with OLED', () => {
    expect(block('[data-oled],\n[data-oled][data-rainbow] .glim-hue')).toEqual({ '--hover': both(STEP[0], STEP[2]) });
  });

  it('steps one further from the same hover for a control that rests on surface3', () => {
    const own = block(':root,\n[data-theme],\n[data-rainbow] .glim-hue');
    expect(own['--hover-up']).toBe('light-dark(oklab(from var(--hover) calc(l - 0.034) a b), oklab(from var(--hover) calc(l + 0.1) a b))');
    STEP.forEach((l, side) => expect(l + (side === 0 ? -0.034 : 0.1)).toBeCloseTo(STEP_UP[side], 2));
  });

  it('keeps the text at 4.5:1 on the further step for every picked accent', () => {
    const themes = [
      { name: 'light', side: 0, ink: '#161616' },
      { name: 'dark', side: 1, ink: '#f4f4f4' },
      { name: 'OLED', side: 2, ink: '#f4f4f4' },
    ];
    const misses: string[] = [];
    for (const accent of [...ACCENTS.map((a) => a.hex), ...RAINBOW, ...SWEEP]) {
      const [, a, b] = oklabOf(accent);
      for (const { name, side, ink } of themes) {
        const r = contrastRatio(ink, srgbOf([STEP_UP[side], a * 0.3, b * 0.3]));
        if (r < MIN_TEXT_CONTRAST) misses.push(`${accent} on ${name}: ${r.toFixed(2)}`);
      }
    }
    expect(misses).toEqual([]);
  });

  it('follows a rainbow position as a Tailwind colour too', () => {
    expect(block('@theme')['--color-hover']).toBe('var(--hover)');
    expect(block('[data-rainbow] .glim-hue')['--color-hover']).toBe('var(--hover)');
  });

  it('keeps the text and the secondary text at 4.5:1 for every picked accent in every theme', () => {
    const themes = [
      { name: 'light', side: 0, inks: ['#161616', '#525252'] },
      { name: 'dark', side: 1, inks: ['#f4f4f4', '#c6c6c6'] },
      { name: 'OLED', side: 2, inks: ['#f4f4f4', '#c6c6c6'] },
    ];
    const misses: string[] = [];
    for (const accent of [...ACCENTS.map((a) => a.hex), ...RAINBOW, ...SWEEP]) {
      const [, a, b] = oklabOf(accent);
      for (const { name, side, inks } of themes) {
        const ground = srgbOf([STEP[side], a * 0.3, b * 0.3]);
        for (const ink of inks) {
          const r = contrastRatio(ink, ground);
          if (r < MIN_TEXT_CONTRAST) misses.push(`${accent} ${ink} on ${name}: ${r.toFixed(2)}`);
        }
      }
    }
    expect(misses).toEqual([]);
  });
});

// The one hover ground, and the grounds that come from the accent, a rainbow
// position, a brand tile or a state hue.
const LIT = /var\(--(hover(?:-up)?|accent[\w-]*|item-hue[\w-]*|tile[\w-]*|brand[\w-]*|status-[\w-]+)\)/;
const SYSTEM = /^(Highlight|HighlightText|ButtonFace|ButtonText|Canvas|CanvasText|LinkText|GrayText|SelectedItem)$/;

describe('every hover in the stylesheet', () => {
  const hovered = rules.filter(({ selector }) => /:hover|\[data-highlighted\]/.test(selector.replace(/:not\([^()]*\)/g, '')));

  it('lights its ground with the accent', () => {
    expect(hovered.length).toBeGreaterThan(20);
    const grey = hovered.flatMap(({ selector, body }) =>
      [...body.matchAll(/(?<![\w-])background(?:-color)?\s*:\s*([^;]+);/g)]
        .map((m) => m[1].trim())
        .filter((v) => v !== 'transparent' && v !== 'none' && !SYSTEM.test(v) && !LIT.test(v))
        .map((v) => `${selector.replace(/\s+/g, ' ')}: ${v}`),
    );
    expect(grey).toEqual([]);
  });

  it('lights a shaded list row', () => {
    expect(block(':where([data-shade]) > [data-band]:is(:hover, :has(:focus-visible))')).toEqual({});
    const band = rules.find((r) => r.selector === ':where([data-shade]) > [data-band]:is(:hover, :has(:focus-visible))');
    expect(band?.body).toMatch(/background-color:\s*var\(--hover\);/);
  });

  it('gives a button and a row the same colour', () => {
    const grounds = rules
      .filter((r) => /^:is\(\.glim-btn-neutral, \.glim-btn-subtle\):not\(\.glim-badge-quiet\):not\(:disabled\):is\(:hover, :focus-visible\)$/.test(r.selector))
      .map((r) => r.body.match(/background-color:\s*([^;]+);/)?.[1].trim());
    expect(grounds).toEqual(['var(--hover)']);
  });
});

describe('a glyph with no ground of its own', () => {
  it('takes the accent under the pointer: the number steppers, the password eye and the version link', () => {
    const ui = read('components/ui.tsx');
    expect(ui.match(/const STEPPER =\s*([\s\S]*?);/)?.[1]).toMatch(/'hover:text-accentGlyph /);
    expect(ui.match(/onClick=\{\(\) => setReveal\(\(r\) => !r\)\}[\s\S]*?className="([^"]+)"/)?.[1]).toMatch(/hover:text-accentGlyph$/);
    expect(read('pages/settings/help/Components.tsx')).toMatch(/underline-offset-2 hover:text-accentInk hover:decoration-current/);
  });
});

describe('a row and a button under the pointer', () => {
  it('take the same colour in the link collector and the downloads', () => {
    const list = read('components/TaskList.tsx');
    const rows = [...list.matchAll(/py-\[var\(--row-pad\)\] transition-colors\s+(hover:bg-[\w-]+) has-\[:focus-visible\]:(bg-[\w-]+)/g)];
    expect(rows.map((m) => [m[1], m[2]])).toEqual([
      ['hover:bg-hover', 'bg-hover'],
      ['hover:bg-hover', 'bg-hover'],
    ]);
  });
});
