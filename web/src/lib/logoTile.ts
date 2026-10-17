// Reads a raster logo for a brand tile (GlimStone's "Brand tiles"): the colour
// the tile fills with under the pointer and the ink that holds on it, the logo
// without a plate behind it, how it stands at rest, and the single-colour mask
// the lit tile shows. The logo generators in scripts/ run it over the bundled
// logos and HosterIcon over an icon the instance fetched, so a logo reads the
// same wherever it came from. Everything here works on raw RGBA pixels.

export interface Pixels {
  width: number;
  height: number;
  data: Uint8ClampedArray<ArrayBuffer>;
}

/** A logo's colours on a brand tile, as the generated lists store them. */
export interface LogoTile {
  /** The fill of the lit tile, the brand's own colour. */
  tile: string;
  /** White or near-black, whichever the tile rule gives the fill. */
  ink: string;
  /**
   * How the logo stands at rest: a logo of one neutral ink in the text's
   * colour, so it reads on both themes; a colour too dark for the dark tile
   * lifted there, one too pale for the light tile deepened there.
   */
  rest?: 'ink' | 'lift' | 'deepen';
  /** The lit tile needs a mask of its own, with the parts that lie on other parts cut out. */
  lit?: boolean;
}

type RGB = [number, number, number];

export const WHITE = '#ffffff';
export const DARK_INK = '#161616';
/** White is the ink while it reaches this contrast on the fill. */
export const INK_FLOOR = 2;

// The tiles a logo rests on, --carbon-surface2 of the two themes, and the
// contrast GlimStone asks of a mark at rest on each.
const SURFACE_DARK: RGB = [0x39, 0x39, 0x39];
const SURFACE_LIGHT: RGB = [0xe8, 0xe8, 0xe8];
const REST_DARK = 3;
const REST_LIGHT = 1.35;

// A pixel counts as drawn from this alpha on.
const SOLID = 128;
// A pixel whose drawn neighbours all lie this close is inside a part rather
// than on the edge between two.
const FLAT = 28;
// Neighbouring inner pixels this close belong to one part, which carries a
// gradient across the part but stops at an edge.
const STEP = 40;
// A logo of more parts than this is a picture, and its lit mask is its outline.
const MAX_PARTS = 8;
// Neutral is grey enough that the hue does not read.
const NEUTRAL_CHROMA = 40;

function luminance([r, g, b]: RGB): number {
  const f = (v: number) => {
    const c = v / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}

function ratio(a: RGB, b: RGB): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

export function rgbOf(hex: string): RGB {
  const h = hex.replace('#', '');
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16)) as RGB;
}

export function hexOf([r, g, b]: RGB): string {
  return `#${[r, g, b].map((v) => Math.round(v).toString(16).padStart(2, '0')).join('')}`;
}

/** contrast is the WCAG contrast ratio of two hex colours. */
export function contrast(a: string, b: string): number {
  return ratio(rgbOf(a), rgbOf(b));
}

/** inkOn is the ink of a tile: white where white reaches INK_FLOOR on it, near-black below that. */
export function inkOn(tile: string): string {
  return contrast(WHITE, tile) >= INK_FLOOR ? WHITE : DARK_INK;
}

const dist = (a: RGB, b: RGB) => Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
const chroma = ([r, g, b]: RGB) => Math.max(r, g, b) - Math.min(r, g, b);

/** blendOf reports whether c lies on the way from one clearly different colour to another. */
function blendOf(c: RGB, a: RGB, b: RGB): boolean {
  const ab = b.map((v, k) => v - a[k]);
  const length = Math.hypot(...ab);
  if (length < 60) return false;
  const t = ab.reduce((s, v, k) => s + v * (c[k] - a[k]), 0) / (length * length);
  if (t < 0 || t > 1) return false;
  return dist(c, a.map((v, k) => v + t * ab[k]) as RGB) < 30;
}

const average = (colours: RGB[]): RGB =>
  colours.reduce<RGB>((s, c) => [s[0] + c[0], s[1] + c[1], s[2] + c[2]], [0, 0, 0]).map((v) => v / colours.length) as RGB;

const colourAt = (px: Pixels, i: number): RGB => [px.data[i * 4], px.data[i * 4 + 1], px.data[i * 4 + 2]];
const drawn = (px: Pixels, i: number) => px.data[i * 4 + 3] >= SOLID;

