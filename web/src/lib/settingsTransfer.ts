// Reading a settings export and working out, key by key, what taking it over
// would actually do to this box.
//
// Pure on purpose: no JSX, no fetch, no React. The preview modal renders what
// this returns and nothing else decides anything, so the two questions that are
// easy to get wrong - "is this row really different" and "is this row
// dangerous" - are answered in one place with a test-shaped signature rather
// than inside a component.

import type { SettingsExportDoc } from './api';
import { same } from '../pages/settings/paths';

/**
 * Which heading a key is drawn under. Nine groups, and the ninth is the point:
 * a key this file has never heard of falls to 'other' and APPEARS, rather than
 * vanishing from a preview that claims to list everything in the file.
 */
export type TransferGroup =
  | 'queue'
  | 'folders'
  | 'archives'
  | 'rules'
  | 'schedule'
  | 'network'
  | 'resolvers'
  | 'look'
  | 'other';

/** The order the groups are drawn in, which is roughly the order somebody
 *  setting up a new box works through them. */
export const TRANSFER_GROUPS: TransferGroup[] = [
  'queue',
  'folders',
  'archives',
  'rules',
  'schedule',
  'network',
  'resolvers',
  'look',
  'other',
];

/**
 * The keys that describe WHICH box this is rather than how it behaves. A mirror
 * of settings.NeverPortable() in Go, and deliberately a mirror rather than
 * something fetched: the server refuses these whatever the browser sends, so
 * this copy has no authority at all. It exists only so the preview can DRAW the
 * row, disabled, with the reason - a control that vanishes teaches nobody, which
 * is ToggleRow's own argument for having a `disabled` state at all.
 *
 * If the two ever disagree, the server wins and the interface is merely showing
 * one row too few or too many. That is why this is safe to duplicate and the
 * validation is not.
 */
export const NEVER_PORTABLE = ['instanceId', 'knownDomains'];

/**
 * The five settings whose value is a folder on disk.
 *
 * They matter because settings.Validate CREATES the folder it claims to be
 * checking (os.MkdirAll, then a write probe). Importing "/mnt/user/downloads"
 * into a container that has no such bind mount does not fail: it succeeds, makes
 * that path inside the container's own writable layer, and every download then
 * lands somewhere that disappears on the next `docker rm`.
 *
 * Written down rather than sniffed out of the value, because "looks like a path"
 * is not a property a string has: a category name, a filename template and a URL
 * all contain slashes. The category table carries folders too, one per row, and
 * is deliberately not covered here - a per-row probe inside a list row is a
 * different piece of interface, and the row is already flagged as replacing the
 * whole table.
 */
export const PATH_KEYS = ['downloadDir', 'workDir', 'watchDir', 'extractTo', 'extractMoveTo'];

/** One key, and everything the preview needs to know about it. */
export interface TransferRow {
  /** Top-level settings key, e.g. "speedLimit". */
  key: string;
  group: TransferGroup;
  /** What this box holds right now, from the settings draft. */
  stored: unknown;
  /** What the file holds. `undefined` when the file does not carry the key at
   *  all, which happens for the identity rows below. */
  incoming: unknown;
  /** The two are the same, so taking it over would change nothing. */
  same: boolean;
  /** Refused outright: in NEVER_PORTABLE. */
  identity: boolean;
  /** In the file, unknown to this build's /api/settings/defaults kinds. */
  unknown: boolean;
  /** Replaces a whole ordered list rather than merging rows into it. */
  wholeList: boolean;
  /** Arrives without its password. */
  secretless: boolean;
  /** A path-valued key naming a folder that is not the stored one. */
  path: boolean;
}

/**
 * parseExport turns a file's text into a document, or throws with a reason
 * somebody can act on.
 *
 * Every refusal names what was wrong. "Invalid file" is how somebody picks the
 * same broken file a second time, which is the argument routes_backup.go already
 * makes for passing the server's own sentence through untouched.
 */
export function parseExport(text: string): SettingsExportDoc {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch (e) {
    throw new Error(e instanceof Error ? e.message : String(e));
  }
  if (!isPlainObject(raw)) throw new Error('the file is not a JSON object');
  const kind = raw.kind;
  if (kind !== 'knightloader-settings') {
    // The archive is the file most likely to be picked here by mistake: it sits
    // in the same downloads folder under a name that differs by one word.
    throw new Error(
      typeof kind === 'string' && kind
        ? `the file says it is "${kind}"`
        : 'the file does not say what it is',
    );
  }
  if (!isPlainObject(raw.settings)) throw new Error('the file carries no settings');
  return raw as unknown as SettingsExportDoc;
}

