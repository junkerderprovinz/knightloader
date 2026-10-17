// The tiles of every picker light up in their brand's colour with an ink that
// holds on it, and the generated lists carry that pair for every logo they
// ship. The pictures below are drawn in code, so each case says what it tests.
import { describe, expect, it } from 'vitest';
import { HOSTER_LOGOS } from './hosterLogos';
import {
  DARK_INK,
  INK_FLOOR,
  WHITE,
  contrast,
  inkOn,
  litOf,
  litPictureOf,
  type Pixels,
  solidOf,
  tileOf,
  withoutPlate,
} from './logoTile';
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

describe('the lit picture', () => {
  const translucent: [number, number, number, number] = [0, 80, 200, 190];
  const halo: [number, number, number, number] = [0, 80, 200, 20];

  it('is a solid mark for a logo drawn translucent, without its faint halo', () => {
    const logo = picture((x, y) => (inside(x, y, 16, 48) ? translucent : inside(x, y, 12, 52) ? halo : null));
    const solid = solidOf(logo);
    expect(at(solid, 32, 32)[3]).toBe(255);
    expect(at(solid, 13, 13)[3]).toBe(0);
    expect(litPictureOf(logo, logo, WHITE)).toEqual({ mask: solid });
  });

  it('keeps the ramp of an antialiased edge', () => {
    const edge = solidOf(picture(() => [0, 0, 0, 96]));
    expect(at(edge, 0, 0)[3]).toBeGreaterThan(96);
    expect(at(edge, 0, 0)[3]).toBeLessThan(255);
  });

  // A square of stripes: its outline is a plain square, its drawing all inside.
  const stripes = picture((x, y) => (inside(x, y, 12, 52) ? (Math.floor(x / 4) % 2 ? BLUE : WHITE_PX) : null));
  const toned = (ink: string) => {
    const lit = litPictureOf(stripes, stripes, ink);
    if (!('tone' in lit)) throw new Error('drawn as a mask');
    return lit.tone;
  };

  it('shows a logo whose drawing lies inside its outline in greys of the ink, not as a blob', () => {
    const tone = toned(WHITE);
    const [white, blue] = [at(tone, 17, 32), at(tone, 13, 32)];
    expect(white).toEqual([255, 255, 255, 255]);
    expect(blue[0]).toBe(blue[1]);
    expect(blue[0]).toBeLessThan(160);
    expect(blue[0]).toBeGreaterThan(100);
  });

  it('tones a logo from the dark end on a tile with a dark ink', () => {
    const tone = toned(DARK_INK);
    expect(at(tone, 13, 32).slice(0, 3)).toEqual([22, 22, 22]);
    expect(at(tone, 17, 32)[0]).toBeGreaterThan(120);
  });

  it('keeps the mask of a logo whose shape carries its drawing', () => {
    const ring = picture((x, y) => (Math.abs(Math.hypot(x - 31.5, y - 31.5) - 20) < 4 ? BLUE : null));
    expect('mask' in litPictureOf(ring, ring, WHITE)).toBe(true);
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
