// The arithmetic of a reorder: where the rows go while one is carried, where
// it lands, and which order the list shows once it has. Free of React, so the
// numbers can be run on their own.

/** A cell's measured box, in the list's content coordinates. */
export interface Kasten {
  y: number;
  h: number;
}

interface Zeile {
  key: string;
  band: string;
  parent?: string;
}

/**
 * blockEnde is where the block of the row at `i` ends, exclusive: the row and
 * the rows right after it that hang under it. A band is reordered in blocks,
 * so a package header takes its open files along and nothing lands between a
 * header and its files.
 */
export function blockEnde(rows: Zeile[], i: number): number {
  const key = rows[i].key;
  let j = i + 1;
  while (j < rows.length && rows[j].parent === key) j++;
  return j;
}

/** Where the block of the row at `i` ends on screen, falling back to the
 *  row's own box while its last row has not been measured. */
function unterkante(kaesten: Record<number, Kasten>, rows: Zeile[], i: number, kopf: Kasten): number {
  const b = kaesten[blockEnde(rows, i) - 1] ?? kopf;
  return b.y + b.h;
}

/**
 * schrittVon is how far a neighbour moves to fill the dragged block's place:
 * its height plus the gap to the next cell. The list lays its cells out with
 * a gap between them, and a neighbour moved by the height alone stops that gap
 * short of the slot it takes once the new order is drawn.
 */
export function schrittVon(kaesten: Record<number, Kasten>, rows: Zeile[], from: number): number {
  const a = kaesten[from];
  if (!a) return 0;
  const nach = kaesten[blockEnde(rows, from)];
  if (nach) return nach.y - a.y;
  const unten = unterkante(kaesten, rows, from, a);
  const vor = kaesten[from - 1];
  return vor ? unten - (vor.y + vor.h) : unten - a.y;
}

/**
 * versatz is where a row of the dragged row's band sits while the gap is at
 * `to`: a row between the origin and the gap moves one step towards the
 * origin, and every other row stays.
 */
export function versatz(index: number, from: number, to: number, schritt: number): number {
  if (from < to && index > from && index <= to) return -schritt;
  if (from > to && index >= to && index < from) return schritt;
  return 0;
}

/**
 * landeziel is how far the dragged block travels from its own slot into the
 * gap: down to where the last block it passed ends, or up to where the first
 * one began. Taken from the measured boxes, because a band's rows need not
 * share a height.
 */
export function landeziel(kaesten: Record<number, Kasten>, rows: Zeile[], from: number, to: number): number {
  const a = kaesten[from];
  const z = kaesten[to];
  if (!a || !z) return 0;
  if (to > from) return unterkante(kaesten, rows, to, z) - unterkante(kaesten, rows, from, a);
  return z.y - a.y;
}

/**
 * spielraum is how far the dragged block may be carried, up and down, from its
 * own slot: to the edges of its band. It cannot land outside the band, and a
 * row carried across rows it can never join promises a drop that is not there.
 * A row not measured yet has no edges to keep to and follows the finger.
 */
export function spielraum(kaesten: Record<number, Kasten>, rows: Zeile[], from: number): [number, number] {
  const a = kaesten[from];
  if (!a) return [-Infinity, Infinity];
  const band = rows[from].band;
  const boden = unterkante(kaesten, rows, from, a);
  let oben = 0;
  let unten = 0;
  rows.forEach((r, i) => {
    const b = kaesten[i];
    if (!b || r.band !== band) return;
    oben = Math.min(oben, b.y - a.y);
    unten = Math.max(unten, unterkante(kaesten, rows, i, b) - boden);
  });
  return [oben, unten];
}

/**
 * reiheNach is the band's order after the row at `from` is dropped on the slot
 * of the row at `to`, or null when the drop moved nothing.
 */
export function reiheNach(rows: Zeile[], from: number, to: number): { band: string; keys: string[] } | null {
  const zeile = rows[from];
  const ziel = rows[to];
  if (from === to || !zeile || !ziel || zeile.band !== ziel.band) return null;
  const keys = rows.filter((r) => r.band === zeile.band).map((r) => r.key);
  const von = keys.indexOf(zeile.key);
  const nach = keys.indexOf(ziel.key);
  keys.splice(nach, 0, ...keys.splice(von, 1));
  return { band: zeile.band, keys };
}

/**
 * ordneBand shows `rows` with one band in the given order. Each of the band's
 * rows takes the rows hanging under it along, and the blocks trade places
 * among the slots the band already holds, so nothing outside it moves. A row
 * the order does not name, one that arrived after the drop, goes after the
 * ones it does.
 */
export function ordneBand<T extends Zeile>(rows: T[], band: string, keys: string[]): T[] {
  const bloecke = new Map<string, T[]>();
  rows.forEach((r, i) => {
    if (r.band === band) bloecke.set(r.key, rows.slice(i, blockEnde(rows, i)));
  });
  const genannt = new Set(keys);
  const folge = [
    ...keys.flatMap((k) => {
      const b = bloecke.get(k);
      return b ? [b] : [];
    }),
    ...[...bloecke].filter(([k]) => !genannt.has(k)).map(([, b]) => b),
  ];
  const aus: T[] = [];
  let n = 0;
  for (let i = 0; i < rows.length; ) {
    if (rows[i].band === band) {
      aus.push(...folge[n++]);
      i = blockEnde(rows, i);
    } else {
      aus.push(rows[i++]);
    }
  }
  return aus;
}

/**
 * bandFolgt says whether `rows` already show the band in the given order.
 * Only the rows both know are compared, so a row that finished and left the
 * list does not keep a dropped order on screen.
 */
export function bandFolgt(rows: Zeile[], band: string, keys: string[]): boolean {
  const live = rows.filter((r) => r.band === band).map((r) => r.key);
  const da = new Set(live);
  const soll = keys.filter((k) => da.has(k));
  const genannt = new Set(soll);
  return live.filter((k) => genannt.has(k)).every((k, i) => k === soll[i]);
}
