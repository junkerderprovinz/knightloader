// The properties window of a link or a package, and the add screen's options,
// send what changed and nothing else, in the order the web sends it.
//
// A save that sends a box nobody touched would wipe the values a package's
// links disagree on, or send a rename the instance refuses for a name it
// already has. So the window opens with what the links carry, a box they
// disagree on opens empty, a package is renamed before its links change and a
// save that stopped halfway sends only the rest when it is pressed again. A
// folder outside the download folders is refused to a member of the group,
// and the window says so in words that point at the folder rather than at the
// web UI. The options of the add screen reach POST /api/links as the web's
// add form sends them, and with none set the body is the plain paste.
//
// The check compiles src/api/taskEdit.ts, src/components/TaskEdit.tsx and
// src/screens/AddDownloadScreen.tsx with the Babel that Expo already installs
// and renders them on stand-ins for React and React Native, with the instance
// replaced by a recorder of the calls it is sent.
//
// Run by hand and by CI, from mobile/: `node check-task-edit.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const { en } = await import(pathToFileURL(join(here, 'src', 'i18n', 'en.ts')).href);
const refusal = await import(pathToFileURL(join(here, 'src', 'api', 'refusal.ts')).href);
const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};
const t = (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]);

let calls = [];
let refuse = () => null;
let older = false;
const request = async (_conn, base, path, init) => {
  const body = init?.body ? JSON.parse(init.body) : undefined;
  calls.push(init?.method === 'POST' ? { path: base + path, body } : { path: base + path });
  const e = refuse(path, body);
  if (e) throw e;
  if (older && init?.method !== 'POST') throw refusal.refusedCall(404, 'no such endpoint');
  if (path === '/download-folders') return { folders: ['/downloads', '/media/serien'] };
  if (path === '/categories') return { categories: [{ id: 'serien', name: 'Serien' }] };
  if (path === '/queue/priorities') {
    return ['lowest', 'lower', 'low', 'default', 'high', 'higher', 'highest'].map((id, i) => ({ id, value: i - 3 }));
  }
  return path === '/links' ? [{ id: 'new' }] : undefined;
};
const posts = () => calls.filter((c) => c.body);
const ownLogin = () => refusal.refusedCall(403, JSON.stringify({ error: 'sign in there', code: 'ownLoginNeeded' }));

const api = compile(join(here, 'src', 'api', 'taskEdit.ts'), { './client': { request }, './refusal': refusal });

const view = {
  'react-native': {
    Modal: 'Modal',
    ScrollView: 'ScrollView',
    StyleSheet: { create: (s) => s },
    TouchableOpacity: 'TouchableOpacity',
    View: 'View',
    useWindowDimensions: () => ({ width: 400, height: 800 }),
  },
  '../api/client': { ApiError: refusal.ApiError, errorText: refusal.errorText },
  '../api/taskEdit': api,
  '../i18n/I18nContext': { useT: () => ({ t }) },
  '../theme/AppearanceContext': { useAppearance: () => ({ c: {}, corners: {} }) },
  '../theme/MotionContext': { useMotion: () => ({ motion: 'off' }) },
  '../theme/tokens': { TYPE: {} },
};
const edit = compile(join(here, 'src', 'components', 'TaskEdit.tsx'), {
  ...view,
  './glim': { GlimButton: 'GlimButton', NotchCard: 'NotchCard', WellSelector: 'WellSelector' },
  './IconBadge': { Check: 'Check', Cross: 'Cross' },
  './InfoTip': { InfoTip: 'InfoTip' },
  './Moving': { NoArrival: 'NoArrival' },
  './Text': { Text: 'Text', TextInput: 'TextInput' },
});

const conn = { id: 'home', name: 'Home' };
const task = (id, more = {}) => ({ id, url: `https://host.example/${id}`, name: `${id}.mkv`, package: 'Batch', status: 'collected', resolver: 'http', ...more });

/** Opens the window on target and answers what it shows and what it was told. */
async function open(target, packages = ['Batch', 'Other']) {
  const said = { closed: 0, saved: 0 };
  const w = mount(() => edit.TaskEdit({ conn, base: '/api', target, packages, onClose: () => said.closed++, onSaved: () => said.saved++ }));
  await w.settle();
  const of = (type, label) => w.shown().elements.find((e) => e.type === type && (label === undefined || e.props.label === label))?.props;
  return {
    said,
    box: (key) => of(edit.EditBox, t(key)),
    type: async (key, value) => {
      of(edit.EditBox, t(key)).onChange(value);
      await w.settle();
    },
    pick: async (value) => {
      of('WellSelector').onPick(value);
      await w.settle();
    },
    well: () => of('WellSelector'),
    chips: () => w.shown().elements.filter((e) => e.type === edit.PickChips).map((e) => e.props.values),
    save: async () => {
      calls = [];
      await of('GlimButton', t('edit.save')).onPress();
      await w.settle();
    },
    words: () => w.shown().words.join(' '),
  };
}

