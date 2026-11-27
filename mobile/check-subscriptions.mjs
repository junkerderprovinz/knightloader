// The subscriptions screen keeps an instance's subscriptions on the member
// routes the phone reaches (internal/api/group_routes.go), never on
// /api/settings, which a member of the group may not read.
//
// A save reads the list again and replaces the one row it edited, so a change
// made in the web interface meanwhile survives it. A row the instance would
// refuse is caught before it is sent, a refused folder is worded as the
// folder, and the test shows each entry with its length and, where it is
// skipped, why. A pasted link is looked up first and a MediathekViewWeb search
// is offered as one.
//
// The check compiles src/api/subscriptions.ts, src/screens/SubscriptionsScreen.tsx
// and src/components/SubscriptionEdit.tsx with the Babel that Expo already
// installs and renders them on stand-ins for React and React Native, with the
// instance replaced by a recorder of the calls it is sent.
//
// Run by hand and by CI, from mobile/: `node check-subscriptions.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const { en } = await import(pathToFileURL(join(here, 'src', 'i18n', 'en.ts')).href);
const { de } = await import(pathToFileURL(join(here, 'src', 'i18n', 'de.ts')).href);
const refusal = await import(pathToFileURL(join(here, 'src', 'api', 'refusal.ts')).href);
const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};
const t = (key, vars = {}) => {
  if (!(key in en)) problems.push(`no English text for ${key}`);
  return Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key] ?? key);
};

// The instance: its stored rows, what it says of them, and what a test and a
// look-up find.
let stored = [];
let calls = [];
let refuse = () => null;
let health = [];
let found = null;
let looked = null;
const request = async (_conn, base, path, init) => {
  const body = init?.body ? JSON.parse(init.body) : undefined;
  const method = init?.method ?? 'GET';
  calls.push(body === undefined ? { method, path: base + path } : { method, path: base + path, body });
  const e = refuse(method, path, body);
  if (e) throw e;
  if (path === '/feeds/subscriptions' && method === 'GET') return stored;
  if (path === '/feeds/subscriptions' && method === 'PUT') {
    stored = body;
    return stored;
  }
  if (path === '/feeds') return health;
  if (path === '/feeds/test') return found;
  if (path === '/feeds/resolve') return looked;
  if (path === '/download-folders') return { folders: ['/downloads', '/media/serien'] };
  if (path === '/queue/priorities') {
    return ['lowest', 'lower', 'low', 'default', 'high', 'higher', 'highest'].map((id, i) => ({ id, value: i - 3 }));
  }
  return undefined;
};
const conn = { id: 'home', name: 'Home' };
const puts = () => calls.filter((c) => c.method === 'PUT');

const api = compile(join(here, 'src', 'api', 'subscriptions.ts'), {
  './client': { request },
  './refusal': refusal,
});

// The calls.
{
  stored = [{ url: 'https://a.example/feed.xml', intervalMinutes: 0 }];
  calls = [];
  expect('the rows come from the member route', [await api.fetchSubscriptions(conn), calls], [
    stored,
    [{ method: 'GET', path: '/api/feeds/subscriptions' }],
  ]);
  stored = null;
  expect('an instance with no subscriptions answers an empty list', await api.fetchSubscriptions(conn), []);
}

{
  stored = [
    { url: 'https://a.example/feed.xml', intervalMinutes: 0 },
    { url: 'https://b.example/feed.xml', intervalMinutes: 30 },
  ];
  calls = [];
  const next = await api.saveSubscription(conn, 'https://b.example/feed.xml', {
    url: 'https://b.example/feed.xml',
    intervalMinutes: 60,
    title: 'B',
  });
  expect('a save reads the list again before it writes', calls[0], { method: 'GET', path: '/api/feeds/subscriptions' });
  expect('and replaces the row it edited, in its place', puts(), [
    {
      method: 'PUT',
      path: '/api/feeds/subscriptions',
      body: [
        { url: 'https://a.example/feed.xml', intervalMinutes: 0 },
        { url: 'https://b.example/feed.xml', intervalMinutes: 60, title: 'B' },
      ],
    },
  ]);
  expect('and answers the list as the instance stored it', next, stored);
}

{
  // Somebody added a row in the web interface while the phone had the list open.
  stored = [
    { url: 'https://a.example/feed.xml', intervalMinutes: 0 },
    { url: 'https://web.example/feed.xml', intervalMinutes: 0 },
  ];
  calls = [];
  await api.saveSubscription(conn, null, { url: 'https://new.example/feed.xml', intervalMinutes: 0 });
  expect('a new row goes to the end of the list as it is now', puts()[0].body.map((r) => r.url), [
    'https://a.example/feed.xml',
    'https://web.example/feed.xml',
    'https://new.example/feed.xml',
  ]);
}

