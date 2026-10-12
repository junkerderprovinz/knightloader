/**
 * Talking to the group through the relay.
 *
 * Like the phone, the extension joins the phrase group as a client and asks a
 * sibling to act for it, through routes a group member may reach
 * (groupRoutes in internal/api/group_routes.go). Membership is the
 * credential, so no token, password or address is needed.
 *
 * Each call opens a socket, says hello, asks and closes. The extension only
 * acts on a button press, and a long-lived socket would fight the MV3 service
 * worker's idle shutdown.
 *
 * The frame layer ports mobile/src/api/relayFrame.ts, itself a port of
 * internal/relay/seal.go and protocol.go. The AAD layout, the nonce length and
 * the nonce||ciphertext framing must match byte for byte. AES-GCM comes from
 * WebCrypto.
 */

const RELAY_NONCE_LEN = 12; // relay.nonceLen, AES-GCM's standard nonce size
const RELAY_HELLO = 'hello';
const RELAY_ANNOUNCE = 'announce';
const RELAY_PRESENCE = 'presence';
const RELAY_PROXY_REQUEST = 'proxy-request';
const RELAY_PROXY_RESPONSE = 'proxy-response';

/**
 * How long to listen for the group before deciding who is in it. The relay
 * pushes one `announce` per sibling after hello with no closing frame, so the
 * roster is settled after a quiet period, capped for a group nobody else is in.
 */
const RELAY_ROSTER_QUIET_MS = 350;
const RELAY_ROSTER_MAX_MS = 2500;

/** How long one call may take, socket included. The sibling stages links
 *  synchronously, and giving up early would report a failure for a send that
 *  still happens. */
const RELAY_TIMEOUT_MS = 20000;

const relayUtf8 = (s) => new TextEncoder().encode(s);
const relayFromUtf8 = (b) => new TextDecoder().decode(b);

/**
 * The routing fields travel in the clear, so they are bound into the seal and a
 * redirected frame will not open. The per-direction label stops an answer from
 * being replayed as a question.
 */
const relayRequestAAD = (requestId, target) => relayUtf8(`proxy-request\x00${requestId}\x00${target}`);
const relayResponseAAD = (requestId) => relayUtf8(`proxy-response\x00${requestId}`);
/** An announce keeps only the instance id in the clear and binds its seal to
 *  it, so a relay cannot attach one identity to another connection. Mirrors
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

/** Nonce ‖ ciphertext, base64, the framing relay.seal writes. */
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
 * relayOpen reverses relaySeal and returns null for every failure, like
 * relay.ErrSealed: the caller acts the same either way, and telling them apart
 * would help an attacker.
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

/** A random id for one call. The target runs each id once, so it is random
 *  rather than counted per socket. */
function relayRequestId() {
  const b = crypto.getRandomValues(new Uint8Array(8));
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

/**
 * relayMemberId mirrors relay.MemberID: the id a member of kind goes by with
 * the raw 32 bytes of its public key. An instance's is the first 20 bytes of
 * the key's SHA-256 in hex, a phone's and a browser's the whole hash after
 * their prefix.
 */
async function relayMemberId(kind, publicKey) {
  const hash = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', publicKey)), (b) =>
    b.toString(16).padStart(2, '0'),
  ).join('');
  if (kind === 'instance') return hash.slice(0, 40);
  return (kind === 'mobile' ? 'phone-' : 'browser-') + hash;
}

/**
 * relayFingerprint mirrors relay.Fingerprint: how a person compares a member
 * key, the start of its SHA-256 in four groups.
 */
async function relayFingerprint(publicKey) {
  const hash = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', publicKey)), (b) =>
    b.toString(16).padStart(2, '0'),
  ).join('');
  return [hash.slice(0, 4), hash.slice(4, 8), hash.slice(8, 12), hash.slice(12, 16)].join(' ');
}

const RELAY_CALL_DOMAIN = 'knightloader/relay/member-call/v2\x00';
const RELAY_RESULT_DOMAIN = 'knightloader/relay/member-result/v3\x00';

/** Eight bytes, big-endian. Lengths, statuses and the time stay far below 2^53. */
function relayUint64(n) {
  const hi = Math.floor(n / 0x100000000);
  const lo = n >>> 0;
  return [hi >>> 24, (hi >>> 16) & 255, (hi >>> 8) & 255, hi & 255, lo >>> 24, (lo >>> 16) & 255, (lo >>> 8) & 255, lo & 255];
}

/** The layout every signed message has: the domain, every field (text or raw
 *  bytes) with its length, then n. Mirrors signedMessage in relay/member.go. */
