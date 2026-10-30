// Human-readable formatting for sizes, speeds, durations and dates. Every value
// comes back ready to render anywhere: a number with its unit keeps its order
// in right-to-left text (lib/bidi.ts). Sizes and speeds are decimal, 1 MB being
// 1000 kB, the way disks, lines and hosters count them.

import { isolate, ltr } from './bidi';

function bytes(n: number): string {
  const u = ['B', 'kB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1000 && i < u.length - 1) {
    v /= 1000;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}

/** fmtBytes prints a size, or a dash for none. */
export function fmtBytes(n: number): string {
  if (!n || n < 0) return '-';
  return ltr(bytes(n));
}

/**
 * fmtTotal prints a size where zero is an answer, such as a full disk or
 * nothing loaded yet: "0 B", where fmtBytes's dash would read as missing data.
 */
export const fmtTotal = (n: number): string => ltr(n > 0 ? bytes(n) : '0 B');

/**
 * fmtGB prints an allowance in gigabytes whatever its size, so accounts read
 * side by side compare at a glance.
 */
export function fmtGB(n: number): string {
  if (!n || n < 0) return ltr('0 GB');
  const gb = n / 1e9;
  if (gb >= 1000) return ltr(`${Math.round(gb).toLocaleString()} GB`);
  // "0.0 GB" for a few megabytes looks like a broken column.
  if (gb > 0 && gb < 0.1) return ltr('< 0.1 GB');
  return ltr(`${gb.toFixed(gb < 10 ? 1 : 0)} GB`);
}

/** fmtSpeed prints a transfer rate, or nothing for a transfer at rest. */
export const fmtSpeed = (n: number): string => (n > 0 ? ltr(`${bytes(n)}/s`) : '');

/** fmtRate is fmtSpeed where standing still is a reading too: "0 B/s". */
export const fmtRate = (n: number): string => fmtSpeed(n) || ltr('0 B/s');

export function fmtEta(loaded: number, size: number, speed: number): string {
  if (speed <= 0 || size <= 0 || loaded >= size) return '';
  const secs = Math.round((size - loaded) / speed);
  if (secs < 60) return ltr(`${secs}s`);
  if (secs < 3600) return ltr(`${Math.round(secs / 60)}min`);
  const h = Math.floor(secs / 3600);
  const m = Math.round((secs % 3600) / 60);
  return ltr(`${h}h ${m}min`);
}

/**
 * fmtUptime prints a duration in fmtEta's untranslated units, such as `4d 6h`,
 * matching the task list. Only the two largest units are shown. Seconds appear
 * below a minute, where `0min` would look like not running.
 */
export function fmtUptime(seconds: number): string {
  const secs = Math.max(0, Math.floor(seconds));
  if (secs < 60) return ltr(`${secs}s`);
  if (secs < 3600) return ltr(`${Math.floor(secs / 60)}min`);
  if (secs < 86400) {
    const h = Math.floor(secs / 3600);
    return ltr(`${h}h ${Math.floor((secs % 3600) / 60)}min`);
  }
  const d = Math.floor(secs / 86400);
  return ltr(`${d}d ${Math.floor((secs % 86400) / 3600)}h`);
}

/** fmtPct prints a share of a hundred, "45%". */
export const fmtPct = (n: number): string => ltr(`${n}%`);

/** fmtUnit prints a number with a unit that has no formatter of its own: "120 ms". */
export const fmtUnit = (n: number | string, unit: string): string => ltr(`${n} ${unit}`);

/**
 * The units a speed is entered in, smallest first, each stepping by a round
 * amount of its own. Every speed field in the app is a UnitNumberInput over
 * these, so they all read and step alike.
 */
export const RATE_UNITS = [
  { label: 'kB/s', factor: 1000, step: 250_000 },
  { label: 'MB/s', factor: 1e6, step: 1e6 },
  { label: 'GB/s', factor: 1e9, step: 1e9 },
] as const;

/** The units a file size is entered in, RATE_UNITS without the "/s". */
export const SIZE_UNITS = RATE_UNITS.map((u) => ({ ...u, label: u.label.slice(0, -2) }));

export type RateUnit = (typeof RATE_UNITS)[number]['label'];

/**
 * splitRate turns a stored bytes-per-second limit into the number and unit a
 * person would have typed: "1.5 MB/s" rather than "1500 kB/s", but 900 kB/s
 * rather than 0.9 MB/s.
 */
export function splitRate(bytesPerSecond: number): { value: number; unit: RateUnit } {
  const n = Math.max(0, Math.round(bytesPerSecond));
  if (n === 0) return { value: 0, unit: 'kB/s' };
  let chosen: (typeof RATE_UNITS)[number] = RATE_UNITS[0];
  for (const u of RATE_UNITS) {
    if (n >= u.factor) chosen = u;
  }
  return { value: n / chosen.factor, unit: chosen.label };
}

/** fmtRateValue prints the number beside the unit, with at most two decimals
 *  and no trailing zeros. */
export function fmtRateValue(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '';
  return String(Math.round(value * 100) / 100);
}

export function pct(loaded: number, size: number, done: boolean): number {
  if (size > 0) return Math.min(100, Math.round((loaded / size) * 100));
  return done ? 100 : 0;
}

// omitempty does not drop a zero time.Time, so an unfinished task carries year
// one. Comparing the year survives a change in the server's precision.
const GO_ZERO_YEAR = 1;

// Cached per locale: building an Intl.DateTimeFormat per row per repaint makes
// long lists stutter.
const dateFormats = new Map<string, Intl.DateTimeFormat>();

// <html lang> follows the language picker, which spares this module the i18n
// provider. Empty means the runtime's default.
function uiLocale(): string {
  return document.documentElement.lang || '';
}

function dateFormat(locale: string): Intl.DateTimeFormat {
  let f = dateFormats.get(locale);
  if (!f) {
    // With the time, since two downloads from one afternoon are common.
    f = new Intl.DateTimeFormat(locale || undefined, { dateStyle: 'short', timeStyle: 'short' });
    dateFormats.set(locale, f);
  }
  return f;
}

/**
 * fmtDate prints a timestamp in the reader's locale, short form. Absent,
 * unparseable and Go zero timestamps print as empty rather than as
 * "Invalid Date" or year one.
 */
export function fmtDate(iso: string | undefined, locale = uiLocale()): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return '';
  return isolate(dateFormat(locale).format(d));
}

