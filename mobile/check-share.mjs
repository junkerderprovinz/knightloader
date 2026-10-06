// Text another app shares reaches an instance only after a tap here, and a
// share that fails says why.
//
// Any app on the phone can start this screen with an explicit intent, which
// never shows the share sheet, so the sheet cannot be what confirms the send.
// With one instance paired the screen still waits for its card to be tapped.
// A failed send keeps the reason it failed with: a relay connection saved
// before frames were sealed says to add it again, and a sentence that only
// says the links were not sent leaves nothing to do about it. The Add links
// screen keeps the reason the same way, and says "Server:" only when a server
// answered.
//
// The sending app's title names the package. expo-share-intent reads it from
// EXTRA_TITLE only, and browsers send it as EXTRA_SUBJECT, so
// plugins/withSharedText.js copies the one into the other in MainActivity
// before the library sees the intent, on a cold start and on a new intent. A
// text file from a file manager comes as text/plain with no EXTRA_TEXT, which
// the library would pass on empty, so the plugin puts the file's text there.
// A file that gives no text, empty or not readable, gets a marker instead, and
// the share screen says what happened rather than the app dropping it.
// That part runs the plugin over the MainActivity.kt prebuild starts from.
//
// The check compiles both screens with the Babel that Expo already installs
// and renders them on a small stand-in for React and React Native, with the
// saved connections and the calls to the instance replaced.
//
// Run by hand and by CI, from mobile/: `node check-share.mjs`
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const { en } = await import(pathToFileURL(join(here, 'src', 'i18n', 'en.ts')).href);
const problems = [];
const fail = (why) => problems.push(why);

const t = (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]);

// One component, hooks kept by call order, effects run after each render.
let cells = [];
let at = 0;
let pending = [];
let dirty = false;
const same = (a, b) => a && b && a.length === b.length && a.every((x, i) => Object.is(x, b[i]));
const cell = (make) => {
  const i = at++;
  if (!(i in cells)) cells[i] = make();
  return cells[i];
};
const React = {
  useState(initial) {
    const s = cell(() => {
      const state = { value: initial };
      state.set = (next) => {
        const value = typeof next === 'function' ? next(state.value) : next;
        if (Object.is(value, state.value)) return;
        state.value = value;
        dirty = true;
      };
      return state;
    });
    return [s.value, s.set];
  },
  useRef: (initial) => cell(() => ({ current: initial })),
  useCallback(f, deps) {
    const c = cell(() => ({}));
    if (!same(c.deps, deps)) Object.assign(c, { f, deps });
    return c.f;
  },
  useEffect(effect, deps) {
    const c = cell(() => ({}));
    if (same(c.deps, deps)) return;
    c.deps = deps;
    pending.push(effect);
  },
};
const element = (type, props) => ({ type, props });

let connections = [];
let sent = [];
let sendFails = null;
class ApiError extends Error {
  constructor(message, status, code) {
    super(message);
    this.status = status;
    this.code = code;
  }
}
const modules = {
  react: React,
  'react/jsx-runtime': { jsx: element, jsxs: element, Fragment: 'Fragment' },
  'react-native': {
    ActivityIndicator: 'ActivityIndicator',
    Image: 'Image',
    ScrollView: 'ScrollView',
    View: 'View',
    StyleSheet: { create: (styles) => styles },
  },
  '../api/client': {
    ApiError,
    addSharedText: async (conn, text, title) => {
      sent.push({ conn: conn.id, text, title });
      if (sendFails) throw sendFails;
      return [{ id: 't1' }];
    },
    addLinks: async (conn, links) => {
      sent.push({ conn: conn.id, links });
      if (sendFails) throw sendFails;
      return [{ id: 't1' }];
    },
    // client.ts's errorText for an error with no code the app words itself.
    errorText: (_t, e) => (e instanceof Error ? e.message : String(e)),
  },
  '../storage/connections': { listConnections: async () => connections },
  '../watch/watch': { startWatch: async () => {} },
  '../theme/AppearanceContext': { useAppearance: () => ({ c: {}, corners: { card: {} } }) },
  '../theme/tokens': { TYPE: {} },
  '../i18n/I18nContext': { useT: () => ({ t }) },
  '../components/glim': { CardButton: 'CardButton', DefaultBadge: 'DefaultBadge', GlimButton: 'GlimButton' },
  '../components/IconBadge': { Connect: 'Connect', Cross: 'Cross', Plus: 'Plus' },
  '../components/InfoTip': { InfoTip: 'InfoTip' },
  '../components/Moving': { Arrive: 'Arrive' },
  '../components/Text': { Text: 'Text', TextInput: 'TextInput' },
  '../../assets/android-icon-foreground.png': 1,
};

