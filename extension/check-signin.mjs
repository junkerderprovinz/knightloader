/**
 * Passing the browser's sign-ins on to KnightLoader (src/signin.js), run
 * against a stubbed `chrome`.
 *
 *   1. The access is the cookies on the listed sites' own domains, kept once
 *      granted. A browser that already gives taking downloads over the cookies
 *      and every site needs no question, and a switch turned off stays off.
 *      Turning it off gives back what no other feature uses.
 *   2. A browser that is already signed in hands the session to every
 *      instance that lists the site, once, and again only when the session
 *      changes. Only the site's own cookies travel.
 *   3. A browser without a sign-in sends nothing; signing out tells every
 *      instance that had the session, once.
 *   4. Private stores are never read. A container that is signed in wins over
 *      a default store that is not, also with none of its tabs open, and of
 *      two signed-in stores the one that changed last.
 *   5. Only a change to a listed site's sign-in cookie outside a private store
 *      wakes the watcher, and nothing is read or sent while it is off.
 *   6. Each list tells the instances which sites this browser may read, also
 *      right after the browser allowed one and once after switching off. An
 *      instance waiting for a sign-in gets the session again, and a tab on a
 *      site's sign-in page wakes the watcher.
 *
 * Run by CI and by hand: `node extension/check-signin.mjs`
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const manifest = JSON.parse(readFileSync(join(here, 'src', 'manifest.json'), 'utf8'));
const problems = [];
const fail = (why) => problems.push(why);

const event = () => ({ addListener() {} });

const patreon = { id: 'patreon', name: 'Patreon', host: 'patreon.com', loginUrl: 'https://www.patreon.com/login', domains: ['patreon.com'], signIn: ['session_id'], waiting: false };
const youtube = { id: 'youtube', name: 'YouTube', host: 'youtube.com', loginUrl: 'https://accounts.google.com/', domains: ['youtube.com'], signIn: ['SID', 'LOGIN_INFO'], waiting: false };

const cookie = (over) => ({
  domain: '.patreon.com',
  hostOnly: false,
  path: '/',
  secure: true,
  httpOnly: true,
  session: false,
  expirationDate: 1.9e9,
  sameSite: 'lax',
  firstPartyDomain: '',
  name: 'session_id',
  value: 'S1',
  ...over,
});
const tracker = cookie({ name: 'patreon_device_id', value: 'D1', httpOnly: false });
const stray = cookie({ domain: '.notpatreon.com', name: 'session_id', value: 'not-patreon' });

/**
 * A browser: cookie stores by id, what the extension was granted, and what
 * else is switched on. `contains` answers as the browsers do, where
 * <all_urls> covers every site origin.
 */
