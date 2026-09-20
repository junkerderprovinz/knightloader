// Wire types and helpers for the event targets: where this instance reports
// when something happens. The only part in lib/api.ts is the Settings field.
//
// The types mirror internal/notify's Go shapes field for field, and the
// constants are copies of the Go constants with the same names.

/** notify's GET, POST and PUT. A closed union, unlike the triggers, because
 *  the server accepts only these three. */
export type TargetMethod = 'GET' | 'POST' | 'PUT';

/** notify.RedactedValue. Stored header values are served as this and sent back
 *  untouched; the server restores them while the row keeps its address. */
export const REDACTED = '********';

/** notify.DefaultAttempts and the ceiling Sanitize clamps to. 0 means no
 *  opinion and is stored as 0. */
export const DEFAULT_ATTEMPTS = 3;
export const MAX_ATTEMPTS = 5;

/** notify.DefaultTimeoutSeconds and its ceiling; 0 reads as for attempts. */
export const DEFAULT_TIMEOUT_SECONDS = 15;
export const MAX_TIMEOUT_SECONDS = 60;

/** notify.MaxResponseBody: how much of the far end's answer the test panel gets. */
export const MAX_RESPONSE_BODY = 8192;

/**
 * One stored row, mirroring notify.Target. Not called EventTarget, which is a
 * DOM global. `triggers` is an open string list, since a newer server may fire
 * an event this build does not know.
 */
export interface EventTargetRow {
  id: string;
  name: string;
  enabled: boolean;
  url: string;
  method?: TargetMethod;
  /** Stored secrets come back as REDACTED and are sent back untouched. A new
   *  address drops the stored values, so they cannot be sent elsewhere
   *  (notify.Merge). */
  headers?: Record<string, string>;
  body?: string;
  triggers?: string[];
  /** 0 is "no opinion", never "off": it resolves to DEFAULT_ATTEMPTS. */
  attempts: number;
  /** 0 is "no opinion": it resolves to DEFAULT_TIMEOUT_SECONDS. */
  timeoutSeconds: number;
}

/** GET /api/eventtargets: the configured rows joined onto live health. */
export interface EventTargetStatus {
  id: string;
  name: string;
  enabled: boolean;
  /** The URL's host only, so a token in the query is not shown. */
  host: string;
  /** RFC3339. Absent means nothing since the server started; the health table
   *  lives in memory. */
  lastAttempt?: string;
  /** RFC3339, absent on the same terms. */
  lastOk?: string;
  lastStatus: number;
  /** The transport failure, redacted by the server. Empty when the far end
   *  answered; its answer is in lastStatus and lastCode. */
  lastError?: string;
  /** A notify.Problem code, translated as settings.eventTargets.problem.<code>. */
  lastCode?: string;
  /** Requests made, retries included. */
  attempts: number;
  sent: number;
  /** Messages that never left the queue because it was full. */
  dropped: number;
}

/** POST /api/eventtargets/test, answered with 200 even when the far end refused. */
export interface EventTargetTest {
  /** The request as it went out, with stored header values masked again. */
  sent: { method: string; url: string; headers: Record<string, string>; body: string };
  status: number;
  statusText: string;
  durationMs: number;
  responseHeaders: Record<string, string>;
  body: string;
  truncated: boolean;
  /** Empty when the far end answered at all, whatever it answered. */
  error?: string;
  code?: string;
}

/** GET /api/eventtargets/placeholders, from the expander's own table. */
export interface Placeholder {
  /** Without the %% wrapper: "task.name". */
  name: string;
  /** "always" | "task" | "package" | "extract" | "reconnect" | "account" | "captcha" */
  scope: string;
  /** Which triggers carry it; empty means every trigger does. */
  triggers?: string[];
}

export class EventTargetApiError extends Error {
  code?: string;
  constructor(message: string, code?: string) {
    super(message);
    this.name = 'EventTargetApiError';
    this.code = code;
  }
}

