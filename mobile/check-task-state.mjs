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
const { rowWord, packageState, isParked, packageCard, splitByCard, unpackingByTask, unpackProgress, unpackPercent } = await import(pathToFileURL(join(here, 'src', 'api', 'taskState.ts')).href);

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
expect('a download under way', rowWord(task('running', { resolver: 'direct' })), 'running');
expect('a torrent still downloading', rowWord(task('running', { resolver: 'torrent' })), 'leeching');
expect('a torrent a debrid service fetches', rowWord(task('running', { resolver: 'realdebrid' })), 'running');
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

// The part of the list a package is shown in, by the web's rule.
expect('a package whose links have all downloaded is finished', packageCard([task('done'), task('done', { unpack: 'done' })]), 'finished');
expect('a package with a link still queued stays in the download list', packageCard([task('done'), task('queued')]), 'downloads');
expect('a package with an archive still unpacking stays', packageCard([task('done'), task('extracting')]), 'downloads');
expect('a package whose archive did not unpack stays', packageCard([task('done', { unpack: 'password' })]), 'downloads');
expect('a package with a failed link stays', packageCard([task('done'), task('error')]), 'downloads');
const torrent = (status, more = {}) => task(status, { resolver: 'torrent', ...more });
expect('a finished package holding a torrent goes to Torrents', packageCard([task('done'), torrent('done', { seeding: true })]), 'torrents');
expect('a torrent stays in Torrents once it stopped seeding', packageCard([torrent('done', { seeding: false })]), 'torrents');
expect('a torrent still downloading stays in the download list', packageCard([torrent('running')]), 'downloads');
expect('a torrent a debrid service fetched is an ordinary link', packageCard([task('done', { resolver: 'realdebrid' })]), 'finished');
expect('a torrent downloaded again goes back to the download list', packageCard([torrent('queued')]), 'downloads');
expect(
  'a disabled link that never downloaded does not hold a package back',
  packageCard([task('done'), task('queued', { enabled: false })]),
  'finished',
);
expect('a disabled link still running does', packageCard([task('done'), task('running', { enabled: false })]), 'downloads');
expect('a package switched off whole stays', packageCard([task('queued', { enabled: false })]), 'downloads');
expect('a package switched off whole stays though part of it downloaded', packageCard([task('done', { enabled: false }), task('queued', { enabled: false })]), 'downloads');
expect(
  'a finished package goes back to the download list when a link is downloaded again',
  packageCard([task('done'), task('queued')]),
  'downloads',
);

const named = (id, pkg, status, more = {}) => ({ id, package: pkg, status, ...more });
const split = (list, on) =>
  Object.fromEntries(Object.entries(splitByCard(list, on)).map(([card, tasks]) => [card, tasks.map((t) => t.id)]));
const mixed = [
  named('a', 'Done', 'done'),
  named('b', 'Busy', 'running'),
  named('c', 'Upload', 'done', { resolver: 'torrent', seeding: true }),
  named('d', '', 'done'),
  named('e', '', 'queued'),
];
expect('the list splits into its three parts, loose links one by one', split(mixed, { finished: true, torrents: true }), {
  downloads: ['b', 'e'],
  finished: ['a', 'd'],
  torrents: ['c'],
});
expect('with Torrents switched off a torrent is finished like any other download', split(mixed, { finished: true, torrents: false }), {
  downloads: ['b', 'e'],
  finished: ['a', 'c', 'd'],
  torrents: [],
});
expect('with both parts switched off everything stays in the download list', split(mixed, { finished: false, torrents: false }), {
  downloads: ['a', 'b', 'c', 'd', 'e'],
  finished: [],
  torrents: [],
});

// While an archive unpacks, the bar on each of its parts shows how far it has
// got, as on the web and in JDownloader.
const job = (id, status, parts, more = {}) => ({ id, taskId: parts[0], status, parts, ...more });
const unpackingOf = (id, status, jobs) => unpackProgress({ id, status }, unpackingByTask(jobs));
const running = job('j', 'running', ['a1', 'a2'], { unpacked: 400, size: 1000 });
expect('the part the unpacking started on', unpackingOf('a1', 'extracting', [running]), {
  unpacked: 400,
  size: 1000,
  failed: false,
});
expect('another part of the same set', unpackingOf('a2', 'done', [running]), { unpacked: 400, size: 1000, failed: false });
expect('a file outside the set', unpackingOf('b', 'done', [running]), null);
expect('a part downloading again', unpackingOf('a2', 'running', [running]), null);
expect('an archive waiting for its turn', unpackingOf('a1', 'extracting', [job('j', 'queued', ['a1'])]), null);
expect('an archive unpacked', unpackingOf('a1', 'done', [job('j', 'done', ['a1'], { unpacked: 9, size: 9 })]), null);
expect(
  'a failed unpacking keeps where it stopped',
  unpackingOf('a2', 'done', [job('j', 'error', ['a1', 'a2'], { unpacked: 250, size: 1000 })]),
  { unpacked: 250, size: 1000, failed: true },
);
expect(
  'a retry replaces the failure before it',
  unpackingOf('a1', 'extracting', [job('old', 'error', ['a1'], { unpacked: 5, size: 10 }), job('new', 'running', ['a1'])]),
  { unpacked: 0, size: 0, failed: false },
);
expect('a cancelled unpacking hands the row back', unpackingOf('a1', 'done', [running, job('k', 'cancelled', ['a1', 'a2'])]), null);
expect('an older server names only the first volume', unpackingOf('a1', 'extracting', [{ id: 'j', taskId: 'a1', status: 'running' }]), {
  unpacked: 0,
  size: 0,
  failed: false,
});
expect('the share done', unpackPercent({ unpacked: 400, size: 1000, failed: false }), 40);
expect('no share without a size', unpackPercent({ unpacked: 400, size: 0, failed: false }), null);

if (problems.length > 0) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('task and package words hold');