function makeChrome({ stores = { 'firefox-default': [] }, granted = [], origins = [], storage = {}, privateStores = [], unlisted = [] } = {}) {
  const asked = [];
  const removed = [];
  const read = [];
  const have = { permissions: new Set(granted), origins: new Set(origins) };
  const store = { ...storage };
  const chrome = {
    runtime: { onInstalled: event(), onMessage: event(), onStartup: event(), onConnect: event(), getManifest: () => manifest, getURL: (p) => p },
    storage: {
      onChanged: event(),
      local: {
        get: async (keys) => {
          const list = keys == null ? Object.keys(store) : [].concat(keys);
          return structuredClone(Object.fromEntries(list.filter((k) => k in store).map((k) => [k, store[k]])));
        },
        set: async (o) => Object.assign(store, structuredClone(o)),
        remove: async (keys) => [].concat(keys).forEach((k) => delete store[k]),
      },
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
    },
    permissions: {
      onAdded: event(),
      onRemoved: event(),
      request: async (p) => {
        asked.push(p);
        (p.permissions ?? []).forEach((x) => have.permissions.add(x));
        (p.origins ?? []).forEach((x) => have.origins.add(x));
        return true;
      },
      contains: async (p) =>
        (p.permissions ?? []).every((x) => have.permissions.has(x)) &&
        (p.origins ?? []).every((o) => have.origins.has(o) || have.origins.has('<all_urls>')),
      remove: async (p) => {
        removed.push(p);
        (p.permissions ?? []).forEach((x) => have.permissions.delete(x));
        (p.origins ?? []).forEach((x) => have.origins.delete(x));
        return true;
      },
    },
    cookies: {
      onChanged: event(),
      getAllCookieStores: async () =>
        Object.keys(stores)
          .filter((id) => !unlisted.includes(id))
          .map((id) => ({ id, tabIds: [], incognito: privateStores.includes(id) })),
      getAll: async (q) => {
        if (!(q.storeId in stores)) throw new Error('Invalid cookie store id');
        read.push(q);
        const d = q.domain;
        return (stores[q.storeId] ?? []).filter((c) => {
          const cd = c.domain.replace(/^\./, '');
          return cd === d || cd.endsWith(`.${d}`) || d.endsWith(`.${cd}`);
        });
      },
    },
    action: { setBadgeText() {}, setBadgeBackgroundColor() {}, setTitle() {} },
    scripting: { getRegisteredContentScripts: async () => [], registerContentScripts: async () => {}, unregisterContentScripts: async () => {} },
    declarativeNetRequest: { updateEnabledRulesets: async () => {} },
    contextMenus: { create() {}, removeAll: async () => {}, onClicked: event() },
    alarms: { create() {}, onAlarm: event(), clear: async () => {} },
    tabs: { onRemoved: event(), onUpdated: event() },
    commands: { onCommand: event() },
  };
  return { chrome, asked, removed, read, have, store, stores };
}

function load(chrome) {
  const ctx = vm.createContext({
    chrome,
    console: { log() {}, warn() {}, error() {} },
    setTimeout,
    clearTimeout,
    crypto: globalThis.crypto,
    TextEncoder,
    TextDecoder,
    URL,
    navigator: { language: 'en-US', userAgent: 'Mozilla/5.0 (Test)' },
    WebSocket: class {},
    indexedDB: {},
  });
  const scripts = manifest.background.scripts.filter((f) => f !== 'background.js');
  if (!scripts.includes('signin.js')) fail('the background does not load signin.js, so nothing watches the sign-ins');
  for (const f of scripts.includes('signin.js') ? scripts : [...scripts, 'signin.js']) {
    vm.runInContext(readFileSync(join(here, 'src', f), 'utf8'), ctx, { filename: f });
  }
  return ctx;
}

const plain = (v) => (v === undefined ? v : JSON.parse(JSON.stringify(v)));
const fn = (ctx, name) => {
  try {
    return vm.runInContext(name, ctx);
  } catch {
    fail(`signin.js has no ${name}`);
    return async () => ({});
  }
};

/** Two instances; `lists` says which sites each lists and `answer` how a hand-over is taken. */
function group({ lists = { a: [patreon], b: [patreon] }, answer = () => ({ status: 200, body: JSON.stringify({ state: 'signedIn', account: 'Jane Doe' }) }) } = {}) {
  const siblings = Object.keys(lists).map((id) => ({ instanceId: id, name: id === 'a' ? 'attic' : 'cellar' }));
  const sent = [];
  const asked = [];
  const call = async (id, method, path, body) => {
    if (method === 'GET' && path.startsWith('/api/sitelogins/handover?')) {
      asked.push(`${id}:${new URL(path, 'https://x').searchParams.get('reads')}`);
      return { status: 200, body: JSON.stringify(lists[id]) };
    }
    if (method === 'POST') {
      sent.push({ id, path, cookies: JSON.parse(body).cookies });
      return answer(id, path);
    }
    return { status: 404, body: '' };
  };
  return { siblings, call, sent, asked };
}

const sentSummary = (sent) => sent.map((s) => `${s.id}:${s.path.split('/')[3]}:${s.cookies.map((c) => c.value).join('+') || 'none'}`).join(' ');

