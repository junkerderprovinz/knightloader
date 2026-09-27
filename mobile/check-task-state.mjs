// The word a row and a package header show, from the status the instance
// sends. The instance says "done" and "error", and a package that holds a
// failed link or archive must not read as finished, nor hide the failure
// behind a link that is still running.
//
// src/api/taskState.ts imports nothing at run time, so node strips its types
// and these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-task-state.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { rowWord, packageState, isParked } = await import(pathToFileURL(join(here, 'src', 'api', 'taskState.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const task = (status, more = {}) => ({ status, ...more });

expect('a finished download', rowWord(task('done')), 'finished');
expect('a failed download', rowWord(task('error')), 'failed');
expect('a finished download whose archive did not unpack', rowWord(task('done', { unpack: 'error' })), 'notUnpacked');
expect('one that wants a password', rowWord(task('done', { unpack: 'password' })), 'notUnpacked');
expect('a staged link', rowWord(task('collected')), 'collected');
expect('a status from a newer instance', rowWord(task('somethingNew')), null);

expect(
  'a package still running reads as running and counts its failure',
  packageState([task('done'), task('error'), task('running')]),
  { word: 'running', failed: 1 },
);
expect('a package with a dead link and nothing going on has failed', packageState([task('done'), task('error')]), {
  word: 'failed',
  failed: 1,
});
expect(
  'a package whose archive did not unpack, counted once for its two parts',
  packageState([task('done', { unpack: 'error', archivePart: 1 }), task('done', { unpack: 'error', archivePart: 2 })]),
  { word: 'notUnpacked', failed: 1 },
);
expect(
  'a failed archive beside a download still queued',
  packageState([task('done', { unpack: 'error' }), task('queued')]),
  { word: 'queued', failed: 1 },
);
expect('a package that is fine', packageState([task('done', { unpack: 'done' }), task('done')]), {
  word: 'finished',
  failed: 0,
});

const seeding = (more = {}) => task('done', { seeding: true, ...more });
expect('a finished torrent still uploading', rowWord(seeding()), 'seeding');
expect('a torrent that stopped seeding', rowWord(task('done', { seeding: false })), 'finished');
expect('a seeding torrent whose archive did not unpack', rowWord(seeding({ unpack: 'error' })), 'notUnpacked');
expect('a package with a torrent still seeding', packageState([task('done'), seeding()]), {
  word: 'seeding',
  failed: 0,
});
expect('a package that still owes a download beside a seeding torrent', packageState([seeding(), task('queued')]), {
  word: 'queued',
  failed: 0,
});
expect('a seeding torrent beside a dead link', packageState([seeding(), task('error')]), {
  word: 'seeding',
  failed: 1,
});
expect(
  'a seeding torrent beside an archive that did not unpack',
  packageState([seeding(), task('done', { unpack: 'error' })]),
  { word: 'notUnpacked', failed: 1 },
);

expect('a disabled link is parked', isParked([task('queued', { enabled: false })]), true);
expect('an enabled link is not', isParked([task('queued', { enabled: true })]), false);
expect(
  'a package is parked only when every link in it is disabled',
  [isParked([task('queued', { enabled: false }), task('done', { enabled: true })]), isParked([])],
  [false, false],
);

if (problems.length > 0) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('task and package words hold');
