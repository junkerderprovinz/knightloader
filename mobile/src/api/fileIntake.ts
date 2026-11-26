import { ApiError, errorText, request } from './client';
import { base64Decode } from './base64';
import { fmtBytes } from './stats';
import type { TranslationKey } from '../i18n/en';
import type { ServerConnection, Task, TorrentFileView } from './types';

// A .torrent, a link container or an .nzb picked on the phone, sent the way the
// web UI's file drop sends one (web/src/components/FileDrop.tsx). The group
// carries a call's body as JSON, so the file goes as base64 to the routes that
// take it so (internal/api/routes_containers.go, routes_torrents.go).

/**
 * The largest file the app sends. A call crosses the relay in one frame of
 * 16 MiB with its body coded as base64 twice on the way, and the file is base64
 * once more inside the body, which leaves room for 6 MiB.
 */
export const MAX_FILE_BYTES = 6 << 20;

/** The file tree POST /api/torrents/parse/phone answers with. Nothing is staged yet. */
export interface TorrentTree {
  uri: string;
  name: string;
  private: boolean;
  totalSize: number;
  files: TorrentFileView[];
}

/** A staged task as far as the result line reads it. */
type Staged = Task & { skipped?: boolean; skipReason?: string };

interface ContainerAnswer {
  kind: string;
  links?: number;
  created?: Staged[];
  handedTo?: 'jd' | 'usenet';
  expiresIn?: number;
  service?: string;
  own?: boolean;
}

/** What became of a file, kept structured so the line follows a language change. */
export type Outcome =
  | { file: string; kind: 'container-staged'; links: number; created: number; pkg: string }
  | { file: string; kind: 'container-handed'; expiresIn: number }
  | { file: string; kind: 'nzb-sent'; service: string; own: boolean }
  | { file: string; kind: 'torrent-staged'; pkg: string }
  | { file: string; kind: 'torrent-held'; reason: string }
  | { file: string; kind: 'torrent-duplicate' }
  | { file: string; kind: 'failed'; reason: Reason };

/** Why a file was not taken: a sentence of the app's own, or the server's. */
export type Reason = { key: TranslationKey; max?: string } | { error: unknown };

const isNZB = (name: string) => name.toLowerCase().endsWith('.nzb');
const isTorrentName = (name: string) => name.toLowerCase().endsWith('.torrent');

/**
 * looksLikeTorrent judges a file by its name, then as the server's
 * torrent.LooksLikeTorrent does: a bencoded dictionary with an "info" key in
 * its first 64 KiB. Only such a file goes to the torrent routes first, so a
 * container is not sent twice.
 */
export function looksLikeTorrent(name: string, base64: string): boolean {
  if (isTorrentName(name)) return true;
  if (isNZB(name)) return false;
  const head = base64Decode(base64.slice(0, Math.ceil((64 << 10) / 3) * 4));
  if (head[0] !== 0x64) return false;
  const key = [0x34, 0x3a, 0x69, 0x6e, 0x66, 0x6f];
  for (let i = 1; i + key.length <= head.length; i++) {
    if (key.every((b, j) => head[i + j] === b)) return true;
  }
  return false;
}

/** tooBig is the refusal for a file larger than the app sends, or null. */
export function tooBig(size: number | undefined): Reason | null {
  if (size === undefined || size <= MAX_FILE_BYTES) return null;
  return { key: 'addFile.tooBig', max: fmtBytes(MAX_FILE_BYTES) };
}

export async function parseTorrentFile(conn: ServerConnection, base: string, name: string, data: string): Promise<TorrentTree> {
  return request<TorrentTree>(conn, base, '/torrents/parse/phone', {
    method: 'POST',
    body: JSON.stringify({ name, data }),
  });
}

/**
 * stageTorrent turns a parsed uri into a task. Without selectedPaths the
 * torrent file selection chooses, as on the web. Null means the torrent was
 * already in the list.
 */
export async function stageTorrent(
  conn: ServerConnection,
  base: string,
  uri: string,
  pkg: string,
  selectedPaths?: string[],
): Promise<Staged | null> {
  return request<Staged | null>(conn, base, '/torrents', {
    method: 'POST',
    body: JSON.stringify({ uri, package: pkg, selectedPaths }),
  });
}

async function uploadContainer(conn: ServerConnection, base: string, name: string, data: string, pkg: string) {
  return request<ContainerAnswer>(conn, base, '/containers/phone', {
    method: 'POST',
    body: JSON.stringify({ name, data, package: pkg }),
  });
}

/**
 * commitTorrent stages a reviewed tree. A selection left as the file rules
 * ticked it is not sent, so the rules choose again; any change by hand is sent
 * as it is.
 */
