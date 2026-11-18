// A picker window's logic, ported from GlimStone's core ("Pickers and wizards"):
// one search across the names and the words each choice carries, and the
// arrow keys across the tiles' grid. Nothing here knows what is being picked.

/** The page's language, which a picker's tiles are sorted in. */
export const pageLang = (): string | undefined => document.documentElement.lang || undefined;

/** Lowercases and drops accents and hyphens, so "zeitplane" finds "Zeitpläne" and "offsite" finds "Off-site". */
function fold(text: string): string {
  return text.normalize('NFD').replace(/\p{M}/gu, '').replace(/ß/g, 'ss').replace(/-/g, '').toLowerCase();
}

/** Whether every word of the query turns up in at least one of the names. */
export function pickMatches(query: string, names: readonly string[]): boolean {
  const said = names.map(fold);
  return fold(query)
    .split(/\s+/)
    .filter(Boolean)
    .every((word) => said.some((name) => name.includes(word)));
}

export interface PickBox {
  left: number;
  top: number;
  width: number;
  height: number;
}

/**
 * Where a key moves the focus from tile `from`, given every tile's box in
 * order: the tile beside it, the nearest one in the row above or below, the
 * first or the last. Up from the first row is the search. Null for a key the
 * grid leaves alone.
 */
export function pickKey(key: string, from: number, boxes: readonly PickBox[], rtl: boolean): number | 'search' | null {
  const last = boxes.length - 1;
  const forward = rtl ? 'ArrowLeft' : 'ArrowRight';
  const back = rtl ? 'ArrowRight' : 'ArrowLeft';
  if (key === 'Home') return 0;
  if (key === 'End') return last;
  if (key === forward) return Math.min(from + 1, last);
  if (key === back) return Math.max(from - 1, 0);
  if (key !== 'ArrowUp' && key !== 'ArrowDown') return null;

  const here = boxes[from]!;
  const centre = (b: PickBox) => b.left + b.width / 2;
  // A row is every tile whose top lies within half a tile of the first one's.
  const sameRow = (a: PickBox, b: PickBox) => Math.abs(a.top - b.top) < Math.min(a.height, b.height) / 2;
  const down = key === 'ArrowDown';
  const beyond = boxes
    .map((box, index) => ({ box, index }))
    .filter(({ box }) => !sameRow(box, here) && (down ? box.top > here.top : box.top < here.top));
  if (beyond.length === 0) return down ? from : 'search';
  const near = beyond.reduce((a, b) => (Math.abs(b.box.top - here.top) < Math.abs(a.box.top - here.top) ? b : a));
  const row = beyond.filter(({ box }) => sameRow(box, near.box));
  return row.reduce((a, b) => (Math.abs(centre(b.box) - centre(here)) < Math.abs(centre(a.box) - centre(here)) ? b : a)).index;
}

const TILE = '.glim-provider-pick button.glim-readme-btn';

/**
 * Handles a key on a tile of the picker's `list`: moves the focus across the
 * grid, or up into `search`. Returns whether the key was used, so the page
 * does not also scroll.
 */
export function pickTileKey(key: string, tile: Element, list: HTMLElement, search: HTMLElement | null): boolean {
  const tiles = [...list.querySelectorAll<HTMLElement>(TILE)];
  const from = tiles.indexOf(tile as HTMLElement);
  if (from < 0) return false;
  const to = pickKey(key, from, tiles.map((t) => t.getBoundingClientRect()), getComputedStyle(list).direction === 'rtl');
  if (to === null) return false;
  (to === 'search' ? search : tiles[to])?.focus();
  return true;
}

/**
 * Handles a key in the picker's search. Escape empties a field with text in
 * it before the window hears it, ArrowDown goes to the first tile, and Enter
 * picks the one tile a query has left. Returns whether the key was used.
 */
export function pickSearchKey(key: string, query: string, list: HTMLElement | null, clear: () => void): boolean {
  const tiles = list ? [...list.querySelectorAll<HTMLElement>(TILE)] : [];
  if (key === 'Escape' && query !== '') {
    clear();
    return true;
  }
  if (key === 'ArrowDown' && tiles[0]) {
    tiles[0].focus();
    return true;
  }
  if (key === 'Enter' && query.trim() !== '' && tiles.length === 1) {
    tiles[0]!.click();
    return true;
  }
  return false;
}