// 1. The access.
{
  const ctx = load(makeChrome().chrome);
  const access = plain(fn(ctx, 'signinAccess')([patreon, youtube]));
  const want = {
    permissions: ['cookies'],
    origins: ['https://patreon.com/*', 'https://*.patreon.com/*', 'https://youtube.com/*', 'https://*.youtube.com/*'],
  };
  if (JSON.stringify(access) !== JSON.stringify(want)) fail(`signinAccess = ${JSON.stringify(access)}, want ${JSON.stringify(want)}`);
}
{
  // Nothing granted yet: it is wanted but has to ask.
  const { chrome } = makeChrome();
  const ctx = load(chrome);
  const st = plain(await fn(ctx, 'signinState')([patreon]));
  if (!st.wanted || st.on) fail(`with nothing granted, signinState = ${JSON.stringify(st)}, want wanted and not on`);
}
{
  // Taking downloads over already holds the cookies and every site.
  const { chrome } = makeChrome({ granted: ['cookies', 'downloads', 'notifications'], origins: ['<all_urls>'], storage: { takeoverEnabled: true } });
  const ctx = load(chrome);
  const st = plain(await fn(ctx, 'signinState')([patreon, youtube]));
  if (!st.on) fail(`with taking over on, signinState = ${JSON.stringify(st)}, want on without asking`);
}
{
  // Turned off in the settings, it stays off whatever else holds the access.
  const { chrome } = makeChrome({ granted: ['cookies'], origins: ['<all_urls>'], storage: { signinEnabled: false } });
  const ctx = load(chrome);
  const st = plain(await fn(ctx, 'signinState')([patreon]));
  if (st.wanted || st.on) fail(`switched off, signinState = ${JSON.stringify(st)}, want off`);
}
{
  // Allowing asks once for exactly the access and keeps it.
  const { chrome, asked, removed, store } = makeChrome({ storage: { signinEnabled: false } });
  const ctx = load(chrome);
  const ok = await fn(ctx, 'requestSignins')([patreon]);
  if (!ok || asked.length !== 1 || JSON.stringify(asked[0]) !== JSON.stringify({ permissions: ['cookies'], origins: ['https://patreon.com/*', 'https://*.patreon.com/*'] })) {
    fail(`requestSignins asked ${JSON.stringify(asked)}`);
  }
  if (store.signinEnabled !== true) fail(`after allowing, signinEnabled = ${store.signinEnabled}, want true`);
  if (!(await fn(ctx, 'signinState')([patreon])).on) fail('after allowing, the sign-ins are not passed on');
  await fn(ctx, 'syncSignIns')(group().siblings, group().call);
  if (removed.length) fail(`a sync gave back ${JSON.stringify(removed)}, want the access kept`);
}
{
  // Turning off gives back the cookies and the sites' domains.
  const { chrome, removed, store } = makeChrome({ granted: ['cookies'], origins: ['https://patreon.com/*', 'https://*.patreon.com/*'], storage: { signinEnabled: true, signinSites: [patreon] } });
  const ctx = load(chrome);
  await fn(ctx, 'releaseSignins')();
  const back = plain(removed).flatMap((p) => [...(p.permissions ?? []), ...(p.origins ?? [])]);
  if (back.join() !== 'cookies,https://patreon.com/*,https://*.patreon.com/*') fail(`turning off gave back ${back}, want the cookies and the site's origins`);
  if (store.signinEnabled !== false) fail(`after turning off, signinEnabled = ${store.signinEnabled}, want false`);
}
{
  // While taking over runs on the cookies, only the sites' domains go.
  const { chrome, removed } = makeChrome({ granted: ['cookies', 'downloads', 'notifications'], origins: ['<all_urls>', 'https://patreon.com/*', 'https://*.patreon.com/*'], storage: { takeoverEnabled: true, signinEnabled: true, signinSites: [patreon] } });
  const ctx = load(chrome);
  await fn(ctx, 'releaseSignins')();
  const back = plain(removed).flatMap((p) => [...(p.permissions ?? []), ...(p.origins ?? [])]);
  if (back.join() !== 'https://patreon.com/*,https://*.patreon.com/*') fail(`with taking over on, turning off gave back ${back}, want the site's origins alone`);
}
{
  // Switching taking over off keeps the cookies the sign-ins still read.
  const { chrome, removed } = makeChrome({ granted: ['cookies', 'downloads', 'notifications'], origins: ['<all_urls>', 'https://patreon.com/*', 'https://*.patreon.com/*'], storage: { takeoverEnabled: true, signinEnabled: true, signinSites: [patreon] } });
  const ctx = load(chrome);
  await vm.runInContext('releaseFeature', ctx)('takeoverEnabled');
  const back = plain(removed).flatMap((p) => p.permissions ?? []);
  if (back.includes('cookies')) fail(`switching taking over off gave back ${back}, want the cookies kept for the sign-ins`);
}