function neighbours(px: Pixels, i: number): number[] {
  const x = i % px.width;
  const out: number[] = [];
  if (x > 0) out.push(i - 1);
  if (x < px.width - 1) out.push(i + 1);
  if (i >= px.width) out.push(i - px.width);
  if (i < px.width * (px.height - 1)) out.push(i + px.width);
  return out;
}

const onBorder = (px: Pixels, i: number) => {
  const x = i % px.width;
  const y = Math.floor(i / px.width);
  return x === 0 || y === 0 || x === px.width - 1 || y === px.height - 1;
};

interface Parts {
  /** Each pixel's part, -1 where nothing is drawn. */
  label: Int32Array;
  size: number[];
  mean: RGB[];
  /** How many parts lie between a part and the ground: 0 for one the ground touches. */
  depth: number[];
  drawn: number;
}

/**
 * partsOf splits a logo into the areas of one colour or one gradient it is
 * drawn from. The edge pixels between two areas join the area they are closer
 * to, so an antialiased edge does not become a thin part of its own.
 */
function partsOf(px: Pixels): Parts {
  const n = px.width * px.height;
  const label = new Int32Array(n).fill(-1);
  const colours = Array.from({ length: n }, (_, i) => colourAt(px, i));
  const core = new Uint8Array(n);
  let drawnCount = 0;
  for (let i = 0; i < n; i++) {
    if (!drawn(px, i)) continue;
    drawnCount++;
    core[i] = neighbours(px, i).every((j) => !drawn(px, j) || dist(colours[i], colours[j]) <= FLAT) ? 1 : 0;
  }

  const size: number[] = [];
  const sum: RGB[] = [];
  const grow = (seed: number, member: (from: number, to: number) => boolean) => {
    const id = size.length;
    size.push(0);
    sum.push([0, 0, 0]);
    const queue = [seed];
    label[seed] = id;
    while (queue.length) {
      const i = queue.pop()!;
      size[id]++;
      for (let c = 0; c < 3; c++) sum[id][c] += colours[i][c];
      for (const j of neighbours(px, i)) {
        if (label[j] === -1 && member(i, j)) {
          label[j] = id;
          queue.push(j);
        }
      }
    }
  };
  for (let i = 0; i < n; i++) {
    if (core[i] && label[i] === -1) grow(i, (a, b) => core[b] === 1 && dist(colours[a], colours[b]) < STEP);
  }
  const meanOf = (id: number): RGB => sum[id].map((v) => v / size[id]) as RGB;

  // The parts within two pixels of a pixel, which an edge two pixels wide lies between.
  const touchingAt = (i: number) => {
    const found = new Set<number>();
    for (const j of neighbours(px, i)) {
      if (label[j] !== -1) found.add(label[j]);
      for (const k of neighbours(px, j)) if (label[k] !== -1) found.add(label[k]);
    }
    return found;
  };
  // The edge pixels, a ring at a time from the parts outwards. A pixel joins
  // a part it nearly matches, or the nearer of two parts it is a blend of; a
  // fleck of another colour, too small to have an inside, waits to become a
  // part of its own.
  const assign = (anyway: boolean) => {
    for (let progress = true; progress; ) {
      const taken: [number, number][] = [];
      for (let i = 0; i < n; i++) {
        if (label[i] !== -1 || !drawn(px, i)) continue;
        const near = [...new Set(neighbours(px, i).map((j) => label[j]).filter((id) => id !== -1))];
        if (near.length === 0) continue;
        const c = colours[i];
        near.sort((a, b) => dist(c, meanOf(a)) - dist(c, meanOf(b)));
        const between = near.some((a) =>
          [...touchingAt(i)].some((b) => b !== a && blendOf(c, meanOf(a), meanOf(b))),
        );
        if (anyway || dist(c, meanOf(near[0])) < 60 || between) taken.push([i, near[0]]);
      }
      for (const [i, id] of taken) {
        label[i] = id;
        size[id]++;
        for (let c = 0; c < 3; c++) sum[id][c] += colours[i][c];
      }
      progress = taken.length > 0;
    }
  };
  assign(false);
  const flecks = size.length;
  for (let i = 0; i < n; i++) {
    if (drawn(px, i) && label[i] === -1) {
      grow(i, (a, b) => drawn(px, b) && label[b] === -1 && dist(colours[a], colours[b]) < STEP);
    }
  }
  // A fleck's antialiased rim is a blend of the fleck and what it lies on.
  for (let id = flecks; id < size.length; id++) {
    const around = new Set<number>();
    for (let i = 0; i < n; i++) {
      if (label[i] !== id) continue;
      for (const j of neighbours(px, i)) if (label[j] !== -1 && label[j] !== id) around.add(label[j]);
    }
    const others = [...around];
    const pair = others
      .flatMap((a) => others.map((b) => [a, b]))
      .find(([a, b]) => a < b && blendOf(meanOf(id), meanOf(a), meanOf(b)));
    if (!pair) continue;
    const into = dist(meanOf(id), meanOf(pair[0])) <= dist(meanOf(id), meanOf(pair[1])) ? pair[0] : pair[1];
    for (let i = 0; i < n; i++) if (label[i] === id) label[i] = into;
    size[into] += size[id];
    for (let c = 0; c < 3; c++) sum[into][c] += sum[id][c];
    size[id] = 0;
  }
  assign(true);

  const mean = size.map((_, id) => meanOf(id));
  const depth = size.map(() => Infinity);
  const queue: number[] = [];
  for (let i = 0; i < n; i++) {
    const id = label[i];
    if (id === -1 || depth[id] === 0) continue;
    if (onBorder(px, i) || neighbours(px, i).some((j) => label[j] === -1)) {
      depth[id] = 0;
      queue.push(id);
    }
  }
  const touching = size.map(() => new Set<number>());
  for (let i = 0; i < n; i++) {
    if (label[i] === -1) continue;
    for (const j of neighbours(px, i)) {
      if (label[j] !== -1 && label[j] !== label[i]) touching[label[i]].add(label[j]);
    }
  }
  while (queue.length) {
    const id = queue.shift()!;
    for (const other of touching[id]) {
      if (depth[other] > depth[id] + 1) {
        depth[other] = depth[id] + 1;
        queue.push(other);
      }
    }
  }
  return { label, size, mean, depth, drawn: drawnCount };
}

