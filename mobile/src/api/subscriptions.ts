import { request } from './client';
import { ApiError, errorText } from './refusal';
import type { ServerConnection } from './types';
import type { TranslationKey } from '../i18n/en';

// An instance's subscriptions as the phone reaches them. The rows are the
// settings key `feeds` (feed.Subscription), but a member of the group may not
// read /api/settings, which carries every path and connection, so the phone
// uses the member routes the instance keeps for subscriptions alone.

export type Kind = 'rss' | 'channel' | 'mediathek';

/** A yt-dlp host preset (ytdlp.HosterPreset): which rows a staged video link
 *  gets and their quality. Audio only is `variants: ["audio"]`. */
export interface MediaChoice {
  variants: string[];
  videoFormat: string;
  quality: string;
  audioFormat: string;
  audioBitrate: string;
}

/** yt-dlp's SponsorBlock categories, cut out or marked as chapters. */
export interface SponsorBlock {
  remove: string[];
  mark: string[];
}

/** What a channel hands over on its first poll instead of only remembering. */
export type Backfill = { mode: 'all' } | { mode: 'count' | 'days'; n: number };

export interface MediathekQuery {
  channel: string;
  topic: string;
  title: string;
  quality: 'hd' | 'normal' | 'low';
  subtitles: boolean;
}

/** One subscription. Every field past the interval holds no opinion at its
 *  zero value, and the address is its identity on the instance. */
export interface Subscription {
  url: string;
  kind?: '' | Kind;
  title?: string;
  /** 0 is the instance's quarter of an hour. */
  intervalMinutes: number;
  titleFilter?: string;
  excludeFilter?: string;
  minMinutes?: number;
  maxMinutes?: number;
  dir?: string;
  /** -3..3, and 0 is a priority, unlike an absent one. */
  priority?: number | null;
  autoStart?: boolean;
  media?: MediaChoice | null;
  naming?: '' | 'channel';
  library?: '' | 'series';
  sponsorBlock?: SponsorBlock | null;
  backfill?: Backfill | null;
  allowDownloaded?: boolean;
  mediathek?: MediathekQuery | null;
}

/** One row of GET /api/feeds. The table lives in the instance's memory, so
 *  without lastPolledAt nothing has been looked at since it started. */
export interface FeedHealth {
  url: string;
  kind?: string;
  title?: string;
  polling: boolean;
  lastPolledAt?: string;
  error?: string;
  seeded: boolean;
  remembered: number;
}

export type SkipReason = 'include' | 'exclude' | 'short' | 'long' | 'downloaded';

export interface TestEntry {
  title: string;
  link: string;
  matches: boolean;
  /** Seconds, 0 where the feed does not say. */
  duration: number;
  /** Why the entry would not be taken, absent when it would. */
  reason?: SkipReason;
}

/** What POST /api/feeds/test found. A feed that could not be read comes back
 *  with `error` set rather than as a refusal. */
export interface FeedTest {
  title: string;
  entries: TestEntry[];
  total: number;
  matched: number;
  error?: string;
}

/** What POST /api/feeds/resolve makes of a pasted link. A YouTube channel
 *  comes as its feed, with the channel itself beside it for older videos. */
export interface Resolved extends Partial<Subscription> {
  channel?: Partial<Subscription>;
}

/** A row refused before anything was sent, with what to say. */
export class SubscriptionFault extends Error {
  key: TranslationKey;

  constructor(key: TranslationKey) {
    super(key);
    this.key = key;
  }
}

export const DEFAULT_INTERVAL_MINUTES = 15;
const MAX_INTERVAL_MINUTES = 10080;

export async function fetchSubscriptions(conn: ServerConnection): Promise<Subscription[]> {
  return (await request<Subscription[] | null>(conn, '/api', '/feeds/subscriptions')) ?? [];
}

async function writeSubscriptions(conn: ServerConnection, rows: Subscription[]): Promise<Subscription[]> {
  const stored = await request<Subscription[] | undefined>(conn, '/api', '/feeds/subscriptions', {
    method: 'PUT',
    body: JSON.stringify(rows),
  });
  return stored ?? rows;
}

