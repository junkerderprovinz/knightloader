import { getRandomBytes } from 'expo-crypto';
import TcpSocket from 'react-native-tcp-socket';
import { base64Decode, utf8ToBytes } from '../api/base64';
import { request } from '../api/client';
import { sealSignedCall } from '../api/relayFrame';
import { fromHex } from '../api/sha256';
import type { ServerConnection } from '../api/types';
import { memberOptions } from '../storage/relayIdentity';
import { STREAM_PREFIX, directSignature, readAnswer } from './directStream';

// Where an instance takes direct calls, and where it says which address that
// is (kit/group's DirectPath and hello).
const DIRECT_PATH = '/api/group/call';
const HELLO_PATH = '/kit/group/v1/hello';

// A piece on the local network comes in well under this, and an address that
// stopped answering should hand the playback to the relay soon.
const CALL_TIMEOUT_MS = 15_000;

/**
 * StreamBusy is an instance sending as many streams as it may. The piece goes
 * through the relay, and the next one tries the network again.
 */
export class StreamBusy extends Error {}

/** A reader of a file's bytes, from and length as the range route takes them. */
export type RangeReader = (from: number, length: number) => Promise<Uint8Array>;

/**
 * directReader reads ranges of task id's file as streams from the instance on
 * this phone's network, or is null where the phone cannot: a connection paired
 * before it kept the peer-auth key, a peer behind the instance, or an instance
 * that names no plain address on its network.
 *
 * The call goes over a socket of its own rather than fetch, which Android
 * holds to the app's no-cleartext rule. What travels is sealed and signed as
 * on the relay, so the plain connection shows nobody on the network more than
 * the relay would.
 */
export async function directReader(conn: ServerConnection, id: string, file: string, base: string): Promise<RangeReader | null> {
  if (base !== '/api' || !conn.peerAuthKey || !conn.relayFrameKey) return null;
  const hello = await request<{ url?: string }>(conn, '', HELLO_PATH).catch(() => null);
  const at = /^http:\/\/([a-z0-9.-]+)(?::(\d+))?\/?$/i.exec(hello?.url ?? '');
  const { selfId, memberSeed } = await memberOptions();
  if (!at || !memberSeed) return null;
  const host = at[1];
  const port = at[2] ? Number(at[2]) : 80;
  const frameKey = fromHex(conn.relayFrameKey);
  const peerAuth = fromHex(conn.peerAuthKey);
  const target = conn.instanceId;

  return async (from, length) => {
    const q = new URLSearchParams({ from: String(from), length: String(length) });
    if (file) q.set('file', file);
    const path = `${STREAM_PREFIX}/api/tasks/${encodeURIComponent(id)}/range?${q.toString()}`;
    const requestId = Array.from(getRandomBytes(16), (b) => b.toString(16).padStart(2, '0')).join('');
    const sealed = sealSignedCall(frameKey, memberSeed, requestId, target, { method: 'GET', path, from: selfId });
    const body = new Uint8Array(utf8ToBytes(JSON.stringify({ requestId, target, sealed })));
    const unix = String(Math.floor(Date.now() / 1000));
    const head =
      `POST ${DIRECT_PATH} HTTP/1.0\r\nHost: ${host}:${port}\r\nContent-Type: application/json\r\n` +
      `Content-Length: ${body.length}\r\nX-Knightloader-Peer: ${selfId}\r\nX-Knightloader-Target: ${target}\r\n` +
      `X-Knightloader-Time: ${unix}\r\nX-Knightloader-Signature: ${directSignature(peerAuth, selfId, unix, body)}\r\n\r\n`;
    const raw = await exchange(host, port, head, body);
    if (/^HTTP\/1\.\d 503 /.test(String.fromCharCode(...raw.subarray(0, 16)))) throw new StreamBusy();
    const answer = readAnswer(raw, { frameKey, requestId, target, sealedCall: base64Decode(sealed) });
    if (answer.status === 503) throw new StreamBusy();
    if (answer.status !== 200) throw new Error(`the instance refused the stream with ${answer.status}`);
    return answer.bytes;
  };
}

/** exchange sends one HTTP/1.0 request and reads the answer until the instance closes. */
function exchange(host: string, port: number, head: string, body: Uint8Array): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const chunks: Uint8Array[] = [];
    let timer: ReturnType<typeof setTimeout>;
    const socket = TcpSocket.createConnection({ host, port }, () => {
      socket.write(head, 'latin1');
      socket.write(body);
    });
    const quiet = () => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        socket.destroy();
        reject(new Error('the instance stopped answering on the local network'));
      }, CALL_TIMEOUT_MS);
    };
    quiet();
    socket.on('data', (chunk) => {
      chunks.push(typeof chunk === 'string' ? new Uint8Array(utf8ToBytes(chunk)) : new Uint8Array(chunk));
      quiet();
    });
    socket.on('error', (err) => {
      clearTimeout(timer);
      reject(err);
    });
    socket.on('close', () => {
      clearTimeout(timer);
      const out = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0));
      let at = 0;
      for (const c of chunks) {
        out.set(c, at);
        at += c.length;
      }
      resolve(out);
    });
  });
}
