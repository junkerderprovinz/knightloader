// What each release changed, and what the interface remembers about it
// (GlimStone, "First start and what's new"). After an update a glowing dot
// marks each change of every release since the one the instance ran before, at
// the place itself and on every link that leads there. A click hides a dot, and
// what was seen is kept in the stored interface state, so it follows the user
// between browsers.
import type { TranslationKey } from './i18n';
import { pageId } from '../pages/settings/folded';

/** A change on a page, marked on its element and on the way there. */
interface Placed {
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

/** A change no page shows, such as how downloads behave. Its dot stands in the window's list only. */
interface Unplaced {
  id: string;
  to?: undefined;
  text: TranslationKey;
  names?: Record<string, TranslationKey>;
}

export interface Release {
  /** The release that brings these changes, as x.y.z. */
  version: string;
  changes: (Placed | Unplaced)[];
}

export type Change = (Placed | Unplaced) & { since: string };
export type PlacedChange = Placed & { since: string };

export function placed(change: Change): change is PlacedChange {
  return change.to !== undefined;
}

const ACCOUNTS = ['/accounts', '/settings/accounts'];
const INSTANCES = ['/instances', '/settings/instances'];
const LISTS = ['/downloads', '/collector'];
const INTEGRATIONS = ['/settings/integrations'];

/** Every release that brought something new, newest first. */
export const RELEASES: Release[] = [
  {
    version: '2.1.3',
    changes: [
      {
        id: 'bug-report-speed',
        to: ['/settings/health'],
        text: 'whatsnew.change.bugReportSpeed',
        names: { name: 'settings.diagnostics.download' },
      },
    ],
  },
  {
    version: '2.1.2',
    changes: [
      {
        id: 'quiet-playback',
        to: ['/settings/automation'],
        text: 'whatsnew.change.quietPlayback',
        names: { name: 'settings.quietmode.title', servers: 'settings.playback.serversTitle' },
      },
      {
        id: 'quiet-upload',
        to: ['/settings/automation'],
        text: 'whatsnew.change.quietUpload',
        names: { name: 'settings.quietmode.uploadLimit', quiet: 'settings.quietmode.title' },
      },
      // This one and the next are drawn only while the playback module is on.
      {
        id: 'quiet-unpack',
        to: ['/settings/automation'],
        text: 'whatsnew.change.quietUnpack',
        names: { name: 'settings.playback.holdExtraction' },
      },
      {
        id: 'quiet-waive',
        to: ['/settings/automation'],
        text: 'whatsnew.change.quietWaive',
        names: { name: 'settings.playback.waive' },
      },
    ],
  },
  {
    version: '2.1.1',
    changes: [
      {
        id: 'puid-pgid',
        to: ['/settings/health'],
        text: 'whatsnew.change.puidPgid',
        names: { name: 'settings.owner.identityTitle' },
      },
      { id: 'compose-file', text: 'whatsnew.change.composeFile' },
    ],
  },
  {
    version: '2.1.0',
    changes: [
      { id: 'vpn', to: INTEGRATIONS, text: 'whatsnew.change.vpn', names: { name: 'settings.vpn.title' } },
      { id: 'vpn-wait', text: 'whatsnew.change.vpnWait' },
      // Drawn only once a VPN is set up.
      {
        id: 'vpn-ports',
        to: INTEGRATIONS,
        text: 'whatsnew.change.vpnPorts',
        names: { name: 'settings.vpn.col.port' },
      },
      { id: 'mcp', to: INTEGRATIONS, text: 'whatsnew.change.mcp', names: { name: 'settings.access.mcp.title' } },
      { id: 'accounts-card-add', to: ACCOUNTS, text: 'whatsnew.change.accountsCardAdd' },
      {
        id: 'integrations-page',
        to: ['/settings/integrations'],
        text: 'whatsnew.change.integrationsPage',
        names: { name: 'settings.nav.integrations', accounts: 'settings.nav.accounts' },
      },
      {
        id: 'drive-integrations',
        to: ['/settings/integrations'],
        text: 'whatsnew.change.driveIntegrations',
        names: { name: 'settings.module.debriddrive', page: 'settings.nav.integrations' },
      },
      { id: 'health-diagnostics', to: ['/settings/health'], text: 'whatsnew.change.healthDiagnostics' },
      {
        id: 'proxy-check-moved',
        to: ['/settings/network'],
        text: 'whatsnew.change.proxyCheckMoved',
        names: { name: 'settings.selftest.proxy.title' },
      },
      {
        id: 'advanced-upkeep',
        to: ['/settings/advanced'],
        text: 'whatsnew.change.advancedUpkeep',
        names: { maintenance: 'settings.dbmaint.title', metrics: 'settings.module.metrics' },
      },
      // Drawn on every module's switch, across the settings pages.
      { id: 'module-switches', to: ['/settings'], text: 'whatsnew.change.moduleSwitches' },
      {
        id: 'jd-tool',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.jdTool',
        names: { name: 'settings.resolvers.toolsTitle' },
      },
      {
        id: 'ytdlp-updates',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.ytdlpUpdates',
        names: { name: 'settings.look.updatesTitle', page: 'settings.nav.look' },
      },
      {
        id: 'video-format',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.videoFormat',
        names: { name: 'columns.variant.video', audio: 'settings.resolvers.audioTitle' },
      },
      {
        id: 'track-tiles',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.trackTiles',
        names: { name: 'settings.resolvers.embedTitle' },
      },
      {
        id: 'files-merged',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.filesMerged',
        names: {
          name: 'settings.resolvers.filesCard',
          files: 'settings.resolvers.filesTitle',
          check: 'settings.resolvers.measureTitle',
          live: 'settings.resolvers.liveTitle',
        },
      },
      // Drawn only once a VPN is set up.
      {
        id: 'vpn-routes',
        to: ['/settings/integrations'],
        text: 'whatsnew.change.vpnRoutes',
        names: { name: 'settings.vpn.routesTitle', vpn: 'settings.vpn.title' },
      },
    ],
  },
  {
    version: '2.0.0',
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
        id: 'arrival-splash',
        to: ['/'],
        text: 'whatsnew.change.arrivalSplash',
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
      { id: 'limit-markers', to: ['/', '/downloads'], text: 'whatsnew.change.limitMarkers' },
      { id: 'decimal-units', to: ['/settings/downloads'], text: 'whatsnew.change.decimalUnits' },
      {
        id: 'seed-rate',
        to: ['/downloads'],
        text: 'whatsnew.change.seedRate',
        names: { name: 'downloads.torrentsTitle', column: 'columns.speed' },
      },
      {
        id: 'line-section',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.lineSection',
        names: { name: 'settings.colours', section: 'settings.lines.title' },
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
        id: 'quick-rows',
        to: ['/downloads'],
        text: 'whatsnew.change.quickRows',
        names: { name: 'quick.title' },
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
      { id: 'instance-kind', to: INSTANCES, text: 'whatsnew.change.instanceKind', names: { name: 'instances.title' } },
      {
        id: 'instance-badges',
        to: INSTANCES,
        text: 'whatsnew.change.instanceBadges',
        names: { name: 'instances.title' },
      },
      { id: 'app-logo', to: INSTANCES, text: 'whatsnew.change.appLogo', names: { name: 'instances.title' } },
      { id: 'account-picker', to: ACCOUNTS, text: 'whatsnew.change.accountPicker' },
      { id: 'hoster-logos', to: ACCOUNTS, text: 'whatsnew.change.hosterLogos' },
      { id: 'account-rows', to: ACCOUNTS, text: 'whatsnew.change.accountRows' },
      { id: 'import-gone', to: ACCOUNTS, text: 'whatsnew.change.importGone', names: { name: 'accounts.debrid.title' } },
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
      {
        id: 'priority-names',
        to: ACCOUNTS,
        text: 'whatsnew.change.priorityNames',
        names: { download: 'accounts.routing.priorityTitle', captcha: 'settings.captcha.orderTitle' },
      },
      {
        id: 'priority-start-names',
        to: ACCOUNTS,
        text: 'whatsnew.change.priorityStartNames',
        names: {
          download: 'accounts.routing.priorityTitle',
          link: 'accounts.routing.priorityEntry',
          captcha: 'settings.captcha.orderTitle',
          newCaptcha: 'settings.captcha.orderEntry',
        },
      },
      {
        id: 'priority-accounts',
        to: ACCOUNTS,
        text: 'whatsnew.change.priorityAccounts',
        names: { download: 'accounts.routing.priorityTitle', captcha: 'settings.captcha.orderTitle' },
      },
      {
        id: 'free-first',
        to: ACCOUNTS,
        text: 'whatsnew.change.freeFirst',
        names: {
          name: 'settings.accounts.freeTitle',
          download: 'accounts.routing.priorityTitle',
          captcha: 'settings.captcha.orderTitle',
        },
      },
      { id: 'settings-search', to: ['/settings'], text: 'whatsnew.change.settingsSearch' },
      // Drawn in every window and list that is searched, and listed with the
      // first page that has one in view.
      { id: 'search-box', to: ['/settings', ...LISTS, ...ACCOUNTS], text: 'whatsnew.change.searchBox' },
      {
        id: 'search-button',
        to: [...LISTS, '/settings/advanced', '/settings/diagnostics'],
        text: 'whatsnew.change.searchButton',
        names: {
          search: 'search.toggle',
          downloads: 'nav.downloads',
          collector: 'nav.collector',
          advanced: 'settings.nav.advanced',
          log: 'settings.diagnostics.logTitle',
        },
      },
      { id: 'picker-search-field', to: ACCOUNTS, text: 'whatsnew.change.pickerSearch' },
      { id: 'picker-form', to: ACCOUNTS, text: 'whatsnew.change.pickerForm' },
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
      {
        id: 'look-glyphs',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.lookGlyphs',
        names: { theme: 'settings.theme', shape: 'settings.shape', motion: 'settings.motion.title' },
      },
      {
        id: 'sidebar-card',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.sidebarCard',
        names: { name: 'settings.sidebar.title', accounts: 'nav.accounts', instances: 'nav.instances' },
      },
      // Drawn only in the desktop app, and its sentence says so.
      {
        id: 'window-card',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.windowCard',
        names: { name: 'settings.window.title' },
      },
      // Drawn in the desktop app's window card and in the card a browser tab
      // draws in its place.
      {
        id: 'desktop-card',
        to: ['/settings/appearance'],
        text: 'whatsnew.change.desktopCard',
        names: { awake: 'settings.module.keepawake', name: 'settings.window.title', modules: 'settings.nav.modules' },
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
      {
        id: 'jd-update',
        to: ['/settings/look'],
        text: 'whatsnew.change.jdUpdate',
        names: { name: 'health.part.jd', updates: 'settings.look.updatesTitle' },
      },
      { id: 'row-window', to: ['/settings/rules'], text: 'whatsnew.change.rowWindow' },
      { id: 'pairing-steps', to: ['/settings/pairing'], text: 'whatsnew.change.pairingSteps' },
      { id: 'pairing-group', to: ['/settings/pairing'], text: 'whatsnew.change.pairingGroup' },
      {
        id: 'relay-serve',
        to: ['/settings/pairing'],
        text: 'whatsnew.change.relayServe',
        names: { name: 'relay.serve' },
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
      { id: 'settings-width', to: ['/settings'], text: 'whatsnew.change.settingsWidth' },
      {
        id: 'mirror-policy',
        to: ['/settings/collector'],
        text: 'whatsnew.change.mirrorPolicy',
        names: { name: 'settings.advanced.mirrorsTitle' },
      },
      { id: 'seed-ring', to: ['/settings/torrents'], text: 'whatsnew.change.seedRing' },
      { id: 'rule-switch', to: ['/settings/rules'], text: 'whatsnew.change.ruleSwitch' },
      {
        id: 'merge-files',
        to: ['/settings/resolvers'],
        text: 'whatsnew.change.mergeFiles',
        names: { name: 'settings.resolvers.mergeFiles' },
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
          updates: 'settings.look.updatesTitle',
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
        id: 'news-button',
        to: ['/settings/help'],
        text: 'whatsnew.change.newsButton',
        names: { name: 'settings.about.version', notes: 'settings.look.updatesReleaseNotes', changes: 'whatsnew.changes' },
      },
      // Drawn in the window the Version card's button opens.
      { id: 'news-toggle', to: ['/settings/help'], text: 'whatsnew.change.newsToggle' },
      {
        id: 'manual-button',
        to: ['/settings/help'],
        text: 'whatsnew.change.manualButton',
        names: { name: 'settings.info.manual' },
      },
    ],
  },
];

export const CHANGES: Change[] = RELEASES.flatMap((release) =>
  release.changes.map((change) => ({ ...change, since: release.version })),
);

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

/** releaseOf is a version stamp's release number without its `v`, or '' for a preview or branch build. */
export function releaseOf(version: string): string {
  const v = parse(version);
  return v ? v.join('.') : '';
}

/** newer tells whether release a came after release b. */
export function newer(a: string, b: string): boolean {
  const x = parse(a);
  const y = parse(b);
  if (!x || !y) return false;
  return (x[0] - y[0] || x[1] - y[1] || x[2] - y[2]) > 0;
}

/** What the interface state keeps under the field NEWS_FIELD. */
export interface NewsRecord {
  /** The version the record was last settled for. */
  version: string;
  /** The release the instance ran before the last update, '' when it is not known. */
  from: string;
  /** Every change clicked away, of whichever release. */
  seen: string[];
  /** The window has opened by itself for this version. */
  notes: boolean;
}

/** The record older versions kept: the changes of one release, named by the release before it. */
interface OneRelease {
  version: string;
  after: string;
  seen: string[];
  notes: boolean;
}

export const NEWS_FIELD = 'whatsNew';

function isRecord(v: unknown): v is NewsRecord | OneRelease {
  const r = v as NewsRecord | null;
  return !!r && typeof r === 'object' && typeof r.version === 'string' && Array.isArray(r.seen);
}

/**
 * takeOver reads a record in the form older versions kept. They marked only
 * the release after `after`, so every other change up to the version it ran
 * counts as seen, and of that release the ones clicked away.
 */
function takeOver(old: OneRelease): NewsRecord {
  const marked = [...RELEASES].reverse().find((r) => newer(r.version, old.after))?.version;
  const ran = releaseOf(old.version);
  const clicked = new Set(old.seen);
  const seen = CHANGES.filter(
    (c) => (ran === '' || !newer(c.since, ran)) && (c.since !== marked || clicked.has(c.id)),
  ).map((c) => c.id);
  return { version: old.version, from: '', seen, notes: old.notes === true };
}

/**
 * recordFor is what to keep for the running version given what was kept last.
 * The same version keeps its record. After an update every change of a release
 * newer than the one left is new, what was seen of the releases before stays,
 * and the window is still to open. Nothing kept is a first install, which
 * marks nothing, unless `settled` says the instance was in use before it kept
 * such a record.
 */
export function recordFor(stored: unknown, running: string, settled: boolean): NewsRecord {
  let kept: NewsRecord | null = null;
  if (isRecord(stored)) kept = 'from' in stored && typeof stored.from === 'string' ? stored : takeOver(stored as OneRelease);
  if (kept && kept.version === running) return kept;
  if (!kept && !settled) return { version: running, from: '', seen: CHANGES.map((c) => c.id), notes: true };
  const from = kept ? releaseOf(kept.version) : '';
  // A build without a release number says nothing about what came after it,
  // so what was seen there stays as it was.
  const before = new Set(CHANGES.filter((c) => from === '' || !newer(c.since, from)).map((c) => c.id));
  return { version: running, from, seen: (kept?.seen ?? []).filter((id) => before.has(id)), notes: false };
}

/** changesOf is every change up to the running release; a build without a release number has them all. */
export function changesOf(record: NewsRecord): Change[] {
  const running = releaseOf(record.version);
  return running ? CHANGES.filter((c) => !newer(c.since, running)) : CHANGES;
}

/**
 * releasesShown is what the window shows a card for, newest first: every
 * release since the one left at the last update, any release with a change
 * still unseen, and the running one. The release notes this build carries
 * name the releases that brought no change of their own.
 */
export function releasesShown(record: NewsRecord, unseen: Change[], notes: Record<string, string> = __RELEASE_NOTES__): string[] {
  const running = releaseOf(record.version);
  const shown = new Set(unseen.map((c) => c.since));
  if (running) shown.add(running);
  if (record.from) {
    for (const version of [...Object.keys(notes), ...RELEASES.map((r) => r.version)]) {
      if (newer(version, record.from) && (!running || !newer(version, running))) shown.add(version);
    }
  }
  return [...shown].sort((a, b) => (newer(a, b) ? -1 : newer(b, a) ? 1 : 0));
}

/** routeOf is a route as the app draws it: a folded settings page stands for the page holding its cards. */
export function routeOf(to: string): string {
  const m = /^\/settings\/([^/]+)$/.exec(to);
  return m ? `/settings/${pageId(m[1])}` : to;
}

/** isAt reports whether `here` is one of the addresses a change is drawn at, or a page of a section it is drawn in. */
export function isAt(change: PlacedChange, here: string): boolean {
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

/** bundledNotes is the release notes this build carries for a version, '' when it has none. */
export function bundledNotes(version: string, bundle: Record<string, string> = __RELEASE_NOTES__): string {
  return (bundle[releaseOf(version)] ?? '').trim();
}
