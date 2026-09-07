// The search box as a question with several parts, instead of one substring.
//
// One substring can only ever narrow a list in one direction, and past a few
// hundred rows that stops being enough: the evening's downloads are four
// releases from three hosts in two packages, and the row somebody wants is
// "the big ones from that host, except the samples". Typed as one substring
// that is three separate searches, run by hand, none of which can be combined.
//
// So a query is now a list of terms that all have to hold. A leading minus
// excludes, a prefix aims one term at one field, and a comparison asks about
// the size or the age. EVERYTHING ELSE STAYS PLAIN TEXT, which is the rule the
// rest of this file is arranged around: a file called "S02E04 - 1080p.mkv" or a
// path like "C:\media" must go on finding the row it always found, so an
// unrecognised prefix, an operator with no number behind it and a colon in the
// middle of a name are all searched for literally rather than reported as a
// syntax error. A search box that can be got wrong is a search box people stop
// trusting.
//
// No import that survives compilation: this module is pure so that
// web/check-search-query.mjs can drive the real parser rather than a copy of it
// (Node strips the type-only import below, which is the whole reason it is
// type-only).
import type { Task } from './api';

export type SearchCategory = 'any' | 'name' | 'host' | 'package' | 'comment' | 'url';

/** The fields a bare word is searched in when the picker says "Everything". */
export const SEARCH_FIELDS: Exclude<SearchCategory, 'any'>[] = ['name', 'host', 'package', 'comment', 'url'];

export interface SearchQuery {
  text: string;
  category: SearchCategory;
}

export const EMPTY_SEARCH: SearchQuery = { text: '', category: 'any' };

// Parsing a URL is not free and the answer never changes for one link, while
// the Host column and this filter both ask for it on every row of every
// repaint. The cap is there so a session that has seen a hundred thousand links
// does not keep them all.
const hostCache = new Map<string, string>();

/**
 * hostOf is the file host, which is not the resolver: through a debrid service
 * every row would otherwise claim the same origin.
 *
 * Task.host is what the server says, and when it is filled it wins. Until then
 * the pasted URL's own hostname stands in - the same rule the server side
 * follows, so the answer does not change when the field lands.
 *
 * It lives HERE rather than in components/columns.tsx, which is where it grew
 * up and which now imports it: the Host column and the `host:` search term are
 * the same question, and two copies of it - the search field had one of its own
 * for exactly as long as it had a Host category - is how a row gets filed under
 * one host and found under another.
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

/**
 * fieldOf is what one category reads off a task.
 *
 * `name` falls back to the URL because that is what an unresolved link renders
 * as its name: a search that skipped it would claim no row matches while the
 * matching text is on screen.
 */
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

/**
 * The prefixes that aim a term at one field.
 *
 * The German spellings sit beside the English ones on purpose. This instance is
 * read in German as often as in English, its own list header says "Paket", and
 * somebody who has just read that header types `paket:`; silently searching for
 * the literal text "paket:serie" would be correct by the rules above and useless
 * in practice. They are aliases, never a second syntax - one parser, one set of
 * terms, and a query typed either way means exactly the same thing.
 */
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

/**
 * Binary multipliers, matching lib/format.ts's fmtBytes.
 *
 * The Size column prints KiB/MiB/GiB, so a decimal reading here would make
 * `>500mb` hide rows the list itself calls 512 MiB - a filter that disagrees
 * with the column beside it is read as a bug in the filter, and rightly. `mib`
 * and `gib` are accepted as the same thing for anybody who spells it out.
 */
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

/**
 * Age units in milliseconds. `t` is the German "Tage" beside the English `d`,
 * and `m` is minutes rather than months: an age asked in months is a question
 * about a list nobody keeps, while "added in the last 30 minutes" is the reason
 * anybody reaches for this at all.
 */
const AGE_UNITS: Record<string, number> = {
  s: 1000,
  m: 60_000,
  h: 3_600_000,
  d: 86_400_000,
  t: 86_400_000,
  w: 604_800_000,
};

/**
 * tokenize splits a query into words, with double quotes holding one together.
 *
 * Quoting is what makes a name with a space in it searchable at all. Without it
 * `Big Buck Bunny` is three separate words that must all appear somewhere in a
 * row, which also matches a row where they appear in three different fields in
 * any order - close enough to look like it works and wrong exactly when it
 * matters. An unclosed quote runs to the end of the input rather than being
 * refused: the query is being typed, and the character after the opening quote
 * must already narrow the list.
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
  // An unknown unit is not a size question. `>3x` is somebody's file name, and
  // guessing bytes for it would silently empty the list.
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
  // Days by default, because that is the unit of the question this answers:
  // "what has been sitting here since the weekend".
  const unit = (m[2] || 'd').toLowerCase();
  if (!(unit in AGE_UNITS)) return null;
  const n = Number(m[1].replace(',', '.'));
  return Number.isFinite(n) ? n * AGE_UNITS[unit] : null;
}

/** typedTerm reads `<prefix>:<value>`, or null when the prefix means nothing here. */
function typedTerm(key: string, value: string, negate: boolean): SearchTerm | null {
  // `host:` on its own is not a question, so it stays the literal text somebody
  // has typed so far. It also keeps a half-typed prefix from emptying the list
  // between one keystroke and the next.
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
 *
 * Exported so that web/check-search-query.mjs can assert on the terms
 * themselves: a check that only ever asks "does this row match" cannot tell a
 * query that was understood from one that fell through to plain text and
 * happened to match anyway.
 */
export function parseSearch(text: string, category: SearchCategory = 'any'): SearchTerm[] {
  const out: SearchTerm[] = [];
  for (const token of tokenize(text)) {
    let negate = false;
    let body = token;
    // Only a LEADING minus, and never a token that is nothing but one: a minus
    // inside a word belongs to the word ("S02E04-1080p"), and a lone one is
    // somebody halfway through typing.
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
      // A link nobody has measured yet answers NO to every size question rather
      // than pretending to be zero bytes. Read the other way, `<1gb` would sweep
      // up every freshly pasted link in the list and call them small.
      return t.size > 0 && compare(t.size, term.op, term.bytes);
    case 'age': {
      const added = Date.parse(t.createdAt);
      // Same rule as the size above, for the same reason: a timestamp this
      // browser cannot read is not an age, so it answers no rather than being
      // treated as 1970 and matching every "older than" ever typed.
      if (Number.isNaN(added)) return false;
      const age = now - added;
      return term.olderThan ? age > term.ms : age < term.ms;
    }
  }
}

/**
 * The last parse, kept.
 *
 * matchesSearch is called once per ROW - the pages hand it to Array.filter over
 * the whole list - so parsing inside it would re-parse the identical query a
 * thousand times for every keystroke. One entry is the right size: a filter pass
 * asks the same question of every row, and the next keystroke replaces it.
 */
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
  // Every term, and a negated one has to be false. An empty query matched
  // everything before this file existed and still does.
  return terms.every((term) => holds(t, term, now) !== term.negate);
}
