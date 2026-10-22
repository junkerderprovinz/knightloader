import { base64Decode, base64Encode, bytesToUtf8, encodeBody } from './base64';
import { getRandomBytes } from 'expo-crypto';
import { memberPublicKey, proveId, verifyResult } from './memberKey';
import { openCall, openIdentity, openResult, sealCall, sealIdentity, sealResult, sealSignedCall } from './relayFrame';

// This app's client for internal/relay's wire protocol, the second way it can
// reach an instance: one behind a NAT with no port forwarding and no reverse
// proxy, where nothing on the phone's network can open a connection to it. Both
// ends dial out to the relay instead, and it forwards between them.
//
// The protocol is one JSON frame per message so that a client which speaks only
// plain WebSocket text needs a JSON parser and nothing else. Keep the frame
// shapes below in sync with internal/relay/protocol.go, which is the source of
// truth.
//
// The relay carries request and response frames, not a tunnelled WebSocket, so
// a relay connection polls for its task list as a federation peer does over the
// proxy route (see pollTasks in api/client.ts).

/** Mirrors relay.Announce. */
export interface RelaySibling {
  instanceId: string;
  name: string;
  deployment: string;
  /** True for a connection that uses the relay without being an instance, such
   *  as another phone, which is never a target worth offering. */
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
const T_CHALLENGE = 'challenge';
const T_HELLO = 'hello';
const T_ANNOUNCE = 'announce';
const T_PRESENCE = 'presence';
const T_PROXY_REQUEST = 'proxy-request';
const T_PROXY_RESPONSE = 'proxy-response';

/**
 * connectURL mirrors internal/relay/client.go's function of the same name:
 * people write down the https:// address they gave their reverse proxy, not a
 * WebSocket URL, so both scheme families are accepted. A path ending in
 * /connect names the socket and stays (a relay behind a proxy can be mounted
 * anywhere, and only the person configuring it knows); any other path is where
 * a proxy serves an instance, and the connect path goes below it.
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
  const path = m[3].replace(/\/+$/, '');
  return `${scheme}://${host}${path.endsWith('/connect') ? path : `${path}/relay/connect`}`;
}

/**
 * relayHost mirrors relay.RelayHost: the name the hello's proof gives the relay
 * it is meant for, the host of address in lower case and without the port, the
 * brackets of an IPv6 literal or the dot of a fully qualified name. A relay
 * takes the proof only under a name of its own, so another relay cannot pass
 * its challenge on to this phone.
 */
export function relayHost(address: string): string {
  const trimmed = address.trim();
  const m = (trimmed.includes('://') ? trimmed : `http://${trimmed}`).match(/^[^:]*:\/\/([^/?#]*)/);
  if (!m) return '';
  let host = m[1].slice(m[1].lastIndexOf('@') + 1);
  const bracketed = host.match(/^\[([^\]]*)\](?::\d*)?$/);
  if (bracketed) host = bracketed[1];
  else if (host.split(':').length === 2) host = host.slice(0, host.indexOf(':'));
  return host.replace(/\.$/, '').toLowerCase();
}

interface Pending {
  // The instance asked, whose key has to have signed the answer, and the call
  // as it went out, sealed, which the answer has to name.
  target: string;
  sealed: number[];
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
  /** The seed of this device's member key, which signs every call as selfId;
   *  null sends them unsigned. */
  memberSeed: Uint8Array | null;
  /** What siblings would call this device, if they listed it; they do not. */
  selfName: string;
}

// How long a call waits for a connection that is still coming up. Without it
// the first call after launch always fails: the socket, its hello and the
// relay's answer all have to complete first, and every screen fires its first
// request the instant it mounts.
const CONNECT_WAIT_MS = 6000;

// The relay announces every sibling as soon as a socket joins, so a roster this
// old holds all of them.
const ROSTER_SETTLE_MS = 10_000;

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
    for (const fn of anyListeners) fn();
  }

  /** Dials at once when the client is waiting out a backoff, which after a
   *  spell in the background can be a minute. */
  wake(): void {
    if (this.closed || this.socket || !this.retryTimer) return;
    clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.connect();
  }

  start(): void {
    // A pending retry counts as started. Dialling during a backoff wait would
    // leave the timer to dial again as well, and two sockets racing under one
    // identity end with the relay dropping one of them for good.
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

  /** Whether siblings() holds every instance on the relay, rather than only the
   *  ones announced so far. */
  settled(): boolean {
    return this.open && Date.now() - this.openedAt >= ROSTER_SETTLE_MS;
  }

  /** The instances visible through the relay right now, phones excluded. */
  siblings(): RelaySibling[] {
    return this.sibs.filter((s) => !s.client).slice().sort((a, b) => a.instanceId.localeCompare(b.instanceId));
  }

  /**
   * proxy calls one sibling's REST API through the relay, the same
   * (method, path, body) -> (status, body) shape a direct fetch has.
   *
   * A rejection means the call never produced an answer: the relay is not
   * connected, the target is not, or nobody replied in time. An answer the
   * target produced resolves however bad its status, because "your instance
   * said no" and "your instance is gone" are different outcomes, as in the Go
   * client's Proxy. An answer counts only signed by the key target's id is
   * made from: anybody holding the words can seal one, a "removed" that makes
   * this phone leave its group included, but only the target can sign it.
   */
  async proxy(target: string, method: string, path: string, body?: string): Promise<RelayProxyResult> {
    if (!this.open) await this.waitForConnection();
    return new Promise<RelayProxyResult>((resolve, reject) => {
      if (!this.open || !this.socket) {
        reject(new Error('relay: not connected'));
        return;
      }
      // Random rather than counted: an instance runs each id once per key, and
      // an id somebody else can predict is one they can send first.
      const requestId = Array.from(getRandomBytes(16), (b) => b.toString(16).padStart(2, '0')).join('');
      const timer = setTimeout(() => {
        this.pending.delete(requestId);
        reject(new Error('relay: the instance did not answer in time'));
      }, PROXY_TIMEOUT_MS);
      const pending: Pending = { target, sealed: [], resolve, reject, timer };
      this.pending.set(requestId, pending);

      try {
        // Everything except the two routing fields goes inside the seal, so
        // the relay sees which instance the frame is for and nothing about what
        // is being asked. See relayFrame.ts.
        let sealed: string;
        if (this.opts.memberSeed) {
          sealed = sealSignedCall(this.opts.frameKey, this.opts.memberSeed, requestId, target, {
            method,
            path,
            body,
            from: this.opts.selfId,
          });
        } else {
          sealed = sealCall(this.opts.frameKey, requestId, target, {
            method,
            path,
            ...(body ? { body: encodeBody(body) } : {}),
            from: this.opts.selfId,
          });
        }
        pending.sealed = base64Decode(sealed);
        this.send(T_PROXY_REQUEST, { requestId, target, sealed });
      } catch (err) {
        clearTimeout(timer);
        this.pending.delete(requestId);
        reject(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }

  /**
   * Resolves as soon as the connection is up, or after CONNECT_WAIT_MS
   * whatever happened. The caller re-checks isConnected afterwards, so a
   * timeout here means "carry on and fail properly".
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
      this.sibs = [];
      // The relay speaks first, with the challenge the hello signs (handle).
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
      // Reset only after a connection that lasted. A relay that accepts the
      // socket and then hangs up, over a rejected key or a proxy misrouting the
      // upgrade, must not be dialled once a second for ever.
      if (wasStable) this.backoff = MIN_BACKOFF_MS;
      this.retryTimer = setTimeout(() => {
        // Cleared before dialling, not after: start() treats a pending timer
        // as already on its way, so a fired one left set makes it a no-op.
        this.retryTimer = null;
        this.connect();
      }, this.backoff);
      this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS);
    };
  }

  /** hello introduces this socket once the relay sent the challenge its
   *  proof signs. */
  private hello(challenge: Uint8Array): void {
    // Hello has to be the first frame: the relay reads exactly one, on its
    // own deadline, before it joins this socket to anything.
    try {
      // The proof lets this socket take the place of one the relay still
      // holds for this phone after a dropped connection, which nobody
      // without the phone's key can.
      const seed = this.opts.memberSeed;
      const relay = relayHost(this.opts.url);
      this.send(T_HELLO, {
        key: this.opts.key,
        ...(seed
          ? {
              proof: {
                kind: 'mobile',
                relay,
                key: base64Encode(Array.from(memberPublicKey(seed))),
                sig: base64Encode(Array.from(proveId(seed, this.opts.key, relay, this.opts.selfId, challenge))),
              },
            }
          : {}),
        announce: {
          instanceId: this.opts.selfId,
          // Everything but the id goes into the seal, under the frame key
          // the relay does not hold (see relay.Identity). The device name and
          // the 'mobile' marker give the phone its own card on every
          // instance's Instances page, and the client flag keeps it off the
          // list of instances to call, where it would answer 501 to
          // everything.
          sealed: sealIdentity(this.opts.frameKey, this.opts.selfId, {
            name: this.opts.selfName,
            deployment: 'mobile',
            client: true,
          }),
        },
      });
    } catch {
      this.socket?.close();
      return;
    }
    this.open = true;
    this.emitChange();
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
      case T_CHALLENGE: {
        if (this.open || typeof data.nonce !== 'string') return;
        const nonce = new Uint8Array(base64Decode(data.nonce));
        if (nonce.length !== 32) {
          this.socket?.close();
          return;
        }
        this.hello(nonce);
        return;
      }
      case T_ANNOUNCE: {
        if (typeof data.instanceId !== 'string' || !data.instanceId) return;
        // As in relay.openAnnounce, only a seal that opens names a sibling. The
        // plaintext of an unsealed announce is whatever the sender claims, so
        // that announce, like one whose seal does not open, is listed under its
        // bare id rather than hidden: a roster that disagrees with the relay
        // hides the one symptom that lets anybody diagnose a key mismatch.
        const id =
          (typeof data.sealed === 'string' && data.sealed
            ? openIdentity(this.opts.frameKey, data.instanceId, data.sealed)
            : null) ?? {};
        const sib: RelaySibling = {
          instanceId: data.instanceId,
          name: typeof id.name === 'string' ? id.name : '',
          deployment: typeof id.deployment === 'string' ? id.deployment : '',
          client: id.client === true,
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
        // Error is set only when the relay answered instead of the target
        // ("nobody is connected as that instance"). That is a transport
        // failure, not an answer, so it rejects.
        //
        // It is read before the sealed blob and has to stay that way: the relay
        // writes this field in the clear because it holds no key, so it is the
        // one field on a response a hostile relay can author. Reading it first,
        // and never treating an unsealed response as a result, keeps that to a
        // denial of service.
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
        const status = typeof result.status === 'number' ? result.status : 0;
        const body = typeof result.body === 'string' ? base64Decode(result.body) : [];
        const key = new Uint8Array(typeof result.key === 'string' ? base64Decode(result.key) : []);
        const sig = new Uint8Array(typeof result.sig === 'string' ? base64Decode(result.sig) : []);
        if (!verifyResult(data.requestId, p.target, p.sealed, status, body, key, sig)) {
          p.reject(new Error('relay: the answer is not signed by the instance it claims to come from'));
          return;
        }
        p.resolve({ status, body: bytesToUtf8(body) });
        return;
      }
      case T_PROXY_REQUEST: {
        // Something asked this phone to serve an API call. It has none, so it
        // answers at once rather than letting the caller sit out its timeout,
        // as the Go client does when it has no Serve handler.
        if (typeof data.requestId !== 'string') return;
        // Only for a caller that holds the group's secret. A frame that does
        // not open came from somebody on this relay key who is not in this
        // group, and it gets no reply at all, the same silence the Go client's
        // answer() gives: an error reply would confirm that their key was
        // accepted while their secret was not.
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
        // A newer relay must not break a client that predates one of its
        // frames, so unknown types are ignored rather than treated as errors.
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

// One socket per (relay, key), never one per saved connection. The relay
// identifies a connection by the instance id in its hello, and joining twice
// under the same id makes it treat the second as the first reconnecting and
// drop the original (see Server.Join). Since one relay key is what a person's
// own instances share, per-connection sockets would fight each other.

const clients = new Map<string, RelayClient>();

// Screens that follow every relay at once, including clients made after they
// subscribed, which a per-client subscription cannot reach.
const anyListeners = new Set<() => void>();

/** Calls fn whenever any relay client connects, drops or sees its roster
 *  change. Returns an unsubscribe function. */
export function subscribeAllRelays(fn: () => void): () => void {
  anyListeners.add(fn);
  return () => anyListeners.delete(fn);
}

/** Wakes every shared client that is waiting to redial. */
export function wakeRelayClients(): void {
  for (const [, c] of clients) c.wake();
}

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

/** Closes and forgets a shared client when its connection is removed. */
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