/**
 * fmtDateFull is fmtDate spelled out, weekday and seconds included, for a
 * tooltip or a detail card: there the short form would say nothing the column
 * had not. It keeps no formatter cache, since it runs once per hover rather
 * than once per row on every repaint.
 */
export function fmtDateFull(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return '';
  return isolate(
    new Intl.DateTimeFormat(uiLocale() || undefined, { dateStyle: 'full', timeStyle: 'medium' }).format(d),
  );
}

const clockFormats = new Map<string, Intl.DateTimeFormat>();

function clockFormat(locale: string): Intl.DateTimeFormat {
  let f = clockFormats.get(locale);
  if (!f) {
    // With seconds, so events a moment apart still show their order.
    f = new Intl.DateTimeFormat(locale || undefined, { timeStyle: 'medium' });
    clockFormats.set(locale, f);
  }
  return f;
}

/**
 * fmtClock prints a time of day from epoch milliseconds, for the session
 * event log, whose rows all fall within the tab's lifetime and need no date.
 * Empty for anything that is not a moment.
 */
export function fmtClock(ms: number, locale = uiLocale()): string {
  if (!Number.isFinite(ms) || ms <= 0) return '';
  return isolate(clockFormat(locale).format(new Date(ms)));
}

/**
 * middleEllipsis shortens a long file name in its middle, keeping the start
 * that names the release and the end that names the part and the type.
 */
export function middleEllipsis(name: string, max = 56): string {
  if (name.length <= max) return name;
  const head = Math.ceil((max - 1) * 0.6);
  return `${name.slice(0, head)}…${name.slice(name.length - (max - 1 - head))}`;
}
