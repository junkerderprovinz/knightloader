// A player reaches a file the phone plays through a server on 127.0.0.1 that
// answers one path and nothing else, and a player that seeks asks for a range
// of the file, which the server fetches from the instance in pieces.
//
// src/play/loopbackHttp.ts imports nothing at run time, so node strips its
// types and these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-loopback.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { headOf, askOf, answerHead, piecesOf } = await import(pathToFileURL(join(here, 'src', 'play', 'loopbackHttp.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const path = '/5f2c9a71d0b3e48a';
const req = (line, ...headers) => [line, 'Host: 127.0.0.1:41234', ...headers, '', ''].join('\r\n');

expect('a head not yet complete', headOf('GET / HTTP/1.1\r\nHost: x\r\n'), null);
expect('a complete head', headOf('GET / HTTP/1.1\r\nHost: x\r\n\r\nrest'), 'GET / HTTP/1.1\r\nHost: x');

expect('the whole file', askOf(req(`GET ${path} HTTP/1.1`), path, 1000), { method: 'GET', start: 0, end: 999, partial: false });
expect('a head request', askOf(req(`HEAD ${path} HTTP/1.1`), path, 1000), { method: 'HEAD', start: 0, end: 999, partial: false });
expect('a range from a byte to a byte', askOf(req(`GET ${path} HTTP/1.1`, 'Range: bytes=100-199'), path, 1000), { method: 'GET', start: 100, end: 199, partial: true });
expect('a range to the end', askOf(req(`GET ${path} HTTP/1.1`, 'range: bytes=900-'), path, 1000), { method: 'GET', start: 900, end: 999, partial: true });
expect('the last bytes', askOf(req(`GET ${path} HTTP/1.1`, 'Range: bytes=-10'), path, 1000), { method: 'GET', start: 990, end: 999, partial: true });
expect('a range over the end stops there', askOf(req(`GET ${path} HTTP/1.1`, 'Range: bytes=990-5000'), path, 1000), { method: 'GET', start: 990, end: 999, partial: true });
expect('a range past the end', askOf(req(`GET ${path} HTTP/1.1`, 'Range: bytes=1000-'), path, 1000), 416);
expect('a range that does not read is the whole file', askOf(req(`GET ${path} HTTP/1.1`, 'Range: lines=1-2'), path, 1000), { method: 'GET', start: 0, end: 999, partial: false });
expect('several ranges are the first', askOf(req(`GET ${path} HTTP/1.1`, 'Range: bytes=0-9, 20-29'), path, 1000), { method: 'GET', start: 0, end: 9, partial: true });
expect('another path', askOf(req('GET /other HTTP/1.1'), path, 1000), 404);
expect('the path with a query', askOf(req(`GET ${path}?x=1 HTTP/1.1`), path, 1000), { method: 'GET', start: 0, end: 999, partial: false });
expect('a path that only starts like it', askOf(req(`GET ${path}0 HTTP/1.1`), path, 1000), 404);
expect('a write', askOf(req(`POST ${path} HTTP/1.1`), path, 1000), 405);
expect('no request line', askOf('nonsense', path, 1000), 400);

expect(
  'the whole file',
  answerHead({ method: 'GET', start: 0, end: 999, partial: false }, 1000, 'video/mp4'),
  'HTTP/1.1 200 OK\r\nContent-Type: video/mp4\r\nContent-Length: 1000\r\nAccept-Ranges: bytes\r\nConnection: close\r\n\r\n',
);
expect(
  'a range',
  answerHead({ method: 'GET', start: 100, end: 199, partial: true }, 1000, 'video/mp4'),
  'HTTP/1.1 206 Partial Content\r\nContent-Type: video/mp4\r\nContent-Length: 100\r\nContent-Range: bytes 100-199/1000\r\nAccept-Ranges: bytes\r\nConnection: close\r\n\r\n',
);
expect('a refusal', answerHead(404, 1000, 'video/mp4'), 'HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n');
expect('a range past the end', answerHead(416, 1000, 'video/mp4'), 'HTTP/1.1 416 Range Not Satisfiable\r\nContent-Range: bytes */1000\r\nContent-Length: 0\r\nConnection: close\r\n\r\n');

expect('pieces of a range', piecesOf(100, 349, 100), [[100, 100], [200, 100], [300, 50]]);
expect('one short piece', piecesOf(5, 5, 100), [[5, 1]]);

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: the player gets its one path, in the range it asked for, fetched in pieces');
