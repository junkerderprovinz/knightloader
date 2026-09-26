import type { TranslationKey } from '../i18n/en';
import type {
  CaptchaChallenge,
  CaptchaKind,
  CaptchaSolverRefusal,
  CaptchaSolverReport,
  CaptchaWidgetPayload,
} from './types';

// The rules the captcha screen follows, apart from React so that
// check-captcha.mjs can run them as they are. They are the web UI's
// (components/CaptchaModal.tsx and captchaWidgetUrl in lib/api.ts), so a
// challenge is answered the same way from either side.

// Go's encoding/json writes a zero time.Time as year 1 rather than leaving it out.
const GO_ZERO_YEAR = 1;

/** When ch stops being answerable, in ms since the epoch, or null when the
 *  instance could not say. */
export function expiryMs(ch: Pick<CaptchaChallenge, 'expiresAt'>): number | null {
  if (!ch.expiresAt) return null;
  const d = new Date(ch.expiresAt);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return null;
  return d.getTime();
}

/** Whole seconds left at `now`, never below zero, or null without a deadline. */
export function secondsLeft(ch: Pick<CaptchaChallenge, 'expiresAt'>, now: number): number | null {
  const at = expiryMs(ch);
  return at === null ? null : Math.max(0, Math.round((at - now) / 1000));
}

export function fmtCountdown(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}

/** Nearest deadline first, unknown deadlines last, ties by id: the order
 *  internal/captcha.Store.List answers in. */
export function byDeadline(list: CaptchaChallenge[]): CaptchaChallenge[] {
  const byId = (a: CaptchaChallenge, b: CaptchaChallenge) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
  return list.slice().sort((a, b) => {
    const ea = expiryMs(a);
    const eb = expiryMs(b);
    if (ea === null && eb === null) return byId(a, b);
    if (ea === null) return 1;
    if (eb === null) return -1;
    return ea !== eb ? ea - eb : byId(a, b);
  });
}

/**
 * The challenges in `list` that were not waiting at the last look.
 *
 * `before` is null for the first look at an instance, and a first look finds
 * nothing new: what is already waiting there is shown by the screens, not
 * announced.
 */
export function arrivals(before: CaptchaChallenge[] | null, list: CaptchaChallenge[]): CaptchaChallenge[] {
  if (before === null) return [];
  const seen = new Set(before.map((ch) => ch.id));
  return list.filter((ch) => !seen.has(ch.id));
}

/** A challenge that left the list without an answer from this phone. */
export interface CaptchaDeparture {
  challenge: CaptchaChallenge;
  /** The reasons the server's captchaResolved event gives for the same thing:
   *  gone after its deadline or, before it, answered or dropped elsewhere. */
  reason: 'timedOut' | 'resolved';
}

/**
 * The challenges waiting at the last look that are missing from `list`, apart
 * from those in `settledHere`, which this phone answered or skipped and has
 * already said something about. Told apart the way internal/app's
 * pollCaptchasOnce does.
 */
export function departures(
  before: CaptchaChallenge[] | null,
  list: CaptchaChallenge[],
  settledHere: ReadonlySet<string>,
  now: number,
): CaptchaDeparture[] {
  if (before === null) return [];
  const still = new Set(list.map((ch) => ch.id));
  return before
    .filter((ch) => !still.has(ch.id) && !settledHere.has(ch.id))
    .map((ch) => {
      const at = expiryMs(ch);
      return { challenge: ch, reason: at !== null && now > at ? 'timedOut' : 'resolved' };
    });
}

/** What the watch puts in its banner after one look. */
export interface CaptchaNotice {
  kind: 'arrived' | 'timedOut' | 'resolved';
  challenge: CaptchaChallenge;
}

/**
 * The one thing worth a banner after a look at an instance, or null. A new
 * captcha comes first, since it asks for something. While the list is on
 * screen it says so itself and only a card that vanished needs a word.
 */
