// What an update changed, by release, and what the interface remembers about
// it (GlimStone, "First start and what's new"). After an update a glowing dot
// marks each change at the place itself and on every link that leads there. A
// click hides a dot, and what was seen is kept per version in the stored
// interface state, so it follows the user between browsers.
import type { TranslationKey } from './i18n';
import { pageId } from '../pages/settings/folded';

export interface Change {
  /** The value of the `data-new` attribute the changed element carries, and what "seen" remembers. */
  id: string;
  /**
   * Where the element is drawn. Several when a page has more than one address,
   * in the order a way there is looked for; "Go there" takes the first.
   */
  to: string[];
  text: TranslationKey;
  /** Labels the sentence quotes, read from the labels themselves so the two cannot disagree. */
  names?: Record<string, TranslationKey>;
}

export interface Release {
  /**
   * The last release without these changes. The number of the release that
   * brings them is decided when it is tagged, and this one is known while they
   * are written.
   */
  after: string;
  changes: Change[];
}

const ACCOUNTS = ['/accounts', '/settings/accounts'];
const INSTANCES = ['/instances', '/settings/instances'];

/** Newest first. Only the newest release an update passes is marked. */
export const RELEASES: Release[] = [
  {
    after: '1.8.4',
    changes: [
      { id: 'overview-head', to: ['/'], text: 'whatsnew.change.overviewHead', names: { name: 'nav.overview' } },
      {
        id: 'overview-needs',
        to: ['/'],
        text: 'whatsnew.change.overviewNeeds',
        names: { name: 'overview.needs.title' },
      },
      {
        id: 'overview-customize',
        to: ['/'],
        text: 'whatsnew.change.overviewCustomize',
        names: { name: 'overview.customize' },
      },
      { id: 'folder-vessel', to: ['/'], text: 'whatsnew.change.vessel', names: { name: 'disk.title' } },
      { id: 'page-actions', to: ['/downloads'], text: 'whatsnew.change.pageActions' },
      {
        id: 'collector-options',
        to: ['/collector'],
        text: 'whatsnew.change.collectorOptions',
        names: { cnl: 'settings.module.cnl', watch: 'intake.clipboardWatch', name: 'collector.options' },
      },
      { id: 'instance-cards', to: INSTANCES, text: 'whatsnew.change.instanceCards' },
      {
        id: 'accounts-add',
        to: ACCOUNTS,
        text: 'whatsnew.change.accountsAdd',
        names: { name: 'accounts.newAccount' },
      },
      {
        id: 'priority-flow',
        to: ACCOUNTS,
        text: 'whatsnew.change.priorityFlow',
        names: { name: 'accounts.routing.priorityTitle' },
      },
      { id: 'pairing-steps', to: ['/settings/pairing'], text: 'whatsnew.change.pairingSteps' },
      { id: 'pairing-group', to: ['/settings/pairing'], text: 'whatsnew.change.pairingGroup' },
      { id: 'after-download', to: ['/settings/archives'], text: 'whatsnew.change.afterDownload' },
      {
        id: 'week-band',
        to: ['/settings/schedule'],
        text: 'whatsnew.change.weekBand',
        names: { name: 'settings.module.scheduler' },
      },
      { id: 'reconnect-scene', to: ['/settings/reconnect'], text: 'whatsnew.change.reconnectScene' },
      { id: 'seed-ring', to: ['/settings/torrents'], text: 'whatsnew.change.seedRing' },
      {
        id: 'info-tile',
        to: ['/settings/help'],
        text: 'whatsnew.change.infoTile',
        names: { name: 'settings.nav.help' },
      },
    ],
  },
];

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** sentenceOf is what a change says, with the labels it quotes in the reader's language. */
export function sentenceOf(t: Translate, change: Change): string {
  const names = Object.entries(change.names ?? {}).map(([name, key]) => [name, t(key)]);
  return t(change.text, Object.fromEntries(names));
}

type Version = [number, number, number];

function parse(version: string): Version | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)$/.exec(version.split('+')[0].trim());
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