/**
 * saveSubscription stores row in place of the one stored under `was`, or as a
 * new one when `was` is null. The list is read again first, so a row added or
 * changed in the web interface while the phone had the list open is kept.
 */
export async function saveSubscription(conn: ServerConnection, was: string | null, row: Subscription): Promise<Subscription[]> {
  const next = tidy(row);
  const rows = await fetchSubscriptions(conn);
  const at = was === null ? -1 : rows.findIndex((r) => r.url.trim() === was.trim());
  // The instance would merge a second row for one address into the first.
  if (rows.some((r, i) => i !== at && r.url.trim() === next.url)) throw new SubscriptionFault('subs.duplicate');
  return writeSubscriptions(conn, at < 0 ? [...rows, next] : rows.map((r, i) => (i === at ? next : r)));
}

export async function removeSubscription(conn: ServerConnection, url: string): Promise<Subscription[]> {
  const rows = await fetchSubscriptions(conn);
  return writeSubscriptions(conn, rows.filter((r) => r.url.trim() !== url.trim()));
}

/** fetchFeedHealth is what the instance knows of each subscription, by address. */
export async function fetchFeedHealth(conn: ServerConnection): Promise<Record<string, FeedHealth>> {
  const list = (await request<FeedHealth[] | null>(conn, '/api', '/feeds')) ?? [];
  return Object.fromEntries(list.map((h) => [h.url, h]));
}

/** testSubscription fetches the row's entries once and stages nothing, so it
 *  works on a row that is not saved yet. */
export async function testSubscription(conn: ServerConnection, row: Subscription): Promise<FeedTest> {
  const found = await request<FeedTest | undefined>(conn, '/api', '/feeds/test', {
    method: 'POST',
    body: JSON.stringify(tidy(row)),
  });
  return found ?? { title: '', entries: [], total: 0, matched: 0 };
}

export async function resolveSubscription(conn: ServerConnection, pasted: string): Promise<Resolved> {
  return request<Resolved>(conn, '/api', '/feeds/resolve', {
    method: 'POST',
    body: JSON.stringify({ url: pasted.trim() }),
  });
}

export function kindOf(row: Pick<Subscription, 'kind'>): Kind {
  return row.kind || 'rss';
}

export function nameOf(row: Pick<Subscription, 'url' | 'title'>): string {
  return row.title?.trim() || row.url;
}

/** clampInterval keeps 0 and cuts anything else into the range feed.Sanitize uses. */
export function clampInterval(v: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole === 0) return 0;
  return Math.min(Math.max(whole, 1), MAX_INTERVAL_MINUTES);
}

const TEXT_FIELDS = ['url', 'title', 'titleFilter', 'excludeFilter', 'dir', 'naming', 'library'] as const;
const OFF_FIELDS = ['title', 'titleFilter', 'excludeFilter', 'dir', 'naming', 'library', 'minMinutes', 'maxMinutes', 'autoStart', 'allowDownloaded', 'media', 'sponsorBlock'] as const;

/**
 * tidy is the row as it is saved: text trimmed, and every field at its zero
 * value left out, so a row nobody customised reads as one from before these
 * fields existed. Fields this app does not know stay, since a save from the
 * phone must not take a newer instance's settings away.
 */
export function tidy(row: Subscription): Subscription {
  const out: Record<string, unknown> = { ...row };
  for (const k of TEXT_FIELDS) {
    if (typeof row[k] === 'string') out[k] = row[k].trim();
  }
  for (const k of OFF_FIELDS) {
    if (!out[k]) delete out[k];
  }
  if (!row.kind) delete out.kind;
  // 0 is a priority, so only absence counts as none.
  if (row.priority === null || row.priority === undefined) delete out.priority;
  const kind = kindOf(row);
  if (kind !== 'channel' || !row.backfill) delete out.backfill;
  if (kind !== 'mediathek' || !row.mediathek) delete out.mediathek;
  // A Mediathek search stages the files themselves, so yt-dlp never sees them.
  if (kind === 'mediathek') {
    delete out.media;
    delete out.sponsorBlock;
  }
  return out as unknown as Subscription;
}

