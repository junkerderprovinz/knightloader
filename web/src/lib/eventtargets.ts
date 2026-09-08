// The wire types and helpers for the event targets: where this instance reports
// to when something happens.
//
// Its own module rather than lib/api.ts, for the reason lib/scripts.ts already
// writes down: api.ts is a one-writer-per-wave hot lane, and a feature that
// brings four types, three fetches and half a dozen shared constants would take
// that lane for the whole wave. The one thing that DOES belong in api.ts is the
// field on `Settings`, because the settings document is one shape and this is a
// field of it.
//
// The types below are internal/notify's Go shapes field for field, because the
// package's own JSON tags are the wire format routes_eventtargets.go serialises
// verbatim. Where a number or a string is quoted in a doc comment here, it is
// the Go constant of the same name, not a value chosen on this side: two copies
// of "three attempts" is how a page ends up promising something the server does
// not do.

/** notify.MethodGET / MethodPOST / MethodPUT, which Validate refuses anything
 *  outside of. Closed here as well, unlike `trigger` below, because this one is
 *  not a vocabulary the server grows: it is a deliberately short list, and a
 *  newer build adding DELETE to it would be a decision, not an addition. */
export type TargetMethod = 'GET' | 'POST' | 'PUT';

/** notify.RedactedValue. Every stored header value is served as this and sent
 *  back untouched; the server puts the real one back, and only while the row
 *  still points at the same address. */
export const REDACTED = '********';

/** notify.DefaultAttempts, and the band notify.Sanitize clamps a non-zero value
 *  into. 0 is "no opinion" and is stored as 0, never rewritten to 3. */
export const DEFAULT_ATTEMPTS = 3;
export const MAX_ATTEMPTS = 5;

/** notify.DefaultTimeoutSeconds and the ceiling Sanitize applies. Same reading
 *  of 0 as the attempts above. */
export const DEFAULT_TIMEOUT_SECONDS = 15;
export const MAX_TIMEOUT_SECONDS = 60;

/** notify.MaxResponseBody: how much of the far end's answer the test panel gets. */
export const MAX_RESPONSE_BODY = 8192;

/**
 * One stored row. Mirrors notify.Target field for field.
 *
 * NAMED EventTargetRow AND NOT EventTarget, deliberately: `EventTarget` is a DOM
 * global, and lib/eventLog.ts already made this same call in as many words for
 * its own type ("a project type that shadows one is a type somebody eventually
 * writes by accident meaning the other"). api.ts in particular is full of real
 * DOM types and would be the first place that bit.
 *
 * `triggers` stays an open string list for the reason lib/scripts.ts gives about
 * its own ScriptTrigger: a newer server may fire a twelfth event, and this build
 * must render it as itself rather than fail to compile.
 */
export interface EventTargetRow {
  id: string;
  name: string;
  enabled: boolean;
  url: string;
  method?: TargetMethod;
  /** Values come back as REDACTED for a stored secret and are sent back
   *  untouched. Change the address and the stored value is dropped rather than
   *  carried over - see notify.Merge for why that is a security rule and not a
   *  convenience. */
  headers?: Record<string, string>;
  body?: string;
  triggers?: string[];
  /** 0 is "no opinion", never "off": it resolves to DEFAULT_ATTEMPTS. */
  attempts: number;
  /** 0 is "no opinion": it resolves to DEFAULT_TIMEOUT_SECONDS. */
  timeoutSeconds: number;
}

/** GET /api/eventtargets - the configured rows joined onto live health. */
export interface EventTargetStatus {
  id: string;
  name: string;
  enabled: boolean;
  /** The URL's host only. Never the query, so a token in ?token= is not echoed
   *  to every browser that opens this page. */
  host: string;
  /** RFC3339. ABSENT MEANS "nothing since the server started", not "never": the
   *  health table lives in memory and does not survive a restart. */
  lastAttempt?: string;
  /** RFC3339, absent on the same terms. */
  lastOk?: string;
  lastStatus: number;
  /** The transport failure, already redacted server-side. Empty whenever the far
   *  end answered at all - that answer is in lastStatus and lastCode. */
  lastError?: string;
  /** A notify.Problem code, so the page can say what to try in the reader's own
   *  language: settings.eventTargets.problem.<code>. */
  lastCode?: string;
  /** Requests made, retries included. Beside `sent` it is the retry story. */
  attempts: number;
  sent: number;
  /** Messages that never left the queue because it was full. */
  dropped: number;
}