const signedIn = () => makeChrome({ granted: ['cookies'], origins: ['https://patreon.com/*', 'https://*.patreon.com/*'], storage: { signinEnabled: true }, stores: { 'firefox-default': [cookie({}), tracker, stray] } });

// 2. An existing sign-in goes to every instance, once.
{
  const b = signedIn();
  const ctx = load(b.chrome);
  const g = group();
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (sentSummary(g.sent) !== 'a:patreon:S1+D1 b:patreon:S1+D1') fail(`an existing sign-in sent ${sentSummary(g.sent)}, want the site's cookies to both instances`);
  const fields = g.sent[0] ? Object.keys(g.sent[0].cookies[0]).sort().join() : '';
  if (fields !== 'domain,expirationDate,hostOnly,httpOnly,name,path,secure,session,value') fail(`a cookie travelled with ${fields}`);
  if (!b.read.every((q) => q.storeId === 'firefox-default')) fail(`read ${JSON.stringify(b.read)}, want the default store`);
  if (!plain(b.store.signinSites ?? []).some((s) => s.id === 'patreon')) fail('the listed sites were not kept for the permission prompt');

  const again = group();
  await fn(ctx, 'syncSignIns')(again.siblings, again.call);
  if (again.sent.length) fail(`the same session went out again: ${sentSummary(again.sent)}`);

  // Only a new session goes out again; a tracking cookie changing is no new session.
  b.stores['firefox-default'] = [cookie({}), { ...tracker, value: 'D2' }];
  const tracked = group();
  await fn(ctx, 'syncSignIns')(tracked.siblings, tracked.call);
  if (tracked.sent.length) fail(`a changed tracking cookie sent ${sentSummary(tracked.sent)}, want nothing`);
  b.stores['firefox-default'] = [cookie({ value: 'S2' })];
  const renewed = group();
  await fn(ctx, 'syncSignIns')(renewed.siblings, renewed.call);
  if (sentSummary(renewed.sent) !== 'a:patreon:S2 b:patreon:S2') fail(`a new session sent ${sentSummary(renewed.sent)}, want it to both instances`);
}
{
  // An instance that lists no such site gets nothing, and one that joins later gets it.
  const b = signedIn();
  const ctx = load(b.chrome);
  const first = group({ lists: { a: [patreon], b: [youtube] } });
  await fn(ctx, 'syncSignIns')(first.siblings, first.call);
  if (sentSummary(first.sent) !== 'a:patreon:S1+D1') fail(`with one instance listing the site, sent ${sentSummary(first.sent)}`);
  const later = group({ lists: { a: [patreon], b: [patreon] } });
  await fn(ctx, 'syncSignIns')(later.siblings, later.call);
  if (sentSummary(later.sent) !== 'b:patreon:S1+D1') fail(`an instance listing the site later got ${sentSummary(later.sent)}, want it alone`);
}
{
  // The site turning the session down is not asked again until it changes;
  // an instance that did not answer is.
  const b = signedIn();
  const ctx = load(b.chrome);
  const refused = group({ answer: (id) => (id === 'a' ? { status: 422, body: 'no session' } : { status: 502, body: 'down' }) });
  await fn(ctx, 'syncSignIns')(refused.siblings, refused.call);
  const outage = plain(await fn(ctx, 'syncSignIns')(refused.siblings, refused.call));
  if (outage?.retry !== true) fail(`after an instance did not take the session, syncSignIns = ${JSON.stringify(outage)}, want a retry`);
  const next = group();
  const settled = plain(await fn(ctx, 'syncSignIns')(next.siblings, next.call));
  if (sentSummary(next.sent) !== 'b:patreon:S1+D1') fail(`after a refusal and an outage, sent ${sentSummary(next.sent)}, want only the instance that did not answer`);
  if (settled?.retry !== false) fail(`once every instance has it, syncSignIns = ${JSON.stringify(settled)}, want no retry`);
}
{
  // An instance that does not answer the list is asked again later.
  const b = signedIn();
  const ctx = load(b.chrome);
  const g = group();
  const silent = async (id, method, path, body) => (id === 'b' ? null : g.call(id, method, path, body));
  const got = plain(await fn(ctx, 'syncSignIns')(g.siblings, silent));
  if (got?.retry !== true) fail(`with an instance that did not answer, syncSignIns = ${JSON.stringify(got)}, want a retry`);
}

