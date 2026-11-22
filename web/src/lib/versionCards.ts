// The logic behind the Version and Components cards of the Info tile, copied
// from GlimStone's packages/core/src/versionCards.ts: what the update state
// says, which one action it offers, and the order the components stand in.
// The words come from the locale files under versionCard.* and
// componentsCard.*, which carry GlimStone's own tables.

import { GLIMSTONE_REPO, GLIMSTONE_VERSION } from './glimstoneVersion';
import type { TranslationKey } from './locales/en';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/**
 * How KnightLoader gets a new version: `install` fetches it itself, `guide`
 * opens the instructions for a container or a Flatpak, which cannot replace
 * themselves, and `store` leaves it to the store the app came from.
 */
export type UpdateRoute = 'install' | 'guide' | 'store';

/**
 * Where the updates stand. `store` names the store the app came from, and
 * without a name the card points at whichever store that was; `direct` is an
 * app installed from a file the user downloaded.
 */
export type UpdateState =
  | { kind: 'unchecked' }
  | { kind: 'current'; lastCheck?: Date }
  | { kind: 'checking' }
  | { kind: 'available'; version: string }
  | { kind: 'downloading'; percent: number }
  | { kind: 'ready'; version: string }
  | { kind: 'failed'; reason?: string }
  | { kind: 'store'; store?: string }
  | { kind: 'direct' };

export type UpdateTone = 'neutral' | 'ok' | 'warn' | 'fail';

export interface VersionCardText {
  title: string;
  hint: string;
  whatsNew: string;
  count: (n: number) => string;
  check: string;
  checking: string;
  unchecked: string;
  current: string;
  checked: (at: Date, now?: Date) => string;
  available: (version: string) => string;
  failed: string;
  downloading: (percent: number) => string;
  ready: (version: string) => string;
  update: string;
  restart: (version: string) => string;
  guide: string;
  store: (store: string) => string;
  /** The store sentence without a store's name. */
  anyStore: string;
  direct: string;
  auto: string;
  autoHint: string;
  autoCheck: string;
  autoCheckHint: string;
  unreleased: (version: string) => string;
}

export interface ComponentsCardText {
  title: string;
  hint: string;
  auto: string;
  autoHint: string;
  update: string;
  design: string;
  installer: string;
  unreleased: (version: string) => string;
}

/** A time before now in the language's words: "3 minutes ago", "yesterday". */
export function timeAgo(at: Date, lang: string, now: Date = new Date()): string {
  const seconds = Math.round((at.getTime() - now.getTime()) / 1000);
  const format = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' });
  const steps: [Intl.RelativeTimeFormatUnit, number][] = [
    ['second', 60],
    ['minute', 60],
    ['hour', 24],
    ['day', Infinity],
  ];
  let value = seconds;
  for (const [unit, size] of steps) {
    if (Math.abs(value) < size) return format.format(unit === 'second' ? 0 : value, unit);
    value = Math.round(value / size);
  }
  return format.format(value, 'day');
}

/**
 * The Version card's words in the page's language. The hints of the two
 * switches are KnightLoader's own: a container and a Flatpak look only when
 * this page opens, never once a day.
 */
export function versionCardText(t: Translate, lang: string, autoCheckHint: string): VersionCardText {
  return {
    title: t('versionCard.title'),
    hint: t('versionCard.hint'),
    whatsNew: t('versionCard.whatsNew'),
    count: (n) => n.toLocaleString(lang),
    check: t('versionCard.check'),
    checking: t('versionCard.checking'),
    unchecked: t('versionCard.unchecked'),
    current: t('versionCard.current'),
    checked: (at, now) => t('versionCard.checked', { when: timeAgo(at, lang, now) }),
    available: (version) => t('versionCard.available', { version }),
    failed: t('versionCard.failed'),
    downloading: (percent) =>
      t('versionCard.downloading', { percent: (percent / 100).toLocaleString(lang, { style: 'percent' }) }),
    ready: (version) => t('versionCard.ready', { version }),
    update: t('versionCard.update'),
    restart: (version) => t('versionCard.restart', { version }),
    guide: t('versionCard.guide'),
    store: (store) => t('versionCard.store', { store }),
    anyStore: t('versionCard.anyStore'),
    direct: t('versionCard.direct'),
    auto: t('versionCard.auto'),
    autoHint: t('versionCard.autoHint'),
    autoCheck: t('versionCard.autoCheck'),
    autoCheckHint,
    unreleased: (version) => t('versionCard.unreleased', { version }),
  };
}

/** The Components card's words; the switch's hint says what KnightLoader checks and how often. */
export function componentsCardText(t: Translate): ComponentsCardText {
  return {
    title: t('componentsCard.title'),
    hint: t('componentsCard.hint'),
    auto: t('componentsCard.auto'),
    autoHint: t('settings.info.componentsAutoHint'),
    update: t('componentsCard.update'),
    design: t('componentsCard.design'),
    installer: t('componentsCard.installer'),
    unreleased: (version) => t('componentsCard.unreleased', { version }),
  };
}

/** Puts the app's name where a sentence says "{app}". */
export function withApp(sentence: string, app: string): string {
  return sentence.replaceAll('{app}', app);
}