/** The parts that matter, a hair or a fleck under a hundredth of the logo left out. */
const significant = (p: Parts) => p.size.map((s, id) => (s >= p.drawn * 0.01 ? id : -1)).filter((id) => id >= 0);

/**
 * outlineOf measures what a logo covers: its box's width over its height, how
 * large the box is against the image, and the share of the box inside the
 * logo's outline, holes and all.
 */
function outlineOf(px: Pixels): { aspect: number; reach: number; fill: number } {
  const n = px.width * px.height;
  const outside = new Uint8Array(n);
  const queue: number[] = [];
  for (let i = 0; i < n; i++) {
    if (onBorder(px, i) && !drawn(px, i)) {
      outside[i] = 1;
      queue.push(i);
    }
  }
  while (queue.length) {
    for (const j of neighbours(px, queue.pop()!)) {
      if (!outside[j] && !drawn(px, j)) {
        outside[j] = 1;
        queue.push(j);
      }
    }
  }
  let [minX, minY, maxX, maxY] = [px.width, px.height, -1, -1];
  for (let i = 0; i < n; i++) {
    if (!drawn(px, i)) continue;
    const x = i % px.width;
    const y = Math.floor(i / px.width);
    [minX, minY, maxX, maxY] = [Math.min(minX, x), Math.min(minY, y), Math.max(maxX, x), Math.max(maxY, y)];
  }
  if (maxX < 0) return { aspect: 1, reach: 0, fill: 0 };
  const w = maxX - minX + 1;
  const h = maxY - minY + 1;
  let inside = 0;
  for (let y = minY; y <= maxY; y++) for (let x = minX; x <= maxX; x++) if (!outside[y * px.width + x]) inside++;
  return { aspect: w / h, reach: Math.max(w / px.width, h / px.height), fill: inside / (w * h) };
}

/** A badge: an outline close to a square, a rounded square or a disc. */
const badge = ({ aspect, fill }: { aspect: number; fill: number }, least = 0.74) =>
  aspect >= 0.8 && aspect <= 1.25 && fill >= least;

/**
 * withoutPlate is the logo lifted off the plate it was drawn on, or null for
 * a logo without one: a square or rounded square of one colour that holds the
 * rest of the logo, which may run to its edge. A white or black plate is a
 * background and the logo keeps its colours; a coloured one is the brand's, so
 * a white or black glyph on it takes that colour. A disc counts only where the
 * caller says so, since a round badge is more often the mark itself.
 */