export type FaultField = 'url' | 'titleFilter' | 'excludeFilter' | 'length' | 'backfill' | 'mediathek';

function usableAddress(raw: string): boolean {
  try {
    const u = new URL(raw);
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '';
  } catch {
    return false;
  }
}

// RE2 refuses more than RegExp, so a pattern that passes may still be refused
// at save, but nothing the instance takes is blocked here.
function compiles(pattern: string | undefined): boolean {
  try {
    new RegExp(pattern ?? '');
    return true;
  } catch {
    return false;
  }
}

/** rowFaults asks the questions the instance refuses a whole save over, so
 *  the form can say which box is wrong before anything is sent. */
export function rowFaults(row: Subscription): { field: FaultField; key: TranslationKey }[] {
  const out: { field: FaultField; key: TranslationKey }[] = [];
  const url = row.url.trim();
  if (url === '') out.push({ field: 'url', key: 'subs.urlMissing' });
  else if (!usableAddress(url)) out.push({ field: 'url', key: 'subs.urlInvalid' });
  if (!compiles(row.titleFilter)) out.push({ field: 'titleFilter', key: 'subs.includeInvalid' });
  if (!compiles(row.excludeFilter)) out.push({ field: 'excludeFilter', key: 'subs.excludeInvalid' });
  if (row.minMinutes && row.maxMinutes && row.minMinutes > row.maxMinutes) out.push({ field: 'length', key: 'subs.lengthOrder' });
  const kind = kindOf(row);
  const b = row.backfill;
  if (kind === 'channel' && b && b.mode !== 'all' && !(b.n >= 1)) out.push({ field: 'backfill', key: 'subs.backfillCount' });
  const m = row.mediathek;
  if (kind === 'mediathek' && !(m && [m.channel, m.topic, m.title].some((v) => v.trim() !== ''))) {
    out.push({ field: 'mediathek', key: 'subs.mediathekEmpty' });
  }
  return out;
}

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** skipText says whether a test entry would be taken, and why not. */
export function skipText(t: T, entry: TestEntry, row: Subscription): string {
  switch (entry.reason) {
    case 'include':
      return t('subs.skipInclude');
    case 'exclude':
      return t('subs.skipExclude');
    case 'short':
      return t('subs.skipShort', { n: row.minMinutes ?? 0 });
    case 'long':
      return t('subs.skipLong', { n: row.maxMinutes ?? 0 });
    case 'downloaded':
      return t('subs.skipDownloaded');
  }
  return entry.matches ? t('subs.taken') : t('subs.skipInclude');
}

/** lengthText writes a length in seconds as a player shows it, and nothing
 *  for one the feed does not give. */
export function lengthText(seconds: number): string {
  if (!(seconds > 0)) return '';
  const s = Math.round(seconds);
  const two = (n: number) => String(n).padStart(2, '0');
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h > 0 ? `${h}:${two(m)}:${two(s % 60)}` : `${m}:${two(s % 60)}`;
}

/**
 * saveRefusal words a refused save and names the box it belongs beside. A
 * member is refused a folder outside the download folders with the same code
 * as anything it may not do at all, so with a folder set the refusal points at
 * the folder rather than at the web interface.
 */
export function saveRefusal(t: T, e: unknown, row: Subscription): { field: 'dir' | ''; text: string } {
  if (e instanceof ApiError && e.code === 'ownLoginNeeded' && row.dir?.trim()) {
    return { field: 'dir', text: t('subs.folderRefused') };
  }
  return { field: '', text: errorText(t, e) };
}

export interface Offer {
  key: TranslationKey;
  row: Subscription;
}

function asRow(r: Partial<Subscription>, fallbackUrl: string): Subscription {
  const { channel: _alternative, ...rest } = r as Resolved;
  return { ...rest, url: rest.url || fallbackUrl, intervalMinutes: rest.intervalMinutes ?? 0 };
}