/**
 * diffRows is the preview, as data.
 *
 * `kinds` is GET /api/settings/defaults' own type table, keyed by dotted path.
 * It is what decides whether a key is one this build has: a struct field appears
 * in it only as its leaves ("reconnect.method", never "reconnect"), so the head
 * of each path up to the first dot is the set of top-level keys - the identical
 * rule knownSettingsKeys() follows on the server, so the preview and the import
 * cannot disagree about what exists.
 *
 * Comparison goes through paths.ts's own `same`, never a second JSON compare:
 * Go's `omitempty` drops an empty list on the way out, so a stored `[]` and a
 * never-set field arrive identically, and every such row would otherwise render
 * as "changed" on a fresh install.
 */
export function diffRows(
  doc: SettingsExportDoc,
  stored: Record<string, unknown>,
  kinds: Record<string, string>,
): TransferRow[] {
  const known = new Set<string>();
  for (const path of Object.keys(kinds)) known.add(path.split('.')[0]);

  const incoming = doc.settings as Record<string, unknown>;
  const secretless = secretlessKeys(doc);

  // The file's own keys, plus the identity keys whether or not the file has
  // them: those rows are drawn to say that they never travel, which is worth
  // more than one row of saved space.
  const keys = new Set<string>([...Object.keys(incoming), ...NEVER_PORTABLE]);

  const rows: TransferRow[] = [];
  for (const key of keys) {
    const value = incoming[key];
    const identity = NEVER_PORTABLE.includes(key);
    rows.push({
      key,
      group: groupOf(key),
      stored: stored[key],
      incoming: value,
      same: same(stored[key], value),
      identity,
      unknown: !identity && !known.has(key),
      // Arrays and the map-shaped keys only. A nested object (reconnect, ytdlp,
      // torrent) is replaced whole as well, but it reads as one setting rather
      // than as a table, and "row for row" printed beside it would be a badge
      // that explains nothing.
      wholeList: Array.isArray(value) || kinds[key] === 'list',
      secretless: secretless.has(key),
      path: PATH_KEYS.includes(key) && typeof value === 'string' && !same(stored[key], value),
    });
  }

  const order = new Map(TRANSFER_GROUPS.map((g, i) => [g, i]));
  return rows.sort((a, b) => {
    const g = (order.get(a.group) ?? 0) - (order.get(b.group) ?? 0);
    return g !== 0 ? g : a.key.localeCompare(b.key);
  });
}

/**
 * secretlessKeys is the client's own reading of which keys arrive without their
 * password, mirroring settings.Secretless in Go.
 *
 * Both copies exist because they answer at different times: this one has to mark
 * the row BEFORE anything is written, and the server's runs after, against the
 * document it actually received. The server's is the one that decides what the
 * user is told afterwards; this one only decides which rows carry a badge.
 *
 * Read out of the document, never out of its `secrets` field, for the same
 * reason the Go side does it: `secrets` is a string in a file somebody can edit.
 */
function secretlessKeys(doc: SettingsExportDoc): Set<string> {
  const out = new Set<string>();
  const s = doc.settings as Record<string, unknown>;

  const reconnect = s.reconnect;
  if (isPlainObject(reconnect)) {
    const password = typeof reconnect.password === 'string' ? reconnect.password : '';
    const username = typeof reconnect.username === 'string' ? reconnect.username.trim() : '';
    // The eight asterisks are internal/reconnect's own redaction placeholder.
    // An empty password with no username is NOT reported: a UPnP reconnect needs
    // neither, and a badge on a correctly configured row teaches people to
    // ignore the badge.
    if (password === '********' || (password === '' && username !== '')) out.add('reconnect');
  }

  const connections = s.connections;
  if (Array.isArray(connections)) {
    for (const row of connections) {
      if (!isPlainObject(row)) continue;
      const password = typeof row.password === 'string' ? row.password : '';
      const username = typeof row.username === 'string' ? row.username.trim() : '';
      // hasPassword is proxycfg's own redaction marker: set true and the
      // password blanked, which is an exact reading of "this row had one".
      if ((row.hasPassword === true && password === '') || (password === '' && username !== '')) {
        out.add('connections');
        break;
      }
    }
  }

  const archives = s.archivePasswords;
  if (Array.isArray(archives) && archives.length === 0) out.add('archivePasswords');

  return out;
}

