/**
 * Click'n'Load, decoded in the browser.
 *
 * CnL is how a website hands a batch of links to a download manager: it POSTs
 * an encrypted list to http://127.0.0.1:9666, the port JDownloader has listened
 * on for two decades and which every CnL button hard-codes. KnightLoader's own
 * server speaks the same protocol (internal/cnl) — but only reaches the browser
 * when it runs on the same machine. On the primary deployment, a container on a
 * NAS, 127.0.0.1 inside that container is not the 127.0.0.1 the site means, and
 * the submission goes into the void.
 *
 * This is the other answer to that (jdp, 2026-08-28: "es soll immer
 * funktionieren, daher soll das CnL immer an die erweiterung gehen und die
 * verteilt es dann"): the extension catches the submission inside the page,
 * before it is ever sent, and hands the links to whichever instance the user
 * picks. No process on the desktop, no port to own.
 *
 * The decoding lives here rather than on the server, and that is not a
 * preference: the instance's API rejects a cross-origin request that carries a
 * foreign Origin header, and an extension's fetch always carries one (see
 * internal/bridge/bridge.go's own note on why the bridge gets away with it and
 * a browser does not). So the links have to arrive decoded, through the same
 * /quickadd window every other send already uses.
 */

/** The two spellings of the CnL port. Both appear in the wild. */
const CNL_HOSTS = ['127.0.0.1:9666', 'localhost:9666'];

/** Whether a URL string is aimed at a CnL listener. */
function isCnlUrl(raw) {
  try {
    const u = new URL(raw, location.href);
    return CNL_HOSTS.includes(u.host);
  } catch {
    return false;
  }
}

/**
 * cnlKeyFromJk pulls the AES key out of the `jk` field.
 *
 * `jk` is a one-line JavaScript function returning the key as hex. It is
 * attacker-controlled JavaScript from an arbitrary website, and it is
 * EXTRACTED WITH A REGULAR EXPRESSION, NEVER EXECUTED — running it would be an
 * obvious way to lose the browser. internal/cnl/cnl.go holds the same line for
 * the same reason; if one side ever grows an eval, so has the other by mistake.
 *
 * Some pages send the bare hex with no function around it, which is why the
 * second branch exists.
 */
function cnlKeyFromJk(jk) {
  const text = String(jk ?? '');
  const wrapped = /["']([0-9a-fA-F]{32})["']/.exec(text);
  if (wrapped) return wrapped[1];
  const bare = text.trim();
  return /^[0-9a-fA-F]{32}$/.test(bare) ? bare : '';
}

function hexToBytes(hex) {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
  return out;
}

function b64ToBytes(b64) {
  const bin = atob(String(b64).trim());
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/**
 * stripCnlPadding removes the padding the payload itself carries.
 *
 * A port of internal/cnl/cnl.go's stripPadding, deliberately line for line:
 * "zero + PKCS#7 padding, both occur in the wild", and a decoder that only
 * knew one of them would fail on half the sites with nothing to show for it.
 */
function stripCnlPadding(bytes) {
  let end = bytes.length;
  while (end > 0 && bytes[end - 1] === 0) end--;
  if (end > 0) {
    const pad = bytes[end - 1];
    if (pad > 0 && pad <= 16 && end >= pad) {
      let uniform = true;
      for (let i = end - pad; i < end; i++) {
        if (bytes[i] !== pad) {
          uniform = false;
          break;
        }
      }
      if (uniform) end -= pad;
    }
  }
  return bytes.subarray(0, end);
}

/**
 * cnlDecrypt turns an addcrypted2 submission into a list of links.
 *
 * AES-128-CBC, and the key doubles as the IV — that is the protocol, not a
 * mistake. The encryption is not security either: the key travels with the
 * ciphertext. It exists so a link list is not sitting in the page source for a
 * scraper to harvest.
 *
 * The awkward part is the padding, and it is worth explaining because the
 * workaround looks like a trick. WebCrypto's AES-CBC ALWAYS strips PKCS#7 and
 * throws if it does not find it — but real CnL payloads are sometimes
 * zero-padded instead, so a straight decrypt() fails outright on those. Rather
 * than reimplement AES, this appends one synthetic block that decrypts to
 * exactly the padding WebCrypto insists on:
 *
 *     B = E(key, lastCipherBlock XOR 0x10*16)
 *
 * CBC then decrypts B to D(B) XOR lastCipherBlock = 0x10*16, WebCrypto strips
 * those sixteen bytes as its padding, and hands back the payload untouched —
 * whatever padding it originally carried still on it, for stripCnlPadding above
 * to deal with the same way the Go side does. Encrypting that block needs the
 * key, which we have.
 */
async function cnlDecrypt(jk, crypted) {
  const hex = cnlKeyFromJk(jk);
  if (!hex) throw new Error('cnl: no hex key in jk');
  const key = hexToBytes(hex);
  const ct = b64ToBytes(crypted);
  if (ct.length === 0 || ct.length % 16 !== 0) throw new Error('cnl: ciphertext not block-aligned');

  const enc = await crypto.subtle.importKey('raw', key, 'AES-CBC', false, ['encrypt']);
  const dec = await crypto.subtle.importKey('raw', key, 'AES-CBC', false, ['decrypt']);

  const lastBlock = ct.subarray(ct.length - 16);
  const padBlock = new Uint8Array(16).fill(16);
  // encrypt() appends a padding block of its own; the first 16 bytes are the
  // block we actually want.
  const sealed = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-CBC', iv: lastBlock }, enc, padBlock));

  const padded = new Uint8Array(ct.length + 16);
  padded.set(ct, 0);
  padded.set(sealed.subarray(0, 16), ct.length);

  const plain = new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-CBC', iv: key }, dec, padded));
  const text = new TextDecoder().decode(stripCnlPadding(plain));
  return splitCnlLinks(text);
}

/** splitCnlLinks mirrors internal/cnl's own splitLinks: any whitespace run
 *  separates links, and anything that is not http(s)/ftp is dropped. */
function splitCnlLinks(text) {
  return String(text)
    .split(/[\r\n\s]+/)
    .map((s) => s.trim())
    .filter((s) => /^(https?|ftp):\/\//i.test(s));
}
