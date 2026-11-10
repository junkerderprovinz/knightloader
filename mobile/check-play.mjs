// Which links get a Play button, and as what kind of player. A torrent of
// several files plays the one file the instance picked, and a download that
// stopped halfway has nothing the instance will serve.
//
// src/api/media.ts imports nothing at run time, so node strips its types and
// these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-play.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { mediaKind, hasSomethingToPlay } = await import(pathToFileURL(join(here, 'src', 'api', 'media.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  if (got !== want) problems.push(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};

expect('a film', mediaKind({ name: 'film.MKV' }), 'video');
expect('a song', mediaKind({ name: 'song.flac' }), 'audio');
expect('an installer', mediaKind({ name: 'setup.exe' }), null);
expect('a season pack', mediaKind({ name: 'Season 1', torrentFileCount: 12, torrentMedia: 'video' }), 'video');
expect('an album plays as audio', mediaKind({ name: 'Album', torrentFileCount: 12, torrentMedia: 'audio' }), 'audio');
expect('a torrent without audio or video', mediaKind({ name: 'Software Pack', torrentFileCount: 12 }), null);
expect('a torrent folder named like a film', mediaKind({ name: 'Folder.mkv', torrentFileCount: 3 }), null);

expect('a running download', hasSomethingToPlay({ status: 'running' }), true);
expect('a finished download', hasSomethingToPlay({ status: 'done' }), true);
expect('a download paused halfway', hasSomethingToPlay({ status: 'paused', loaded: 5, size: 10 }), false);
expect('a download that failed halfway', hasSomethingToPlay({ status: 'error', loaded: 5, size: 10 }), false);

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('Play buttons check out.');
