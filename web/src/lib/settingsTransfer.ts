// Reads a settings export and works out, key by key, what importing it would
// change. No React and no fetch: the preview modal only renders what this
// returns.

import type { SettingsExportDoc } from './api';
import { same } from '../pages/settings/paths';

/** Which heading a key is drawn under. A key this file does not know goes to
 *  'other', so the preview still lists it. */
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

/** The order the groups are drawn in, roughly the order of setting up a new box. */
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
 * The keys that identify this box rather than configure it, mirroring
 * settings.NeverPortable(). The server refuses them regardless; this copy only
 * lets the preview show them disabled with the reason.
 */
export const NEVER_PORTABLE = ['instanceId', 'knownDomains'];

/**
 * The settings whose value is a folder. settings.Validate creates a missing
 * folder, so importing "/mnt/user/downloads" into a container without that
 * mount succeeds and downloads land in the container's writable layer. Listed
 * by name, since "looks like a path" is not a property of a string. Category
 * folders are not covered; that row is already flagged as replacing the table.
 */
export const PATH_KEYS = ['downloadDir', 'workDir', 'watchDir', 'extractTo', 'extractMoveTo'];

/** One key, and everything the preview needs to know about it. */
export interface TransferRow {
  /** Top-level settings key, e.g. "speedLimit". */
  key: string;
  group: TransferGroup;
  /** What this box holds right now, from the settings draft. */
  stored: unknown;
  /** What the file holds; `undefined` for an identity key the file lacks. */
  incoming: unknown;
  /** What taking it over writes. It differs from `incoming` where an earlier
   *  build wrote a value this build reads differently (see asImported). */
  arrives: unknown;
  /** Taking it over would change nothing. */
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

/** parseExport turns a file's text into a document, or throws an error that
 *  says what is wrong with it. */
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
    // Most likely the backup archive, which sits beside it under a similar name.
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
 * diffRows is the preview as data. `schema` is GET /api/settings/defaults:
 * `kinds` is its type table by dotted path, and the part before the first dot
 * gives the known top-level keys, the same rule the server's
 * knownSettingsKeys() uses. Values are compared with paths.ts's `same`, which
 * treats an omitted empty list as equal to `[]`.
 */
export function diffRows(
  doc: SettingsExportDoc,
  stored: Record<string, unknown>,
  schema: { values: Record<string, unknown>; kinds: Record<string, string> },
): TransferRow[] {
  const kinds = schema.kinds;
  const known = new Set<string>();
  for (const path of Object.keys(kinds)) known.add(path.split('.')[0]);

  const incoming = doc.settings as Record<string, unknown>;
  const arriving = asImported(incoming, schema.values);
  const secretless = secretlessKeys(doc);

  // The identity keys are always listed, to show that they never travel.
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
      arrives: arriving[key],
      same: same(stored[key], arriving[key]),
      identity,
      unknown: !identity && !known.has(key),
      // Lists and maps only. A nested object is replaced whole too, but it
      // reads as one setting, not a table.
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
 * asImported is the file's settings as the import writes them. The server reads
 * a value an earlier build wrote the way a restart reads that build's own
 * settings file (settings.PortableDoc.Migrated). This mirrors the migrations
 * that change a key such a file carries, migrateStall and migrateAutoStart.
 * `defaults` is /api/settings/defaults' values.
 */
function asImported(s: Record<string, unknown>, defaults: Record<string, unknown>): Record<string, unknown> {
  const out = { ...s };
  // A build without the reconnect shipped with the stall watch off, so its 0
  // was nobody's choice.
  if (s.stallReconnect == null && s.stallTimeout === 0) out.stallTimeout = defaults.stallTimeout;
  // A build without autoConfirm always started a batch that was confirmed.
  if (s.autoConfirm == null && typeof s.autoStart === 'boolean') out.autoStart = true;
  return out;
}

/**
 * secretlessKeys marks the keys that arrive without their password, mirroring
 * settings.Secretless, so the rows can carry a badge before anything is
 * written. It inspects the settings rather than the editable `secrets` field.
 */
function secretlessKeys(doc: SettingsExportDoc): Set<string> {
  const out = new Set<string>();
  const s = doc.settings as Record<string, unknown>;

  const reconnect = s.reconnect;
  if (isPlainObject(reconnect)) {
    const password = typeof reconnect.password === 'string' ? reconnect.password : '';
    const username = typeof reconnect.username === 'string' ? reconnect.username.trim() : '';
    // Eight asterisks is internal/reconnect's redaction placeholder. No
    // password and no username is a valid UPnP setup, not a missing secret.
    if (password === '********' || (password === '' && username !== '')) out.add('reconnect');
  }

  const connections = s.connections;
  if (Array.isArray(connections)) {
    for (const row of connections) {
      if (!isPlainObject(row)) continue;
      const password = typeof row.password === 'string' ? row.password : '';
      const username = typeof row.username === 'string' ? row.username.trim() : '';
      // hasPassword with a blank password is proxycfg's redaction marker.
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

// Which group each key belongs to. A key missing here lands in 'other', still
// visible and importable.
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
  stallReconnect: 'queue',
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
  modulesOff: 'resolvers',
  ytdlp: 'resolvers',
  ytdlpPresets: 'resolvers',
  torrent: 'resolvers',
  captchaSolverOrder: 'resolvers',
  captchaSolverOnlyUnwatched: 'resolvers',
  captchaSolverWait: 'resolvers',

  // Appearance.
  shape: 'look',
  accent: 'look',
  rainbow: 'look',
  rainbowReactive: 'look',
  rainbowRotate: 'look',
  rainbowSeed: 'look',
  rainbowPalette: 'look',
  navLabels: 'look',
  bottomBarLabels: 'look',
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
 * describe renders a value for a table cell as truncated raw JSON. A summary
 * such as "12 entries" on both sides would hide whether anything changes.
 */
export function describe(value: unknown, limit = 90): string {
  if (value === undefined || value === null) return '';
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  if (text === undefined) return '';
  return text.length > limit ? text.slice(0, limit) + '…' : text;
}