{
  stored = [{ url: 'https://a.example/feed.xml', intervalMinutes: 0 }];
  calls = [];
  let fault = null;
  try {
    await api.saveSubscription(conn, null, { url: ' https://a.example/feed.xml ', intervalMinutes: 0 });
  } catch (e) {
    fault = e;
  }
  expect('a second row for an address already subscribed is refused before anything is written', [fault?.key, puts()], [
    'subs.duplicate',
    [],
  ]);
}

{
  stored = [
    { url: 'https://a.example/feed.xml', intervalMinutes: 0 },
    { url: 'https://b.example/feed.xml', intervalMinutes: 0 },
  ];
  calls = [];
  await api.removeSubscription(conn, 'https://a.example/feed.xml');
  expect('a removal writes the list without the row', puts()[0].body.map((r) => r.url), ['https://b.example/feed.xml']);
  calls = [];
  await api.removeSubscription(conn, 'https://b.example/feed.xml');
  expect('removing the last row writes an empty list', puts()[0].body, []);
}

{
  health = [{ url: 'https://a.example/feed.xml', kind: 'rss', title: 'A', polling: true, seeded: true, remembered: 3 }];
  calls = [];
  expect('the health comes from GET /api/feeds, keyed by address', [await api.fetchFeedHealth(conn), calls[0].path], [
    { 'https://a.example/feed.xml': health[0] },
    '/api/feeds',
  ]);
}

