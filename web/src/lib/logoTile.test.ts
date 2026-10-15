// The tiles of every picker light up in their brand's colour with an ink that
// holds on it, and the generated lists carry that pair for every logo they
// ship. The pictures below are drawn in code, so each case says what it tests.
import { describe, expect, it } from 'vitest';
import { HOSTER_LOGOS } from './hosterLogos';
import { DARK_INK, INK_FLOOR, WHITE, contrast, inkOn, litOf, type Pixels, tileOf, withoutPlate } from './logoTile';
import { VPN_APP_ONLY, VPN_PROVIDERS } from './vpnProviders';

const SIZE = 64;
type Paint = (x: number, y: number) => [number, number, number, number] | null;

function picture(paint: Paint): Pixels {
  const data = new Uint8ClampedArray(SIZE * SIZE * 4);
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      const c = paint(x, y);
      if (c) data.set(c, (y * SIZE + x) * 4);
    }
  }
  return { width: SIZE, height: SIZE, data };
}

const at = (px: Pixels, x: number, y: number) => [...px.data.slice((y * SIZE + x) * 4, (y * SIZE + x) * 4 + 4)];
const inside = (x: number, y: number, from: number, to: number) => x >= from && x < to && y >= from && y < to;
const BLUE: [number, number, number, number] = [0, 80, 200, 255];
const WHITE_PX: [number, number, number, number] = [255, 255, 255, 255];
const BLACK_PX: [number, number, number, number] = [0, 0, 0, 255];
const GREEN: [number, number, number, number] = [60, 220, 110, 255];

describe('the ink', () => {
  it('is white while white reaches 2:1 on the tile and near-black below that', () => {
    expect(inkOn('#0078d4')).toBe(WHITE);
    expect(inkOn('#000000')).toBe(WHITE);
    expect(inkOn('#fcc624')).toBe(DARK_INK);
    expect(inkOn('#4ce576')).toBe(DARK_INK);
  });
});

describe('a plate', () => {
  // A blue square with a white bar on it, the way an app icon draws its glyph.
  const icon = picture((x, y) => (inside(x, y, 24, 40) ? WHITE_PX : inside(x, y, 4, 60) ? BLUE : null));

  it('is taken off, and a white glyph on a coloured plate takes the plate colour', () => {
    const plain = withoutPlate(icon)!;
    expect(plain).not.toBeNull();
    expect(at(plain, 10, 10)[3]).toBe(0);
    expect(at(plain, 32, 32)).toEqual(BLUE);
  });

  it('leaves a glyph on a white or black plate its own colour', () => {
    const plain = withoutPlate(picture((x, y) => (inside(x, y, 24, 40) ? BLUE : inside(x, y, 0, 64) ? WHITE_PX : null)))!;
    expect(at(plain, 2, 2)[3]).toBe(0);
    expect(at(plain, 32, 32)).toEqual(BLUE);
  });

  it('counts a disc only where the caller says so', () => {
    const disc = picture((x, y) => {
      const d = Math.hypot(x - 31.5, y - 31.5);
      if (d > 30) return null;
      return inside(x, y, 26, 38) ? WHITE_PX : BLUE;
    });
    expect(withoutPlate(disc)).toBeNull();
    expect(withoutPlate(disc, true)).not.toBeNull();
  });

  it('is not seen in a logo that is one shape of one colour', () => {
    expect(withoutPlate(picture((x, y) => (inside(x, y, 4, 60) ? BLUE : null)))).toBeNull();
  });
});

describe('the lit mask', () => {
  // A green shield with a white window and a black lock in it.
  const shield = picture((x, y) => {
    if (inside(x, y, 28, 36)) return BLACK_PX;
    if (inside(x, y, 20, 44)) return WHITE_PX;
    return Math.hypot(x - 31.5, y - 31.5) <= 30 && y > 4 ? GREEN : null;
  });

  it('cuts out what lies on the logo and inks what lies on that', () => {
    const lit = litOf(shield, tileOf(shield))!;
    expect(at(lit, 32, 8)[3]).toBe(255);
    expect(at(lit, 22, 22)[3]).toBe(0);
    expect(at(lit, 32, 32)[3]).toBe(255);
    expect(at(lit, 1, 1)[3]).toBe(0);
  });

  it('is the outline where nothing lies on the logo', () => {
    expect(litOf(picture((x, y) => (inside(x, y, 10, 30) ? BLUE : null)), '#0050c8')).toBeNull();
  });
});

describe('the tile colour', () => {
  it('is the colour covering most of the logo, its greys left out', () => {
    const logo = picture((x, y) => (inside(x, y, 0, 40) ? WHITE_PX : inside(x, y, 0, 52) ? GREEN : null));
    expect(tileOf(logo)).toBe('#3cdc6e');
  });

  it('is black for a logo drawn in white alone', () => {
    expect(tileOf(picture((x, y) => (inside(x, y, 10, 50) ? WHITE_PX : null)))).toBe('#000000');
  });

  it('is black for a logo of dark greys, not the grey itself', () => {
    const grey: [number, number, number, number] = [36, 36, 36, 255];
    expect(tileOf(picture((x, y) => (inside(x, y, 10, 50) ? grey : null)))).toBe('#000000');
  });
});

describe('the generated lists', () => {
  // Only the names are read; nothing is loaded.
  const shipped = new Set(Object.keys(import.meta.glob('../../public/{hosters,vpn}/*.png')));
  const lists = [
    ...[...HOSTER_LOGOS].map(([name, info]) => ({ name, info, file: `../../public/hosters/${name}` })),
    ...[...VPN_PROVIDERS, ...VPN_APP_ONLY]
      .filter((p) => p.logo)
      .map((p) => ({ name: p.id, info: p.logo!, file: `../../public/vpn/${p.id}` })),
  ];

  it('give every logo a colour and the ink the rule gives it', () => {
    expect(lists.length).toBeGreaterThan(400);
    for (const { name, info } of lists) {
      expect(info.tile, name).toMatch(/^#[0-9a-f]{6}$/);
      expect(info.ink, name).toBe(inkOn(info.tile));
      if (info.ink === WHITE) expect(contrast(info.ink, info.tile), name).toBeGreaterThanOrEqual(INK_FLOOR);
      else expect(contrast(info.ink, info.tile), name).toBeGreaterThan(contrast(WHITE, info.tile));
    }
  });

  it('ship a lit mask exactly for the logos that say they have one', () => {
    for (const { name, info, file } of lists) {
      expect(shipped.has(`${file}.png`), name).toBe(true);
      expect(shipped.has(`${file}.lit.png`), name).toBe(Boolean(info.lit));
    }
  });

  it('give every VPN provider a logo', () => {
    expect([...VPN_PROVIDERS, ...VPN_APP_ONLY].filter((p) => !p.logo).map((p) => p.id)).toEqual([]);
  });
});