const babel = require('@babel/core');
function screen(file) {
  const { code } = babel.transformSync(readFileSync(join(here, 'src', 'screens', file), 'utf8'), {
    filename: file,
    babelrc: false,
    configFile: false,
    presets: [[require.resolve('@babel/preset-typescript'), { isTSX: true, allExtensions: true }]],
    plugins: [
      [require.resolve('@babel/plugin-transform-react-jsx'), { runtime: 'automatic' }],
      require.resolve('@babel/plugin-transform-modules-commonjs'),
    ],
  });
  const mod = { exports: {} };
  new Function('require', 'module', 'exports', code)(
    (name) => {
      if (!(name in modules)) throw new Error(`${file} imports ${name}, which this check has no stand-in for`);
      return modules[name];
    },
    mod,
    mod.exports,
  );
  return mod.exports;
}
const { default: ShareScreen, UNREADABLE_FILE } = screen('ShareScreen.tsx');
const AddDownloadScreen = screen('AddDownloadScreen.tsx').default;

/** Everything on screen: its words, and the controls that can be pressed. */
function draw(node, out) {
  if (node === null || node === undefined || typeof node === 'boolean') return out;
  if (typeof node === 'string' || typeof node === 'number') {
    out.words.push(String(node));
    return out;
  }
  if (Array.isArray(node)) {
    for (const child of node) draw(child, out);
    return out;
  }
  const { type, props } = node;
  if (props.onPress) out.presses.push({ type, label: props.label, props });
  if (props.onChangeText) out.fields.push(props);
  if (props.label) out.words.push(props.label);
  draw(props.children, out);
  return out;
}

const share = () => ShareScreen({ text: 'https://example.com/a.zip', title: 'A file', onOpen() {}, onConnect() {}, onClose() {} });

/** Opens a screen and lets it settle, the way a phone would. */
async function open(paired, view = share) {
  connections = paired;
  sent = [];
  cells = [];
  let shown;
  const render = () => {
    do {
      dirty = false;
      at = 0;
      shown = draw(view(), { words: [], presses: [], fields: [] });
      const run = pending;
      pending = [];
      for (const f of run) f();
    } while (dirty);
  };
  const settle = async () => {
    for (let i = 0; i < 5; i++) {
      await new Promise((r) => setTimeout(r));
      render();
    }
    return shown;
  };
  render();
  return { settle, current: () => shown };
}

const home = { id: 'home', name: 'Home', transport: 'relay' };
const office = { id: 'office', name: 'Office', transport: 'relay' };

{
  sendFails = null;
  const s = await open([home]);
  const shown = await s.settle();
  if (sent.length) fail('with one instance paired, a share is sent before anybody taps anything');
  const card = shown.presses.find((p) => p.type === 'CardButton');
  if (!card) {
    fail('with one instance paired, the screen offers no instance to tap');
  } else {
    card.props.onPress();
    await s.settle();
    if (sent.length !== 1 || sent[0].conn !== 'home') fail(`a tap on the one instance sent ${JSON.stringify(sent)}`);
  }
}

{
  sendFails = null;
  const s = await open([home, office]);
  await s.settle();
  if (sent.length) fail('with two instances paired, a share is sent before anybody picks one');
}

{
  const reason = 'relay: this connection predates encrypted frames - add it again with your phrase';
  sendFails = new Error(reason);
  const s = await open([home, office]);
  const shown = await s.settle();
  shown.presses.find((p) => p.type === 'CardButton')?.props.onPress();
  const after = await s.settle();
  const said = after.words.join(' ');
  if (!said.includes(reason)) fail(`a share the relay could not carry says "${said}" and drops the reason "${reason}"`);

  sendFails = new ApiError('link filter refused it', 400);
  const again = await open([home]);
  (await again.settle()).presses.find((p) => p.type === 'CardButton')?.props.onPress();
  if (!(await again.settle()).words.join(' ').includes('link filter refused it')) fail("a refused share drops the instance's own sentence");
}

{
  sendFails = null;
  const s = await open([home], () => ShareScreen({ text: UNREADABLE_FILE, onOpen() {}, onConnect() {}, onClose() {} }));
  const shown = await s.settle();
  const said = shown.words.join(' ');
  if (!said.includes(t('share.fileUnreadable'))) fail(`a shared file with no text in it says "${said}" instead of why nothing came of it`);
  if (said.includes(UNREADABLE_FILE)) fail('a shared file with no text in it shows the marker the app passes it on with');
  if (shown.presses.some((p) => p.type === 'CardButton')) fail('a shared file with no text in it still offers to send it to an instance');
  if (!shown.presses.some((p) => p.label === en['share.close'])) fail('a shared file with no text in it leaves no way back');
}

