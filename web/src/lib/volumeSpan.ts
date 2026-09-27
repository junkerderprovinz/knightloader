// The spans the volume card asks /api/stats/volume for: 'all', or a count of
// days or months such as '45d' or '6m'.

export type SpanUnit = 'd' | 'm';

/** The card's fixed steps, in the order they are drawn. */
export const SPAN_STEPS = ['7d', '30d', '90d', '12m', 'all'] as const;

export const DEFAULT_SPAN = '30d';

/** The longest span drawn one bar per day, volumeDailyUpTo in routes_stats.go. */
export const SPAN_DAILY_UP_TO = 92;

/** The server's ceilings, volumeMaxDays and volumeMaxMonths in routes_stats.go. */
export const SPAN_MAX: Record<SpanUnit, number> = { d: 730, m: 120 };

export function isStep(span: string): boolean {
  return (SPAN_STEPS as readonly string[]).includes(span);
}

/** readCount reads a field's text as a whole count the unit allows, or null. */
export function readCount(text: string, unit: SpanUnit): number | null {
  if (!/^\s*\d+\s*$/.test(text)) return null;
  const n = Number(text);
  return n >= 1 && n <= SPAN_MAX[unit] ? n : null;
}

/** readSpan splits a span of days or months into its count and unit; null for 'all' and for anything else. */
export function readSpan(span: string): { n: number; unit: SpanUnit } | null {
  const m = /^(\d+)([dm])$/.exec(span);
  if (!m) return null;
  const unit = m[2] as SpanUnit;
  const n = readCount(m[1], unit);
  return n === null ? null : { n, unit };
}

/** storedSpan is a remembered span the server will take, or the default for anything else. */
export function storedSpan(value: unknown): string {
  if (typeof value !== 'string') return DEFAULT_SPAN;
  return value === 'all' || readSpan(value) ? value : DEFAULT_SPAN;
}
