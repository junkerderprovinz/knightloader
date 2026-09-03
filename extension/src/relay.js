/**
 * Talking to the group through the relay.
 *
 * The extension joins the phrase group the same way the phone does — as a
 * CLIENT, not as an instance — and asks a sibling to do something on its behalf.
 * Two calls are all it needs, and both are on the server's own list of what a
 * group member may reach (internal/api/routes_relay.go, relayForwardable):
 *
 *   GET  /api/instances   which instances are in this group
 *   POST /api/links       put these links in one of them
 *
 * That list is why the phrase alone is enough here. A relayed call arrives at
 * the instance marked as coming from a group sibling, and membership IS the
 * credential — no token, no password, and no address for this browser to know.
 * Before this file existed, the options page asked for a name and an address,
 * which was the pre-phrase model still standing in a product that had moved on
 * (jdp, 2026-08-28: "Wieso muss man eine Instanz per Name & Adresse hinzufügen?
 * Das soll doch jetzt alles ausschliesslich via Phrase laufen.").
 *
 * ONE-SHOT, deliberately. The phone holds its socket open because it shows a
 * live task list; this extension sends when somebody presses a button and has
 * nothing to watch in between. So a call opens the socket, says hello, asks,
 * and closes. That also sidesteps the MV3 service worker's own idle shutdown,
 * which a long-lived socket here would be permanently fighting.
 *
 * The frame layer is a port of mobile/src/api/relayFrame.ts, which is itself a
 * port of internal/relay/seal.go and protocol.go. All three have to agree byte
 * for byte or this browser joins a group and can speak to nobody in it, so
 * everything that could drift is stated once, here: the AAD layout, the nonce
 * length, and the nonce||ciphertext framing. AES-GCM comes from WebCrypto
 * rather than a bundled library — the phone ships @noble/ciphers because its
 * runtime's own crypto is not dependable, and a browser has no such excuse.
 */

const RELAY_NONCE_LEN = 12; // relay.nonceLen — AES-GCM's standard nonce size
const RELAY_HELLO = 'hello';
const RELAY_ANNOUNCE = 'announce';
const RELAY_PRESENCE = 'presence';
const RELAY_PROXY_REQUEST = 'proxy-request';
const RELAY_PROXY_RESPONSE = 'proxy-response';

/**
 * How long to listen for the group before deciding who is in it.
 *
 * The relay pushes one `announce` per sibling right after hello — nothing is
 * asked for, so there is no reply to wait on and no "that was all" frame. The
 * only honest stopping rule is a short quiet period after the last one, with a
 * hard cap so a silent group (nobody else online) still finishes.
 */
const RELAY_ROSTER_QUIET_MS = 350;
const RELAY_ROSTER_MAX_MS = 2500;

/** How long one call may take, socket included. Generous: the sibling stages
 *  the links synchronously, and a browser that gave up early would report a
 *  failure for something that then happened anyway. */
const RELAY_TIMEOUT_MS = 20000;

const relayUtf8 = (s) => new TextEncoder().encode(s);
const relayFromUtf8 = (b) => new TextDecoder().decode(b);

/**
 * The routing fields necessarily travel in the clear, so they are bound into
 * the seal: a relay cannot redirect a frame and have it still open. The \x00
 * separator and the per-direction label both matter — the label is what stops
 * an answer being replayed as a question.
 */
const relayRequestAAD = (requestId, target) => relayUtf8(`proxy-request\x00${requestId}\x00${target}`);
const relayResponseAAD = (requestId) => relayUtf8(`proxy-response\x00${requestId}`);
/** An announce keeps exactly one field in the clear — the instance id the
 *  relay routes on — and binds its seal to it, so a relay cannot attach one
 *  instance's sealed identity to another connection. Mirrors
 *  relay.announceAAD. */
const relayAnnounceAAD = (instanceId) => relayUtf8(`announce\x00${instanceId}`);

