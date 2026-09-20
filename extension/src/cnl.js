/**
 * Click'n'Load, decoded in the browser.
 *
 * A site hands links to a download manager by POSTing an encrypted list to
 * http://127.0.0.1:9666. A KnightLoader in a container on a NAS is not at that
 * address, so the extension catches the submission in the page and sends the
 * links to the instance the user picks.
 *
 * The decoding happens here because the instance's API rejects requests with a
 * foreign Origin header, which an extension's fetch always carries. The links
 * arrive decoded through the same /quickadd window every other send uses.
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
 * `jk` is a one-line JavaScript function returning the key as hex, written by
 * an arbitrary website, so the key is extracted with a regular expression and
 * the code is never executed, as in internal/cnl/cnl.go. Some pages send the
 * bare hex instead.
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
 * stripCnlPadding removes zero and PKCS#7 padding, both of which occur in the
 * wild. A line-for-line port of stripPadding in internal/cnl/cnl.go.
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
 * AES-128-CBC with the key as the IV, as the protocol defines. The key travels
 * with the ciphertext, so this only keeps links out of the page source.
 *
 * WebCrypto's AES-CBC always strips PKCS#7 and throws without it, but some
 * payloads are zero-padded. So one extra block is appended that decrypts to
 * exactly the padding WebCrypto wants:
 *
 *     B = E(key, lastCipherBlock XOR 0x10*16)
 *
 * CBC decrypts B to 0x10*16, WebCrypto strips those bytes, and the payload
 * comes back with its original padding for stripCnlPadding.
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

/** splitCnlLinks mirrors splitLinks in internal/cnl: whitespace separates
 *  links, and anything that is not http(s) or ftp is dropped. */
function splitCnlLinks(text) {
  return String(text)
    .split(/[\r\n\s]+/)
    .map((s) => s.trim())
    .filter((s) => /^(https?|ftp):\/\//i.test(s));
}
