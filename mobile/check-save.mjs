// Saving a file onto the phone: which rows offer it, and that the copy comes
// a piece at a time, says how far it has got, and leaves nothing behind when
// it is stopped or fails.
//
// src/api/saveCopy.ts imports nothing at run time, so node strips its types
// and these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-save.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const save = await import(pathToFileURL(join(here, 'src', 'api', 'saveCopy.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const x = JSON.stringify(want);
  if (g !== x) problems.push(`${what}: got ${g}, want ${x}`);
};

const task = (extra) => ({ id: 't', url: 'https://host.example/f', name: 'film.mkv', resolver: 'http', status: 'done', ...extra });
expect('a finished file', save.canSave(task({})), true);
expect('a download still running', save.canSave(task({ status: 'running' })), false);
expect('a torrent of several files saves them all', [save.canSave(task({ torrentFileCount: 3 })), save.isFileSet(task({ torrentFileCount: 3 }))], [true, true]);
expect('a torrent of one file', save.canSave(task({ torrentFileCount: 1 })), true);
expect('a file on a JDownloader elsewhere', save.canSave(task({ resolver: 'jd' })), false);
expect('a link never resolved', save.canSave(task({ name: 'https://host.example/f' })), false);
expect('the name on the phone is the file name', save.saveName(task({ name: 'Season 1/e01.mkv' })), 'e01.mkv');
expect('a type keeps no parameters', save.saveType('text/plain; charset=utf-8'), 'text/plain');
expect('a file of no known type', save.saveType(''), 'application/octet-stream');

// The files of a torrent.
{
  const set = task({ name: 'Season 1', torrentFileCount: 3 });
  const files = [
    { path: 'e01.mkv', size: 10, selected: true, done: 10 },
    { path: 'extras/../making of.mkv', size: 5, selected: true },
    { path: 'e02.mkv', size: 8, selected: false },
  ];
  expect('a file it fetched in full can be saved', save.canSaveTorrentFile(set, files[0]), true);
  expect('a file it left out cannot', save.canSaveTorrentFile(set, files[2]), false);
  expect('a file still coming cannot', save.canSaveTorrentFile(set, { ...files[0], done: 4 }), false);
  expect('a running torrent saves no file', save.canSaveTorrentFile({ ...set, status: 'running' }, files[0]), false);
  expect('the whole set goes into a folder of its name, by place in the list', save.torrentItems(set, files), [
    { file: '0', path: ['Season 1', 'e01.mkv'], size: 10, type: '' },
    { file: '1', path: ['Season 1', 'extras', 'making of.mkv'], size: 5, type: '' },
  ]);
  expect('one file goes in by its own name', save.torrentItems(set, files, 1), [
    { file: '1', path: ['making of.mkv'], size: 5, type: '' },
  ]);
}

const file = (size) => Uint8Array.from({ length: size }, (_, i) => i % 251);
const target = () => {
  const t = { written: [], closed: 0, removed: 0 };
  t.write = async (b) => {
    t.written.push(b.length);
  };
  t.close = () => t.closed++;
  t.remove = () => t.removed++;
  return t;
};

// A whole copy.
{
  const bytes = file(10);
  const asked = [];
  const out = target();
  const seen = [];
  await save.copyFile({
    size: 10,
    piece: 4,
    read: async (from, length) => {
      asked.push([from, length]);
      return bytes.subarray(from, from + length);
    },
    target: out,
    signal: new AbortController().signal,
    onProgress: (d) => seen.push(d),
  });
  expect('the file is asked for in pieces', asked, [[0, 4], [4, 4], [8, 2]]);
  expect('each piece is written as it comes', out.written, [4, 4, 2]);
  expect('progress follows the pieces', seen, [4, 8, 10]);
  expect('a finished copy is closed and kept', [out.closed, out.removed], [1, 0]);
}

// A piece shorter than asked moves on from where it ended.
{
  const asked = [];
  await save.copyFile({
    size: 6,
    piece: 4,
    read: async (from, length) => {
      asked.push(from);
      return file(Math.min(length, 3));
    },
    target: target(),
    signal: new AbortController().signal,
    onProgress() {},
  });
  expect('a short piece is followed from its end', asked, [0, 3]);
}

// Stopped halfway.
{
  const stop = new AbortController();
  const out = target();
  let error;
  await save
    .copyFile({
      size: 12,
      piece: 4,
      read: async () => file(4),
      target: out,
      signal: stop.signal,
      onProgress: (d) => d >= 4 && stop.abort(),
    })
    .catch((e) => (error = e));
  expect('a stopped copy says so', error instanceof save.SaveStopped, true);
  expect('a stopped copy leaves nothing behind', [out.closed, out.removed], [1, 1]);
  expect('a stopped copy writes no more', out.written, [4]);
}

// A piece that does not come.
{
  const out = target();
  let error;
  await save
    .copyFile({
      size: 8,
      piece: 4,
      read: async (from) => {
        if (from > 0) throw new Error('relay down');
        return file(4);
      },
      target: out,
      signal: new AbortController().signal,
      onProgress() {},
    })
    .catch((e) => (error = e));
  expect('a failed copy reports why', error?.message, 'relay down');
  expect('a failed copy leaves nothing behind', out.removed, 1);
  const empty = target();
  error = undefined;
  await save
    .copyFile({ size: 4, read: async () => new Uint8Array(0), target: empty, signal: new AbortController().signal, onProgress() {} })
    .catch((e) => (error = e));
  expect('an empty answer ends the copy rather than asking forever', error?.message, 'the instance sent no bytes');
}

// Several files in turn.
{
  const opened = [];
  const seen = [];
  await save.copyAll({
    items: [
      { file: '0', path: ['a'], size: 5, type: '' },
      { file: '1', path: ['b'], size: 3, type: '' },
    ],
    piece: 4,
    read: async (item, from, length) => file(length),
    open: (item) => {
      opened.push(item.file);
      return target();
    },
    signal: new AbortController().signal,
    onProgress: (done, total) => seen.push([done, total]),
  });
  expect('each file of a set is written in turn', opened, ['0', '1']);
  expect('progress counts over the whole set', seen.at(-1), [8, 8]);
  expect('progress never goes back', seen.every(([d], i) => i === 0 || d >= seen[i - 1][0]), true);
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: a file is saved to the phone piece by piece');
