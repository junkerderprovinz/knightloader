// The search box's query language. A query is a list of terms that must all
// hold: a leading minus excludes, a prefix aims a term at one field, and a
// comparison asks about size or age, as in "host:x >1gb -sample".
//
// Anything else is plain text. An unknown prefix, an operator without a number
// or a colon inside a name ("C:\media") is searched for literally rather than
// reported as an error, so names keep finding their rows.
//
// The only import is type-only, so check-search-query.mjs can run this module
// in Node.
import type { Task } from './api';

export type SearchCategory = 'any' | 'name' | 'host' | 'package' | 'comment' | 'url';

/** The fields a bare word is searched in when the picker says "Everything". */
export const SEARCH_FIELDS: Exclude<SearchCategory, 'any'>[] = ['name', 'host', 'package', 'comment', 'url'];

export interface SearchQuery {
  text: string;
  category: SearchCategory;
}

export const EMPTY_SEARCH: SearchQuery = { text: '', category: 'any' };

// The Host column and this filter parse every row's URL on every repaint, so
// the result is cached, with a cap.
const hostCache = new Map<string, string>();

/**
 * hostOf is the file host, which differs from the resolver behind a debrid
 * service. Task.host wins when the server has set it; until then the URL's
 * hostname stands in, as on the server. The Host column uses it too, so a row
 * is found under the host it shows.
 */
export function hostOf(t: Task): string {
  if (t.host) return t.host;
  if (!t.url) return '';
  let h = hostCache.get(t.url);
  if (h === undefined) {
    try {
      h = new URL(t.url).hostname.replace(/^www\./, '');
    } catch {
      h = '';
    }
    if (hostCache.size > 5000) hostCache.clear();
    hostCache.set(t.url, h);
  }
  return h;
}

/** fieldOf is what one category reads off a task. `name` falls back to the
 *  URL, which is what an unresolved link shows as its name. */
export function fieldOf(t: Task, c: Exclude<SearchCategory, 'any'>): string {
  switch (c) {
    case 'name':
      return t.name || t.url;
    case 'host':
      return hostOf(t);
    case 'package':
      return t.package;
    case 'comment':
      return t.comment ?? '';
    case 'url':
      return t.url;
  }
}

/** The comparisons a size question can ask. */
export type Comparison = '>' | '>=' | '<' | '<=' | '=';

/** One condition a row has to satisfy. `negate` is the leading minus. */
export type SearchTerm =
  | { kind: 'text'; needle: string; field: SearchCategory; negate: boolean }
  | { kind: 'size'; op: Comparison; bytes: number; negate: boolean }
  | { kind: 'age'; olderThan: boolean; ms: number; negate: boolean };

/** The prefixes that aim a term at one field. German spellings are aliases,
 *  since a German list header says "Paket" and people type what they read. */
const FIELD_PREFIX: Record<string, Exclude<SearchCategory, 'any'>> = {
  name: 'name',
  host: 'host',
  hoster: 'host',
  package: 'package',
  paket: 'package',
  comment: 'comment',
  kommentar: 'comment',
  url: 'url',
  link: 'url',
};

/** Age prefixes, same alias arrangement as FIELD_PREFIX above. */
const OLDER_PREFIX = new Set(['older', 'aelter', 'älter']);
const NEWER_PREFIX = new Set(['newer', 'neuer']);
/** `size:>500mb`, the long form of the bare `>500mb`. */
const SIZE_PREFIX = new Set(['size', 'groesse', 'größe']);

/** Binary multipliers, as the Size column prints them, so `>500mb` agrees
 *  with a row showing 512 MiB. */
const SIZE_UNITS: Record<string, number> = {
  '': 1,
  b: 1,
  k: 1024,
  kb: 1024,
  kib: 1024,
  m: 1024 ** 2,
  mb: 1024 ** 2,
  mib: 1024 ** 2,
  g: 1024 ** 3,
  gb: 1024 ** 3,
  gib: 1024 ** 3,
  t: 1024 ** 4,
  tb: 1024 ** 4,
  tib: 1024 ** 4,
};

/** Age units in milliseconds. `t` is German "Tage"; `m` is minutes, not
 *  months. */
const AGE_UNITS: Record<string, number> = {
  s: 1000,
  m: 60_000,
  h: 3_600_000,
  d: 86_400_000,
  t: 86_400_000,
  w: 604_800_000,
};

/**
 * tokenize splits a query into words, with double quotes holding a phrase
 * together. An unclosed quote runs to the end, since the query is still being
 * typed.
 */
export function tokenize(text: string): string[] {
  const out: string[] = [];
  let cur = '';
  let quoted = false;
  const flush = () => {
    if (cur) out.push(cur);
    cur = '';
  };
  for (const ch of text) {
    if (ch === '"') {
      quoted = !quoted;
      continue;
    }
    if (!quoted && (ch === ' ' || ch === '\t' || ch === '\n' || ch === '\r')) {
      flush();
      continue;
    }
    cur += ch;
  }
  flush();
  return out;
}

const COMPARISON = /^(>=|<=|>|<|=)\s*(\d+(?:[.,]\d+)?)\s*([a-zA-Z]*)$/;