/**
 * Which group each key belongs to.
 *
 * A table rather than a switch, and one that is allowed to be incomplete: a key
 * added next month has no entry here and lands in 'other', where it is visible
 * and importable. The alternative - a table that must be updated or the key
 * disappears - is the failure mode this whole feature is trying to avoid, one
 * level up.
 */
const GROUPS: Record<string, TransferGroup> = {
  // Queue and speed.
  maxConcurrent: 'queue',
  maxPerHost: 'queue',
  speedLimit: 'queue',
  chunks: 'queue',
  autoStart: 'queue',
  addAtTop: 'queue',
  onDupes: 'queue',
  onOffline: 'queue',
  autoConfirm: 'queue',
  autoConfirmDelay: 'queue',
  maxRetries: 'queue',
  retry: 'queue',
  stallTimeout: 'queue',
  stallRestart: 'queue',
  stallMaxRestarts: 'queue',
  resumeOnStart: 'queue',
  mirrorPolicy: 'queue',
  keepMirrors: 'queue',
  mirrorFailover: 'queue',
  collisionPolicy: 'queue',
  collisionMaxAttempts: 'queue',
  verifyChecksums: 'queue',
  preParserEnabled: 'queue',

  // Folders, and the numbers that are about disks rather than about speed.
  downloadDir: 'folders',
  workDir: 'folders',
  watchDir: 'folders',
  subfolderByPackage: 'folders',
  diskReserve: 'folders',
  diskLowSpace: 'folders',
  diskCriticalSpace: 'folders',
  volumeCap: 'folders',
  volumeCapResetDay: 'folders',
  volumeCapAction: 'folders',
  volumeCapThrottle: 'folders',
  keepFinishedDays: 'folders',
  historyMax: 'folders',
  reclaimTrust: 'folders',

  // Archives.
  extract: 'archives',
  extractTo: 'archives',
  extractSubfolder: 'archives',
  extractMoveTo: 'archives',
  extractCollision: 'archives',
  archiveDisposal: 'archives',
  archivePasswords: 'archives',
  trashRetentionDays: 'archives',
  deleteInfoFiles: 'archives',

  // Rules and the drawers they file into.
  packagizer: 'rules',
  linkFilter: 'rules',
  categories: 'rules',
  hostRules: 'rules',

  // Timetable and quiet mode.
  schedule: 'schedule',
  idleAction: 'schedule',
  quiet: 'schedule',

  // Connections and reconnect, plus the relay this box dials out to.
  connections: 'network',
  reconnect: 'network',
  relayUrl: 'network',
  relayServe: 'network',
  relayMode: 'network',

  // The backends that fetch things, and the services that answer captchas for
  // them.
  resolverOrder: 'resolvers',
  ytdlp: 'resolvers',
  ytdlpPresets: 'resolvers',
  torrent: 'resolvers',
  captchaSolverOrder: 'resolvers',

  // Appearance.
  shape: 'look',
  accent: 'look',
  rainbow: 'look',
  rainbowReactive: 'look',
  rainbowRotate: 'look',
  rainbowSeed: 'look',
  rainbowPalette: 'look',
  navLabels: 'look',
  hideAccountsFromSidebar: 'look',
  hideInstancesFromSidebar: 'look',
  autoUpdateCheck: 'look',
  autoUpdateInstall: 'look',
};

export function groupOf(key: string): TransferGroup {
  return GROUPS[key] ?? 'other';
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

/**
 * describe renders a value small enough to sit in a table cell.
 *
 * Raw JSON, cut off, and NOT a translated summary like "12 entries": the row is
 * already identified by its raw key (Advanced.tsx sets that precedent and
 * explains it - a key path is an identifier), and a summary that says "12
 * entries" on both sides tells the reader nothing about whether taking it over
 * would change anything. The full value is one click away in the Advanced table.
 */
export function describe(value: unknown, limit = 90): string {
  if (value === undefined || value === null) return '';
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  if (text === undefined) return '';
  return text.length > limit ? text.slice(0, limit) + '…' : text;
}
