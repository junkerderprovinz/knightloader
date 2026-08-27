import { decodeBody, encodeBody } from './base64';
import { openCall, openResult, sealCall, sealResult } from './relayFrame';

// This app's own client for internal/relay's wire protocol - the second way
// it can reach an instance, for the case the direct one cannot cover at all:
// every instance behind a NAT with no port forwarding and no reverse proxy,
// so nothing on the phone's network can open a connection to any of them.
// Both ends dial OUT to the relay instead, and it forwards between them.
//
// It speaks the protocol from internal/relay/protocol.go directly. That is
// deliberate on the Go side, not a coincidence this exploits: the protocol is
// one JSON frame per message specifically so a client that can only speak
// plain WebSocket text - this one - needs a JSON parser and nothing else. Keep
// the frame shapes below in sync with that file; it is the source of truth.
//
// What this is NOT: a way to stream tasks. The relay carries request/response
// frames, not a tunnelled WebSocket, so a relay connection polls for its task
// list exactly as a federation peer already does over the proxy route (see
// pollTasks in api/client.ts). Same reason, same fallback.

/** Mirrors relay.Announce. */
export interface RelaySibling {
  instanceId: string;
  name: string;
  deployment: string;
  /** True for a connection that uses the relay without being an instance -
   *  another phone. Never a target worth offering. */
  client?: boolean;
}

export interface RelayProxyResult {
  status: number;
  /** The response body as text; '' when the answer carried none. */
  body: string;
}

// Mirrors the Go client's own constants, so the two behave alike on a flaky
// link rather than each having its own opinion about it.
const MIN_BACKOFF_MS = 1000;
const MAX_BACKOFF_MS = 60_000;
const STABLE_SESSION_MS = 30_000;
const PROXY_TIMEOUT_MS = 15_000;

// Frame type discriminators - relay.TypeHello and friends.
const T_HELLO = 'hello';
const T_ANNOUNCE = 'announce';
const T_PRESENCE = 'presence';
const T_PROXY_REQUEST = 'proxy-request';
const T_PROXY_RESPONSE = 'proxy-response';

/**
 * connectURL mirrors internal/relay/client.go's function of the same name:
 * people write down the https:// address they gave their reverse proxy, not a
 * WebSocket URL, so both scheme families are accepted and the connect path is
 * appended only when the address does not already carry one (a relay behind a
 * proxy can be mounted anywhere, and only the person configuring it knows).
 *
 * Exported for the connect screen, which validates what was typed before it
 * ever opens a socket with it.
 */
