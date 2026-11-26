// What narrows the Downloads and Collector lists on the phone: the web's quick
// filters, its search language and the collector's facets, run over the task
// list the screen already polls. The rules are the web's
// (web/src/components/ListToolbar.tsx, web/src/lib/searchQuery.ts and
// web/src/components/CollectorFacets.tsx), so a search finds the same rows on
// both.
//
// It imports nothing at run time, so check-narrowing.mjs can load it in node.

import type { Task } from './types';

/** The fields of a task the list reads here that the rows themselves do not. */
export type Listed = Task & {
  host?: string;
  comment?: string;
  filename?: string;
};

export type QuickFilterId =
  | 'running'
  | 'leeching'
  | 'queued'
  | 'paused'
  | 'finished'
  | 'seeding'
  | 'failed'
  | 'offline'
  | 'disabled'
  | 'online'
  | 'uncheckable'
  | 'unchecked'
  | 'stalled';

// omitempty does not drop a zero time.Time, so an unset one arrives as year one.
function happened(iso?: string): boolean {
  if (!iso) return false;
  const d = new Date(iso);
  return !Number.isNaN(d.getTime()) && d.getUTCFullYear() > 1;
}

const repairing = (t: Task) => t.status === 'done' && !!t.repair;

const QUICK_FILTERS: Record<QuickFilterId, (t: Task) => boolean> = {
  // Extracting and a par2 check count as running, since the download is not
  // over yet. A torrent the engine downloads reads Leeching instead.
  running: (t) =>
    (t.status === 'running' && t.resolver !== 'torrent') || repairing(t) || t.status === 'extracting',
  leeching: (t) => t.status === 'running' && t.resolver === 'torrent',
  queued: (t) => t.status === 'queued',
  paused: (t) => t.status === 'paused',
  // A torrent still uploading has a filter of its own.
  finished: (t) => t.status === 'done' && !t.seeding,
  seeding: (t) => t.status === 'done' && !!t.seeding,
  failed: (t) => t.status === 'error',
  online: (t) => t.online === 'online',
  offline: (t) => t.online === 'offline',
  // The host would not say, so these are worth checking again.
  uncheckable: (t) => t.online === 'uncheckable',
  unchecked: (t) => !t.online,
  disabled: (t) => !t.enabled,
  stalled: (t) => happened(t.stalledSince),
};

/** The states a download list is filtered by, in the web's order. */
export const DOWNLOAD_FILTERS: QuickFilterId[] = [
  'running',
  'leeching',
  'queued',
  'paused',
  'finished',
  'seeding',
  'failed',
  'offline',
  'disabled',
  'stalled',
];

/** The collector's, where every row is staged and the question is what a check said. */
export const COLLECTOR_FILTERS: QuickFilterId[] = ['online', 'offline', 'uncheckable', 'unchecked', 'disabled'];

/**
 * offeredFilters counts each filter's matches and offers those with any,
 * keeping an active one at zero so its chip does not vanish under the finger.
 */
export function offeredFilters(
  ids: QuickFilterId[],
  tasks: Task[],
  active: ReadonlySet<QuickFilterId>,
): { id: QuickFilterId; n: number }[] {
  return ids
    .map((id) => ({ id, n: tasks.reduce((s, x) => s + (QUICK_FILTERS[id](x) ? 1 : 0), 0) }))
    .filter(({ id, n }) => n > 0 || active.has(id));
}

/** A union: two filters on show both kinds, since the states exclude each other. */
export function matchesFilters(t: Task, active: ReadonlySet<QuickFilterId>): boolean {
  if (active.size === 0) return true;
  for (const id of active) if (QUICK_FILTERS[id](t)) return true;
  return false;
}

/**
 * hostOf is the file host the server named, or the link's own host name until
 * it has, as the web's Host column reads it.
 */