/** Base64 of raw bytes, the shape encoding/json gives a Go []byte. */
function relayToBase64(bytes) {
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

function relayFromBase64(s) {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function relayGcmKey(raw) {
  if (raw.length !== 32) throw new Error('relay: frame key must be 32 bytes');
  return crypto.subtle.importKey('raw', raw, 'AES-GCM', false, ['encrypt', 'decrypt']);
}

/** Nonce ‖ ciphertext, base64. Exactly the framing relay.seal writes: the Go
 *  side reads the first nonceLen bytes as the nonce and hands the rest to GCM. */
async function relaySeal(frameKey, aad, plaintext) {
  const key = await relayGcmKey(frameKey);
  const nonce = crypto.getRandomValues(new Uint8Array(RELAY_NONCE_LEN));
  const sealed = new Uint8Array(
    await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonce, additionalData: aad }, key, plaintext),
  );
  const framed = new Uint8Array(RELAY_NONCE_LEN + sealed.length);
  framed.set(nonce);
  framed.set(sealed, RELAY_NONCE_LEN);
  return relayToBase64(framed);
}

/**
 * relayOpen reverses relaySeal. Null for every failure — a wrong key, a
 * truncated frame, a tampered one — deliberately not telling them apart, the
 * same choice relay.ErrSealed makes and for the same reason: the caller's move
 * is identical in every case, and distinguishing them tells an attacker which
 * guess was closer.
 */
async function relayOpen(frameKey, aad, sealedB64) {
  try {
    const framed = relayFromBase64(sealedB64);
    if (framed.length <= RELAY_NONCE_LEN) return null;
    const key = await relayGcmKey(frameKey);
    const plain = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: framed.subarray(0, RELAY_NONCE_LEN), additionalData: aad },
      key,
      framed.subarray(RELAY_NONCE_LEN),
    );
    return new Uint8Array(plain);
  } catch {
    return null;
  }
}