export function withoutPlate(px: Pixels, disc = false): Pixels | null {
  const parts = partsOf(px);
  if (parts.drawn === 0) return null;
  const outer = parts.size.map((_, id) => id).filter((id) => parts.depth[id] === 0);
  outer.sort((a, b) => parts.size[b] - parts.size[a]);
  if (outer.length === 0) return null;
  const ground = parts.mean[outer[0]];
  // A glyph that runs to the plate's edge cuts it in pieces of one colour. A
  // white or grey plate may be drawn in two shades of it.
  const neutral = chroma(ground) < NEUTRAL_CHROMA;
  const isPlate = new Set(
    outer.filter((id) => {
      const d = dist(parts.mean[id], ground);
      return d < 30 || (neutral && chroma(parts.mean[id]) < NEUTRAL_CHROMA && d < 70);
    }),
  );
  const plateSize = [...isPlate].reduce((s, id) => s + parts.size[id], 0);
  if (plateSize < parts.drawn * 0.3 || plateSize > parts.drawn * 0.95) return null;

  const n = px.width * px.height;
  let spread = 0;
  let rim = 0;
  let rimOnPlate = 0;
  for (let i = 0; i < n; i++) {
    const id = parts.label[i];
    if (id === -1) continue;
    if (isPlate.has(id)) spread += dist(colourAt(px, i), parts.mean[id]);
    if (onBorder(px, i) || neighbours(px, i).some((j) => parts.label[j] === -1)) {
      rim++;
      if (isPlate.has(id)) rimOnPlate++;
    }
  }
  if (spread / plateSize > 30 || rimOnPlate < rim * 0.7) return null;
  const outline = outlineOf(px);
  if (outline.reach < 0.5 || !badge(outline, disc ? 0.74 : 0.86)) return null;

  // How far the glyph's own colour lies from the plate, from its pixels well
  // away from the plate's colour.
  const onGlyph = (i: number) => parts.label[i] !== -1 && !isPlate.has(parts.label[i]);
  const shades = [...isPlate].map((id) => parts.mean[id]);
  const nearest = (c: RGB) => shades.reduce((a, b) => (dist(c, b) < dist(c, a) ? b : a));
  const away: number[] = [];
  for (let i = 0; i < n; i++) {
    if (onGlyph(i)) away.push(dist(colourAt(px, i), nearest(colourAt(px, i))));
  }
  away.sort((a, b) => a - b);
  const full = away[Math.floor(away.length * 0.7)] ?? 0;
  if (full < 40) return null;

  const glyph: RGB[] = [];
  for (let i = 0; i < n; i++) {
    if (onGlyph(i) && dist(colourAt(px, i), ground) >= full * 0.9) glyph.push(colourAt(px, i));
  }
  const g = average(glyph);
  const oneColour = glyph.filter((c) => dist(c, g) < 50).length >= glyph.length * 0.85;
  const recolour = oneColour && chroma(g) < NEUTRAL_CHROMA && chroma(ground) >= NEUTRAL_CHROMA;

  const out = new Uint8ClampedArray(px.data.length);
  for (let i = 0; i < n; i++) {
    if (!onGlyph(i)) continue;
    const c = colourAt(px, i);
    const under = nearest(c);
    const share = Math.min(1, dist(c, under) / full);
    if (share === 0) continue;
    const own = recolour ? ground : (c.map((v, k) => under[k] + (v - under[k]) / share) as RGB);
    for (let k = 0; k < 3; k++) out[i * 4 + k] = Math.max(0, Math.min(255, own[k]));
    out[i * 4 + 3] = px.data[i * 4 + 3] * share;
  }
  return { width: px.width, height: px.height, data: out };
}

/**
 * tileOf is a logo's tile colour: the colour covering most of it, its greys
 * left out while it has a colour at all. A logo of greys alone lights in
 * black, as GitHub's does: its own dark grey would sit a hair off the window
 * behind the tile.
 */