export function noticeFor(
  before: CaptchaChallenge[] | null,
  list: CaptchaChallenge[],
  settledHere: ReadonlySet<string>,
  now: number,
  listOnScreen: boolean,
): CaptchaNotice | null {
  const fresh = listOnScreen ? [] : arrivals(before, list);
  if (fresh.length > 0) return { kind: 'arrived', challenge: fresh[0] };
  const gone = departures(before, list, settledHere, now);
  return gone.length > 0 ? { kind: gone[0].reason, challenge: gone[0].challenge } : null;
}

/** A tapped point, as fractions of the picture's drawn width and height. */
export interface ClickPoint {
  x: number;
  y: number;
}

/**
 * The answer to a click challenge, in the picture's own pixels.
 *
 * JD takes ClickedPoint {x, y} for one point and MultiClickedPoint
 * {x: [], y: []} for several. The challenge does not say which it wants, so the
 * number of points decides.
 */
export function clickAnswer(points: ClickPoint[], width: number, height: number): string {
  const px = (p: ClickPoint) => ({ x: Math.round(p.x * width), y: Math.round(p.y * height) });
  if (points.length <= 1) return JSON.stringify(points[0] ? px(points[0]) : { x: 0, y: 0 });
  const all = points.map(px);
  return JSON.stringify({ x: all.map((p) => p.x), y: all.map((p) => p.y) });
}

/**
 * Whether the widget page can run `ch`, by the rule captchaWidgetVendor in
 * routes_captcha_widget.go follows: reCAPTCHA and hCaptcha by name, and a
 * challenge without a vendor is left to the page. Any other vendor, such as
 * Cloudflare Turnstile, would get the page's refusal in the web UI as much as
 * here, so its card neither opens a window nor points to the web UI.
 */
export function widgetRuns(ch: CaptchaChallenge): boolean {
  const vendor = ((ch.payload as Partial<CaptchaWidgetPayload> | undefined)?.vendor ?? '').trim().toLowerCase();
  return vendor === '' || vendor === 'recaptcha' || vendor === 'hcaptcha';
}

/**
 * The kinds this phone answers, which it names when it reads the list so the
 * instance holds the paid solvers back for those alone: pictures and clicks
 * everywhere, a widget only on a connection saved by address, the one its page
 * loads from.
 */
export function answeredKinds(relay: boolean): CaptchaKind[] {
  return relay ? ['image', 'click'] : ['image', 'click', 'widget'];
}

/** Whether `ch` can be answered on this phone. */
export function answerableHere(ch: CaptchaChallenge, relay: boolean): boolean {
  return ch.kind === 'widget' ? !relay && widgetRuns(ch) : answeredKinds(relay).includes(ch.kind);
}

/** A catalogue line and what fills it in. */
export interface Phrase {
  key: TranslationKey;
  vars?: Record<string, string>;
}

/**
 * What a card says about the paid solvers, as the web UI's SolverStatus does:
 * the state on the line, and in the bubble why, then every solver that did not
 * deliver. The why speaks to somebody who can answer the challenge, so it is
 * left out where nobody can.
 */
export function solverStatus(
  report: CaptchaSolverReport,
  now: number,
  answerable: boolean,
): { line: Phrase; hint?: Phrase; refusals: Phrase[] } {
  // The solvers stop at one that may hold the task, so there is one at most.
  const taken = report.refusals?.find((r) => r.taken);
  let line: Phrase;
  let hint: Phrase | undefined;
  if (report.state === 'waiting') {
    const left = secondsLeft({ expiresAt: report.until ?? '' }, now) ?? 0;
    line = { key: 'captcha.solverWaiting', vars: { time: fmtCountdown(left) } };
    hint = { key: 'captcha.solverWaitingHint' };
  } else if (report.state === 'solving') {
    line = { key: 'captcha.solverSolving', vars: { solver: report.solver ?? '?' } };
    hint = { key: 'captcha.solverSolvingHint' };
  } else if (taken) {
    line = { key: 'captcha.solverStoppedTaken', vars: { solver: taken.solver } };
    hint = { key: 'captcha.solverNotPassedOn', vars: { solver: taken.solver } };
  } else {
    line = { key: 'captcha.solverStopped' };
  }
  return { line, hint: answerable ? hint : undefined, refusals: (report.refusals ?? []).map(refusal) };
}

