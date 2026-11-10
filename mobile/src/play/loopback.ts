import { AppState } from 'react-native';
import { getRandomBytes } from 'expo-crypto';
import TcpSocket from 'react-native-tcp-socket';
import { answerHead, askOf, headOf, piecesOf } from './loopbackHttp';

/**
 * PLAY_PIECE is how much of a file one call to the instance fetches: a few
 * seconds of a film at once, well inside the 4 MiB the range route sends.
 */
export const PLAY_PIECE = 2 << 20;

// A player keeps its connection open while it plays, even paused, and opens a
// new one to seek. Once none has been open for this long, it is done.
const IDLE_MS = 2 * 60_000;

// A player's request head fits in a few hundred bytes. More without the end of
// the head is no player.
const MAX_HEAD = 16 << 10;

// Writes go over the bridge to the native socket in pieces this large, so a
// piece of the file does not sit in one message.
const WRITE_BYTES = 256 << 10;

/** The file a player plays: how long it is, what it is, and its bytes. */
export interface PlaySource {
  size: number;
  type: string;
  read(from: number, length: number): Promise<Uint8Array>;
}

/** An open way in for a player: where it reads, and how to close it early. */
export interface Loopback {
  url: string;
  close(): void;
}

type Socket = ReturnType<typeof TcpSocket.createConnection>;

/**
 * openLoopback serves source to a media player on 127.0.0.1, at a random port
 * and a path made once for it, which is the only path it answers. Every range
 * the player asks for is fetched from the instance in pieces. It closes once
 * no player has been connected for a while, and when the app comes back to
 * the front with none connected, since the player is then done.
 */
export function openLoopback(source: PlaySource): Promise<Loopback> {
  const path = '/' + Array.from(getRandomBytes(16), (b) => b.toString(16).padStart(2, '0')).join('');
  const sockets = new Set<Socket>();
  let idle: ReturnType<typeof setTimeout> | null = null;
  let closed = false;

  const server = TcpSocket.createServer((socket) => {
    sockets.add(socket);
    if (idle) clearTimeout(idle);
    idle = null;
    socket.on('close', () => {
      sockets.delete(socket);
      if (sockets.size === 0) waitForNext();
    });
    socket.on('error', () => socket.destroy());
    serve(socket);
  });

  const close = () => {
    if (closed) return;
    closed = true;
    if (idle) clearTimeout(idle);
    front.remove();
    for (const s of sockets) s.destroy();
    server.close();
  };
  const waitForNext = () => {
    if (idle) clearTimeout(idle);
    idle = setTimeout(close, IDLE_MS);
  };
  const front = AppState.addEventListener('change', (state) => {
    if (state === 'active' && sockets.size === 0) close();
  });

  const serve = (socket: Socket) => {
    let text = '';
    let answered = false;
    socket.on('data', (chunk) => {
      if (answered) return;
      text += typeof chunk === 'string' ? chunk : chunk.toString('latin1');
      const head = headOf(text);
      if (head === null && text.length <= MAX_HEAD) return;
      answered = true;
      void answer(socket, head === null ? 400 : askOf(head, path, source.size));
    });
  };

  const answer = async (socket: Socket, asked: ReturnType<typeof askOf>) => {
    let gone = false;
    socket.on('close', () => {
      gone = true;
    });
    socket.write(answerHead(asked, source.size, source.type), 'latin1');
    if (typeof asked === 'number' || asked.method === 'HEAD') {
      socket.end();
      return;
    }
    const pieces = piecesOf(asked.start, asked.end, PLAY_PIECE);
    // One piece ahead, so the player is not left waiting for a whole call
    // between two pieces.
    const fetch = (i: number) => {
      const read = source.read(...pieces[i]);
      // A piece fetched ahead for a player that has gone is never awaited.
      read.catch(() => {});
      return read;
    };
    let next = fetch(0);
    try {
      for (let i = 0; i < pieces.length && !gone; i++) {
        const data = await next;
        if (i + 1 < pieces.length) next = fetch(i + 1);
        for (let at = 0; at < data.length && !gone; at += WRITE_BYTES) {
          if (!socket.write(data.subarray(at, at + WRITE_BYTES))) {
            await new Promise<void>((resolve) => {
              socket.once('drain', resolve);
              socket.once('close', () => resolve());
            });
          }
        }
      }
      socket.end();
    } catch {
      // A piece the instance did not send ends the answer short, and the
      // player asks again from where it stopped.
      socket.destroy();
    }
  };

  return new Promise((resolve, reject) => {
    server.on('error', (err) => {
      close();
      reject(err);
    });
    server.listen({ port: 0, host: '127.0.0.1' }, () => {
      const port = server.address()?.port;
      if (!port) {
        close();
        reject(new Error('the player could not be given a port'));
        return;
      }
      waitForNext();
      resolve({ url: `http://127.0.0.1:${port}${path}`, close });
    });
  });
}
