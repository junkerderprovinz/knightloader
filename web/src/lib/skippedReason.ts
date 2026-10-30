import type { SkippedLink } from './api';
import type { TranslationKey } from './i18n';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

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

const BY_CODE: Record<string, TranslationKey> = {
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
const BY_KIND: Record<string, TranslationKey> = {
  container: 'skipped.reason.container',
  nzb: 'skipped.reason.nzb',
  torrent: 'skipped.reason.torrent',
  password: 'skipped.reason.password',
};

/**
 * skippedReason says why a link was not added in the reader's language. A
 * code or kind from a newer server that this one does not know falls back on
 * the server's English sentence.
 */
export function skippedReason(t: Translate, s: SkippedLink): string {
  const p = s.params ?? {};
  const code = s.code ? BY_CODE[s.code] : undefined;
  if (code) {
    const vars: Record<string, string | number> = { ...p };
    if (p.by) vars.by = BY[p.by] ? t(BY[p.by]) : p.by;
    if (p.signal) vars.signal = SIGNAL[p.signal] ? t(SIGNAL[p.signal]) : p.signal;
    for (const k of ['total', 'kept', 'left', 'n']) if (p[k] !== undefined) vars[k] = Number(p[k]);
    return t(code, vars);
  }
  const kind = BY_KIND[s.kind];
  return kind ? t(kind, { detail: s.reason }) : s.reason;
}