/** The line at the start of the card's last row: what the state says, in which tone, and a quieter note after it. */
export function updateLine(state: UpdateState, text: VersionCardText, now?: Date): { line: string; tone: UpdateTone; note?: string } {
  switch (state.kind) {
    case 'unchecked':
      return { line: text.unchecked, tone: 'neutral' };
    case 'current':
      // Up to date is a finding, so it needs a check behind it.
      if (!state.lastCheck) return { line: text.unchecked, tone: 'neutral' };
      return { line: text.current, tone: 'ok', note: text.checked(state.lastCheck, now) };
    case 'checking':
      return { line: text.checking, tone: 'neutral' };
    case 'available':
      return { line: text.available(state.version), tone: 'warn' };
    case 'downloading':
      return { line: text.downloading(state.percent), tone: 'neutral' };
    case 'ready':
      return { line: text.ready(state.version), tone: 'ok' };
    case 'failed':
      return { line: text.failed, tone: 'fail', note: state.reason };
    case 'store':
      return { line: state.store ? text.store(state.store) : text.anyStore, tone: 'neutral' };
    case 'direct':
      return { line: text.direct, tone: 'neutral' };
  }
}

export type UpdateActionKind = 'update' | 'restart' | 'guide';

/**
 * The one button the Version card shows, or null. There is only something to
 * press once a new version is there, and what it does follows the route.
 */
export function updateAction(route: UpdateRoute, state: UpdateState, text: VersionCardText): { kind: UpdateActionKind; label: string } | null {
  if (state.kind === 'ready' && route === 'install') return { kind: 'restart', label: text.restart(state.version) };
  if (state.kind !== 'available') return null;
  switch (route) {
    case 'install':
      return { kind: 'update', label: text.update };
    case 'guide':
      return { kind: 'guide', label: text.guide };
    case 'store':
      return null;
  }
}

/**
 * Whether the card offers "Check for updates", and whether a check is running.
 * Once a new version is found there is nothing left to look for, and a store
 * app looks for nothing.
 */
export function checkButton(route: UpdateRoute, state: UpdateState): { busy: boolean } | null {
  if (route === 'store') return null;
  if (state.kind === 'unchecked' || state.kind === 'current' || state.kind === 'failed') return { busy: false };
  if (state.kind === 'checking') return { busy: true };
  return null;
}

/** The switch above the last row: an app that installs updates itself, or one that only looks for them. */
export function autoSwitch(route: UpdateRoute, text: VersionCardText): { label: string; hint: string } | null {
  if (route === 'install') return { label: text.auto, hint: text.autoHint };
  if (route === 'guide') return { label: text.autoCheck, hint: text.autoCheckHint };
  return null;
}

/** "What's new · 5" while something is unseen, the bare word once nothing is. */
export function whatsNewLabel(text: VersionCardText, unseen = 0): string {
  return unseen > 0 ? `${text.whatsNew} · ${text.count(unseen)}` : text.whatsNew;
}

/**
 * What a row in the Components card is: one of the app's own parts, a program
 * it brings along, the design language, or the installer of a desktop build.
 * The card sorts by it.
 */
export type ComponentKind = 'part' | 'tool' | 'design' | 'installer';

export interface ComponentRow {
  name: string;
  /** Read from the build or from what runs, never typed in. Empty while it is not known. */
  version: string;
  /** The repository whose newest release the number opens. */
  repoUrl?: string;
  kind?: ComponentKind;
  /** What the component is, in the (i) beside its name. */
  hint?: string;
  /** Where the newest release is told about, for a project that publishes no releases in its repository. */
  releasePage?: string;
  /** A short state before the number, such as "Done" or "Not found". */
  status?: string;
  tone?: UpdateTone;
  /** Behind an (i) after the status: what went wrong where something needs doing. */
  detail?: string;
}

const KIND_ORDER: ComponentKind[] = ['part', 'tool', 'design', 'installer'];

/**
 * The rows in the card's fixed order: the app's own parts as the app lists
 * them, then the programs it brings along by name, then the design language,
 * then the installer.
 */
export function componentRows<R extends ComponentRow>(rows: R[]): R[] {
  const rank = (r: ComponentRow) => KIND_ORDER.indexOf(r.kind ?? 'tool');
  return rows
    .map((row, at) => ({ row, at }))
    .sort((a, b) => {
      const byKind = rank(a.row) - rank(b.row);
      if (byKind) return byKind;
      if ((a.row.kind ?? 'tool') === 'tool') return a.row.name.localeCompare(b.row.name, 'en', { sensitivity: 'base' });
      return a.at - b.at;
    })
    .map(({ row }) => row);
}

/** The design language's row, read from the copied version file, so it is never typed in. */
export function designRow(text: ComponentsCardText): ComponentRow {
  return { name: 'GlimStone', hint: text.design, version: GLIMSTONE_VERSION, repoUrl: GLIMSTONE_REPO, kind: 'design' };
}

/** The tag behind a running version string: for `v8.3.1+feature.59b73a6` it is the part before the build metadata. */
export function releaseTag(version: string): string {
  const bare = (version.split('+')[0] ?? '').trim();
  if (!bare) return '';
  return bare.startsWith('v') ? bare : `v${bare}`;
}

/** A version as the card shows it: `v3.8.0` reads "3.8.0". The "v" names the tag, not the version. */
export function shownVersion(version: string): string {
  const trimmed = version.trim();
  return /^v\d/.test(trimmed) ? trimmed.slice(1) : trimmed;
}

/**
 * The number and where it leads. Every version opens the newest release of
 * its project, so a number never leads to a page a folded patch tag has taken
 * away. A number with dots is a release; a build stamp such as "dev" is one
 * only where no repository says otherwise.
 */
export function numberLink(row: Pick<ComponentRow, 'version' | 'repoUrl' | 'releasePage'>): { shown: string; released: boolean; href?: string } {
  const shown = shownVersion(row.version);
  if (row.releasePage) return { shown, released: true, href: row.releasePage };
  if (!row.repoUrl) return { shown, released: true };
  const released = /^v\d+(\.\d+)+$/.test(releaseTag(row.version));
  return released ? { shown, released, href: `${row.repoUrl}/releases/latest` } : { shown, released };
}