export function tileOf(px: Pixels): string {
  const coloured: RGB[] = [];
  let total = 0;
  for (let i = 0; i < px.width * px.height; i++) {
    if (!drawn(px, i)) continue;
    total++;
    const c = colourAt(px, i);
    if (chroma(c) >= 30 && chroma(c) / Math.max(...c) >= 0.25) coloured.push(c);
  }
  if (coloured.length <= total * 0.06) return '#000000';
  // The densest neighbourhood of colours, so a gradient counts as one.
  const bin = (c: RGB) => (c[0] >> 4) * 256 + (c[1] >> 4) * 16 + (c[2] >> 4);
  const counts = new Map<number, number>();
  for (const c of coloured) counts.set(bin(c), (counts.get(bin(c)) ?? 0) + 1);
  const near = (b: number) => {
    const [r, g, bl] = [b >> 8, (b >> 4) & 15, b & 15];
    const out: number[] = [];
    for (let dr = -1; dr <= 1; dr++)
      for (let dg = -1; dg <= 1; dg++)
        for (let db = -1; db <= 1; db++) {
          const [x, y, z] = [r + dr, g + dg, bl + db];
          if (x >= 0 && y >= 0 && z >= 0 && x < 16 && y < 16 && z < 16) out.push(x * 256 + y * 16 + z);
        }
    return out;
  };
  let best = -1;
  let bestCount = -1;
  for (const b of counts.keys()) {
    const count = near(b).reduce((s, x) => s + (counts.get(x) ?? 0), 0);
    if (count > bestCount) [best, bestCount] = [b, count];
  }
  const area = new Set(near(best));
  return hexOf(average(coloured.filter((c) => area.has(bin(c)))));
}

/**
 * litOf is the mask the lit tile shows the logo by, or null where the logo's
 * own outline does. The parts the ground touches are ink, the parts that lie
 * on them cut-outs in the tile colour, the parts on those ink again, so eyes
 * on a face and a lock on a shield stay readable. A badge that this leaves
 * whole, a letter running to the edge of its disc or a picture of many parts,
 * is drawn by what stands out from the tile colour instead, so the badge
 * melts into the tile and the letter stays.
 */
export function litOf(px: Pixels, tile: string): Pixels | null {
  const parts = partsOf(px);
  const cut = (id: number) => id !== -1 && parts.depth[id] % 2 === 1;
  const simple = significant(parts).length <= MAX_PARTS;
  if (simple && parts.size.some((s, id) => cut(id) && s >= parts.drawn * 0.01)) {
    const out = new Uint8ClampedArray(px.data.length);
    let ink = 0;
    for (let i = 0; i < px.width * px.height; i++) {
      if (parts.label[i] === -1 || cut(parts.label[i])) continue;
      out[i * 4 + 3] = px.data[i * 4 + 3];
      ink++;
    }
    // A shape cut down to a few slivers no longer reads as itself.
    if (ink >= parts.drawn * 0.15) return { width: px.width, height: px.height, data: out };
  }
  if (!badge(outlineOf(px))) return null;
  const fill = rgbOf(tile);
  const stands = parts.mean.map((m) => dist(m, fill) > 100);
  const out = new Uint8ClampedArray(px.data.length);
  let ink = 0;
  for (let i = 0; i < px.width * px.height; i++) {
    const id = parts.label[i];
    if (id === -1 || !stands[id]) continue;
    out[i * 4 + 3] = px.data[i * 4 + 3];
    ink++;
  }
  if (ink < parts.drawn * 0.15 || ink > parts.drawn * 0.9) return null;
  return { width: px.width, height: px.height, data: out };
}

/**
 * restOf is how a logo stands on the resting tile; see LogoTile.rest. Only a
 * logo of one shade or a flat one of a few turns to the text's colour, which
 * would make a picture in greys a blot.
 */
export function restOf(px: Pixels): LogoTile['rest'] {
  let total = 0;
  let neutral = 0;
  let dark = 0;
  let light = 0;
  let onDark = 0;
  let onLight = 0;
  for (let i = 0; i < px.width * px.height; i++) {
    if (!drawn(px, i)) continue;
    const c = colourAt(px, i);
    total++;
    if (chroma(c) < NEUTRAL_CHROMA) neutral++;
    if (luminance(c) < 0.2) dark++;
    else if (luminance(c) > 0.6) light++;
    if (ratio(c, SURFACE_DARK) >= REST_DARK) onDark++;
    if (ratio(c, SURFACE_LIGHT) >= REST_LIGHT) onLight++;
  }
  if (total === 0) return undefined;
  if (neutral >= total * 0.85) {
    const oneShade = Math.max(dark, light) >= total * 0.8;
    if (oneShade || significant(partsOf(px)).length <= MAX_PARTS) return 'ink';
  }
  if (onDark < total * 0.25) return 'lift';
  if (onLight < total * 0.25) return 'deepen';
  return undefined;
}

/**
 * logoTileOf reads everything a tile needs from a logo: its colour from the
 * logo as drawn, the rest from the logo lifted off its plate.
 */