// 3. No sign-in, and signing out.
{
  const b = makeChrome({ granted: ['cookies'], origins: ['https://patreon.com/*', 'https://*.patreon.com/*'], stores: { 'firefox-default': [tracker, stray] } });
  const ctx = load(b.chrome);
  const g = group();
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (g.sent.length) fail(`without a sign-in, sent ${sentSummary(g.sent)}`);
}
{
  const b = signedIn();
  const ctx = load(b.chrome);
  const g = group();
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  b.stores['firefox-default'] = [tracker];
  const out = group();
  await fn(ctx, 'syncSignIns')(out.siblings, out.call);
  if (sentSummary(out.sent) !== 'a:patreon:none b:patreon:none') fail(`signing out sent ${sentSummary(out.sent)}, want an empty hand-over to both`);
  const quiet = group();
  await fn(ctx, 'syncSignIns')(quiet.siblings, quiet.call);
  if (quiet.sent.length) fail(`signed out, the next sync sent ${sentSummary(quiet.sent)}, want nothing`);
}

// 4. Which store.
{
  const b = makeChrome({
    granted: ['cookies'],
    origins: ['https://patreon.com/*', 'https://*.patreon.com/*'],
    stores: { 'firefox-default': [tracker], 'firefox-private': [cookie({ value: 'PRIVATE' })], 'firefox-container-2': [cookie({ value: 'C2' })] },
    privateStores: ['firefox-private'],
  });
  const ctx = load(b.chrome);
  const g = group({ lists: { a: [patreon] } });
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (sentSummary(g.sent) !== 'a:patreon:C2') fail(`with a signed-in container, sent ${sentSummary(g.sent)}, want the container's session`);
  if (b.read.some((q) => q.storeId === 'firefox-private')) fail('a private store was read');
}
{
  // Firefox lists a container only while one of its tabs is open.
  const b = makeChrome({
    granted: ['cookies'],
    origins: ['https://patreon.com/*', 'https://*.patreon.com/*'],
    stores: { 'firefox-default': [tracker], 'firefox-container-3': [cookie({ value: 'C3' })] },
    unlisted: ['firefox-container-3'],
  });
  const ctx = load(b.chrome);
  const g = group({ lists: { a: [patreon] } });
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (sentSummary(g.sent) !== 'a:patreon:C3') fail(`with a signed-in container and none of its tabs open, sent ${sentSummary(g.sent)}, want the container's session`);
}
{
  const b = makeChrome({
    granted: ['cookies'],
    origins: ['https://patreon.com/*', 'https://*.patreon.com/*'],
    stores: { 'firefox-default': [cookie({ value: 'D' })], 'firefox-container-2': [cookie({ value: 'C2' })], 'firefox-container-3': [cookie({ value: 'C3' })] },
  });
  const ctx = load(b.chrome);
  const note = fn(ctx, 'noteSignInChange');
  note({ removed: false, cause: 'explicit', cookie: cookie({ value: 'C3', storeId: 'firefox-container-3' }) }, 1000);
  note({ removed: false, cause: 'explicit', cookie: cookie({ value: 'C2', storeId: 'firefox-container-2' }) }, 2000);
  const g = group({ lists: { a: [patreon] } });
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (sentSummary(g.sent) !== 'a:patreon:C2') fail(`of three signed-in stores, sent ${sentSummary(g.sent)}, want the one changed last`);
  if (Object.keys(plain(b.store.signinChanged ?? {})).length === 0) fail('when each store changed was not kept for the next start');
}