{
  calls = [];
  const w = await open({ kind: 'link', task: task('a', { dir: '/downloads', comment: 'mine', chunks: 4 }) });
  expect('the window reads the folders and the categories it offers', calls.map((c) => c.path).sort(), ['/api/categories', '/api/download-folders']);
  expect('the boxes open on what the link carries', [w.box('edit.name').value, w.box('edit.package').value, w.box('edit.folder').value, w.box('edit.comment').value, w.box('edit.chunks').value], ['a.mkv', 'Batch', '/downloads', 'mine', '4']);
  expect('the passwords open empty', [w.box('edit.password').value, w.box('edit.linkPassword').value], ['', '']);
  expect('the link is offered the other packages and the download folders', w.chips(), [['Other'], ['/downloads', '/media/serien']]);
  expect('the category opens on none, with the categories beside it', [w.well().value, w.well().options.map((o) => o.label)], ['', [t('edit.categoryNone'), 'Serien']]);
  await w.save();
  expect('a save with nothing changed sends nothing', posts(), []);
  expect('and closes the window', w.said.closed, 1);
}

{
  const w = await open({ kind: 'link', task: task('a') });
  await w.type('edit.name', 'film.mkv');
  await w.type('edit.package', 'Other');
  await w.type('edit.comment', 'from the phone');
  await w.type('edit.password', 'secret');
  await w.pick('serien');
  await w.save();
  expect('a save sends the package, then the options, then the category', posts(), [
    { path: '/api/tasks/package', body: { ids: ['a'], package: 'Other' } },
    { path: '/api/tasks/options', body: { ids: ['a'], name: 'film.mkv', comment: 'from the phone', password: 'secret' } },
    { path: '/api/tasks/category', body: { ids: ['a'], category: 'serien' } },
  ]);
  expect('a save that went through closes the window and has the list pulled', [w.said.closed, w.said.saved], [1, 1]);
}

{
  const w = await open({ kind: 'link', task: task('a') });
  await w.type('edit.name', 'sub/film.mkv');
  await w.save();
  expect('a name with a slash is refused before anything is sent', [posts(), w.words().includes(t('edit.rename.separator'))], [[], true]);
  await w.type('edit.name', 'a.mkv');
  await w.type('edit.chunks', '20');
  await w.save();
  expect('a connection count past 16 is refused before anything is sent', [posts(), w.words().includes(t('edit.chunksRange'))], [[], true]);
}

{
  const w = await open({ kind: 'link', task: task('a', { dir: '/downloads' }) });
  refuse = (path, body) => (path === '/tasks/options' && body.dir ? ownLogin() : null);
  await w.type('edit.package', 'Other');
  await w.type('edit.folder', '/elsewhere');
  await w.save();
  expect('a folder the instance refuses a member is named as the folder', w.words().includes(t('edit.folderRefused')), true);
  expect('and keeps the window open', w.said.closed, 0);
  refuse = () => null;
  await w.type('edit.folder', '/media/serien');
  await w.save();
  expect('pressed again, the save sends only what had not gone through', posts(), [
    { path: '/api/tasks/options', body: { ids: ['a'], dir: '/media/serien' } },
  ]);
}

{
  const w = await open({ kind: 'link', task: task('a') });
  refuse = (path) =>
    path === '/tasks/options'
      ? refusal.refusedCall(400, JSON.stringify({ error: 'exists', code: 'exists', params: { name: 'b.mkv' } }))
      : null;
  await w.type('edit.name', 'b.mkv');
  await w.save();
  refuse = () => null;
  expect('a refused rename says why, with the name it met', w.words().includes(t('edit.rename.exists', { name: 'b.mkv' })), true);
}

{
  const w = await open({ kind: 'link', task: task('a', { infoHash: 'abc', status: 'running' }) });
  expect('a torrent offers no name box and says why', [w.box('edit.name'), w.words().includes(t('edit.rename.torrent'))], [undefined, true]);
}

{
  const tasks = [
    task('a', { dir: '/downloads', comment: 'same', category: 'serien' }),
    task('b', { dir: '/media/serien', comment: 'same' }),
  ];
  const w = await open({ kind: 'package', name: 'Batch', tasks });
  expect('a box the links disagree on opens empty and says so', [w.box('edit.packageName').value, w.box('edit.folder').value, w.box('edit.folder').placeholder, w.box('edit.comment').value], ['Batch', '', t('edit.mixed'), 'same']);
  expect('a package has no box to move it into another', w.box('edit.package'), undefined);
  expect('a category the links disagree on is none of the choices', w.well().options.some((o) => o.value === w.well().value), false);
  await w.save();
  expect('a package saved unchanged sends nothing, its mixed values kept', posts(), []);
}

{
  const tasks = [task('a'), task('b')];
  const w = await open({ kind: 'package', name: 'Batch', tasks });
  await w.type('edit.packageName', 'Films');
  await w.type('edit.chunks', '2');
  await w.save();
  expect('a package is renamed with its folder, then every link changes', posts(), [
    { path: '/api/tasks/package/rename', body: { ids: ['a', 'b'], name: 'Films' } },
    { path: '/api/tasks/options', body: { ids: ['a', 'b'], chunks: 2 } },
  ]);
}