/**
 * offersOf turns a look-up into what the person can subscribe to. A channel
 * offers its feed, which brings new videos only, and the channel itself, which
 * can bring older ones too. A Mediathek search keeps the link that was pasted
 * as its address when the instance names none.
 */
export function offersOf(found: Resolved, pasted: string): Offer[] {
  const url = pasted.trim();
  if (found.kind === 'mediathek') return [{ key: 'subs.offerSearch', row: asRow(found, url) }];
  if (found.channel) {
    return [
      { key: 'subs.offerNew', row: asRow(found, url) },
      { key: 'subs.offerOlder', row: asRow(found.channel, url) },
    ];
  }
  return [{ key: 'subs.offerFeed', row: asRow(found, url) }];
}

/** healthText is a subscription's last check as the list and the editor show
 *  it, and whether it is trouble. */
export function healthText(t: T, lang: string, h: FeedHealth | undefined): { text: string; trouble: boolean } {
  if (h?.error) return { text: t(h.polling ? 'subs.checkFailed' : 'subs.notPolled', { error: h.error }), trouble: true };
  if (!h?.lastPolledAt) return { text: t('subs.notChecked'), trouble: false };
  const when = new Date(h.lastPolledAt).toLocaleString(lang, { dateStyle: 'short', timeStyle: 'short' });
  return { text: t('subs.checked', { when }), trouble: false };
}

/** What a yt-dlp choice starts as once somebody takes it over from the host
 *  preset: the video at its best, nothing else. */
export const DEFAULT_MEDIA: MediaChoice = { variants: ['video'], videoFormat: 'best', quality: 'best', audioFormat: 'best', audioBitrate: '' };

export const QUALITIES = ['best', '2160p', '1440p', '1080p', '720p', '480p', '360p'];
export const VIDEO_FORMATS = ['best', 'mp4 avc1', 'mp4 hevc', 'mp4 av1', 'webm vp9', 'webm av1'];
export const AUDIO_FORMATS = ['best', 'aac', 'alac', 'flac', 'mp3', 'opus', 'vorbis', 'wav'];
export const EXTRAS = ['subtitle', 'thumbnail', 'description'] as const;

export function mediaWhat(m: MediaChoice): 'video' | 'audio' {
  return m.variants.includes('video') || !m.variants.includes('audio') ? 'video' : 'audio';
}

/** withWhat swaps the main row a staged link gets and keeps its extras. */
export function withWhat(m: MediaChoice, what: 'video' | 'audio'): MediaChoice {
  return { ...m, variants: [what, ...m.variants.filter((v) => v !== 'video' && v !== 'audio')] };
}

export function withExtra(m: MediaChoice, extra: (typeof EXTRAS)[number], on: boolean): MediaChoice {
  const rest = m.variants.filter((v) => v !== extra);
  return { ...m, variants: on ? [...rest, extra] : rest };
}

/** yt-dlp's SponsorBlock categories, the two that can only be marked last. */
export const SPONSOR_CATEGORIES = [
  'sponsor',
  'intro',
  'outro',
  'selfpromo',
  'preview',
  'filler',
  'interaction',
  'music_offtopic',
  'hook',
  'poi_highlight',
  'chapter',
] as const;
export type SponsorCategory = (typeof SPONSOR_CATEGORIES)[number];
export const MARK_ONLY: readonly SponsorCategory[] = ['poi_highlight', 'chapter'];

export type SponsorChoice = 'off' | 'mark' | 'remove';

export function sponsorChoice(sb: SponsorBlock, cat: SponsorCategory): SponsorChoice {
  if (sb.remove.includes(cat)) return 'remove';
  return sb.mark.includes(cat) ? 'mark' : 'off';
}

export function withSponsor(sb: SponsorBlock, cat: SponsorCategory, choice: SponsorChoice): SponsorBlock {
  const remove = sb.remove.filter((c) => c !== cat);
  const mark = sb.mark.filter((c) => c !== cat);
  if (choice === 'remove') remove.push(cat);
  if (choice === 'mark') mark.push(cat);
  return { remove, mark };
}
