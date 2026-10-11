import type { ComponentType, SVGProps } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import list from '../../scripts/glyphs.json';
import { MAIL_SVG, MANUAL_SVG } from './appMarks';
import * as glyphs from './glyphs';

type Glyph = ComponentType<SVGProps<SVGSVGElement>>;

const components = Object.fromEntries(
  Object.entries(glyphs).filter(([name]) => name.startsWith('Icon')),
) as Record<string, Glyph>;

const drawn = list.filter((entry) => entry.name in components);

function markup(name: string): string {
  const Icon = components[name];
  return renderToStaticMarkup(<Icon />);
}

function viewBox(svg: string): number[] {
  return /viewBox="([^"]+)"/.exec(svg)![1].split(' ').map(Number);
}

describe('the generated glyphs', () => {
  it('are all drawings from the list', () => {
    const listed = new Set(list.map((entry) => entry.name));
    expect(Object.keys(components).filter((name) => !listed.has(name))).toEqual(['IconCheckDrawn']);
  });

  it.each(drawn)('$name shows its ink across three quarters of the box, in the middle', ({ name, viewBox: crop }) => {
    // The list's own square crop to the ink, widened by a third around its centre.
    const [x, y, side] = crop.split(' ').map(Number);
    const want = [x - side / 6, y - side / 6, (side * 4) / 3, (side * 4) / 3];
    viewBox(markup(name)).forEach((n, i) => expect(Math.abs(n - want[i])).toBeLessThan(side / 1000));
  });

  it.each(drawn)('$name drawn tight fills the box the list crops it to', ({ name, viewBox: crop }) => {
    const Icon = components[name] as ComponentType<glyphs.GlyphProps>;
    const [x, y, side] = crop.split(' ').map(Number);
    viewBox(renderToStaticMarkup(<Icon tight />)).forEach((n, i) =>
      expect(Math.abs(n - [x, y, side, side][i])).toBeLessThan(side / 1000),
    );
  });

  it.each(drawn)('$name is the drawing the list has', ({ name, svg }) => {
    const shapes = (s: string) => [...s.matchAll(/<(path|rect|circle|ellipse)\b/g)].length;
    const paths = (s: string) => [...s.matchAll(/ d="([^"]+)"/g)].map((m) => m[1]);
    const out = markup(name);
    expect(paths(out)).toEqual(paths(svg));
    expect(shapes(out)).toBe(shapes(svg));
  });

  it.each([
    ['the envelope', MAIL_SVG, 'IconMail', 'IconMailOpen'],
    ['the book', MANUAL_SVG, 'IconManual', 'IconManualOpen'],
  ])('%s on its button is the pair the list has, shut and open', (_, pair, shut, open) => {
    const paths = (s: string) => [...s.matchAll(/ d="([^"]+)"/g)].map((m) => m[1]);
    const listed = (name: string) => paths(list.find((entry) => entry.name === name)!.svg);
    expect(paths(pair)).toEqual([...listed(shut), ...listed(open)]);
  });

  it('draw the check in over the check itself', () => {
    const plain = markup('IconCheck');
    const drawing = markup('IconCheckDrawn');
    expect(viewBox(drawing)).toEqual(viewBox(plain));
    expect(drawing).toContain(/<path[^>]*>/.exec(plain)![0]);
  });
});