// 5. What wakes it, and off means off.
{
  const ctx = load(makeChrome({ storage: { signinSites: [patreon] } }).chrome);
  const matters = fn(ctx, 'signInCookieChanged');
  const cases = [
    [cookie({ storeId: 'firefox-default' }), true],
    [cookie({ storeId: 'firefox-container-2', domain: 'www.patreon.com' }), true],
    [cookie({ storeId: 'firefox-default', name: 'patreon_device_id' }), false],
    [cookie({ storeId: 'firefox-private' }), false],
    [cookie({ storeId: '1' }), false],
    [cookie({ storeId: 'firefox-default', domain: '.notpatreon.com' }), false],
  ];
  for (const [c, want] of cases) {
    const got = matters([patreon], { removed: false, cause: 'explicit', cookie: c });
    if (got !== want) fail(`signInCookieChanged(${c.name} on ${c.domain} in ${c.storeId}) = ${got}, want ${want}`);
  }
}
{
  const b = makeChrome({ granted: ['cookies'], origins: ['<all_urls>'], storage: { signinEnabled: false }, stores: { 'firefox-default': [cookie({})] } });
  const ctx = load(b.chrome);
  const g = group();
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (g.sent.length || b.read.length) fail(`switched off, a sync read ${b.read.length} times and sent ${sentSummary(g.sent)}`);
}

// 6. What the instances hear, and an instance that waits.
{
  const b = signedIn();
  const ctx = load(b.chrome);
  const g = group();
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (g.asked.join(' ') !== 'a: b: a:patreon b:patreon') fail(`a first sync asked ${g.asked.join(' ')}, want the lists and then the site this browser reads`);
  const again = group();
  await fn(ctx, 'syncSignIns')(again.siblings, again.call);
  if (again.asked.join(' ') !== 'a:patreon b:patreon') fail(`the next sync asked ${again.asked.join(' ')}, want each list once with the site this browser reads`);

  const waits = group({ lists: { a: [{ ...patreon, waiting: true }], b: [patreon] } });
  await fn(ctx, 'syncSignIns')(waits.siblings, waits.call);
  if (sentSummary(waits.sent) !== 'a:patreon:S1+D1') fail(`with one instance waiting, sent ${sentSummary(waits.sent)}, want the same session to that one again`);

  b.store.signinEnabled = false;
  const off = group();
  await fn(ctx, 'syncSignIns')(off.siblings, off.call);
  if (off.asked.join(' ') !== 'a: b:' || off.sent.length) fail(`switched off, a sync asked ${off.asked.join(' ')} and sent ${sentSummary(off.sent)}, want the lists once saying nothing is read`);
  const quiet = group();
  await fn(ctx, 'syncSignIns')(quiet.siblings, quiet.call);
  if (quiet.asked.length) fail(`switched off and said so, the next sync asked ${quiet.asked.join(' ')}`);
}
{
  // Without the access, the lists say this browser reads nothing.
  const ctx = load(makeChrome({ storage: { signinSites: [patreon] } }).chrome);
  const g = group();
  await fn(ctx, 'syncSignIns')(g.siblings, g.call);
  if (g.asked.join(' ') !== 'a: b:') fail(`without the access, a sync asked ${g.asked.join(' ')}, want lists that read nothing`);
}
{
  const ctx = load(makeChrome().chrome);
  const opened = fn(ctx, 'signInPageOpened');
  const cases = [
    ['https://www.patreon.com/login', true],
    ['https://www.patreon.com/login?ru=%2Fhome', true],
    ['https://www.patreon.com/home', false],
    ['https://patreon.com.example/login', false],
    ['not an address', false],
  ];
  for (const [url, want] of cases) {
    const got = opened([patreon, { ...youtube, loginUrl: undefined }], url);
    if (got !== want) fail(`signInPageOpened(${url}) = ${got}, want ${want}`);
  }
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: the sign-ins of listed sites go to KnightLoader by themselves, once, and signing out follows');
