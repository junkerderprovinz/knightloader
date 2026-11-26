// Why a link did not make the collector, in the reader's language: a link a
// filter rule, a banned tracker or the history rejected, and one folded into a
// link already in the list or lost before it got there. The web words the same
// codes in web/src/components/FilteredLinks.tsx, web/src/lib/rejectionReason.ts
// and web/src/lib/skippedReason.ts.
//
// It imports nothing at run time, so check-narrowing.mjs can load it in node.

import type { TranslationKey } from '../i18n/en';
import type { Task } from './types';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** A rejected link, as the task list carries it. */
export type Held = Task & {
  skipped?: boolean;
  skipReason?: string;
  skipCode?: string;
  skipParams?: Record<string, string>;
  matchedRules?: string[];
  origin?: string;
};

/** One entry of GET /api/collector/skipped, internal/app.SkippedLink. */
export interface SkippedLink {
  url: string;
  kind: string;
  reason: string;
  code?: string;
  params?: Record<string, string>;
  ofId?: string;
  signal?: string;
  at: string;
}

/** A moment as the phone's own locale writes it, or the text as it came. */
export function when(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, { day: 'numeric', month: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit' });
}

const REJECTION: Record<string, TranslationKey> = {
  bannedTracker: 'collector.filtered.reason.bannedTracker',
  filterRule: 'collector.filtered.reason.filterRule',
  filterRuleReason: 'collector.filtered.reason.filterRuleReason',
  downloaded: 'collector.filtered.reason.downloaded',
};

/**
 * heldReason says what caught a rejected link and why. A code from a newer
 * instance keeps the instance's English sentence.
 */
export function heldReason(t: Translate, h: Held): string {
  // Every filter verdict names its rule, so a hold naming none is the
  // tracker ban's.
  const by =
    h.skipCode === 'downloaded'
      ? t('collector.filtered.byHistory')
      : (h.matchedRules ?? []).join(', ') || t('settings.torrents.bannedTrackers');
  const key = h.skipCode ? REJECTION[h.skipCode] : undefined;
  let why = h.skipReason ?? '';
  if (key) {
    const p = h.skipParams ?? {};
    why = t(key, p.finished ? { ...p, finished: when(p.finished) } : p);
  }
  return `${by}: ${why}`;
}

const ORIGIN: Record<string, TranslationKey> = {
  paste: 'collector.filtered.origin.paste',
  crawl: 'collector.filtered.origin.crawl',
  cnl: 'collector.filtered.origin.cnl',
  watch: 'collector.filtered.origin.watch',
  container: 'collector.filtered.origin.container',
  jdownloader: 'collector.filtered.origin.jdownloader',
};

/** Where a link came from, or the instance's own word for a source this build does not know. */
export function originWord(t: Translate, origin?: string): string {
  if (!origin) return '';
  return ORIGIN[origin] ? t(ORIGIN[origin]) : origin;
}

const BY: Record<string, TranslationKey> = {
  downloaded: 'skipped.by.downloaded',
  trackers: 'skipped.by.trackers',
  filter: 'skipped.by.filter',
};

const SIGNAL: Record<string, TranslationKey> = {
  url: 'skipped.signal.url',
  name: 'skipped.signal.name',
  size: 'skipped.signal.size',
  'name+size': 'skipped.signal.nameSize',
  hash: 'skipped.signal.hash',
};

const SKIP_CODE: Record<string, TranslationKey> = {
  sameLink: 'skipped.reason.sameLink',
  sameTorrent: 'skipped.reason.sameTorrent',
  heldLink: 'skipped.reason.heldLink',
  heldTorrent: 'skipped.reason.heldTorrent',
  sameFile: 'skipped.reason.sameFile',
  heldFile: 'skipped.reason.heldFile',
  playlistCapped: 'skipped.reason.playlistCapped',
  playlistDropped: 'skipped.reason.playlistDropped',
};

// The kinds with one sentence each, which quote the tool's own words.
const SKIP_KIND: Record<string, TranslationKey> = {
  container: 'skipped.reason.container',
  nzb: 'skipped.reason.nzb',
  torrent: 'skipped.reason.torrent',
  password: 'skipped.reason.password',
};

/** skippedReason says why a link was not added, or the instance's English for a code it does not know. */
export function skippedReason(t: Translate, s: SkippedLink): string {
  const p = s.params ?? {};
  const code = s.code ? SKIP_CODE[s.code] : undefined;
  if (code) {
    const vars: Record<string, string | number> = { ...p };
    if (p.by) vars.by = BY[p.by] ? t(BY[p.by]) : p.by;
    if (p.signal) vars.signal = SIGNAL[p.signal] ? t(SIGNAL[p.signal]) : p.signal;
    for (const k of ['total', 'kept', 'left', 'n']) if (p[k] !== undefined) vars[k] = Number(p[k]);
    return t(code, vars);
  }
  const kind = SKIP_KIND[s.kind];
  return kind ? t(kind, { detail: s.reason }) : s.reason;
}

/** The links the filter, the ban or the history holds, oldest first. */
export function heldOf(tasks: Task[]): Held[] {
  return (tasks as Held[]).filter((x) => x.skipped).sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1));
}