/** POST /api/eventtargets/test - 200 even when the far end refused. */
export interface EventTargetTest {
  /** The request as it went out, with every stored header value masked again:
   *  this browser was never shown them. */
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

/** GET /api/eventtargets/placeholders - straight from the expander's own table. */
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

/** Mirrors api.ts's json() decoder: the {error,code} envelope where a route
 *  sends one, the raw text otherwise - never JSON.parse'd blind, which is the
 *  exact bug api.ts's own doc comment on json() describes fixing. */
async function decode<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.text()).trim();
    try {
      const p = JSON.parse(body) as { error?: string; code?: string };
      if (p && typeof p.error === 'string') throw new EventTargetApiError(p.error, p.code);
    } catch (e) {
      if (e instanceof EventTargetApiError) throw e;
      // Not JSON at all, which is what http.Error produces.
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
 * The placeholder vocabulary, from the expander itself.
 *
 * Never throws and resolves to an empty list on failure, so the card that shows
 * it simply stays out. A guessed list would be worse than none: it would offer
 * names this build does not fill in, which expand to their own literal text and
 * arrive in somebody's chat room looking like a bug in KnightLoader.
 */
export async function fetchPlaceholders(): Promise<Placeholder[]> {
  try {
    return (await decode<Placeholder[]>(await fetch('/api/eventtargets/placeholders'))) ?? [];
  } catch {
    return [];
  }
}

/**
 * Really sends one made-up event to this row, right now. Not a dry run - see the
 * route's own comment and the copy beside the button.
 *
 * The whole draft goes over, including the header values as they stand, which
 * for a stored secret is the REDACTED placeholder. The server merges the real
 * value back in under the same rule the save path uses.
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
 * Whether the server will take this address, checked with the same questions
 * notify.Validate asks: is it readable, is the scheme http or https, does it
 * name a host.
 *
 * A local copy of a server-side rule is normally a liability, but this one earns
 * itself for the reason Feeds.tsx's own copy does: the refusal it stands in for
 * does not refuse this row, it refuses the ENTIRE settings document, including
 * whatever somebody was editing two pages away. JavaScript's URL parser is not
 * Go's, so this is deliberately the loose half and the server stays the
 * authority.
 *
 * The placeholders come out first, exactly as Validate strips them: "%%" is not
 * a valid percent-escape, so `new URL()` refuses every address that uses one.
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
 * The address's host and nothing else of it, or "" when it will not parse.
 *
 * Mirrors notify.Target.Host, and is used for the same reason: the query is
 * where ntfy and Gotify put their credential, so the collapsed row and the "this
 * leaves the machine" line show the host alone. A saved row gets this from the
 * server; an unsaved one has nowhere else to get it, which is why it exists on
 * this side too.
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
 * Every closed %%name%% replaced by one harmless character, or null when the
 * template is not balanced.
 *
 * A walk and not a count, mirroring notify.stripPlaceholders: "%%a%%%%b%%" has
 * four markers and is fine, "100%% done" has one and is not, and both have the
 * same parity as something valid.
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

/** Whether every %% in this template is closed. An unclosed one is refused by
 *  the server and takes the whole settings document down with it, so it must
 *  never reach the draft. */
export const balancedTemplate = (s: string): boolean => stripPlaceholders(s) !== null;

/**
 * The headers as one line each, "Name: value", which is how a header is written
 * everywhere else a person meets one.
 *
 * Sorted by name, because encoding/json sorts map keys and the row would
 * otherwise reorder itself under the cursor every time a save came back.
 */
export function formatHeaders(h?: Record<string, string>): string {
  if (!h) return '';
  return Object.keys(h)
    .sort()
    .map((k) => `${k}: ${h[k]}`)
    .join('\n');
}

/**
 * The other direction, or null when a line is not a header.
 *
 * Null and not "skip the bad line": a header quietly dropped is a token that
 * quietly stops being sent, and the far end then answers 401 with nothing here
 * to explain it. The row marks itself instead and does not commit.
 *
 * Split on the FIRST colon only - a value routinely contains one ("Bearer x:y",
 * a URL, a time) - and only the name is trimmed. The value keeps its edges for
 * the reason notify.sanitizeHeaders gives: spaces are legal in a header value
 * and eating them produces a refusal from somebody else's server with nothing on
 * this side to explain it.
 */
export function parseHeaders(text: string): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const raw of text.split('\n')) {
    if (raw.trim() === '') continue;
    const at = raw.indexOf(':');
    if (at <= 0) return null;
    const name = raw.slice(0, at).trim();
    if (name === '') return null;
    // One space after the colon is the way a header is written and is not part
    // of the value; anything beyond that is.
    out[name] = raw.slice(at + 1).replace(/^ /, '');
  }
  return out;
}
