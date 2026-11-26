// How the quick settings' two fields read and write their numbers, the rules
// of the web's speed limit and simultaneous downloads fields
// (UnitNumberInput and NumberInput in web/src/components/ui.tsx).
//
// It imports nothing at run time, so check-narrowing.mjs can load it in node.

export interface RateUnit {
  label: 'kB/s' | 'MB/s' | 'GB/s';
  factor: number;
}

/** Decimal, as the speed graph prints rates. Untranslated symbols. */
export const RATE_UNITS: RateUnit[] = [
  { label: 'kB/s', factor: 1e3 },
  { label: 'MB/s', factor: 1e6 },
  { label: 'GB/s', factor: 1e9 },
];

/** The settings page's own bounds for how many downloads run at once. */
export const MAX_CONCURRENT = { min: 1, max: 64 };

/** unitFor is the largest unit the value fills at least once, kB/s for nothing. */
export function unitFor(bytes: number): RateUnit {
  let found = RATE_UNITS[0];
  for (const u of RATE_UNITS) if (u.factor <= bytes) found = u;
  return found;
}

/** amountIn is a value in a unit, with at most two decimals and no trailing zeros. */
export function amountIn(bytes: number, u: RateUnit): string {
  return String(Math.round((bytes / u.factor) * 100) / 100);
}

/**
 * bytesOf reads a typed amount in a unit, a decimal comma included, or null
 * for anything that is no amount, which the field then holds without saving.
 */
export function bytesOf(text: string, u: RateUnit): number | null {
  const m = /^\s*(\d+(?:[.,]\d*)?|[.,]\d+)\s*$/.exec(text);
  if (!m) return null;
  return Math.round(Number(m[1].replace(',', '.')) * u.factor);
}

/** stepCount moves a count by one within the bounds. */
export function stepCount(n: number, by: 1 | -1): number {
  return Math.min(MAX_CONCURRENT.max, Math.max(MAX_CONCURRENT.min, n + by));
}

/** countOf reads a typed count, held to the bounds, or null for anything that is no whole number. */
export function countOf(text: string): number | null {
  if (!/^\s*\d+\s*$/.test(text)) return null;
  return Math.min(MAX_CONCURRENT.max, Math.max(MAX_CONCURRENT.min, Number(text)));
}