/** parseSize reads `>500mb`, or nothing at all if that is not what this is. */
function parseSize(s: string): { op: Comparison; bytes: number } | null {
  const m = COMPARISON.exec(s.trim());
  if (!m) return null;
  const unit = m[3].toLowerCase();
  // An unknown unit is not a size question; `>3x` may be part of a name.
  if (!(unit in SIZE_UNITS)) return null;
  const n = Number(m[2].replace(',', '.'));
  if (!Number.isFinite(n)) return null;
  return { op: m[1] as Comparison, bytes: n * SIZE_UNITS[unit] };
}

const DURATION = /^(\d+(?:[.,]\d+)?)\s*([a-zA-Z]*)$/;

/** parseAge reads `7d`, `2h`, `30m`, or a bare number as days. */
function parseAge(s: string): number | null {
  const m = DURATION.exec(s.trim());
  if (!m) return null;
  const unit = (m[2] || 'd').toLowerCase();
  if (!(unit in AGE_UNITS)) return null;
  const n = Number(m[1].replace(',', '.'));
  return Number.isFinite(n) ? n * AGE_UNITS[unit] : null;
}

/** typedTerm reads `<prefix>:<value>`, or null when the prefix means nothing here. */
function typedTerm(key: string, value: string, negate: boolean): SearchTerm | null {
  // A bare `host:` stays literal text, so a half-typed prefix does not empty
  // the list.
  if (!value.trim()) return null;
  const field = FIELD_PREFIX[key];
  if (field) return { kind: 'text', needle: value.trim().toLowerCase(), field, negate };
  if (SIZE_PREFIX.has(key)) {
    const size = parseSize(value);
    return size ? { kind: 'size', ...size, negate } : null;
  }
  if (OLDER_PREFIX.has(key) || NEWER_PREFIX.has(key)) {
    const ms = parseAge(value);
    return ms === null ? null : { kind: 'age', olderThan: OLDER_PREFIX.has(key), ms, negate };
  }
  return null;
}

/**
 * parseSearch turns the typed text into the conditions a row has to satisfy.
 * Exported so check-search-query.mjs can tell an understood term from one that
 * fell through to plain text.
 */
export function parseSearch(text: string, category: SearchCategory = 'any'): SearchTerm[] {
  const out: SearchTerm[] = [];
  for (const token of tokenize(text)) {
    let negate = false;
    let body = token;
    // Only a leading minus negates, and a lone "-" is still being typed.
    if (body.length > 1 && body.startsWith('-')) {
      negate = true;
      body = body.slice(1);
    }
    const bare = parseSize(body);
    if (bare) {
      out.push({ kind: 'size', ...bare, negate });
      continue;
    }
    const colon = body.indexOf(':');
    if (colon > 0) {
      const typed = typedTerm(body.slice(0, colon).toLowerCase(), body.slice(colon + 1), negate);
      if (typed) {
        out.push(typed);
        continue;
      }
    }
    const needle = body.trim().toLowerCase();
    if (needle) out.push({ kind: 'text', needle, field: category, negate });
  }
  return out;
}

function compare(value: number, op: Comparison, against: number): boolean {
  switch (op) {
    case '>':
      return value > against;
    case '>=':
      return value >= against;
    case '<':
      return value < against;
    case '<=':
      return value <= against;
    case '=':
      return value === against;
  }
}

/** holds answers one term for one row, before the minus is applied. */
function holds(t: Task, term: SearchTerm, now: number): boolean {
  switch (term.kind) {
    case 'text': {
      if (term.field !== 'any') return fieldOf(t, term.field).toLowerCase().includes(term.needle);
      return SEARCH_FIELDS.some((f) => fieldOf(t, f).toLowerCase().includes(term.needle));
    }
    case 'size':
      // An unmeasured link answers no, or `<1gb` would match every fresh paste.
      return t.size > 0 && compare(t.size, term.op, term.bytes);
    case 'age': {
      const added = Date.parse(t.createdAt);
      // An unreadable timestamp answers no rather than counting as 1970.
      if (Number.isNaN(added)) return false;
      const age = now - added;
      return term.olderThan ? age > term.ms : age < term.ms;
    }
  }
}

// The last parse. matchesSearch runs once per row, so a one-entry cache
// parses each query once per filter pass.
let lastParse: { text: string; category: SearchCategory; terms: SearchTerm[] } | null = null;

/** compileSearch is parseSearch with that one-entry cache in front of it. */
export function compileSearch(q: SearchQuery): SearchTerm[] {
  if (lastParse && lastParse.text === q.text && lastParse.category === q.category) return lastParse.terms;
  const terms = parseSearch(q.text, q.category);
  lastParse = { text: q.text, category: q.category, terms };
  return terms;
}

/** matchesSearch is the filter itself, so the pages cannot disagree about it. */
export function matchesSearch(t: Task, q: SearchQuery): boolean {
  const terms = compileSearch(q);
  if (terms.length === 0) return true;
  const now = Date.now();
  // Every term must hold, and a negated one must not.
  return terms.every((term) => holds(t, term, now) !== term.negate);
}