{
  const add = async (fails) => {
    sendFails = fails;
    const s = await open([], () => AddDownloadScreen({ conn: home, onDone() {} }));
    (await s.settle()).fields[0]?.onChangeText('https://example.com/a.zip');
    (await s.settle()).presses.find((p) => p.label === en['addDownload.button'])?.props.onPress();
    const said = (await s.settle()).words.join(' ');
    if (sent.length !== 1) fail(`Add links sent ${JSON.stringify(sent)}`);
    return said;
  };
  const reason = 'relay: this connection predates encrypted frames - add it again with your phrase';
  let said = await add(new Error(reason));
  if (!said.includes(reason)) fail(`links the relay could not carry say "${said}" and drop the reason "${reason}"`);
  said = await add(new TypeError('Network request failed'));
  if (!said.includes('Network request failed')) fail(`links that never reached the instance say "${said}" and drop the reason`);
  said = await add(new ApiError('link filter refused it', 400));
  if (!said.includes('link filter refused it')) fail(`refused links say "${said}" and drop the instance's own sentence`);
  const local = 'this connection has to be added again with the phrase';
  said = await add(new ApiError(local, 0, 'addAgain'));
  if (!said.includes(t('addDownload.errorSend', { message: local }))) {
    fail(`links the app refused before asking any server say "${said}", as if the server had answered`);
  }
}

{
  const app = JSON.parse(readFileSync(join(here, 'app.json'), 'utf8'));
  const plugins = app.expo.plugins.map((p) => (Array.isArray(p) ? p[0] : p));
  if (!plugins.includes('./plugins/withSharedText')) fail('app.json does not run withSharedText, so a title sent as EXTRA_SUBJECT and a shared text file are dropped');
  const { addShareFallbacks } = require('./plugins/withSharedText.js');
  const template = execFileSync(
    'tar',
    ['xzOf', join('node_modules', 'expo', 'template.tgz'), 'package/android/app/src/main/java/com/helloworld/MainActivity.kt'],
    { cwd: here, encoding: 'utf8' },
  );
  const kt = addShareFallbacks(template);
  const body = (fn) => {
    const start = kt.indexOf(`fun ${fn}(`);
    return start < 0 ? '' : kt.slice(start, kt.indexOf('\n  }', start));
  };
  const before = (fn, first, then) => {
    const b = body(fn);
    return b.includes(first) && b.indexOf(first) < b.indexOf(then);
  };
  if (!before('onCreate', 'fillShareExtras(intent)', 'super.onCreate(')) {
    fail('a share that starts the app reaches expo-share-intent before its title and text are filled in');
  }
  if (!before('onNewIntent', 'fillShareExtras(intent)', 'super.onNewIntent(')) {
    fail('a share that reaches the running app gets to expo-share-intent before its title and text are filled in');
  }
  const fill = body('fillShareExtras');
  if (!/EXTRA_SUBJECT[^\n]*putExtra\(Intent\.EXTRA_TITLE/.test(fill)) fail('fillShareExtras does not put the subject in EXTRA_TITLE');
  if (!/putExtra\(Intent\.EXTRA_TEXT[^\n]*sharedFileText\(/.test(fill)) {
    fail('a text file shared as text/plain never reaches EXTRA_TEXT, so the app opens and drops it');
  }
  if (!/putExtra\(Intent\.EXTRA_TEXT[^\n]*\?: UNREADABLE_FILE/.test(fill) || !kt.includes(`UNREADABLE_FILE = "${UNREADABLE_FILE}"`)) {
    fail('a shared file that gives no text reaches the library empty, so the app opens and drops it without a word');
  }
  const read = body('sharedFileText');
  if (!read.includes('SCHEME_CONTENT') || !read.includes('GET_PROVIDERS')) fail("sharedFileText reads file paths or this app's own providers for any app that asks");
  if (!/val authority = uri\.authority\?\.substringAfterLast\('@'\)[\s\S]*authority in it\.authority/.test(read)) {
    fail("sharedFileText takes content://0@ followed by this app's own provider for another app's, though ContentResolver drops the 0@");
  }
  for (const name of ['ContentResolver', 'PackageManager', 'Uri', 'IOException']) {
    if (!new RegExp(`^import [\\w.]+\\.${name}$`, 'm').test(kt)) fail(`MainActivity.kt uses ${name} without importing it`);
  }
  if (addShareFallbacks(kt) !== kt) fail('running prebuild twice adds the share fallbacks twice');
}

if (problems.length) {
  console.error(`check-share: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(
  'ok: a share waits for a tap on an instance, also with one paired, a failed share keeps its reason, ' +
    'a title sent as the subject names the package, a shared text file arrives as its text, ' +
    'and a shared file with no text says so',
);