function compare(a: Version, b: Version): number {
  return a[0] - b[0] || a[1] - b[1] || a[2] - b[2];
}

/** releaseOf is a version stamp's release number without its `v`, or '' for a preview or branch build. */
export function releaseOf(version: string): string {
  const v = parse(version);
  return v ? v.join('.') : '';
}

/**
 * releaseSince is the newest release whose changes an update from `from` to
 * `running` passes. A stamp that is no release number is a build of what comes
 * next when it runs, and older than every release when it was left.
 */
export function releaseSince(from: string, running: string, releases: Release[] = RELEASES): Release | undefined {
  const was = parse(from);
  const now = parse(running);
  return releases.find((r) => {
    const after = parse(r.after);
    if (!after) return false;
    return (!was || compare(after, was) >= 0) && (!now || compare(after, now) < 0);
  });
}

/** What the interface state keeps about the running version, under the field NEWS_FIELD. */
export interface NewsRecord {
  version: string;
  /** The `after` of the release whose changes are marked, '' for none. */
  after: string;
  seen: string[];
  /** The release notes have opened by themselves for this version. */
  notes: boolean;
  /** The switch in the list of changes that hides every dot. */
  dots: boolean;
}

export const NEWS_FIELD = 'whatsNew';

function isRecord(v: unknown): v is NewsRecord {
  const r = v as NewsRecord | null;
  return !!r && typeof r === 'object' && typeof r.version === 'string' && Array.isArray(r.seen);
}

/**
 * recordFor is what to keep for the running version given what was kept last.
 * The same version keeps its record. Another one starts over with every dot of
 * its release and the release notes still to open. Nothing kept is a first
 * install, which marks nothing, unless `settled` says the instance was in use
 * before it kept such a record.
 */
export function recordFor(stored: unknown, running: string, settled: boolean): NewsRecord {
  if (isRecord(stored) && stored.version === running) return stored;
  const updated = isRecord(stored) || settled;
  if (!updated) return { version: running, after: '', seen: [], notes: true, dots: true };
  const from = isRecord(stored) ? stored.version : '';
  return { version: running, after: releaseSince(from, running)?.after ?? '', seen: [], notes: false, dots: true };
}

/** changesOf is the list a record marks. */
export function changesOf(record: NewsRecord): Change[] {
  return RELEASES.find((r) => r.after === record.after)?.changes ?? [];
}

/** routeOf is a route as the app draws it: a folded settings page stands for the page holding its cards. */
export function routeOf(to: string): string {
  const m = /^\/settings\/([^/]+)$/.exec(to);
  return m ? `/settings/${pageId(m[1])}` : to;
}

/** isAt reports whether `here` is one of the addresses a change is drawn at. */
export function isAt(change: Change, here: string): boolean {
  return change.to.some((to) => routeOf(to) === here);
}

/**
 * trail lists the addresses whose links lead from `here` towards `to`: the
 * page itself and the section above it. A link to where the reader already is,
 * or to a section they are inside, leads nowhere new and is left out.
 */
export function trail(to: string, here: string): string[] {
  const route = routeOf(to);
  const parts = route.split('/').filter(Boolean);
  const steps = parts.length === 0 ? ['/'] : parts.map((_, i) => `/${parts.slice(0, i + 1).join('/')}`);
  return steps.filter((step) => step !== here && !here.startsWith(`${step}/`));
}

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';

/** releaseNotesPage is where GitHub shows a version's release notes, or every release for a build without a tag. */
export function releaseNotesPage(version: string): string {
  const release = releaseOf(version);
  return release ? `${REPO_URL}/releases/tag/v${release}` : `${REPO_URL}/releases`;
}

/** bundledNotes is the release notes this build carries for a version, '' when they are for another one. */
export function bundledNotes(version: string, bundle: { version: string; text: string } = __RELEASE_NOTES__): string {
  const release = releaseOf(version);
  return release !== '' && release === bundle.version ? bundle.text.trim() : '';
}
