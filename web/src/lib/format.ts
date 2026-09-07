// Human-readable formatting for sizes, speeds and ETAs.

export function fmtBytes(n: number): string {
  if (!n || n < 0) return '—';
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}

/**
 * An allowance in gigabytes, always, whatever its size (jdp, 2026-09-07:
 * "kann man das Volumen nicht in GB angeben? bei allen").
 *
 * Two departures from fmtBytes above, both on purpose:
 *
 *  - The unit never changes. An account's allowance is a figure you compare
 *    against another account's and against what the vendor's own page
 *    advertises, and a column that says "980 MB" on one row and "1.2 TB" on the
 *    next makes that comparison a mental arithmetic exercise.
 *  - Decimal gigabytes, not gibibytes. Every debrid vendor advertises its
 *    allowance in decimal ("400 GB"), so a binary reading would print 372 GB
 *    for the plan the customer bought as 400.
 */
export function fmtGB(n: number): string {
  if (!n || n < 0) return '0 GB';
  const gb = n / 1e9;
  if (gb >= 1000) return `${Math.round(gb).toLocaleString()} GB`;
  // A real but tiny figure is worth saying as "less than", not as "0.0 GB":
  // measured live on the preview instance, a TorBox account that had moved a
  // few megabytes read "0.0 GB geladen", which looks like a broken column
  // rather than like a small number.
  if (gb > 0 && gb < 0.1) return '< 0,1 GB';
  return `${gb.toFixed(gb < 10 ? 1 : 0)} GB`;
}

export const fmtSpeed = (n: number): string => (n > 0 ? `${fmtBytes(n)}/s` : '');

export function fmtEta(loaded: number, size: number, speed: number): string {
  if (speed <= 0 || size <= 0 || loaded >= size) return '';
  const secs = Math.round((size - loaded) / speed);
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.round(secs / 60)}m`;
  const h = Math.floor(secs / 3600);
  const m = Math.round((secs % 3600) / 60);
  return `${h}h ${m}m`;
}

/** The units a speed limit may be entered in, smallest first. */
export const RATE_UNITS = [
  { label: 'KiB/s', factor: 1024 },
  { label: 'MiB/s', factor: 1024 * 1024 },
  { label: 'GiB/s', factor: 1024 * 1024 * 1024 },
] as const;

export type RateUnit = (typeof RATE_UNITS)[number]['label'];

/**
 * splitRate turns a stored bytes-per-second limit into the number and unit a
 * person would have typed. The unit is chosen so the number stays readable —
 * "1.5 MiB/s" rather than "1536 KiB/s" — but it never climbs so far that the
 * number turns into a fraction: 900 KiB/s stays in KiB/s instead of becoming
 * 0.88 MiB/s, which reads as a rounding error rather than as a setting.
 */
export function splitRate(bytesPerSecond: number): { value: number; unit: RateUnit } {
  const n = Math.max(0, Math.round(bytesPerSecond));
  if (n === 0) return { value: 0, unit: 'KiB/s' };
  let chosen: (typeof RATE_UNITS)[number] = RATE_UNITS[0];
  for (const u of RATE_UNITS) {
    if (n >= u.factor) chosen = u;
  }
  return { value: n / chosen.factor, unit: chosen.label };
}

/** joinRate is the inverse: what to store for a number the user typed. */
export function joinRate(value: number, unit: RateUnit): number {
  const u = RATE_UNITS.find((x) => x.label === unit) ?? RATE_UNITS[0];
  return Math.max(0, Math.round(value * u.factor));
}

/**
 * fmtRateValue prints the number beside the unit. Trailing zeros are dropped
 * so an exact limit shows as "2" and not "2.00", and the value is capped at two
 * decimals because a third would be under a kilobyte and nobody is steering
 * their line that finely.
 */
export function fmtRateValue(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '';
  return String(Math.round(value * 100) / 100);
}

export function pct(loaded: number, size: number, done: boolean): number {
  if (size > 0) return Math.min(100, Math.round((loaded / size) * 100));
  return done ? 100 : 0;
}

// Go's encoding/json does not drop a zero time.Time — omitempty has no effect on
// a struct — so an unfinished task arrives carrying year one rather than no
// field at all. Comparing the year is enough and costs nothing; parsing the
// literal string would break the moment the server changed its precision.
const GO_ZERO_YEAR = 1;

// One formatter per locale, kept. Intl.DateTimeFormat is expensive to construct
// and a finished-at column builds one per row per repaint without this, which on
// a few hundred rows is the difference between a list that scrolls and one that
// stutters.
const dateFormats = new Map<string, Intl.DateTimeFormat>();

// The language picker stamps <html lang> at boot and on every change, so reading
// it follows the user's choice without this module importing the i18n provider —
// which would pull the whole dictionary loader into a file that formats numbers.
// Empty falls through to undefined, which is the runtime's own default.
function uiLocale(): string {
  return document.documentElement.lang || '';
}

function dateFormat(locale: string): Intl.DateTimeFormat {
  let f = dateFormats.get(locale);
  if (!f) {
    // Short date and short time together: two downloads that finished this
    // afternoon are the common case, and a date alone cannot tell them apart.
    f = new Intl.DateTimeFormat(locale || undefined, { dateStyle: 'short', timeStyle: 'short' });
    dateFormats.set(locale, f);
  }
  return f;
}

/**
 * fmtDate prints a timestamp in the reader's own locale, short form.
 *
 * Empty for anything that is not a moment in time — absent, unparseable, or Go's
 * zero timestamp. A finished-at cell for a download that has not finished is
 * blank; printing "1.1.1" or "Invalid Date" there would be a value, and a value
 * is something people try to explain.
 */
export function fmtDate(iso: string | undefined, locale = uiLocale()): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return '';
  return dateFormat(locale).format(d);
}