export function logoTileOf(drawnLogo: Pixels, plain: Pixels): { info: LogoTile; lit: Pixels | null } {
  const tile = tileOf(drawnLogo);
  const lit = litOf(plain, tile);
  const info: LogoTile = { tile, ink: inkOn(tile) };
  const rest = restOf(plain);
  if (rest) info.rest = rest;
  if (lit) info.lit = true;
  return { info, lit };
}

// The lit mask's alpha climbs from FAINT to FULL, so a logo drawn translucent
// lights as a solid mark and a halo fainter than FAINT drops away, while an
// antialiased edge keeps a ramp.
const FAINT = 32;
const FULL = 160;

/** solidOf is a mask fully opaque wherever the logo has real coverage. */
export function solidOf(px: Pixels): Pixels {
  const data = new Uint8ClampedArray(px.data);
  for (let i = 3; i < data.length; i += 4) data[i] = ((px.data[i] - FAINT) * 255) / (FULL - FAINT);
  return { width: px.width, height: px.height, data };
}

// A colour step this large between two neighbours is a line of the drawing.
const LINE = 60;
// A silhouette filling BLOB_FILL of its box that would lose BLOB_LOST of the logo's lines is a blob.
const BLOB_FILL = 0.6;
const BLOB_LOST = 0.65;

/**
 * blobOf reports whether a logo's lit mask would be a shapeless blob: compact,
 * with most of the logo's lines drawn inside it in colour, where one ink
 * cannot show them, rather than on its outline.
 */
function blobOf(logo: Pixels, mask: Pixels): boolean {
  let [minX, minY, maxX, maxY] = [mask.width, mask.height, -1, -1];
  let covered = 0;
  let outline = 0;
  let inner = 0;
  for (let i = 0; i < mask.width * mask.height; i++) {
    if (!drawn(mask, i)) continue;
    covered++;
    const [x, y] = [i % mask.width, Math.floor(i / mask.width)];
    [minX, minY, maxX, maxY] = [Math.min(minX, x), Math.min(minY, y), Math.max(maxX, x), Math.max(maxY, y)];
    const around = neighbours(mask, i);
    if (onBorder(mask, i) || around.some((j) => !drawn(mask, j))) outline++;
    else if (around.some((j) => dist(colourAt(logo, i), colourAt(logo, j)) > LINE)) inner++;
  }
  if (covered === 0) return false;
  const fill = covered / ((maxX - minX + 1) * (maxY - minY + 1));
  return fill >= BLOB_FILL && inner / (inner + outline) >= BLOB_LOST;
}

// How far the greys of a toned logo reach from the ink towards the other end.
const TONE_SPAN = 0.55;

/**
 * toneOf is a logo in greys that start at the tile's ink and reach part of the
 * way to the other end, its lightest colour at the ink on a white ink and its
 * darkest on a dark one, so its inner drawing reads on the tile.
 */
function toneOf(px: Pixels, ink: string): Pixels {
  const n = px.width * px.height;
  const luma = (i: number) => (0.299 * px.data[i * 4] + 0.587 * px.data[i * 4 + 1] + 0.114 * px.data[i * 4 + 2]) / 255;
  let [low, high] = [1, 0];
  for (let i = 0; i < n; i++) {
    if (!drawn(px, i)) continue;
    [low, high] = [Math.min(low, luma(i)), Math.max(high, luma(i))];
  }
  const range = high - low || 1;
  const from = rgbOf(ink)[0];
  const light = luminance(rgbOf(ink)) > 0.5;
  const data = solidOf(px).data;
  for (let i = 0; i < n; i++) {
    const level = (luma(i) - low) / range;
    const away = light ? 1 - level : level;
    const grey = from + ((light ? 0 : 255) - from) * away * TONE_SPAN;
    data.fill(grey, i * 4, i * 4 + 3);
  }
  return { width: px.width, height: px.height, data };
}

/**
 * The lit tile's picture of a logo: its mask made solid, or, for a logo whose
 * mask would be a blob, the logo itself in greys of the ink.
 */
export type LitPicture = { mask: Pixels } | { tone: Pixels };

/** litPictureOf reads the lit tile's picture from a logo and the mask it lights by. */
export function litPictureOf(logo: Pixels, mask: Pixels, ink: string): LitPicture {
  const solid = solidOf(mask);
  return blobOf(logo, solid) ? { tone: toneOf(logo, ink) } : { mask: solid };
}