{
  const tasks = [task('a', { package: '' }), task('b', { package: '' })];
  const w = await open({ kind: 'package', name: '', tasks });
  await w.type('edit.packageName', 'Fresh');
  await w.save();
  expect('links outside any package are put into the one named', posts(), [
    { path: '/api/tasks/package', body: { ids: ['a', 'b'], package: 'Fresh' } },
  ]);
}

{
  older = true;
  calls = [];
  const w = await open({ kind: 'link', task: task('a') });
  older = false;
  expect('an instance without the lists leaves the folder box to be typed into and offers no category', [w.chips(), w.well()], [[['Other'], []], undefined]);
}

expect('no options is the plain paste', JSON.parse(JSON.stringify(api.addLinksBody(['https://a', 'https://b'], api.NO_ADD_OPTIONS))), { links: 'https://a\nhttps://b' });
expect('no options counts as none set', api.addOptionsSet(api.NO_ADD_OPTIONS), false);
const options = { ...api.NO_ADD_OPTIONS, package: ' Pack ', dir: '/downloads', category: 'serien', priority: '2', autoExtract: 'off', password: 'pw', overrule: true };
expect('the options go out as the web add form sends them', JSON.parse(JSON.stringify(api.addLinksBody(['https://a'], options))), {
  links: 'https://a',
  package: 'Pack',
  dir: '/downloads',
  category: 'serien',
  priority: 2,
  autoExtract: false,
  password: 'pw',
  overrule: true,
});
expect('a comment alone counts as set', api.addOptionsSet({ ...api.NO_ADD_OPTIONS, comment: 'x' }), true);

const add = compile(join(here, 'src', 'screens', 'AddDownloadScreen.tsx'), {
  ...view,
  '../components/glim': { CardButton: 'CardButton', GlimButton: 'GlimButton', GlimRow: 'GlimRow', GlimToggle: 'GlimToggle', WellSelector: 'WellSelector' },
  '../components/IconBadge': { Cross: 'Cross', Plus: 'Plus' },
  '../components/TaskEdit': edit,
  '../components/AddFileSection': { AddFileSection: 'AddFileSection' },
  '../components/InfoTip': { InfoTip: 'InfoTip' },
  '../components/Text': { Text: 'Text', TextInput: 'TextInput' },
  '../watch/watch': { startWatch: async () => {} },
});

{
  let done = 0;
  const s = mount(() => add.default({ conn, onDone: () => done++ }));
  await s.settle();
  const of = (type, label) => s.shown().elements.find((e) => e.type === type && (label === undefined || e.props.label === label))?.props;
  of('TextInput').onChangeText('https://host.example/a.zip');
  of(add.AddOptionsSection).onChange({ ...api.NO_ADD_OPTIONS, dir: '/elsewhere' });
  await s.settle();
  refuse = (path) => (path === '/links' ? ownLogin() : null);
  calls = [];
  await of('GlimButton', t('addDownload.button')).onPress();
  await s.settle();
  refuse = () => null;
  expect('links sent into a folder the instance refuses say it is the folder', s.shown().words.join(' ').includes(t('addDownload.errorServer', { message: t('edit.folderRefused') })), true);
  of(add.AddOptionsSection).onChange({ ...api.NO_ADD_OPTIONS, category: 'serien', comment: 'phone' });
  await s.settle();
  calls = [];
  await of('GlimButton', t('addDownload.button')).onPress();
  await s.settle();
  expect('the add screen sends its options with the links', posts(), [
    { path: '/api/links', body: { links: 'https://host.example/a.zip', category: 'serien', comment: 'phone' } },
  ]);
  expect('and is done once they are in', done, 1);
}

{
  let value = api.NO_ADD_OPTIONS;
  calls = [];
  const s = mount(() => add.AddOptionsSection({ conn, base: '/api', value, onChange: (v) => (value = v) }));
  await s.settle();
  expect('the options read nothing while folded', calls, []);
  const of = (type, label) => s.shown().elements.find((e) => e.type === type && (label === undefined || e.props.label === label))?.props;
  of('CardButton').onPress();
  await s.settle();
  expect('opened, they read the folders, the categories and the priorities', calls.map((c) => c.path).sort(), ['/api/categories', '/api/download-folders', '/api/queue/priorities']);
  const wells = s.shown().elements.filter((e) => e.type === 'WellSelector').map((e) => e.props);
  expect('the priorities are offered highest first, in the words of the ladder', wells[1]?.options.map((o) => o.label), ['highest', 'higher', 'high', 'default', 'low', 'lower', 'lowest'].map((id) => t(`edit.priority.${id}`)));
  wells[1]?.onPick('3');
  expect('a priority picked is the ladder value', value.priority, '3');
}

if (problems.length) {
  console.error(`check-task-edit: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: the properties window and the add options send what changed, in order, and word what the instance refuses');
