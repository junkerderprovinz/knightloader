// Wire types and helpers for the event targets: the services this instance
// tells when something happens. The only part in lib/api.ts is the Settings
// field. The types mirror parleyport kit's notify.Channel.

/** The kinds a channel can be. healthchecks and unraid exist in the kit but are not offered here. */
export type ChannelKind =
  | 'webhook'
  | 'discord'
  | 'slack'
  | 'gotify'
  | 'ntfy'
  | 'matrix'
  | 'smtp'
  | 'apprise'
  | 'healthchecks'
  | 'unraid';

/** When a channel sends: never, on failures, warnings and critical events, or always. */
export type ChannelOn = 'never' | 'failure' | 'always';

export type WebhookMethod = 'GET' | 'POST' | 'PUT';
export type SmtpTls = 'starttls' | 'tls' | 'none';

/** notify.RedactedValue: a stored secret is served as this and sent back untouched. */
export const REDACTED = '********';

export const DEFAULT_ATTEMPTS = 3;
export const MAX_ATTEMPTS = 5;
export const DEFAULT_TIMEOUT_SECONDS = 15;
export const MAX_TIMEOUT_SECONDS = 60;

/** One stored channel. Only the block named by kind is read. */
export interface Channel {
  id: string;
  kind: ChannelKind;
  name?: string;
  enabled: boolean;
  /** Empty means failure. */
  on?: ChannelOn;
  /** Event kinds this channel takes; empty means every one that `on` lets through. */
  events?: string[];
  /** 0 or absent means the default. */
  attempts?: number;
  timeoutSeconds?: number;
  webhook?: { url: string; method?: WebhookMethod; headers?: Record<string, string>; body?: string };
  discord?: { url: string };
  slack?: { url: string };
  gotify?: { url: string; token?: string; priorityFailure?: number; prioritySuccess?: number };
  ntfy?: { url: string; token?: string };
  matrix?: { homeserver: string; room: string; token: string };
  smtp?: { host: string; port?: number; username?: string; password?: string; from: string; to: string; tls?: SmtpTls };
  apprise?: { url: string; urls?: string[]; tags?: string };
  healthchecks?: { url?: string; byKey?: Record<string, string> };
}

/** GET /api/eventtargets: the configured channels joined onto live health. */
export interface EventTargetStatus {
  id: string;
  name: string;
  kind: ChannelKind;
  enabled: boolean;
  /** RFC3339. Absent means nothing since the server started. */
  lastAttempt?: string;
  lastOk?: string;
  lastStatus: number;
  /** The failure in English, free of secrets. */
  lastError?: string;
  /** A kit result code, translated as settings.eventTargets.problem.<code>. */
  lastCode?: string;
  attempts: number;
  sent: number;
  dropped: number;
}

/** POST /api/eventtargets/test, answered with 200 even when the far end refused. */
export interface EventTargetTest {
  ok: boolean;
  attempts: number;
  status: number;
  code?: string;
  error?: string;
  durationMs: number;
}

/** GET /api/eventtargets/placeholders. */
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

async function decode<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.text()).trim();
    try {
      const p = JSON.parse(body) as { error?: string; code?: string };
      if (p && typeof p.error === 'string') throw new EventTargetApiError(p.error, p.code);
    } catch (e) {
      if (e instanceof EventTargetApiError) throw e;
    }
    throw new EventTargetApiError(body || String(r.status));
  }
  const text = await r.text();
  return text ? (JSON.parse(text) as T) : (undefined as T);
}

export async function fetchEventTargets(): Promise<EventTargetStatus[]> {
  return (await decode<EventTargetStatus[]>(await fetch('/api/eventtargets'))) ?? [];
}

/** fetchPlaceholders returns the placeholder vocabulary, or an empty list on failure. */
export async function fetchPlaceholders(): Promise<Placeholder[]> {
  try {
    return (await decode<Placeholder[]>(await fetch('/api/eventtargets/placeholders'))) ?? [];
  } catch {
    return [];
  }
}

/**
 * testEventTarget really sends one made-up event to this channel. The draft
 * goes over with its masked secrets, and the server puts the stored ones back
 * while the channel keeps its destination.
 */
export async function testEventTarget(c: Channel): Promise<EventTargetTest> {
  return decode<EventTargetTest>(
    await fetch('/api/eventtargets/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...c, enabled: true }),
    }),
  );
}

/** newChannel is a channel of a kind with nothing filled in, switched off. */
export function newChannel(id: string, kind: ChannelKind, name: string): Channel {
  const c: Channel = { id, kind, name, enabled: false, on: 'failure' };
  switch (kind) {
    case 'webhook':
      c.webhook = { url: '', method: 'POST' };
      break;
    case 'matrix':
      c.matrix = { homeserver: '', room: '', token: '' };
      break;
    case 'smtp':
      c.smtp = { host: '', from: '', to: '', tls: 'starttls' };
      break;
    case 'apprise':
      c.apprise = { url: '' };
      break;
    case 'healthchecks':
      c.healthchecks = {};
      break;
    case 'unraid':
      break;
    default:
      c[kind] = { url: '' };
  }
  return c;
}

/** addressOf is the address a channel sends to, for the row and the host line. */
export function addressOf(c: Channel): string {
  switch (c.kind) {
    case 'matrix':
      return c.matrix?.homeserver ?? '';
    case 'smtp':
      return c.smtp?.host ?? '';
    case 'unraid':
      return '';
    case 'healthchecks':
      return c.healthchecks?.url ?? '';
    default:
      return c[c.kind]?.url ?? '';
  }
}

/** nextId returns the lowest unused positive number, as the kit's migration does. */
export function nextId(channels: readonly Channel[]): string {
  const taken = new Set(channels.map((c) => c.id));
  for (let n = 1; ; n++) {
    if (!taken.has(String(n))) return String(n);
  }
}

/**
 * usableAddress asks the kit's question of an address: does it parse as http
 * or https with a host. Placeholders are stripped first, as `new URL()`
 * rejects "%%".
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

/** hostOf returns the address's host, or "" when it does not parse. */
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
 * character, or returns null when a marker is unclosed.
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

/** Whether every %% in this template is closed. */
export const balancedTemplate = (s: string): boolean => stripPlaceholders(s) !== null;

/** formatHeaders writes the headers one per line as "Name: value", sorted. */
export function formatHeaders(h?: Record<string, string>): string {
  if (!h) return '';
  return Object.keys(h)
    .sort()
    .map((k) => `${k}: ${h[k]}`)
    .join('\n');
}

/**
 * parseHeaders is the inverse of formatHeaders, or null when a line is not a
 * header, since dropping it would quietly stop a token being sent.
 */
export function parseHeaders(text: string): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const raw of text.split('\n')) {
    if (raw.trim() === '') continue;
    const at = raw.indexOf(':');
    if (at <= 0) return null;
    const name = raw.slice(0, at).trim();
    if (name === '') return null;
    out[name] = raw.slice(at + 1).replace(/^ /, '');
  }
  return out;
}