export function hostOf(t: Listed): string {
  if (t.host) return t.host;
  const m = /^[a-z][a-z0-9+.-]*:\/\/(?:[^/?#@]*@)?([^/?#:]+)/i.exec(t.url || '');
  return m ? m[1].toLowerCase().replace(/^www\./, '') : '';
}

type Field = 'name' | 'host' | 'package' | 'comment' | 'url';
const FIELDS: Field[] = ['name', 'host', 'package', 'comment', 'url'];

/** Where a bare word is searched: every field, or the one the web's picker names. */
export type SearchCategory = Field | 'any';


function fieldOf(t: Listed, f: Field): string {
  switch (f) {
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

type Comparison = '>' | '>=' | '<' | '<=' | '=';

type Term =
  | { kind: 'text'; needle: string; field: Field | 'any'; negate: boolean }
  | { kind: 'size'; op: Comparison; bytes: number; negate: boolean }
  | { kind: 'age'; olderThan: boolean; ms: number; negate: boolean };

// German spellings too, since a German list says "Paket" and people type
// what they read.
const FIELD_PREFIX: Record<string, Field> = {
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
const OLDER_PREFIX = new Set(['older', 'aelter', 'älter']);
const NEWER_PREFIX = new Set(['newer', 'neuer']);
const SIZE_PREFIX = new Set(['size', 'groesse', 'größe']);

// Decimal, as the rows print sizes, so ">500mb" agrees with a row showing 500 MB.
const SIZE_UNITS: Record<string, number> = {
  '': 1,
  b: 1,
  k: 1e3,
  kb: 1e3,
  kib: 1024,
  m: 1e6,
  mb: 1e6,
  mib: 1024 ** 2,
  g: 1e9,
  gb: 1e9,
  gib: 1024 ** 3,
  t: 1e12,
  tb: 1e12,
  tib: 1024 ** 4,
};

// "t" is German "Tage", and "m" is minutes rather than months.
const AGE_UNITS: Record<string, number> = { s: 1e3, m: 6e4, h: 36e5, d: 864e5, t: 864e5, w: 6048e5 };

/** Words, with double quotes holding a phrase together; an open quote runs to the end. */
function tokenize(text: string): string[] {
  const out: string[] = [];
  let cur = '';
  let quoted = false;
  for (const ch of text) {
    if (ch === '"') {
      quoted = !quoted;
      continue;
    }
    if (!quoted && /\s/.test(ch)) {
      if (cur) out.push(cur);
      cur = '';
      continue;
    }
    cur += ch;
  }
  if (cur) out.push(cur);
  return out;
}

function parseSize(s: string): { op: Comparison; bytes: number } | null {
  const m = /^(>=|<=|>|<|=)\s*(\d+(?:[.,]\d+)?)\s*([a-zA-Z]*)$/.exec(s.trim());
  if (!m) return null;
  const unit = m[3].toLowerCase();
  // ">3x" may be part of a name.
  if (!(unit in SIZE_UNITS)) return null;
  return { op: m[1] as Comparison, bytes: Number(m[2].replace(',', '.')) * SIZE_UNITS[unit] };
}

function parseAge(s: string): number | null {
  const m = /^(\d+(?:[.,]\d+)?)\s*([a-zA-Z]*)$/.exec(s.trim());
  if (!m) return null;
  const unit = (m[2] || 'd').toLowerCase();
  return unit in AGE_UNITS ? Number(m[1].replace(',', '.')) * AGE_UNITS[unit] : null;
}

function typedTerm(key: string, value: string, negate: boolean): Term | null {
  // A bare "host:" stays text, so a half-typed prefix does not empty the list.
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
 * parseSearch turns typed text into terms that must all hold. A leading minus
 * excludes, a prefix aims a word at one field, and anything it does not
 * understand is searched for as plain text, so names keep finding their rows.
 * A bare word goes to `category`.
 */
export function parseSearch(text: string, category: SearchCategory = 'any'): Term[] {
  const out: Term[] = [];
  for (const token of tokenize(text)) {
    let negate = false;
    let body = token;
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

function holds(t: Listed, term: Term, now: number): boolean {
  switch (term.kind) {
    case 'text':
      if (term.field !== 'any') return fieldOf(t, term.field).toLowerCase().includes(term.needle);
      return FIELDS.some((f) => fieldOf(t, f).toLowerCase().includes(term.needle));
    case 'size':
      // An unmeasured link answers no, or "<1gb" would match every fresh paste.
      return t.size > 0 && compare(t.size, term.op, term.bytes);
    case 'age': {
      const added = Date.parse(t.createdAt);
      if (Number.isNaN(added)) return false;
      const age = now - added;
      return term.olderThan ? age > term.ms : age < term.ms;
    }
  }
}

/** matchesSearch is the search itself, every term holding and a negated one not. */
export function matchesSearch(t: Listed, terms: Term[], now = Date.now()): boolean {
  return terms.every((term) => holds(t, term, now) !== term.negate);
}

export type FacetKind = 'host' | 'fileType' | 'package';
export type FileType = 'archive' | 'video' | 'audio' | 'image' | 'document' | 'other';
export type Facets = Record<FacetKind, ReadonlySet<string>>;

export const NO_FACETS: Facets = { host: new Set(), fileType: new Set(), package: new Set() };

const EXT_TYPE: Record<string, FileType> = {};
for (const [type, exts] of Object.entries({
  archive: 'zip rar 7z tar gz tgz bz2 xz iso dlc dmg cbr cbz',
  video: 'mp4 mkv avi mov wmv flv webm m4v mpg mpeg ts',
  audio: 'mp3 flac wav aac ogg m4a wma',
  image: 'jpg jpeg png gif webp bmp tiff svg',
  document: 'pdf epub mobi azw3 doc docx txt',
})) {
  for (const ext of exts.split(' ')) EXT_TYPE[ext] = type as FileType;
}

/** fileTypeOf sorts a link by its extension, the tail parts of a split archive included. */
export function fileTypeOf(t: Listed): FileType {
  const name = t.filename || t.name || t.url;
  const base = name.split(/[?#]/, 1)[0] ?? name;
  const ext = /\.([a-z0-9]{1,8})$/i.exec(base)?.[1].toLowerCase() ?? '';
  return EXT_TYPE[ext] ?? (/^(r\d{2,3}|z\d{2}|\d{3})$/.test(ext) ? 'archive' : 'other');
}

const facetOf: Record<FacetKind, (t: Listed) => string> = {
  host: hostOf,
  fileType: fileTypeOf,
  package: (t) => t.package || '',
};

/** A union within one kind and an intersection across them, as on the web. */
export function matchesFacets(t: Listed, f: Facets): boolean {
  return (Object.keys(facetOf) as FacetKind[]).every((k) => f[k].size === 0 || f[k].has(facetOf[k](t)));
}

/**
 * facetOptions is one kind's values with how many links hold each, the most
 * common first. A chosen value with nothing left stays at zero, or nobody
 * could switch it off.
 */
export function facetOptions(tasks: Listed[], kind: FacetKind, active: ReadonlySet<string>): { value: string; n: number }[] {
  const counts = new Map<string, number>();
  for (const t of tasks) {
    const v = facetOf[kind](t);
    counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  for (const v of active) if (!counts.has(v)) counts.set(v, 0);
  return [...counts.entries()]
    .map(([value, n]) => ({ value, n }))
    .sort((a, b) => b.n - a.n || a.value.localeCompare(b.value));
}

/** Everything that narrows one list. */
export interface Narrowing {
  search: string;
  // Set by a saved view from the web, whose search can aim at one field.
  category: SearchCategory;
  filters: ReadonlySet<QuickFilterId>;
  facets: Facets;
}

export const NOTHING_NARROWED: Narrowing = { search: '', category: 'any', filters: new Set(), facets: NO_FACETS };

/** Whether anything is set, a filter that hides nothing included. */
export function isNarrowed(n: Narrowing): boolean {
  return (
    n.search.trim() !== '' ||
    n.filters.size > 0 ||
    n.facets.host.size + n.facets.fileType.size + n.facets.package.size > 0
  );
}

/** narrow keeps the rows every part of n lets through. */
export function narrow<T extends Listed>(tasks: T[], n: Narrowing, now = Date.now()): T[] {
  if (!isNarrowed(n)) return tasks;
  const terms = parseSearch(n.search, n.category);
  return tasks.filter((t) => matchesFilters(t, n.filters) && matchesFacets(t, n.facets) && matchesSearch(t, terms, now));
}

/** toggled is set with value switched. */
export function toggled<T>(set: ReadonlySet<T>, value: T): Set<T> {
  const next = new Set(set);
  if (next.has(value)) next.delete(value);
  else next.add(value);
  return next;
}
