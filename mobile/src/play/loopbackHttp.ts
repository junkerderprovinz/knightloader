// The HTTP the player speaks to the server on 127.0.0.1 (loopback.ts): one
// request per connection, the head read, the range it asks for, and the head
// of the answer. Kept free of the socket so check-loopback.mjs runs it.

/** What a player asked for: the first and last byte, both included. */
export interface Asked {
  method: 'GET' | 'HEAD';
  start: number;
  end: number;
  /** Whether it asked for a range, which is answered with a 206. */
  partial: boolean;
}

/** headOf is the request head in text, up to the blank line, or null while it has not all arrived. */
export function headOf(text: string): string | null {
  const end = text.indexOf('\r\n\r\n');
  return end < 0 ? null : text.slice(0, end);
}

/**
 * askOf reads a request head for the one path the server answers, against a
 * file of size bytes. A number is the status to refuse it with.
 */
export function askOf(head: string, path: string, size: number): Asked | number {
  const [line, ...headers] = head.split('\r\n');
  const parts = line.split(' ');
  if (parts.length !== 3 || !parts[2].startsWith('HTTP/')) return 400;
  const [method, target] = parts;
  const q = target.indexOf('?');
  if ((q < 0 ? target : target.slice(0, q)) !== path) return 404;
  if (method !== 'GET' && method !== 'HEAD') return 405;
  const whole: Asked = { method, start: 0, end: size - 1, partial: false };
  const range = headers.find((h) => h.toLowerCase().startsWith('range:'));
  if (!range) return whole;
  // Only the first of several ranges is sent: a player asks for one.
  const m = /^bytes=(\d*)-(\d*)/.exec(range.slice('range:'.length).trim());
  if (!m || (m[1] === '' && m[2] === '')) return whole;
  if (m[1] === '') {
    const last = Math.min(Number(m[2]), size);
    return { method, start: size - last, end: size - 1, partial: true };
  }
  const start = Number(m[1]);
  if (start >= size) return 416;
  const end = m[2] === '' ? size - 1 : Math.min(Number(m[2]), size - 1);
  if (end < start) return whole;
  return { method, start, end, partial: true };
}

const REASONS: Record<number, string> = {
  400: 'Bad Request',
  404: 'Not Found',
  405: 'Method Not Allowed',
  416: 'Range Not Satisfiable',
  502: 'Bad Gateway',
};

/** answerHead is the head of the answer to asked, or to a refusal. */
export function answerHead(asked: Asked | number, size: number, type: string): string {
  if (typeof asked === 'number') {
    const range = asked === 416 ? `Content-Range: bytes */${size}\r\n` : '';
    return `HTTP/1.1 ${asked} ${REASONS[asked] ?? 'Error'}\r\n${range}Content-Length: 0\r\nConnection: close\r\n\r\n`;
  }
  const length = asked.end - asked.start + 1;
  const status = asked.partial ? '206 Partial Content' : '200 OK';
  const range = asked.partial ? `Content-Range: bytes ${asked.start}-${asked.end}/${size}\r\n` : '';
  return `HTTP/1.1 ${status}\r\nContent-Type: ${type}\r\nContent-Length: ${length}\r\n${range}Accept-Ranges: bytes\r\nConnection: close\r\n\r\n`;
}

/** piecesOf splits the bytes from start to end, both included, into [from, length] pieces of at most size. */
export function piecesOf(start: number, end: number, size: number): [number, number][] {
  const out: [number, number][] = [];
  for (let from = start; from <= end; from += size) out.push([from, Math.min(size, end - from + 1)]);
  return out;
}
