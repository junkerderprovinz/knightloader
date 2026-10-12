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
   * in the order a way there is looked for; "Go there" takes the first. The
   * address of a section stands for every page in it.
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
const LISTS = ['/downloads', '/collector'];

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
      {
        id: 'fill-surface',
        to: ['/'],
        text: 'whatsnew.change.fillSurface',
        names: { name: 'disk.title', volume: 'volume.meter' },
      },
      {
        id: 'transfer-flow',
        to: ['/'],
        text: 'whatsnew.change.transferFlow',
        names: { name: 'overview.flow.title' },
      },
      {
        id: 'volume-controls',
        to: ['/'],
        text: 'whatsnew.change.volumeControls',
        names: { name: 'volume.title' },
      },
      { id: 'piece-map', to: ['/downloads'], text: 'whatsnew.change.pieceMap' },
      { id: 'seeding-line', to: ['/'], text: 'whatsnew.change.seedingLine' },
      { id: 'seeding-always', to: ['/', '/downloads'], text: 'whatsnew.change.seedingAlways' },
      {
        id: 'line-colours',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.lineColours',
        names: { name: 'settings.colours' },
      },
      // Drawn on every page of a narrow window, and listed with the first of them.
      { id: 'phone-bar', to: ['/'], text: 'whatsnew.change.phoneBar' },
      { id: 'page-actions', to: ['/'], text: 'whatsnew.change.pageActions' },
      {
        id: 'queue-state',
        to: ['/downloads'],
        text: 'whatsnew.change.queueState',
        names: { play: 'queue.play', pause: 'queue.pause' },
      },
      {
        id: 'list-bar',
        to: LISTS,
        text: 'whatsnew.change.listBar',
        names: { filter: 'list.filter', sort: 'list.sort', search: 'search.toggle' },
      },
      { id: 'list-rows', to: LISTS, text: 'whatsnew.change.listRows' },
      { id: 'row-pick', to: LISTS, text: 'whatsnew.change.rowPick' },
      {
        id: 'collector-options',
        to: ['/collector'],
        text: 'whatsnew.change.collectorOptions',
        names: { cnl: 'settings.module.cnl', watch: 'intake.clipboardWatch', name: 'collector.options' },
      },
      {
        id: 'intake-glyphs',
        to: ['/collector'],
        text: 'whatsnew.change.intakeGlyphs',
        names: { cnl: 'settings.module.cnl', watch: 'intake.clipboardWatch', intake: 'settings.sectionLinkIntake' },
      },
      {
        id: 'add-links-home',
        to: ['/collector'],
        text: 'whatsnew.change.addLinksHome',
        names: { name: 'collector.add' },
      },
      {
        id: 'collector-filter',
        to: ['/collector'],
        text: 'whatsnew.change.collectorFilter',
        names: { filter: 'list.filter' },
      },
      { id: 'instance-cards', to: INSTANCES, text: 'whatsnew.change.instanceCards' },
      { id: 'net-handover', to: INSTANCES, text: 'whatsnew.change.handover' },
      {
        id: 'accounts-add',
        to: ACCOUNTS,
        text: 'whatsnew.change.accountsAdd',
        names: { name: 'accounts.newAccount' },
      },
      {
        id: 'account-kinds',
        to: ACCOUNTS,
        text: 'whatsnew.change.accountKinds',
        names: { name: 'accounts.newAccount' },
      },
      { id: 'account-picker', to: ACCOUNTS, text: 'whatsnew.change.accountPicker' },
      { id: 'account-rows', to: ACCOUNTS, text: 'whatsnew.change.accountRows' },
      {
        id: 'priority-flow',
        to: ACCOUNTS,
        text: 'whatsnew.change.priorityFlow',
        names: { name: 'accounts.routing.priorityTitle' },
      },
      {
        id: 'priority-tips',
        to: ACCOUNTS,
        text: 'whatsnew.change.priorityTips',
        names: { name: 'accounts.routing.priorityTitle' },
      },
      {
        id: 'captcha-priority',
        to: ACCOUNTS,
        text: 'whatsnew.change.captchaPriority',
        names: { name: 'settings.captcha.orderTitle', download: 'accounts.routing.priorityTitle' },
      },
      { id: 'settings-search', to: ['/settings'], text: 'whatsnew.change.settingsSearch' },
      // Drawn in every window and list that is searched, and listed with the
      // first page that has one in view.
      { id: 'search-box', to: ['/settings', ...LISTS, ...ACCOUNTS], text: 'whatsnew.change.searchBox' },
      {
        id: 'display-card',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.displayCard',
        names: {
          name: 'settings.look.displayTitle',
          lang: 'lang.label',
          theme: 'settings.theme',
          shape: 'settings.shape',
          motion: 'settings.motion.title',
          rows: 'settings.rowHeight.title',
        },
      },
      // Drawn only in the desktop app, and its sentence says so.
      {
        id: 'window-card',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.windowCard',
        names: { name: 'settings.window.title' },
      },
      {
        id: 'updates-card',
        to: ['/settings/look'],
        text: 'whatsnew.change.updatesCard',
        names: { name: 'settings.look.updatesTitle' },
      },
      {
        id: 'built-in',
        to: ['/settings/look'],
        text: 'whatsnew.change.builtIn',
        names: { name: 'settings.look.builtInTitle', updates: 'settings.look.updatesTitle' },
      },
      { id: 'row-window', to: ['/settings/rules'], text: 'whatsnew.change.rowWindow' },
      { id: 'pairing-steps', to: ['/settings/pairing'], text: 'whatsnew.change.pairingSteps' },
      { id: 'pairing-group', to: ['/settings/pairing'], text: 'whatsnew.change.pairingGroup' },
      {
        id: 'pairing-approval',
        to: ['/settings/pairing'],
        text: 'whatsnew.change.pairingApproval',
        names: { name: 'pairing.pendingTitle' },
      },
      { id: 'pairing-own-key', to: ['/settings/pairing'], text: 'whatsnew.change.pairingOwnKey' },
      {
        id: 'words-again',
        to: ['/settings/pairing'],
        text: 'whatsnew.change.wordsAgain',
        names: { name: 'pairing.show' },
      },
      {
        id: 'relay-serve',
        to: ['/settings/pairing'],
        text: 'whatsnew.change.relayServe',
        names: { name: 'relay.serve' },
      },
      {
        id: 'password-reset',
        to: ['/settings/access'],
        text: 'whatsnew.change.passwordReset',
        names: { name: 'auth.forgot' },
      },
      { id: 'app-key', to: ['/settings/access'], text: 'whatsnew.change.appKey', names: { name: 'auth.password' } },
      {
        id: 'trusted-proxies',
        to: ['/settings/access'],
        text: 'whatsnew.change.trustedProxies',
        names: { name: 'settings.access.trustedProxies' },
      },
      { id: 'after-download', to: ['/settings/archives'], text: 'whatsnew.change.afterDownload' },
      {
        id: 'week-band',
        to: ['/settings/schedule'],
        text: 'whatsnew.change.weekBand',
        names: { name: 'settings.module.scheduler' },
      },
      {
        id: 'day-curve',
        to: ['/settings/schedule'],
        text: 'whatsnew.change.dayCurve',
        names: { name: 'settings.module.scheduler' },
      },
      {
        id: 'routes',
        to: ['/settings/connections'],
        text: 'whatsnew.change.routes',
        names: { name: 'settings.module.connections' },
      },
      { id: 'reconnect-scene', to: ['/settings/reconnect'], text: 'whatsnew.change.reconnectScene' },
      {
        id: 'ways-in',
        to: ['/settings/collector'],
        text: 'whatsnew.change.waysIn',
        names: { rainbow: 'settings.rainbow' },
      },
      // Every settings page has such rows, and the crawler's depth is the one shown.
      { id: 'setting-rows', to: ['/settings/collector'], text: 'whatsnew.change.settingRows' },
      {
        id: 'mirror-policy',
        to: ['/settings/collector'],
        text: 'whatsnew.change.mirrorPolicy',
        names: { name: 'settings.advanced.mirrorsTitle' },
      },
      { id: 'seed-ring', to: ['/settings/torrents'], text: 'whatsnew.change.seedRing' },
      { id: 'rule-switch', to: ['/settings/rules'], text: 'whatsnew.change.ruleSwitch' },
      {
        id: 'track-merge',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.trackMerge',
        names: { name: 'settings.resolvers.embedTitle', rainbow: 'settings.rainbow' },
      },
      {
        id: 'merge-files',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.mergeFiles',
        names: { name: 'settings.resolvers.mergeFiles' },
      },
      {
        id: 'video-card',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.videoCard',
        names: { name: 'columns.variant.video' },
      },
      {
        id: 'files-card',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.filesCard',
        names: { name: 'settings.resolvers.filesTitle' },
      },
      {
        id: 'audio-best',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.audioBest',
        names: {
          best: 'settings.resolvers.quality.best',
          name: 'settings.resolvers.audioTitle',
          defaults: 'settings.resolvers.variantDefaults',
        },
      },
      {
        id: 'variant-blocks',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.variantBlocks',
        names: { name: 'settings.resolvers.variantDefaults' },
      },
      { id: 'selftest-pulse', to: ['/settings/health'], text: 'whatsnew.change.selftestPulse' },
      {
        id: 'health-reason',
        to: ['/settings/health'],
        text: 'whatsnew.change.healthReason',
        names: { name: 'settings.health.parts' },
      },
      {
        id: 'jd-state',
        to: ['/settings/health', '/settings/look'],
        text: 'whatsnew.change.jdState',
        names: {
          free: 'settings.accounts.freeTitle',
          parts: 'settings.health.parts',
          health: 'settings.nav.health',
          builtIn: 'settings.look.builtInTitle',
        },
      },
      {
        id: 'health-parts',
        to: ['/settings/health'],
        text: 'whatsnew.change.healthParts',
        names: { name: 'settings.health.parts' },
      },
      { id: 'test-button', to: ['/accounts'], text: 'whatsnew.change.testButton' },
      // Drawn in the captcha window, which a test captcha on the Accounts page opens.
      {
        id: 'captcha-refresh',
        to: ACCOUNTS,
        text: 'whatsnew.change.captchaRefresh',
        names: { name: 'captcha.title', refresh: 'captcha.refresh' },
      },
      {
        id: 'fdroid',
        to: ['/settings/browsertools'],
        text: 'whatsnew.change.fdroid',
        names: { soon: 'settings.browsertools.soon' },
      },
      {
        id: 'info-tile',
        to: ['/settings/help'],
        text: 'whatsnew.change.infoTile',
        names: { name: 'settings.nav.help' },
      },
      {
        id: 'release-notes',
        to: ['/settings/help'],
        text: 'whatsnew.change.releaseNotes',
        names: { name: 'settings.look.updatesReleaseNotes' },
      },
      {
        id: 'manual-button',
        to: ['/settings/help'],
        text: 'whatsnew.change.manualButton',
        names: { name: 'settings.info.manual' },
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

/** isAt reports whether `here` is one of the addresses a change is drawn at, or a page of a section it is drawn in. */
export function isAt(change: Change, here: string): boolean {
  return change.to.some((to) => {
    const route = routeOf(to);
    return route === here || here.startsWith(`${route}/`);
  });
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
