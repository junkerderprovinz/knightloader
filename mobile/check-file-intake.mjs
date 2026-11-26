// A file picked on the Add links screen goes where the web UI's file drop
// sends it: a .torrent to the torrent routes, which show a torrent of several
// files for a choice before anything is staged, and a link container or an
// .nzb to the container route. The routes are the ones that take the file as
// JSON, since the group carries a call's body as text.
//
// Only a file that looks like a torrent is parsed first, so a container is
// not sent twice. A file larger than one relay frame carries is refused on the
// phone with the most it may be, and an instance from before these routes says
// to update it instead of showing a bare 404. Every language words the new
// lines with the placeholders English has.
//
// The check renders src/components/AddFileSection.tsx on stand-ins for React,
// React Native, the picker and the file system, with src/api/fileIntake.ts
// compiled against a stand-in for the calls to the instance.
//
// Run by hand and by CI, from mobile/: `node check-file-intake.mjs`
import { readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const { en } = await import(pathToFileURL(join(here, 'src', 'i18n', 'en.ts')).href);
const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};
const t = (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]);

class ApiError extends Error {
  constructor(message, status, code) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

// The instance: each call is recorded, and answer decides what comes back.
let calls = [];
let answer = () => ({});
const client = {
  ApiError,
  errorText: (_t, e) => (e instanceof Error ? e.message : String(e)),
  request: async (_conn, base, path, init) => {
    const body = init?.body ? JSON.parse(init.body) : undefined;
    calls.push({ base, path, body });
    const r = answer(path, body);
    if (r instanceof Error) throw r;
    return r;
  },
};
const base64 = compile(join(here, 'src', 'api', 'base64.ts'), {});
const fmtBytes = (n) => `${(n / (1 << 20)).toFixed(1)} MB`;
const intake = compile(join(here, 'src', 'api', 'fileIntake.ts'), {
  './client': client,
  './base64': base64,
  './stats': { fmtBytes },
});

// The phone: what the picker hands over, and the files in its cache.
let picked = { canceled: true, assets: null };
const disk = new Map();
const deleted = [];
class File {
  constructor(uri) {
    this.uri = uri;
  }
  async base64() {
    if (!disk.has(this.uri)) throw new Error('gone');
    return disk.get(this.uri);
  }
  delete() {
    deleted.push(this.uri);
  }
}
const encode = (text) => base64.base64Encode([...Buffer.from(text, 'latin1')]);
const pick = (...files) => {
  picked = {
    canceled: false,
    assets: files.map((f, i) => {
      const uri = `file:///cache/${i}-${f.name}`;
      if (f.bytes !== undefined) disk.set(uri, encode(f.bytes));
      return { name: f.name, size: f.size ?? f.bytes?.length, uri };
    }),
  };
};

const { AddFileSection } = compile(join(here, 'src', 'components', 'AddFileSection.tsx'), {
  'react-native': {
    ActivityIndicator: 'ActivityIndicator',
    ScrollView: 'ScrollView',
    View: 'View',
    StyleSheet: { create: (s) => s },
  },
  'expo-document-picker': { getDocumentAsync: async () => picked },
  'expo-file-system': { File },
  '../api/fileIntake': intake,
  '../api/stats': { fmtBytes },
  '../theme/AppearanceContext': { useAppearance: () => ({ c: {}, corners: {} }) },
  '../theme/tokens': { NUM: {}, TYPE: {} },
  '../i18n/I18nContext': { useT: () => ({ t }) },
  './glim': { GlimButton: 'GlimButton', GlimToggle: 'GlimToggle' },
  './IconBadge': { Check: 'Check', Cross: 'Cross', Folder: 'Folder', Plus: 'Plus' },
  './InfoTip': { InfoTip: 'InfoTip' },
  './Text': { Text: 'Text' },
});

let added = 0;
const conn = { id: 'c1' };
const section = mount(() => AddFileSection({ conn, base: '/api', pkg: 'Films', onAdded: () => added++ }));
const button = (label) => section.shown().elements.find((e) => e.type === 'GlimButton' && e.props.label === label)?.props;
const toggles = () => section.shown().elements.filter((e) => e.type === 'GlimToggle').map((e) => e.props);
const said = () => section.shown().words.join(' ');
const choose = async () => {
  calls = [];
  button(t('addFile.choose')).onPress();
  await section.settle();
};

// A bencoded torrent: "4:info" after the announce list, as the keys sort.
const torrentBytes = 'd8:announce30:udp://tracker.example:6969/ann4:infod4:name4:Packee';
const tree = (files) => ({ uri: 'data:application/x-bittorrent;base64,AAAA', name: 'Pack', private: true, totalSize: 0, files });

expect('a file named .torrent looks like one', intake.looksLikeTorrent('a.torrent', ''), true);
expect('bencoded bytes look like a torrent whatever the name', intake.looksLikeTorrent('a.bin', encode(torrentBytes)), true);
expect('a DLC does not', intake.looksLikeTorrent('a.dlc', encode('ZGxjIGNvbnRlbnQ=')), false);
expect('a dictionary without an info key does not', intake.looksLikeTorrent('a.txt', encode('d3:foo3:bare')), false);
expect('an .nzb never does', intake.looksLikeTorrent('a.nzb', encode(torrentBytes)), false);

await section.settle();
expect('the section offers a file', !!button(t('addFile.choose')), true);

// A container goes once, to the container route, with the package.
answer = (path) => (path === '/containers/phone' ? { kind: 'dlc', links: 3, created: [{ package: 'Films' }, { package: 'Films' }] } : {});
pick({ name: 'release.dlc', bytes: 'ZGxjIGNvbnRlbnQ=' });
await choose();
expect('a container goes to the container route alone', calls.map((c) => c.path), ['/containers/phone']);
expect('with its name, its bytes and the package', calls[0]?.body, {
  name: 'release.dlc',
  data: encode('ZGxjIGNvbnRlbnQ='),
  package: 'Films',
});
expect(
  'the line says what was staged and what was known',
  said().includes(`${t('addFile.stagedIn', { n: 2, file: 'release.dlc', pkg: 'Films' })} ${t('addFile.alsoKnown', { n: 1 })}`),
  true,
);
expect('the picked copy is cleared from the cache', deleted.includes('file:///cache/0-release.dlc'), true);
expect('something reaching the instance is reported', added, 1);

// A torrent of several files is shown before anything is staged.
answer = (path) => {
  if (path === '/torrents/parse/phone') {
    return tree([
      { path: 'film.mkv', size: 900, selected: true },
      { path: 'sample.mkv', size: 50, selected: false },
    ]);
  }
  if (path === '/torrents') return { id: 't1', package: 'Films' };
  return {};
};
pick({ name: 'pack.torrent', bytes: torrentBytes });
await choose();
expect('a .torrent is parsed and nothing staged yet', calls.map((c) => c.path), ['/torrents/parse/phone']);
expect(
  'the review shows the files and the rules choice',
  [said().includes('film.mkv'), said().includes('sample.mkv'), toggles().map((x) => x.value)],
  [true, true, [true, false]],
);
expect('the summary counts the selection', said().includes(t('addFile.treeSummary', { n: 1, total: 2, size: fmtBytes(900) })), true);
expect('a private torrent says so', said().includes(t('addFile.treePrivate')), true);
calls = [];
button(t('addFile.treeAdd')).onPress();
await section.settle();
expect('the selection the rules made is left to them', calls.map((c) => [c.path, c.body?.selectedPaths]), [['/torrents', undefined]]);
expect('the staged line names the package', said().includes(t('addFile.torrentStagedIn', { file: 'pack.torrent', pkg: 'Films' })), true);

pick({ name: 'pack.torrent', bytes: torrentBytes });
await choose();
toggles()[1].onChange(true);
await section.settle();
calls = [];
button(t('addFile.treeAdd')).onPress();
await section.settle();
expect('a choice by hand is sent as it is', calls[0]?.body?.selectedPaths, ['film.mkv', 'sample.mkv']);

pick({ name: 'pack.torrent', bytes: torrentBytes });
await choose();
calls = [];
button(t('addDownload.cancel')).onPress();
await section.settle();
expect('cancelling the review stages nothing', [calls.length, said().includes('film.mkv')], [0, false]);

pick({ name: 'pack.torrent', bytes: torrentBytes });
await choose();
button(t('addFile.selectNone')).onPress();
await section.settle();
expect('with every file off the add button is off', button(t('addFile.treeAdd'))?.disabled, true);
button(t('addDownload.cancel')).onPress();
await section.settle();

// A torrent of one file is staged at once.
answer = (path) => (path === '/torrents/parse/phone' ? tree([{ path: 'one.iso', size: 9, selected: true }]) : null);
pick({ name: 'one.torrent', bytes: torrentBytes });
await choose();
expect('a torrent of one file is staged without a review', calls.map((c) => c.path), ['/torrents/parse/phone', '/torrents']);
expect('one already in the list says so', said().includes(t('addFile.torrentDuplicate', { file: 'one.torrent' })), true);

// A .torrent that does not parse says why, rather than what the container route makes of it.
answer = (path) => (path === '/torrents/parse/phone' ? new ApiError('the info dictionary is missing', 400) : {});
pick({ name: 'broken.torrent', bytes: torrentBytes });
await choose();
expect('a broken .torrent is not tried as a container', calls.map((c) => c.path), ['/torrents/parse/phone']);
expect(
  'and keeps the parser reason',
  said().includes(t('addFile.failed', { file: 'broken.torrent', reason: 'the info dictionary is missing' })),
  true,
);

// What the phone refuses itself, and what an instance refuses with a code.
pick({ name: 'huge.nzb', size: intake.MAX_FILE_BYTES + 1 });
await choose();
expect('a file larger than a frame carries is not sent', calls.length, 0);
expect(
  'and the line says how much may go',
  said().includes(t('addFile.failed', { file: 'huge.nzb', reason: t('addFile.tooBig', { max: fmtBytes(intake.MAX_FILE_BYTES) }) })),
  true,
);

pick({ name: 'lost.dlc' });
await choose();
expect('a file the app cannot read is not sent', [calls.length, said().includes(t('addFile.unreadable'))], [0, true]);

answer = () => new ApiError('no such endpoint: POST /api/containers/phone', 404);
pick({ name: 'release.ccf', bytes: 'CCF' });
await choose();
expect('an instance from before the routes asks to be updated', said().includes(t('addFile.tooOld')), true);

answer = () => new ApiError('no backend', 503, 'noJD');
pick({ name: 'release.ccf', bytes: 'CCF' });
await choose();
expect('an encrypted container with no JD says it as the web does', said().includes(t('addFile.noJD')), true);

answer = () => ({ kind: 'nzb', handedTo: 'usenet', service: 'TorBox', own: false });
pick({ name: 'release.nzb', bytes: '<?xml version="1.0"?><nzb/>' });
await choose();
expect('an .nzb goes to the container route alone', calls.map((c) => c.path), ['/containers/phone']);
expect('and its line names the service', said().includes(t('addFile.usenet', { file: 'release.nzb', service: 'TorBox' })), true);

// Two torrents to review at once: the second waits for another pick.
answer = (path) =>
  path === '/torrents/parse/phone'
    ? tree([
        { path: 'a', size: 1, selected: true },
        { path: 'b', size: 1, selected: true },
      ])
    : {};
pick({ name: 'one.torrent', bytes: torrentBytes }, { name: 'two.torrent', bytes: torrentBytes });
await choose();
expect('only the first of two torrents is parsed', calls.length, 1);
expect('the second says why it waits', said().includes(t('addFile.failed', { file: 'two.torrent', reason: t('addFile.onlyOne') })), true);
section.unmount();

// The largest file the app sends fits one relay frame: its body is the file
// as base64 inside JSON, which the frame codes as base64 twice more, 16/9 of
// it, with 4 KiB for the seal and the envelope (kit/relay's readLimit).
const body = JSON.stringify({ name: 'x'.repeat(255), data: 'A'.repeat(Math.ceil(intake.MAX_FILE_BYTES / 3) * 4), package: 'p'.repeat(255) });
expect('the largest file fits a relay frame', (body.length * 16) / 9 + 4096 <= 16 << 20, true);

// Every language keeps the placeholders the English lines have.
const marks = (s) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
for (const f of readdirSync(join(here, 'src', 'i18n')).filter((f) => /^[a-z]{2}\.ts$/.test(f))) {
  const lang = f.slice(0, 2);
  const dict = (await import(pathToFileURL(join(here, 'src', 'i18n', f)).href))[lang];
  for (const key of Object.keys(en).filter((k) => k.startsWith('addFile.'))) {
    expect(`${lang} ${key} placeholders`, marks(dict[key] ?? ''), marks(en[key]));
  }
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log(
  'ok: a picked container goes once to the container route, a torrent of several files is reviewed before staging, the refusals say why, and the largest file fits a relay frame',
);