{
  found = { title: 'A', entries: [], total: 0, matched: 0 };
  calls = [];
  await api.testSubscription(conn, { url: 'https://a.example/feed.xml', intervalMinutes: 0, excludeFilter: 'Trailer', minMinutes: 10 });
  expect('the test sends the whole row', calls, [
    {
      method: 'POST',
      path: '/api/feeds/test',
      body: { url: 'https://a.example/feed.xml', intervalMinutes: 0, excludeFilter: 'Trailer', minMinutes: 10 },
    },
  ]);
  looked = { kind: 'rss', url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UC1', title: 'K' };
  calls = [];
  await api.resolveSubscription(conn, ' https://www.youtube.com/@k ');
  expect('a look-up sends the link as pasted, trimmed', calls, [
    { method: 'POST', path: '/api/feeds/resolve', body: { url: 'https://www.youtube.com/@k' } },
  ]);
}

// What a row is.
{
  expect('a row with no kind is a feed', [api.kindOf({ url: 'x', intervalMinutes: 0 }), api.kindOf({ url: 'x', intervalMinutes: 0, kind: 'channel' })], [
    'rss',
    'channel',
  ]);
  expect('a row is named by its title, else its address', [
    api.nameOf({ url: 'https://a.example/feed.xml', intervalMinutes: 0, title: ' Kurzgesagt ' }),
    api.nameOf({ url: 'https://a.example/feed.xml', intervalMinutes: 0, title: '' }),
  ], ['Kurzgesagt', 'https://a.example/feed.xml']);
}

{
  const tidy = api.tidy({
    url: ' https://a.example/feed.xml ',
    kind: 'rss',
    title: '',
    intervalMinutes: 0,
    titleFilter: ' ',
    excludeFilter: 'Trailer ',
    minMinutes: 0,
    maxMinutes: 0,
    dir: '',
    priority: 0,
    autoStart: false,
    allowDownloaded: false,
    media: null,
    naming: '',
    library: '',
    sponsorBlock: null,
    backfill: { mode: 'all' },
    mediathek: { channel: 'ARD', topic: '', title: '', quality: 'hd', subtitles: false },
  });
  expect('a saved row leaves out what holds no opinion, keeps priority 0 and drops what its kind does not use', tidy, {
    url: 'https://a.example/feed.xml',
    kind: 'rss',
    intervalMinutes: 0,
    excludeFilter: 'Trailer',
    priority: 0,
  });
  expect('a channel keeps its backfill and a Mediathek search its query', [
    api.tidy({ url: 'u', kind: 'channel', intervalMinutes: 0, backfill: { mode: 'count', n: 20 } }).backfill,
    api.tidy({ url: 'u', kind: 'mediathek', intervalMinutes: 0, mediathek: { channel: 'ARD', topic: 'Tatort', title: '', quality: 'hd', subtitles: true } })
      .mediathek,
  ], [{ mode: 'count', n: 20 }, { channel: 'ARD', topic: 'Tatort', title: '', quality: 'hd', subtitles: true }]);
  expect('the interval is cut into what the instance keeps, 0 stays its default', [0, 0.4, 2.6, 99999, -5].map(api.clampInterval), [0, 0, 3, 10080, 1]);
}

{
  const faults = (row) => api.rowFaults({ url: 'https://a.example/feed.xml', intervalMinutes: 0, ...row }).map((f) => f.key);
  expect('a usable row has no faults', faults({}), []);
  expect('an address has to be http or https', [faults({ url: '' }), faults({ url: 'ftp://a.example/x' }), faults({ url: 'nonsense' })], [
    ['subs.urlMissing'],
    ['subs.urlInvalid'],
    ['subs.urlInvalid'],
  ]);
  expect('both patterns have to compile', [faults({ titleFilter: '(' }), faults({ excludeFilter: '[' })], [['subs.includeInvalid'], ['subs.excludeInvalid']]);
  expect('the shortest length cannot pass the longest', [faults({ minMinutes: 30, maxMinutes: 10 }), faults({ minMinutes: 30 }), faults({ maxMinutes: 10 })], [
    ['subs.lengthOrder'],
    [],
    [],
  ]);
  expect('a backfill by count or days needs at least one', [
    faults({ kind: 'channel', backfill: { mode: 'count', n: 0 } }),
    faults({ kind: 'channel', backfill: { mode: 'days', n: 30 } }),
    faults({ kind: 'channel', backfill: { mode: 'all' } }),
  ], [['subs.backfillCount'], [], []]);
  expect('a Mediathek search needs a channel, a topic or a title', [
    faults({ kind: 'mediathek', mediathek: { channel: '', topic: ' ', title: '', quality: 'hd', subtitles: false } }),
    faults({ kind: 'mediathek', mediathek: { channel: '', topic: 'Tatort', title: '', quality: 'hd', subtitles: false } }),
  ], [['subs.mediathekEmpty'], []]);
}

// What the test shows.
{
  const row = { url: 'u', intervalMinutes: 0, minMinutes: 10, maxMinutes: 90 };
  const why = (reason) => api.skipText(t, { title: 'x', link: 'l', matches: false, duration: 0, reason }, row);
  expect('each skipped entry says why', ['include', 'exclude', 'short', 'long', 'downloaded'].map(why), [
    t('subs.skipInclude'),
    t('subs.skipExclude'),
    t('subs.skipShort', { n: 10 }),
    t('subs.skipLong', { n: 90 }),
    t('subs.skipDownloaded'),
  ]);
  expect('a taken entry says so', api.skipText(t, { title: 'x', link: 'l', matches: true, duration: 0 }, row), t('subs.taken'));
  expect('a length is minutes and seconds, hours where it has them, nothing where unknown', [0, 59, 754, 5025].map(api.lengthText), [
    '',
    '0:59',
    '12:34',
    '1:23:45',
  ]);
}

{
  const e = refusal.refusedCall(403, JSON.stringify({ error: 'sign in there', code: 'ownLoginNeeded', field: 'feeds.1' }));
  expect('a refused folder is worded as the folder', api.saveRefusal(t, e, { url: 'u', intervalMinutes: 0, dir: '/elsewhere' }), {
    field: 'dir',
    text: t('subs.folderRefused'),
  });
  const bad = refusal.refusedCall(400, JSON.stringify({ error: 'feed row 2: the exclude pattern does not compile', field: 'feeds.1' }));
  expect("any other refusal keeps the instance's sentence", api.saveRefusal(t, bad, { url: 'u', intervalMinutes: 0 }), {
    field: '',
    text: 'feed row 2: the exclude pattern does not compile',
  });
  const member = refusal.refusedCall(403, JSON.stringify({ error: 'x', code: 'ownLoginNeeded' }));
  expect('a refusal for a member without a folder is the usual one', api.saveRefusal(t, member, { url: 'u', intervalMinutes: 0 }), {
    field: '',
    text: t('error.ownLoginNeeded'),
  });
}

// The look-up offers.
{
  const yt = {
    kind: 'rss',
    url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UC1',
    title: 'K',
    channel: { kind: 'channel', url: 'https://www.youtube.com/@k', title: 'K' },
  };
  expect('a channel is offered as its new videos and, apart, with older ones', api.offersOf(yt, 'https://www.youtube.com/@k').map((o) => [o.key, o.row.kind, o.row.url]), [
    ['subs.offerNew', 'rss', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC1'],
    ['subs.offerOlder', 'channel', 'https://www.youtube.com/@k'],
  ]);
  expect('an offer never carries the alternative along', 'channel' in api.offersOf(yt, 'x')[0].row, false);
  const mvw = { kind: 'mediathek', title: 'Tatort', minMinutes: 80, mediathek: { channel: 'ARD', topic: 'Tatort', title: '', quality: 'hd', subtitles: false } };
  const pasted = 'https://mediathekviewweb.de/#query=!ard%20%23tatort%20%3E80';
  expect('a Mediathek search is offered as one, under the link that was pasted', api.offersOf(mvw, pasted).map((o) => [o.key, o.row.url, o.row.minMinutes]), [
    ['subs.offerSearch', pasted, 80],
  ]);
  expect('a plain feed is offered as it is', api.offersOf({ kind: 'rss', url: 'https://a.example/feed.xml' }, 'https://a.example/feed.xml').map((o) => o.key), [
    'subs.offerFeed',
  ]);
}

// The screens.
const view = {
  'react-native': {
    Modal: 'Modal',
    RefreshControl: 'RefreshControl',
    ScrollView: 'ScrollView',
    StyleSheet: { create: (s) => s },
    TouchableOpacity: 'TouchableOpacity',
    View: 'View',
  },
  'expo-clipboard': { getStringAsync: async () => 'https://www.youtube.com/@pasted' },
  '../api/client': { ApiError: refusal.ApiError, errorText: refusal.errorText },
  '../api/subscriptions': api,
  '../api/taskEdit': {
    fetchDownloadFolders: async () => ['/downloads', '/media/serien'],
    fetchPriorities: async () => ['highest', 'higher', 'high', 'default', 'low', 'lower', 'lowest'].map((id, i) => ({ id, value: 3 - i })),
  },
  '../i18n/I18nContext': { useT: () => ({ t, lang: 'en' }) },
  '../theme/AppearanceContext': { useAppearance: () => ({ c: {}, corners: {}, accentInk: '' }) },
  '../theme/MotionContext': { useMotion: () => ({ motion: 'off' }) },
  '../theme/tokens': { TYPE: {} },
  'react-native-safe-area-context': { useSafeAreaInsets: () => ({ top: 24, bottom: 48, left: 0, right: 0 }) },
  '../components/glim': {
    CardButton: 'CardButton',
    GlimButton: 'GlimButton',
    GlimRow: 'GlimRow',
    GlimToggle: 'GlimToggle',
    NotchCard: 'NotchCard',
    WellSelector: 'WellSelector',
  },
  '../components/IconBadge': {
    __esModule: true,
    default: 'IconBadge',
    Back: 'Back',
    Check: 'Check',
    Cross: 'Cross',
    Paste: 'Paste',
    Plus: 'Plus',
    Search: 'Search',
    Trash: 'Trash',
  },
  '../components/InfoTip': { InfoTip: 'InfoTip' },
  '../components/ConfirmDialog': { ConfirmDialog: 'ConfirmDialog' },
  '../components/Moving': { MovingScroll: 'MovingScroll', NoArrival: 'NoArrival' },
  '../components/Text': { Text: 'Text', TextInput: 'TextInput' },
  '../components/TaskEdit': { EditBox: 'EditBox', EditField: 'EditField', PickChips: 'PickChips' },
};
const local = Object.fromEntries(Object.entries(view).map(([k, v]) => [k.replace('../components/', './'), v]));
const editMod = compile(join(here, 'src', 'components', 'SubscriptionEdit.tsx'), { ...view, ...local });
const screenMod = compile(join(here, 'src', 'screens', 'SubscriptionsScreen.tsx'), {
  ...view,
  '../components/SubscriptionEdit': { SubscriptionEdit: 'SubscriptionEdit' },
});

/** The elements under a node, the node itself not counted. */
function inside(node) {
  const out = [];
  const walk = (n) => {
    if (n === null || n === undefined || typeof n !== 'object') return;
    if (Array.isArray(n)) return n.forEach(walk);
    if (!('type' in n && 'props' in n)) return;
    out.push(n);
    for (const v of Object.values(n.props)) if (typeof v === 'object') walk(v);
  };
  for (const v of Object.values(node.props)) if (typeof v === 'object') walk(v);
  return out;
}

function handle(w) {
  const all = () => w.shown().elements;
  const of = (type, label) => all().find((e) => e.type === type && (label === undefined || e.props.label === label))?.props;
  const field = (key) => all().find((e) => e.type === 'EditField' && e.props.label === t(key));
  return {
    of,
    words: () => w.shown().words.join(' '),
    box: (key) => of('EditBox', t(key)),
    well: (key) => {
      const f = field(key);
      return f ? inside(f).find((e) => e.type === 'WellSelector')?.props : undefined;
    },
    toggle: (key) => of('GlimToggle', t(key)),
    row: (key) => of('GlimRow', t(key)),
    type: async (key, value) => {
      of('EditBox', t(key)).onChange(value);
      await w.settle();
    },
    pick: async (key, value) => {
      inside(field(key)).find((e) => e.type === 'WellSelector').props.onPick(value);
      await w.settle();
    },
    flip: async (key) => {
      const p = of('GlimToggle', t(key));
      p.onChange(!p.value);
      await w.settle();
    },
    press: async (key, vars) => {
      await of('GlimButton', t(key, vars)).onPress();
      await w.settle();
    },
  };
}

async function editor(open, more = {}) {
  const said = { closed: 0, saved: [] };
  const w = mount(() =>
    editMod.SubscriptionEdit({ conn, open, onClose: () => said.closed++, onSaved: (rows) => said.saved.push(rows), ...more }),
  );
  await w.settle();
  return { said, w, ...handle(w) };
}

{
  stored = [
    { url: 'https://a.example/feed.xml', intervalMinutes: 0, title: 'Kurzgesagt' },
    { url: 'https://www.youtube.com/@b', kind: 'channel', intervalMinutes: 0 },
    { url: 'https://mediathekviewweb.de/#query=tatort', kind: 'mediathek', intervalMinutes: 0, title: 'Tatort' },
  ];
  health = [
    { url: 'https://a.example/feed.xml', polling: true, lastPolledAt: '2026-11-27T10:05:00Z', seeded: true, remembered: 3 },
    { url: 'https://www.youtube.com/@b', polling: true, lastPolledAt: '2026-11-27T10:05:00Z', error: 'yt-dlp: channel not found', seeded: false, remembered: 0 },
  ];
  calls = [];
  const s = mount(() => screenMod.default({ conn, onBack: () => {} }));
  await s.settle();
  const h = handle(s);
  expect('the screen reads the rows and their health', calls.map((c) => c.path).sort(), ['/api/feeds', '/api/feeds/subscriptions']);
  const words = h.words();
  expect('each row shows its name, else its address', [words.includes('Kurzgesagt'), words.includes('https://www.youtube.com/@b'), words.includes('Tatort')], [true, true, true]);
  expect('and its kind', [t('subs.kind.rss'), t('subs.kind.channel'), t('subs.kind.mediathek')].map((k) => words.includes(k)), [true, true, true]);
  const when = new Date('2026-11-27T10:05:00Z').toLocaleString('en', { dateStyle: 'short', timeStyle: 'short' });
  expect('and its last check, its trouble, or that it was not checked yet', [
    words.includes(t('subs.checked', { when })),
    words.includes(t('subs.checkFailed', { error: 'yt-dlp: channel not found' })),
    words.includes(t('subs.notChecked')),
  ], [true, true, true]);

  s.shown().elements.filter((e) => e.type === 'CardButton')[1].props.onPress();
  await s.settle();
  const open = h.of('SubscriptionEdit').open;
  expect('a row opens the editor on itself', [open.was, open.row.url], ['https://www.youtube.com/@b', 'https://www.youtube.com/@b']);
  expect('with what the instance knows of it', h.of('SubscriptionEdit').health?.error, 'yt-dlp: channel not found');
}

{
  stored = [];
  const s = mount(() => screenMod.default({ conn, onBack: () => {} }));
  await s.settle();
  expect('no subscriptions says so', handle(s).words().includes(t('subs.empty')), true);
}

{
  refuse = (_m, path) => (path === '/feeds/subscriptions' ? refusal.refusedCall(404, 'no such endpoint: GET /api/feeds/subscriptions') : null);
  const s = mount(() => screenMod.default({ conn, onBack: () => {} }));
  await s.settle();
  refuse = () => null;
  expect('an instance too old for the member routes is told apart', handle(s).words().includes(t('subs.tooOld')), true);
}

{
  stored = [];
  looked = {
    kind: 'rss',
    url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UC1',
    title: 'Kurzgesagt',
    channel: { kind: 'channel', url: 'https://www.youtube.com/@kurzgesagt', title: 'Kurzgesagt' },
  };
  const s = mount(() => screenMod.default({ conn, onBack: () => {} }));
  await s.settle();
  const h = handle(s);
  await h.press('subs.paste');
  expect('paste puts the clipboard into the link box', h.box('subs.link').value, 'https://www.youtube.com/@pasted');
  await h.type('subs.link', 'https://www.youtube.com/@kurzgesagt');
  calls = [];
  await h.press('subs.lookUp');
  expect('look up asks the instance what the link is', calls, [
    { method: 'POST', path: '/api/feeds/resolve', body: { url: 'https://www.youtube.com/@kurzgesagt' } },
  ]);
  expect('a channel offers its new videos, and older ones apart', [
    h.of('GlimButton', t('subs.offerNew')) !== undefined,
    h.of('GlimButton', t('subs.offerOlder')) !== undefined,
    h.words().includes('Kurzgesagt'),
  ], [true, true, true]);
  await h.press('subs.offerOlder');
  const open = h.of('SubscriptionEdit').open;
  expect('an offer opens the editor on a new row, nothing saved yet', [open.was, open.row.kind, open.row.url, puts()], [
    null,
    'channel',
    'https://www.youtube.com/@kurzgesagt',
    [],
  ]);
}

{
  looked = { kind: 'mediathek', title: 'Tatort', minMinutes: 80, mediathek: { channel: 'ARD', topic: 'Tatort', title: '', quality: 'hd', subtitles: false } };
  const s = mount(() => screenMod.default({ conn, onBack: () => {} }));
  await s.settle();
  const h = handle(s);
  await h.type('subs.link', 'https://mediathekviewweb.de/#query=!ard%20%23tatort');
  await h.press('subs.lookUp');
  await h.press('subs.offerSearch');
  expect('a Mediathek search link offers to subscribe to the search', h.of('SubscriptionEdit').open.row.mediathek, looked.mediathek);
}

{
  refuse = (_m, path) => (path === '/feeds/resolve' ? refusal.refusedCall(400, 'no feed was found at that address') : null);
  const s = mount(() => screenMod.default({ conn, onBack: () => {} }));
  await s.settle();
  const h = handle(s);
  await h.type('subs.link', 'https://blog.example/');
  await h.press('subs.lookUp');
  refuse = () => null;
  expect('a link the instance found nothing at says so', h.words().includes('no feed was found at that address'), true);
  await h.press('subs.byHand');
  expect('and can still be set up by hand', h.of('SubscriptionEdit').open, { was: null, row: { url: 'https://blog.example/', intervalMinutes: 0 } });
}

// The editor.
{
  stored = [{ url: 'https://a.example/feed.xml', intervalMinutes: 30, title: 'A', titleFilter: 'Folge', dir: '/downloads' }];
  const e = await editor({ was: 'https://a.example/feed.xml', row: stored[0] });
  expect('the boxes open on the row', [e.box('subs.name').value, e.box('subs.interval').value, e.box('subs.include').value, e.box('subs.folder').value], [
    'A',
    '30',
    'Folge',
    '/downloads',
  ]);
  expect('a stored address can be read but not typed over', [e.box('subs.url'), e.words().includes('https://a.example/feed.xml')], [undefined, true]);
  expect('the folder is offered the download folders', e.of('PickChips').values, ['/downloads', '/media/serien']);
  expect('the priority is offered as not set, then the ladder', e.well('subs.priority').options.map((o) => o.value), ['unset', '3', '2', '1', '0', '-1', '-2', '-3']);
  expect('a feed has no backfill and no Mediathek boxes', [e.well('subs.backfill'), e.box('subs.mtTopic')], [undefined, undefined]);
  expect('starting on its own is off, with its warning beside it', [e.toggle('subs.autoStart').value, e.row('subs.autoStart').info], [false, t('subs.autoStartHint')]);
  expect('taking what was downloaded before is off, and says why', [e.toggle('subs.allowDownloaded').value, e.row('subs.allowDownloaded').info], [
    false,
    t('subs.allowDownloadedHint'),
  ]);

  await e.type('subs.exclude', 'Trailer');
  await e.type('subs.minMinutes', '10');
  await e.pick('subs.priority', '1');
  await e.pick('subs.naming', 'channel');
  await e.pick('subs.library', 'series');
  await e.flip('subs.autoStart');
  await e.flip('subs.allowDownloaded');
  calls = [];
  await e.press('subs.save');
  expect('a save writes every field that was set', puts()[0]?.body, [
    {
      url: 'https://a.example/feed.xml',
      intervalMinutes: 30,
      title: 'A',
      titleFilter: 'Folge',
      dir: '/downloads',
      excludeFilter: 'Trailer',
      minMinutes: 10,
      priority: 1,
      naming: 'channel',
      library: 'series',
      autoStart: true,
      allowDownloaded: true,
    },
  ]);
  expect('and hands the stored list back and closes', [e.said.saved.length, e.said.closed], [1, 1]);
}

{
  const e = await editor({ was: null, row: { url: 'https://a.example/feed.xml', intervalMinutes: 0 } });
  await e.type('subs.include', '(Folge');
  calls = [];
  await e.press('subs.save');
  expect('a pattern that does not compile is named and nothing is sent', [e.words().includes(t('subs.includeInvalid')), puts(), e.said.closed], [true, [], 0]);
}

{
  stored = [];
  const e = await editor({ was: null, row: { url: 'https://a.example/feed.xml', intervalMinutes: 0 } });
  expect('a new row can be typed into and has nothing to remove', [e.box('subs.url')?.value, e.of('GlimButton', t('subs.remove'))], ['https://a.example/feed.xml', undefined]);
  await e.type('subs.folder', '/elsewhere');
  refuse = (method) =>
    method === 'PUT' ? refusal.refusedCall(403, JSON.stringify({ error: 'sign in there', code: 'ownLoginNeeded', field: 'feeds.0' })) : null;
  await e.press('subs.save');
  refuse = () => null;
  expect('a folder the instance refuses a member is named as the folder', [e.words().includes(t('subs.folderRefused')), e.said.closed], [true, 0]);
}

{
  const e = await editor({ was: null, row: { url: 'https://www.youtube.com/@b', kind: 'channel', intervalMinutes: 0 } });
  expect('a channel starts out bringing only new videos', e.well('subs.backfill').value, 'none');
  await e.pick('subs.backfill', 'count');
  expect('a count opens on twenty', e.box('subs.backfillVideos').value, '20');
  await e.type('subs.backfillVideos', '5');
  await e.pick('subs.media', 'own');
  await e.pick('subs.mediaWhat', 'audio');
  await e.pick('subs.sponsorBlock', 'own');
  await e.pick('subs.sb.sponsor', 'remove');
  await e.pick('subs.sb.intro', 'mark');
  expect('a highlight can only be marked', e.well('subs.sb.poi_highlight').options.map((o) => o.value), ['off', 'mark']);
  stored = [];
  calls = [];
  await e.press('subs.save');
  const row = puts()[0]?.body[0];
  expect('the backfill, the media choice and SponsorBlock are saved', [row?.backfill, row?.media, row?.sponsorBlock], [
    { mode: 'count', n: 5 },
    { variants: ['audio'], videoFormat: 'best', quality: 'best', audioFormat: 'best', audioBitrate: '' },
    { remove: ['sponsor'], mark: ['intro'] },
  ]);
}

{
  const media = { variants: ['video', 'subtitle'], videoFormat: 'best', quality: '1080p', audioFormat: 'best', audioBitrate: '' };
  const e = await editor({ was: null, row: { url: 'https://www.youtube.com/@b', kind: 'channel', intervalMinutes: 0, media } });
  expect('a media choice opens on what it holds', [
    e.well('subs.media').value,
    e.well('subs.mediaWhat').value,
    e.well('subs.quality').value,
    e.toggle('subs.extraSubtitles').value,
    e.toggle('subs.extraThumbnail').value,
  ], ['own', 'video', '1080p', true, false]);
  await e.flip('subs.extraThumbnail');
  expect('an extra joins the rows a video gets', e.well('subs.mediaWhat') && e.toggle('subs.extraThumbnail').value, true);
  await e.pick('subs.kind', 'mediathek');
  expect('a Mediathek search has its own boxes and no yt-dlp choice', [e.box('subs.mtTopic') !== undefined, e.well('subs.media'), e.well('subs.sponsorBlock'), e.well('subs.backfill')], [
    true,
    undefined,
    undefined,
    undefined,
  ]);
  await e.type('subs.mtTopic', 'Tatort');
  await e.pick('subs.mtQuality', 'normal');
  await e.flip('subs.mtSubtitles');
  stored = [];
  calls = [];
  await e.press('subs.save');
  const row = puts()[0]?.body[0];
  expect('its query is saved, and nothing of the video it had', [row?.mediathek, 'media' in (row ?? {})], [
    { channel: '', topic: 'Tatort', title: '', quality: 'normal', subtitles: true },
    false,
  ]);
}

{
  found = {
    title: 'Kurzgesagt',
    total: 12,
    matched: 1,
    entries: [
      { title: 'Why the Sun', link: 'https://y/1', matches: true, duration: 754 },
      { title: 'Trailer', link: 'https://y/2', matches: false, duration: 61, reason: 'exclude' },
      { title: 'Old one', link: 'https://y/3', matches: false, duration: 0, reason: 'downloaded' },
    ],
  };
  const row = { url: 'https://a.example/feed.xml', intervalMinutes: 0, excludeFilter: 'Trailer' };
  const e = await editor({ was: null, row });
  calls = [];
  await e.press('subs.test');
  expect('the test sends the row as it stands in the form', calls, [{ method: 'POST', path: '/api/feeds/test', body: row }]);
  const words = e.words();
  expect('and shows the first entries with their length and why one is skipped', [
    words.includes('Why the Sun'),
    words.includes('12:34'),
    words.includes(t('subs.taken')),
    words.includes(t('subs.skipExclude')),
    words.includes(t('subs.skipDownloaded')),
    words.includes(t('subs.testCount', { matched: 1, total: 12 })),
  ], [true, true, true, true, true, true]);
  found = { title: '', entries: [], total: 0, matched: 0, error: 'HTTP 404' };
  await e.press('subs.test');
  expect('a feed that could not be read says why', e.words().includes(t('subs.testFailed', { error: 'HTTP 404' })), true);
}

{
  stored = [
    { url: 'https://a.example/feed.xml', intervalMinutes: 0, title: 'A' },
    { url: 'https://b.example/feed.xml', intervalMinutes: 0 },
  ];
  const e = await editor({ was: 'https://a.example/feed.xml', row: stored[0] });
  await e.press('subs.remove');
  expect('remove asks first', e.of('ConfirmDialog').visible, true);
  calls = [];
  await e.of('ConfirmDialog').onConfirm();
  await e.w.settle();
  expect('and then writes the list without the row', puts()[0]?.body.map((r) => r.url), ['https://b.example/feed.xml']);
  expect('and closes', [e.said.saved.length, e.said.closed], [1, 1]);
}

{
  stored = [{ url: 'https://a.example/feed.xml', intervalMinutes: 0 }];
  const e = await editor({ was: 'https://a.example/feed.xml', row: stored[0] });
  const page = e.w.shown().elements.find((el) => el.type === 'MovingScroll');
  const bottom = [page.props.contentContainerStyle].flat().reduce((n, st) => st?.paddingBottom ?? n, 0);
  expect('the end of the form scrolls clear of the navigation bar', bottom >= 48 + 32, true);
  // The innermost view around the button, which the walk reaches last.
  const rowOf = (key) =>
    e.w
      .shown()
      .elements.filter((el) => el.type === 'View' && inside(el).some((x) => x.type === 'GlimButton' && x.props.label === t(key)))
      .at(-1);
  const saveRow = rowOf('subs.save');
  expect('remove stands apart from cancel and save, so both keep their words whole', [
    inside(saveRow).some((x) => x.props?.label === t('subs.cancel')),
    inside(saveRow).some((x) => x.props?.label === t('subs.remove')),
  ], [true, false]);
}

// The way in, from the settings.
{
  const cardMod = compile(join(here, 'src', 'components', 'SubscriptionsCard.tsx'), {
    ...view,
    ...local,
    './glim': { ...view['../components/glim'], UnavailableNotice: 'UnavailableNotice' },
  });
  const opened = [];
  const home = { id: 'home', name: 'Home' };
  const office = { id: 'office', name: 'Office' };
  const two = mount(() => cardMod.SubscriptionsCard({ hue: 11, connections: [home, office], onOpen: (c) => opened.push(c.id) }));
  const buttons = two.shown().elements.filter((e) => e.type === 'CardButton');
  expect('the settings offer the subscriptions of every saved instance', [buttons.map((b) => b.props.accessibilityLabel), two.shown().words.includes('Office')], [
    [t('subs.settingsOpen', { name: 'Home' }), t('subs.settingsOpen', { name: 'Office' })],
    true,
  ]);
  buttons[1].props.onPress();
  expect('and open the one pressed', opened, ['office']);
  const none = mount(() => cardMod.SubscriptionsCard({ hue: 11, connections: [], onOpen: () => {} }));
  expect('without a connection they say what is missing', none.shown().elements.find((e) => e.type === 'UnavailableNotice')?.props.reason, t('subs.settingsNone'));
}

// The words, in both languages the screen is written in.
{
  const dashes = new RegExp(`[${String.fromCharCode(0x2013, 0x2014)}]`);
  const keys = Object.keys(en).filter((k) => k.startsWith('subs.'));
  if (keys.length === 0) problems.push('no subs.* strings in en.ts');
  for (const k of keys) {
    if (!(k in de)) problems.push(`no German text for ${k}`);
    for (const [lang, v] of [['en', en[k]], ['de', de[k] ?? '']]) {
      if (dashes.test(v)) problems.push(`${lang} ${k} has a dash`);
    }
    if ((de[k] ?? '').includes(';')) problems.push(`de ${k} has a semicolon`);
    if (/\bsources?\b/i.test(en[k])) problems.push(`en ${k} says source`);
    if (/Quelle/.test(de[k] ?? '')) problems.push(`de ${k} says Quelle`);
    if (/\b(Sie|Ihr|Ihre|Ihnen)\b/.test(de[k] ?? '')) problems.push(`de ${k} is not in du-form`);
  }
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: subscriptions keep to the member routes, a save replaces only its own row, and the test says why an entry is skipped');