function relaySignedMessage(domain, n, fields) {
  const parts = [relayUtf8(domain)];
  for (const field of fields) {
    const bytes = typeof field === 'string' ? relayUtf8(field) : field;
    parts.push(new Uint8Array(relayUint64(bytes.length)), bytes);
  }
  parts.push(new Uint8Array(relayUint64(n)));
  const out = new Uint8Array(parts.reduce((len, p) => len + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

/**
 * relayCallMessage is what a member signs for a call. Mirrors relay.CallMessage
 * byte for byte; the instance refuses a call whose signature does not hold.
 */
function relayCallMessage(target, call) {
  return relaySignedMessage(RELAY_CALL_DOMAIN, call.sent, [call.id, target, call.method, call.path, call.body || '', call.from, call.kind]);
}

/**
 * relayResultMessage mirrors relay.ResultMessage: what instance from signs when
 * it answers the call that went out sealed as the bytes sealedCall under
 * requestId. The call is named by the hash of those bytes; body is the
 * answer's raw bytes.
 */
async function relayResultMessage(requestId, from, sealedCall, status, body) {
  const call = new Uint8Array(await crypto.subtle.digest('SHA-256', sealedCall));
  return relaySignedMessage(RELAY_RESULT_DOMAIN, status, [requestId, from, call, body]);
}

/**
 * relayVerifyResult mirrors relay.VerifyResult: whether an opened answer to
 * this browser's own call, sent sealed as sealedCall under requestId, is
 * target's, signed by the key target's instance id is made from. Anybody
 * holding the words can seal an answer, a "removed" included, but only the
 * target can sign one, and it signs it for one call as it arrived.
 */
async function relayVerifyResult(requestId, target, sealedCall, result) {
  try {
    const key = relayFromBase64(result.key || '');
    const sig = relayFromBase64(result.sig || '');
    if (key.length !== 32 || sig.length !== 64 || (await relayMemberId('instance', key)) !== target) return false;
    const body = result.body ? relayFromBase64(result.body) : new Uint8Array();
    const message = await relayResultMessage(requestId, target, sealedCall, result.status, body);
    const pub = await crypto.subtle.importKey('raw', key, 'Ed25519', false, ['verify']);
    return await crypto.subtle.verify('Ed25519', pub, sig, message);
  } catch {
    return false;
  }
}

/**
 * relaySealCall seals one call as relay.SealCall does, stamped with its request
 * id and the time, which the target checks so a relay cannot send it again, and
 * with this browser's member id. With `member`, a { privateKey, publicKey }
 * pair whose publicKey is the raw 32 bytes, the call is signed as
 * relay.SealSignedCall signs it: the instance pinned that key to the member id
 * and takes the id from nobody else.
 *
 * It resolves with the sealed call, in base64, and as the bytes the answer has
 * to name (relayVerifyResult).
 */
async function relaySealCall(frameKey, requestId, target, method, path, body, from, member) {
  const call = { method, path, body: body || '', id: requestId, sent: Math.floor(Date.now() / 1000), from: from || '', kind: 'extension' };
  let signed = {};
  let sig = new Uint8Array();
  if (member && from) {
    sig = new Uint8Array(await crypto.subtle.sign('Ed25519', member.privateKey, relayCallMessage(target, call)));
    signed = { kind: call.kind, key: relayToBase64(member.publicKey), sig: relayToBase64(sig) };
  }
  const sealed = await relaySeal(
    frameKey,
    relayRequestAAD(requestId, target),
    relayUtf8(
      JSON.stringify({
        method,
        path,
        ...(body ? { body: relayToBase64(relayUtf8(body)) } : {}),
        id: requestId,
        sent: call.sent,
        ...(from ? { from } : {}),
        ...signed,
      }),
    ),
  );
  return { sealed, asked: relayFromBase64(sealed) };
}

/**
 * relaySession opens one socket, joins the group, calls `work` with
 * { siblings, call } and closes again, resolving with what `work` returns.
 * `siblings` leaves out clients such as phones and browsers, which cannot take
 * a download. `selfId` routes this one session; `memberId`, when given, is
 * what the group knows the browser by, and travels only inside the seals;
 * `memberKey` signs its calls under that id (see relaySealCall).
 */
async function relaySession({ url, key, frameKey, selfId, memberId, memberKey, selfName, onRemoved }, work) {
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

  // Frames are handled one after another. Opening a seal awaits WebCrypto, and
  // a presence frame that overtook its sibling's announce would otherwise be
  // undone by it, putting an instance that left back on the list.
  let inbox = Promise.resolve();
  socket.onmessage = (event) => {
    inbox = inbox.then(() => handleFrame(event)).catch(() => {});
  };

  const handleFrame = async (event) => {
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
        // As in the Go port, only a seal that opens says who a peer is. The
        // relay can write plaintext fields itself, so an announce without a
        // seal, or with one that does not open, is listed as a bare id.
        let id = {};
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
        // A known id is that instance reconnecting.
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
        // The relay writes the error field and holds no key, so it is
        // plaintext. An unsealed response is never a result.
        if (d.error) {
          p.reject(new Error(`relay: ${d.error}`));
          return;
        }
        const plain = await relayOpen(frameKey, relayResponseAAD(d.requestId), d.sealed || '');
        if (!plain) {
          // Nobody holding this phrase wrote that frame.
          p.reject(new Error('relay: the answer could not be opened'));
          return;
        }
        try {
          const result = JSON.parse(relayFromUtf8(plain));
          if (!(await relayVerifyResult(d.requestId, p.target, p.asked, result))) {
            p.reject(new Error('relay: the answer is not signed by the instance it claims to come from'));
            return;
          }
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
    // Hello has to be the first frame; the relay reads one on a deadline
    // before joining the socket to anything.
    send(RELAY_HELLO, {
      key,
      announce: {
        instanceId: selfId,
        // Everything but the routing id is sealed. The client flag keeps this
        // browser off the other instances' lists of download targets.
        sealed: await relaySeal(
          frameKey,
          relayAnnounceAAD(selfId),
          relayUtf8(
            JSON.stringify({
              name: selfName,
              deployment: 'extension',
              client: true,
              ...(memberId ? { member: memberId } : {}),
            }),
          ),
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
      // The relay sees only the two routing fields, not what is asked.
      const { sealed, asked } = await relaySealCall(frameKey, requestId, target, method, path, body, memberId || selfId, memberId ? memberKey : null);
      const answer = new Promise((resolve, reject) => {
        const t = setTimeout(() => {
          pending.delete(requestId);
          reject(new Error('relay: the instance did not answer'));
        }, RELAY_TIMEOUT_MS);
        pending.set(requestId, {
          target,
          asked,
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
      send(RELAY_PROXY_REQUEST, { requestId, target, sealed });
      const res = await answer;
      if (res.status === 410 && res.body === 'removed') await onRemoved?.();
      return res;
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