function refusal(r: CaptchaSolverRefusal): Phrase {
  if (r.code === 'unsupported') return { key: 'captcha.solverUnsupported', vars: { solver: r.solver } };
  if (r.code === 'noAnswer') return { key: 'captcha.solverNoAnswer', vars: { solver: r.solver } };
  if (r.code === 'failed') return { key: 'captcha.solverFailed', vars: { solver: r.solver } };
  const reason = r.detail ? `${r.detail} (${r.code})` : r.code;
  return { key: r.taken ? 'captcha.solverGaveUp' : 'captcha.solverRefused', vars: { solver: r.solver, reason } };
}

/**
 * The widget page's path below /api, with the rendering data in the query
 * string, since the page looks nothing up by id. `lang` is the app's language,
 * which the vendor's widget then speaks too.
 */
export function widgetPath(ch: CaptchaChallenge, lang: string): string {
  const p = (ch.payload ?? {}) as Partial<CaptchaWidgetPayload>;
  const q = new URLSearchParams();
  if (p.vendor) q.set('vendor', p.vendor);
  if (p.siteKey) q.set('siteKey', p.siteKey);
  if (p.type) q.set('type', p.type);
  if (p.enterprise) q.set('enterprise', '1');
  if (p.v3Action) q.set('v3Action', p.v3Action);
  if (p.secureToken) q.set('secureToken', p.secureToken);
  if (lang) q.set('lang', lang);
  if (ch.host) q.set('host', ch.host);
  if (ch.prompt) q.set('prompt', ch.prompt);
  return `/captcha/${encodeURIComponent(ch.id)}/widget?${q.toString()}`;
}

/** What the widget page reports, as routes_captcha_widget.go documents it. */
export interface WidgetMessage {
  kind: 'ready' | 'solved' | 'expired' | 'error' | 'unsolvable';
  /** The token for "solved", the reason for "error" and "unsolvable". */
  detail: string | null;
}

const WIDGET_KINDS: readonly string[] = ['ready', 'solved', 'expired', 'error', 'unsolvable'];

/** The widget page's message about challenge `id`, or null for anything else
 *  that reaches the app from the page. */
export function widgetMessage(raw: string, id: string): WidgetMessage | null {
  let m: { source?: unknown; id?: unknown; kind?: unknown; detail?: unknown };
  try {
    m = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!m || m.source !== 'knightloader-captcha-widget' || m.id !== id) return null;
  if (typeof m.kind !== 'string' || !WIDGET_KINDS.includes(m.kind)) return null;
  return { kind: m.kind as WidgetMessage['kind'], detail: typeof m.detail === 'string' ? m.detail : null };
}

/** Who kept the widget window from showing the challenge. */
export type WidgetFailure = { by: 'instance'; status: number } | { by: 'network' } | { by: 'vendor'; code: string };

/**
 * Tells apart the instance refusing its own widget page with an HTTP status,
 * a page or vendor script that never arrived, and a code the vendor sent back.
 * `detail` is the page's "error" detail, null when the page itself failed.
 */
export function widgetFailure(httpStatus: number | null, detail: string | null): WidgetFailure {
  if (httpStatus !== null) return { by: 'instance', status: httpStatus };
  if (detail === null || detail === 'script' || detail === 'timeout' || detail === 'network') return { by: 'network' };
  return { by: 'vendor', code: detail };
}

/**
 * The script a WebView runs in the widget page to hand its messages to the app.
 *
 * The page posts to window.parent at its own origin. In a WebView it is the top
 * window, so its parent is itself and the message arrives there; this passes
 * the page's own messages on to React Native and nothing else, not the
 * vendor's frames talking to their script. It runs twice, before the page and
 * after it, because the first run can land before the new document exists, so
 * it installs itself once.
 */
export const WIDGET_BRIDGE = `
if (!window.klWidgetBridge) {
  window.klWidgetBridge = true;
  window.addEventListener('message', function (e) {
    var d = e.data;
    if (e.origin === window.location.origin && d && d.source === 'knightloader-captcha-widget') {
      window.ReactNativeWebView.postMessage(JSON.stringify(d));
    }
  });
}
true;`;
