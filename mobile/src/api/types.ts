// Mirrors internal/core/task.go's Task struct on the server. Keep field
// names and types in sync with that file, not the other way around, since
// the server is the source of truth for this shape.
export type TaskStatus =
  | 'queued'
  | 'running'
  | 'paused'
  | 'finished'
  | 'failed'
  | 'extracting'
  | string; // the server's Status enum has more values than are worth hard-coding here

export interface Task {
  id: string;
  url: string;
  name: string;
  package: string;
  resolver: string;
  /** Whether this goes out on an account or anonymously. Only set for a link a
   *  hoster is on the other end of; absent means the question does not apply. */
  mode?: 'free' | 'premium';
  /** What the backend is doing right now for a task that is running but not
   *  moving bytes: "Captcha recognition", "Waiting for reconnect". */
  note?: string;
  size: number;
  loaded: number;
  speed: number;
  status: TaskStatus;
  error?: string;
  createdAt: string;
  dir?: string;
  online?: string;
  retries?: number;
  priority: number;
  position: number;
  checksum?: string;
  /** A collected variant row its host's preset leaves out; the collector does
   *  not show it. */
  variantOff?: boolean;
}

export interface AuthState {
  enabled: boolean;
  authenticated: boolean;
}

// A saved connection is one KnightLoader this phone talks to, over one of two
// transports. Every screen and every call in api/client.ts takes a
// ServerConnection without asking which kind it got; request() is the single
// place that branches.

/** Reached by opening an HTTP connection straight to it. */
export interface DirectConnection {
  // Absent on every connection saved before relay support existed. Those are
  // all direct ones, so a missing kind reads as direct and the stored list
  // needs no migration; see isRelayConnection.
  kind?: 'direct';
  id: string; // stable local id, so a rename doesn't orphan the "last active" pointer
  baseUrl: string; // e.g. "https://192.168.10.10:1234", no trailing slash
  token: string; // the API token secret, sent as "Authorization: Bearer <token>"
  name: string; // whatever the user called this connection, e.g. "Home KL"
}

/**
 * Reached through a relay both ends dial out to, for the case where nothing on
 * this phone's network can open a connection to the instance.
 *
 * It still carries a token: a relay-proxied call is replayed against the
 * target's own API and hits its normal auth guard, so a password-protected
 * instance needs one as it would over HTTP. The relay key gets the call to the
 * instance, the token gets it past the door.
 */
export interface RelayConnection {
  kind: 'relay';
  id: string;
  name: string;
  relayUrl: string; // the relay's address, as typed - normalised when dialled
  relayKey: string; // the relay's only credential, shared with the instances
  /**
   * Hex of the 32-byte key this connection's proxy frames are sealed under,
   * seedphrase.ts's deriveFrameKey of the same secret relayKey came from.
   *
   * Stored rather than re-derived on use because the phrase is not kept: it is
   * typed once, both keys come out of it, and the words are gone. Keeping the
   * frame key stores no more than relayKey already does.
   *
   * Optional so a connection saved before this existed still parses. Such a
   * connection cannot talk to anything, since its frames are unsealed and every
   * instance ignores those, so it is treated as needing to be added again; see
   * relayRequest in client.ts.
   */
  relayFrameKey?: string;
  instanceId: string; // which sibling on that key this connection is for
  token: string; // API token for that instance; '' when it has no password
}

export type ServerConnection = DirectConnection | RelayConnection;

export function isRelayConnection(c: ServerConnection): c is RelayConnection {
  return c.kind === 'relay';
}

// Mirrors internal/federation.Instance, a peer the connected server knows
// about. A peer has no separate token: /api/instances/{name}/* on the connected
// server proxies task, link and queue requests to it with that server's own
// credentials, so the app needs the peer's registered name and nothing else.
// Reaching a relay-visible peer needs no relay wire protocol here either, since
// the one server this app is connected to proxies those like stored peers, as
// the web UI's Instances and Dashboard pages do.
export interface Instance {
  // The address every proxied call is built from. For a relay peer this is its
  // InstanceID rather than the name it announced, so it does not change when
  // something else about the peer list does (see
  // federation.Manager.reachable). Render displayName instead of this one.
  name: string;
  url: string;
  // What a relay peer calls itself, present only when it differs from `name`.
  // A label; nothing addresses a peer by it. Falls back to `name`.
  displayName?: string;
  // Set only for a peer reached through the relay right now; a stored peer
  // carries none. `url` is empty for one of these, which is why
  // InstancesScreen shows a "connected via relay" line instead.
  relayId?: string;
}

// Mirrors internal/app.QueueState (app_queue.go): the master switch for one
// instance's queue, and how many transfers are in flight.
export interface QueueState {
  halted: boolean;
  stopMark?: string;
  running: number;
}

// Mirrors internal/captcha's Challenge and its payloads, the same shapes the
// web UI's lib/api.ts reads.
export type CaptchaKind = 'image' | 'click' | 'widget' | 'unsupported';

/** The payload of an 'image' or 'click' challenge: a complete data: URL. */
export interface CaptchaImagePayload {
  dataUrl: string;
}

/** The payload of a 'widget' challenge, everything the instance's widget page
 *  needs to render the vendor's script. */
export interface CaptchaWidgetPayload {
  /** The service JD's challenge class names, such as "recaptcha"; widgetRuns
   *  says whether the widget page can run it. */
  vendor: string;
  siteKey: string;
  siteUrl: string;
  contextUrl: string;
  type?: string;
  enterprise?: boolean;
  v3Action?: string;
  secureToken?: string;
}

/** The payload of an 'unsupported' challenge: JD's own name for it. */
export interface CaptchaUnsupportedPayload {
  vendor: string;
}

export interface CaptchaChallenge {
  /** Opaque: handed back to answer and skip unchanged. */
  id: string;
  source: string;
  host: string;
  taskId?: string;
  kind: CaptchaKind;
  /** What the hoster asks, in its own language. */
  prompt?: string;
  payload?: CaptchaImagePayload | CaptchaWidgetPayload | CaptchaUnsupportedPayload;
  /** When it stops being answerable. Go writes an unknown deadline as year 1. */
  expiresAt: string;
}

/** How far a skip reaches, captcha.AbortScope on the server. */
export type CaptchaAbortScope = 'skip-once' | 'blacklist-hoster' | 'blacklist-everywhere';