export function connectURL(raw: string): string | null {
  const trimmed = raw.trim();
  const m = trimmed.match(/^(https?|wss?):\/\/([^/?#]+)([^?#]*)/i);
  if (!m) return null;
  const scheme = { http: 'ws', https: 'wss', ws: 'ws', wss: 'wss' }[m[1].toLowerCase()];
  const host = m[2];
  const path = m[3] === '' || m[3] === '/' ? '/relay/connect' : m[3].replace(/\/$/, '');
  return `${scheme}://${host}${path}`;
}

interface Pending {
  resolve: (r: RelayProxyResult) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export interface RelayClientOptions {
  url: string;
  key: string;
  /**
   * The 32-byte key every proxy frame is sealed under - seedphrase.ts's
   * deriveFrameKey, mirroring relay.DeriveFrameKey.
   *
   * Separate from `key` and it has to stay that way: `key` is what this
   * client hands the relay in its hello frame, so a frame key equal to it
   * would be a key the relay already holds. See relayFrame.ts.
   */
  frameKey: Uint8Array;
  /** This device's stable id on the relay. See storage/relayIdentity.ts. */
  selfId: string;
  /** What siblings would call this device, if they listed it - they do not. */
  selfName: string;
}

// How long a call waits for a connection that is still coming up before
// giving up on it. Without this the FIRST call after launch always fails -
// the socket, its hello and the relay's answer all have to complete first,
// and every screen fires its first request immediately on mount.
const CONNECT_WAIT_MS = 6000;

export class RelayClient {
  private readonly opts: RelayClientOptions;
  private socket: WebSocket | null = null;
  private open = false;
  private closed = false;
  private backoff = MIN_BACKOFF_MS;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private openedAt = 0;
  private sibs: RelaySibling[] = [];
  private pending = new Map<string, Pending>();
  private seq = 0;
  // A set, not a single callback: one client is shared by every connection
  // through the same relay (see relayClientFor), and the connect screen
  // watches the same client a saved connection is already using. A lone
  // onChange would silently belong to whichever caller registered last.
  private listeners = new Set<() => void>();

  constructor(opts: RelayClientOptions) {
    this.opts = opts;
  }

  /** Returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private emitChange(): void {
    for (const fn of this.listeners) fn();
  }

  start(): void {
    // A pending retry counts as started: without this check, calling start()
    // during a backoff wait would dial immediately AND leave the timer to
    // dial again, leaving two sockets racing under one identity - which the
    // relay resolves by dropping one of them, forever.
    if (this.closed || this.socket || this.retryTimer) return;
    this.connect();
  }

  close(): void {
    this.closed = true;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.failAllPending('the relay connection was closed');
    this.open = false;
    this.sibs = [];
    const s = this.socket;
    this.socket = null;
    s?.close();
  }

  isConnected(): boolean {
    return this.open;
  }

  /** The instances visible through the relay right now, phones excluded. */
  siblings(): RelaySibling[] {
    return this.sibs.filter((s) => !s.client).slice().sort((a, b) => a.instanceId.localeCompare(b.instanceId));
  }

  /**
   * proxy calls one sibling's REST API through the relay, the same
   * (method, path, body) -> (status, body) shape a direct fetch has.
   *
   * A rejection means the call never produced an answer - the relay is not
   * connected, the target is not, or nobody replied in time. An answer the
   * target actually produced resolves, however bad its status: "your instance
   * said no" and "your instance is gone" are different outcomes and a caller
   * needs to tell them apart, exactly as the Go client's Proxy does.
   */
  async proxy(target: string, method: string, path: string, body?: string, authorization?: string): Promise<RelayProxyResult> {
    // Give a connection that is still coming up a moment, rather than failing
    // a call that would have worked a second later. Every screen fires its
    // first request the instant it mounts, which on a cold start is while the
    // socket, the hello and the relay's reply are all still in flight.
    if (!this.open) await this.waitForConnection();
    return new Promise<RelayProxyResult>((resolve, reject) => {
      if (!this.open || !this.socket) {
        reject(new Error('relay: not connected'));
        return;
      }
      // Unique per client instance and never reused, which is all the relay
      // needs: it matches a response to a request only within this one
      // connection's pending set.
      const requestId = `${Date.now().toString(36)}-${(this.seq++).toString(36)}`;
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        reject(new Error('relay: the instance did not answer in time'));
      }, PROXY_TIMEOUT_MS);
      this.pending.set(requestId, { resolve, reject, timer });

      try {
        // Everything except the two routing fields goes inside the seal, so
        // the relay carrying this frame sees which instance it is for and
        // nothing about what is being asked - including, crucially, the
        // bearer token below, which is the one thing here that keeps working
        // for whoever reads it. See relayFrame.ts.
        this.send(T_PROXY_REQUEST, {
          requestId,
          target,
          sealed: sealCall(this.opts.frameKey, requestId, target, {
            method,
            path,
            ...(body ? { body: encodeBody(body) } : {}),
            ...(authorization ? { authorization } : {}),
          }),
        });
      } catch (err) {
        clearTimeout(timer);
        this.pending.delete(requestId);
        reject(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }

  /**
   * Resolves as soon as the connection is up, or after CONNECT_WAIT_MS
   * whatever happened - the caller re-checks isConnected afterwards, so a
   * timeout here just means "carry on and fail properly".
   */
  private waitForConnection(): Promise<void> {
    this.start(); // a no-op if already connected or already retrying
    return new Promise<void>((resolve) => {
      let done = false;
      const finish = () => {
        if (done) return;
        done = true;
        unsubscribe();
        clearTimeout(timer);
        resolve();
      };
      const unsubscribe = this.subscribe(() => {
        if (this.open) finish();
      });
      const timer = setTimeout(finish, CONNECT_WAIT_MS);
      // The connection may have come up between the isConnected check and
      // the subscribe above.
      if (this.open) finish();
    });
  }

  private send(type: string, data: unknown): void {
    if (!this.socket) throw new Error('relay: not connected');
    this.socket.send(JSON.stringify({ type, data }));
  }

  private connect(): void {
    if (this.closed) return;
    const url = connectURL(this.opts.url);
    if (!url) {
      // A malformed address cannot be retried into working, but this client
      // is started from saved settings that a UI already validated, so it
      // stays quiet rather than throwing into whatever called start().
      return;
    }
    const socket = new WebSocket(url);
    this.socket = socket;

    socket.onopen = () => {
      if (this.socket !== socket) return;
      this.openedAt = Date.now();
      this.open = true;
      this.sibs = [];
      // Hello has to be the FIRST frame - the relay reads exactly one, on its
      // own deadline, before it will join this socket to anything.
      try {
        this.send(T_HELLO, {
          key: this.opts.key,
          announce: {
            instanceId: this.opts.selfId,
            name: this.opts.selfName,
            deployment: 'mobile',
            // Says "route to me, but do not list me as somewhere to go" - see
            // relay.Announce.Client. Without it this phone would appear as a
            // browsable instance on every other instance's Instances page and
            // answer 501 to everything asked of it.
            client: true,
          },
        });
      } catch {
        socket.close();
        return;
      }
      this.emitChange();
    };

    socket.onmessage = (event) => {
      if (this.socket !== socket) return;
      this.handle(String(event.data));
    };

    socket.onerror = () => {
      // Nothing to do here: a socket that errors also closes, and onclose is
      // where the single reconnect path lives. Handling both would double
      // every retry.
    };

    socket.onclose = () => {
      if (this.socket !== socket) return;
      const wasStable = this.open && Date.now() - this.openedAt >= STABLE_SESSION_MS;
      this.open = false;
      this.socket = null;
      this.sibs = [];
      this.failAllPending('the relay connection dropped before the answer came back');
      this.emitChange();
      if (this.closed) return;
      // Reset only after a connection that actually lasted: a relay that
      // accepts the socket and then hangs up - a rejected key, a proxy
      // misrouting the upgrade - must not be dialled once a second forever.
      if (wasStable) this.backoff = MIN_BACKOFF_MS;
      this.retryTimer = setTimeout(() => {
        // Cleared before dialling, not after: start() treats a pending timer
        // as "already on its way", and leaving a fired one set would make it
        // a no-op for good.
        this.retryTimer = null;
        this.connect();
      }, this.backoff);
      this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS);
    };
  }

  private handle(raw: string): void {
    let env: { type?: string; data?: any };
    try {
      env = JSON.parse(raw);
    } catch {
      return;
    }
    const data = env.data ?? {};
    switch (env.type) {
      case T_ANNOUNCE: {
        if (typeof data.instanceId !== 'string' || !data.instanceId) return;
        const sib: RelaySibling = {
          instanceId: data.instanceId,
          name: typeof data.name === 'string' ? data.name : '',
          deployment: typeof data.deployment === 'string' ? data.deployment : '',
          client: data.client === true,
        };
        // An announce for an id already known is that instance reconnecting,
        // not a second one - replace rather than append.
        this.sibs = [...this.sibs.filter((s) => s.instanceId !== sib.instanceId), sib];
        this.emitChange();
        return;
      }
      case T_PRESENCE: {
        if (typeof data.instanceId !== 'string') return;
        if (data.online !== true) {
          this.sibs = this.sibs.filter((s) => s.instanceId !== data.instanceId);
          this.emitChange();
        }
        return;
      }
      case T_PROXY_RESPONSE: {
        if (typeof data.requestId !== 'string') return;
        const p = this.pending.get(data.requestId);
        if (!p) return;
        this.pending.delete(data.requestId);
        clearTimeout(p.timer);
        // Error is set only when the RELAY answered instead of the target
        // ("nobody is connected as that instance"). That is a transport
        // failure, not an answer, so it rejects.
        //
        // Checked BEFORE the sealed blob and it must stay that way: the relay
        // writes this field in the clear because it holds no key, so it is
        // the one field on a response a hostile relay can author. Answering
        // it first, and never treating an unsealed response as a result, is
        // what keeps that limited to denial of service.
        if (typeof data.error === 'string' && data.error) {
          p.reject(new Error(`relay: ${data.error}`));
          return;
        }
        const result =
          typeof data.sealed === 'string'
            ? openResult(this.opts.frameKey, data.requestId, data.sealed)
            : null;
        if (!result) {
          // On this key, but not holding this group's secret - or something
          // rewrote the frame on the way. Neither is an answer, and neither
          // may be allowed to look like one.
          p.reject(new Error('relay: the instance answered unreadably'));
          return;
        }
        p.resolve({
          status: typeof result.status === 'number' ? result.status : 0,
          body: typeof result.body === 'string' && result.body ? decodeBody(result.body) : '',
        });
        return;
      }
      case T_PROXY_REQUEST: {
        // Something asked this phone to serve an API call. It has none. Answer
        // at once rather than letting the caller sit out its own timeout -
        // the same thing the Go client does when it has no Serve handler.
        if (typeof data.requestId !== 'string') return;
        // But only for a caller that actually holds the group's secret. A
        // frame that will not open came from somebody on this relay key who
        // is not in this group, and it gets no reply at all - the same silence
        // the Go client's answer() gives, and for the same reason: an error
        // reply would confirm that their key was accepted while their secret
        // was not.
        if (
          typeof data.sealed !== 'string' ||
          !openCall(this.opts.frameKey, data.requestId, this.opts.selfId, data.sealed)
        ) {
          return;
        }
        try {
          this.send(T_PROXY_RESPONSE, {
            requestId: data.requestId,
            sealed: sealResult(this.opts.frameKey, data.requestId, {
              status: 501,
              body: encodeBody('the mobile app serves no API of its own'),
            }),
          });
        } catch {
          // The socket went away underneath the reply; the caller's own
          // timeout covers it.
        }
        return;
      }
      default:
        // Unknown frame types are ignored, not treated as errors - a newer
        // relay must not break a client that predates one of its frames.
        return;
    }
  }

  private failAllPending(reason: string): void {
    for (const [, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(new Error(`relay: ${reason}`));
    }
    this.pending.clear();
  }
}

// --- Shared clients ------------------------------------------------------
//
// One socket per (relay, key), never one per saved connection. Two reasons,
// and the second one is not cosmetic: the relay identifies a connection by
// the instance id in its hello, and joining twice under the same id makes it
// treat the second as the first RECONNECTING and drop the original (see
// Server.Join). Several connections through one relay - the ordinary case,
// since one relay key is exactly what a person's own instances share - would
// therefore fight each other for the socket.

const clients = new Map<string, RelayClient>();

// JSON.stringify of the pair, not the two joined by a separator: a key is
// an opaque secret that could contain any character, and a separator it
// happens to contain would make two different relays share one client.
const keyOf = (url: string, key: string) => JSON.stringify([url.trim(), key]);

/**
 * Returns the shared, started client for one relay, creating it if needed.
 *
 * Watch it with client.subscribe() rather than passing a callback in: the
 * client returned here is frequently one that already existed, and options
 * given to a call that did not construct it would be silently dropped.
 */
export function relayClientFor(opts: RelayClientOptions): RelayClient {
  const id = keyOf(opts.url, opts.key);
  let c = clients.get(id);
  if (!c) {
    c = new RelayClient(opts);
    clients.set(id, c);
  }
  c.start();
  return c;
}

/** Closes and forgets a shared client - used when its connection is removed. */
export function closeRelayClient(url: string, key: string): void {
  const id = keyOf(url, key);
  clients.get(id)?.close();
  clients.delete(id);
}

/** Closes every shared client. Used by Settings' "remove all connections". */
export function closeAllRelayClients(): void {
  for (const [, c] of clients) c.close();
  clients.clear();
}