// decode works like api.ts's json(): a refusal throws the {error,code}
// envelope where the route sends one, and the raw text otherwise.
async function decode<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.text()).trim();
    try {
      const p = JSON.parse(body) as { error?: string; code?: string };
      if (p && typeof p.error === 'string') throw new EventTargetApiError(p.error, p.code);
    } catch (e) {
      if (e instanceof EventTargetApiError) throw e;
      // Plain text, as http.Error writes it.
    }
    throw new EventTargetApiError(body || String(r.status));
  }
  if (r.status === 204) return undefined as T;
  const text = await r.text();
  return text ? (JSON.parse(text) as T) : (undefined as T);
}

export async function fetchEventTargets(): Promise<EventTargetStatus[]> {
  return (await decode<EventTargetStatus[]>(await fetch('/api/eventtargets'))) ?? [];
}

/**
 * fetchPlaceholders returns the placeholder vocabulary, or an empty list on
 * failure so the card stays hidden. A guessed list could offer names that are
 * never filled in.
 */
export async function fetchPlaceholders(): Promise<Placeholder[]> {
  try {
    return (await decode<Placeholder[]>(await fetch('/api/eventtargets/placeholders'))) ?? [];
  } catch {
    return [];
  }
}

/**
 * testEventTarget really sends one made-up event to this row; it is not a dry
 * run. The draft goes over as it stands, masked secrets included, and the
 * server merges the stored values back as it does on save.
 */
export async function testEventTarget(t: EventTargetRow): Promise<EventTargetTest> {
  return decode<EventTargetTest>(
    await fetch('/api/eventtargets/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(t),
    }),
  );
}

/**
 * usableAddress asks notify.Validate's questions: does it parse, is it http or
 * https, does it name a host. A refused row would fail the whole settings
 * save, so it is caught here; the server stays the authority, since
 * JavaScript's URL parser is not Go's. Placeholders are stripped first, as
 * `new URL()` rejects "%%".
 */
export function usableAddress(raw: string): boolean {
  const stripped = stripPlaceholders(raw.trim());
  if (stripped === null) return false;
  try {
    const u = new URL(stripped);
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '';
  } catch {
    return false;
  }
}

/**
 * hostOf returns the address's host, or "" when it does not parse, like
 * notify.Target.Host: ntfy and Gotify put the credential in the query. Saved
 * rows get it from the server; this covers unsaved ones.
 */
export function hostOf(raw: string): string {
  const stripped = stripPlaceholders(raw.trim());
  if (stripped === null) return '';
  try {
    return new URL(stripped).host;
  } catch {
    return '';
  }
}

/**
 * stripPlaceholders replaces every closed %%name%% with one harmless
 * character, or returns null when a marker is unclosed. A walk, like
 * notify.stripPlaceholders, since counting markers cannot tell valid from
 * invalid.
 */
export function stripPlaceholders(s: string): string | null {
  let out = '';
  let rest = s;
  for (;;) {
    const open = rest.indexOf('%%');
    if (open < 0) return out + rest;
    const close = rest.indexOf('%%', open + 2);
    if (close < 0) return null;
    out += rest.slice(0, open) + 'x';
    rest = rest.slice(close + 2);
  }
}

/** Whether every %% in this template is closed. The server refuses an
 *  unclosed one along with the whole settings save. */
export const balancedTemplate = (s: string): boolean => stripPlaceholders(s) !== null;

/**
 * formatHeaders writes the headers one per line as "Name: value", sorted like
 * encoding/json sorts map keys, so a save does not reorder them.
 */
export function formatHeaders(h?: Record<string, string>): string {
  if (!h) return '';
  return Object.keys(h)
    .sort()
    .map((k) => `${k}: ${h[k]}`)
    .join('\n');
}

/**
 * parseHeaders is the inverse of formatHeaders, or null when a line is not a
 * header; dropping it silently would stop a token being sent. It splits on the
 * first colon, since values often contain one, and trims only the name,
 * because spaces are legal in a value.
 */
export function parseHeaders(text: string): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const raw of text.split('\n')) {
    if (raw.trim() === '') continue;
    const at = raw.indexOf(':');
    if (at <= 0) return null;
    const name = raw.slice(0, at).trim();
    if (name === '') return null;
    // The one space after the colon is formatting, not part of the value.
    out[name] = raw.slice(at + 1).replace(/^ /, '');
  }
  return out;
}