/** A random id for one call. Only has to be unique within this socket. */
function relayRequestId() {
  const b = crypto.getRandomValues(new Uint8Array(8));
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

/**
 * relaySession opens one socket, joins the group, hands the caller the roster
 * and a way to ask a sibling something, then closes — whatever the caller did.
 *
 * One socket per user action, not one held open: see this file's own opening
 * comment. `work` receives { siblings, call } and whatever it returns is what
 * this resolves with.
 *
 * `siblings` excludes clients (other phones, other browsers): they are routable
 * but they are not somewhere to send a download to.
 */
async function relaySession({ url, key, frameKey, selfId, selfName }, work) {
  const socket = await new Promise((resolve, reject) => {
    let ws;
    try {
      ws = new WebSocket(url);
    } catch (e) {
      reject(e instanceof Error ? e : new Error(String(e)));
      return;
    }
    const t = setTimeout(() => {
      try {
        ws.close();
      } catch {
        /* already closing */
      }
      reject(new Error('relay: timed out reaching the relay'));
    }, RELAY_TIMEOUT_MS);
    ws.onopen = () => {
      clearTimeout(t);
      resolve(ws);
    };
    ws.onerror = () => {
      clearTimeout(t);
      reject(new Error('relay: could not reach the relay'));
    };
  });

  const siblings = new Map();
  const pending = new Map();
  let onRosterFrame = null;
  let closedReason = null;

  socket.onclose = () => {
    closedReason = closedReason || new Error('relay: the connection closed');
    for (const p of pending.values()) p.reject(closedReason);
    pending.clear();
  };
  socket.onerror = () => {
    closedReason = new Error('relay: the connection failed');
  };

  socket.onmessage = async (event) => {
    let frame;
    try {
      frame = JSON.parse(String(event.data));
    } catch {
      return;
    }
    const d = frame?.data ?? {};
    switch (frame?.type) {
      case RELAY_ANNOUNCE: {
        if (typeof d.instanceId !== 'string' || !d.instanceId) return;
        // The identity arrives sealed — see relay.Identity. Three cases, the
        // same three the Go and mobile ports handle: a seal that opens is
        // used; no seal at all is an instance still on a version from before
        // this and its plaintext is read as it always was; a seal that will
        // NOT open is a peer on another frame key, kept in the roster under
        // its id with no name rather than hidden, so a key mismatch shows up
        // as an unnamed instance instead of as nothing at all.
        let id = d;
        if (typeof d.sealed === 'string' && d.sealed) {
          const plain = await relayOpen(frameKey, relayAnnounceAAD(d.instanceId), d.sealed);
          id = {};
          if (plain) {
            try {
              id = JSON.parse(relayFromUtf8(plain));
            } catch {
              id = {};
            }
          }
        }
        // An announce for an id already known is that instance reconnecting,
        // not a second one — replace rather than append.
        siblings.set(d.instanceId, {
          instanceId: d.instanceId,
          name: typeof id.name === 'string' ? id.name : '',
          deployment: typeof id.deployment === 'string' ? id.deployment : '',
          client: id.client === true,
        });
        onRosterFrame?.();
        return;
      }
      case RELAY_PRESENCE: {
        if (typeof d.instanceId === 'string' && d.online !== true) siblings.delete(d.instanceId);
        onRosterFrame?.();
        return;
      }
      case RELAY_PROXY_RESPONSE: {
        const p = pending.get(d.requestId);
        if (!p) return;
        pending.delete(d.requestId);
        // The error field is plaintext because the RELAY writes it and holds no
        // key. Read it first, and never treat an unsealed response as a result.
        if (d.error) {
          p.reject(new Error(`relay: ${d.error}`));
          return;
        }
        const plain = await relayOpen(frameKey, relayResponseAAD(d.requestId), d.sealed || '');
        if (!plain) {
          // Not an answer from the sibling at all: the frame that came back
          // could not have been written by anybody holding this phrase.
          p.reject(new Error('relay: the answer could not be opened'));
          return;
        }
        try {
          const result = JSON.parse(relayFromUtf8(plain));
          p.resolve({
            status: result.status,
            body: result.body ? relayFromUtf8(relayFromBase64(result.body)) : '',
          });
        } catch (e) {
          p.reject(e instanceof Error ? e : new Error(String(e)));
        }
        return;
      }
      default:
    }
  };

  const send = (type, data) => socket.send(JSON.stringify({ type, data }));

  try {
    // Hello has to be the FIRST frame: the relay reads exactly one, on its own
    // deadline, before it will join this socket to anything.
    send(RELAY_HELLO, {
      key,
      announce: {
        instanceId: selfId,
        // Everything except the id the relay routes on goes into the seal:
        // the name, the 'extension' marker, and the client flag that says
        // "route to me, but do not list me as somewhere to go" (without which
        // this browser would appear as a browsable instance on every other
        // instance's Instances page and answer 501 to everything asked of
        // it). All three are for siblings; none of them is for the relay.
        sealed: await relaySeal(
          frameKey,
          relayAnnounceAAD(selfId),
          relayUtf8(JSON.stringify({ name: selfName, deployment: 'extension', client: true })),
        ),
      },
    });

    // Settle the roster before handing over. See RELAY_ROSTER_QUIET_MS.
    await new Promise((resolve) => {
      const cap = setTimeout(resolve, RELAY_ROSTER_MAX_MS);
      let quiet = setTimeout(resolve, RELAY_ROSTER_QUIET_MS);
      onRosterFrame = () => {
        clearTimeout(quiet);
        quiet = setTimeout(() => {
          clearTimeout(cap);
          resolve();
        }, RELAY_ROSTER_QUIET_MS);
      };
    });
    onRosterFrame = null;

    /** Asks one sibling one thing. `target` is its relay instance id. */
    const call = async (target, method, path, body) => {
      if (closedReason) throw closedReason;
      const requestId = relayRequestId();
      const answer = new Promise((resolve, reject) => {
        const t = setTimeout(() => {
          pending.delete(requestId);
          reject(new Error('relay: the instance did not answer'));
        }, RELAY_TIMEOUT_MS);
        pending.set(requestId, {
          resolve: (v) => {
            clearTimeout(t);
            resolve(v);
          },
          reject: (e) => {
            clearTimeout(t);
            reject(e);
          },
        });
      });
      send(RELAY_PROXY_REQUEST, {
        requestId,
        target,
        // Everything except the two routing fields goes inside the seal, so the
        // relay carrying this frame sees which instance it is for and nothing
        // about what is being asked.
        sealed: await relaySeal(
          frameKey,
          relayRequestAAD(requestId, target),
          relayUtf8(
            JSON.stringify({
              method,
              path,
              ...(body ? { body: relayToBase64(relayUtf8(body)) } : {}),
            }),
          ),
        ),
      });
      return answer;
    };

    return await work({
      siblings: [...siblings.values()].filter((s) => !s.client).sort((a, b) => a.instanceId.localeCompare(b.instanceId)),
      call,
    });
  } finally {
    try {
      socket.close();
    } catch {
      /* already closing */
    }
  }
}