export async function commitTorrent(
  conn: ServerConnection,
  base: string,
  file: string,
  tree: TorrentTree,
  selected: boolean[],
  pkg: string,
): Promise<Outcome> {
  const untouched = selected.every((v, i) => v === tree.files[i].selected);
  const paths = untouched ? undefined : tree.files.filter((_, i) => selected[i]).map((f) => f.path);
  try {
    const task = await stageTorrent(conn, base, tree.uri, pkg, paths);
    if (!task) return { file, kind: 'torrent-duplicate' };
    if (task.skipped) return { file, kind: 'torrent-held', reason: task.skipReason ?? '' };
    return { file, kind: 'torrent-staged', pkg: task.package ?? '' };
  } catch (error) {
    return { file, kind: 'failed', reason: { error } };
  }
}

/**
 * sendFile sends one picked file, as the web's file drop does: a torrent with
 * more than one file comes back as its tree for review, anything else as the
 * outcome.
 */
export async function sendFile(
  conn: ServerConnection,
  base: string,
  name: string,
  data: string,
  pkg: string,
): Promise<Outcome | { tree: TorrentTree }> {
  if (looksLikeTorrent(name, data)) {
    try {
      const tree = await parseTorrentFile(conn, base, name, data);
      if (tree.files.length > 1) return { tree };
      return commitTorrent(conn, base, name, tree, tree.files.map(() => true), pkg);
    } catch (error) {
      // A file named .torrent is one, and why it would not parse says more
      // than the container route refusing it.
      if (isTorrentName(name)) return { file: name, kind: 'failed', reason: { error } };
    }
  }
  try {
    const r = await uploadContainer(conn, base, name, data, pkg);
    if (r.handedTo === 'jd') return { file: name, kind: 'container-handed', expiresIn: r.expiresIn ?? 0 };
    if (r.handedTo === 'usenet') return { file: name, kind: 'nzb-sent', service: r.service ?? '', own: !!r.own };
    const created = r.created ?? [];
    // Read from the tasks, since a Packagizer rule may have renamed the package.
    const landed = new Set(created.map((c) => c.package).filter(Boolean));
    return {
      file: name,
      kind: 'container-staged',
      links: r.links ?? created.length,
      created: created.length,
      pkg: landed.size === 1 ? [...landed][0] : '',
    };
  } catch (error) {
    return { file: name, kind: 'failed', reason: { error } };
  }
}

// The upload refusals the server sends with a code, worded as the web words them.
const REFUSALS: Partial<Record<string, TranslationKey>> = {
  noJD: 'addFile.noJD',
  noUsenet: 'addFile.noUsenet',
};

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** reasonText is why a file was not taken, in this app's words where it has them. */
export function reasonText(t: T, reason: Reason): string {
  if ('key' in reason) return t(reason.key, reason.max ? { max: reason.max } : undefined);
  const e = reason.error;
  if (e instanceof ApiError) {
    const key = e.code ? REFUSALS[e.code] : undefined;
    if (key) return t(key);
    // An instance from before these routes has none to answer with.
    if (e.status === 404 || e.status === 405) return t('addFile.tooOld');
  }
  return errorText(t, e);
}

/** outcomeText is the line a file's outcome shows, and the tone it is shown in. */
export function outcomeText(t: T, o: Outcome): { text: string; tone: 'ok' | 'muted' | 'warn' | 'fail' } {
  switch (o.kind) {
    case 'failed':
      return { text: t('addFile.failed', { file: o.file, reason: reasonText(t, o.reason) }), tone: 'fail' };
    case 'nzb-sent':
      return {
        text: o.own ? t('addFile.usenetOwn', { file: o.file }) : t('addFile.usenet', { file: o.file, service: o.service }),
        tone: 'muted',
      };
    case 'container-handed':
      return { text: t('addFile.handed', { file: o.file, n: o.expiresIn }), tone: 'muted' };
    case 'torrent-duplicate':
      return { text: t('addFile.torrentDuplicate', { file: o.file }), tone: 'muted' };
    case 'torrent-held':
      return { text: t('addFile.torrentHeld', { file: o.file, reason: o.reason }), tone: 'warn' };
    case 'torrent-staged':
      return {
        text: o.pkg ? t('addFile.torrentStagedIn', { file: o.file, pkg: o.pkg }) : t('addFile.torrentStaged', { file: o.file }),
        tone: 'ok',
      };
    case 'container-staged': {
      if (o.created === 0) return { text: t('addFile.allKnown', { file: o.file, n: o.links }), tone: 'muted' };
      const staged = o.pkg
        ? t('addFile.stagedIn', { n: o.created, file: o.file, pkg: o.pkg })
        : t('addFile.staged', { n: o.created, file: o.file });
      const known = o.links - o.created;
      return { text: known > 0 ? `${staged} ${t('addFile.alsoKnown', { n: known })}` : staged, tone: 'ok' };
    }
  }
}
