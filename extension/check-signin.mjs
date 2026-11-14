/**
 * Handing a site's sign-in over to KnightLoader (src/signin.js), run against
 * a stubbed `chrome`.
 *
 *   1. The access asked for is the cookies of the one site, on its own
 *      domains, and nothing else.
 *   2. It is asked for before anything else happens, so the click still
 *      counts as the user's, and given back once the cookies are read unless
 *      another feature runs on it.
 *   3. Only the site's own cookies travel, from the cookie store of the tab
 *      the sign-in came from. A refusal or a browser without a sign-in sends
 *      nothing.
 *   4. The popup offers only the sites an instance waits for, and ignores a
 *      site whose domains could reach further than one site.
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

const patreon = { id: 'patreon', name: 'Patreon', host: 'patreon.com', loginUrl: 'https://www.patreon.com/login', domains: ['patreon.com'], waiting: true };

function makeChrome({ grant = true, takeover = false, allSites = false, cookies = [] } = {}) {
  const order = [];
  const asked = [];
  const removed = [];
  const read = [];
  const store = { takeoverEnabled: takeover };
  const chrome = {
    runtime: { onInstalled: event(), onMessage: event(), onStartup: event(), onConnect: event(), getManifest: () => manifest, getURL: (p) => p },
    storage: {
      onChanged: event(),
      local: {
        get: async (keys) => {
          const list = keys == null ? Object.keys(store) : [].concat(keys);
          return Object.fromEntries(list.filter((k) => k in store).map((k) => [k, store[k]]));
        },
        set: async (o) => Object.assign(store, o),
        remove: async () => {},
      },
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
    },
    permissions: {
      onAdded: event(),
      onRemoved: event(),
      request: async (p) => {
        order.push('request');
        asked.push(p);
        return grant;
      },
      contains: async (p) =>
        (takeover || allSites) &&
        (p.permissions ?? []).every((x) => takeover && ['downloads', 'cookies', 'notifications'].includes(x)) &&
        (p.origins ?? []).every((o) => o === '<all_urls>'),
      remove: async (p) => {
        removed.push(p);
        return true;
      },
    },
    cookies: {
      getAll: async (q) => {
        order.push('cookies');
        read.push(q);
        return cookies;
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
  return { chrome, order, asked, removed, read };
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
  for (const f of [...manifest.background.scripts.filter((f) => f !== 'background.js'), 'signin.js']) {
    vm.runInContext(readFileSync(join(here, 'src', f), 'utf8'), ctx, { filename: f });
  }
  return ctx;
}

const plain = (v) => JSON.parse(JSON.stringify(v));
const sessionCookie = { domain: '.patreon.com', hostOnly: false, path: '/', secure: true, httpOnly: true, session: false, expirationDate: 1.9e9, name: 'session_id', value: 'S1', storeId: 'firefox-container-2', sameSite: 'lax' };
const strayCookie = { domain: '.notpatreon.com', hostOnly: false, path: '/', secure: true, session: false, expirationDate: 1.9e9, name: 'x', value: 'NOT-PATREON' };

// 1. The access.
{
  const ctx = load(makeChrome().chrome);
  const access = plain(vm.runInContext('siteAccess', ctx)(patreon));
  const want = { permissions: ['cookies'], origins: ['https://patreon.com/*', 'https://*.patreon.com/*'] };
  if (JSON.stringify(access) !== JSON.stringify(want)) fail(`siteAccess(patreon) = ${JSON.stringify(access)}, want ${JSON.stringify(want)}`);
}

// 2 and 3. A hand-over.
{
  const { chrome, order, asked, removed, read } = makeChrome({ cookies: [sessionCookie, strayCookie] });
  const ctx = load(chrome);
  const sent = [];
  const send = async (method, path, body) => {
    order.push('send');
    sent.push({ method, path, body: JSON.parse(body) });
    return { status: 200, body: JSON.stringify({ id: 'patreon', state: 'signedIn', account: 'Jane Doe' }) };
  };
  const got = plain(await vm.runInContext('signInWith', ctx)(patreon, { storeId: 'firefox-container-2', send }));
  if (got.outcome !== 'done' || got.account !== 'Jane Doe') fail(`signInWith = ${JSON.stringify(got)}, want done as Jane Doe`);
  if (order[0] !== 'request') fail(`the first thing a hand-over did was ${order[0]}, want the permission request`);
  if (order.indexOf('send') < order.lastIndexOf('cookies')) fail(`the session was sent before the cookies were read: ${order}`);
  if (asked.length !== 1) fail(`${asked.length} permission requests, want one`);
  if (!read.every((q) => q.storeId === 'firefox-container-2' && q.domain === 'patreon.com')) fail(`cookies read with ${JSON.stringify(read)}, want the site's domain in the tab's cookie store`);
  const body = sent[0]?.body;
  if (sent.length !== 1 || sent[0].method !== 'POST' || sent[0].path !== '/api/sitelogins/patreon/handover') fail(`sent ${JSON.stringify(sent)}`);
  const names = (body?.cookies ?? []).map((c) => c.name);
  if (names.join() !== 'session_id') fail(`the hand-over carried ${names}, want the site's session cookie alone`);
  if (body?.cookies?.[0] && ('storeId' in body.cookies[0] || 'sameSite' in body.cookies[0])) fail('a cookie travelled with fields the instance does not read');
  const back = plain(removed);
  if (back.length !== 1 || back[0].permissions?.join() !== 'cookies' || back[0].origins?.length !== 2) fail(`gave back ${JSON.stringify(back)}, want the cookies and the site's origins`);
}

// 2. Taking downloads over keeps the cookies, and the site's own domains go
// back even while Click'n'Load holds all websites, which Firefox lists apart.
{
  const { chrome, removed } = makeChrome({ takeover: true, cookies: [sessionCookie] });
  const ctx = load(chrome);
  const send = async () => ({ status: 200, body: '{}' });
  await vm.runInContext('signInWith', ctx)(patreon, { send });
  const back = plain(removed).flatMap((p) => [...(p.permissions ?? []), ...(p.origins ?? [])]);
  if (back.join() !== 'https://patreon.com/*,https://*.patreon.com/*') fail(`with taking over on, the hand-over gave back ${back}, want the site's origins alone`);
}
{
  const { chrome, removed } = makeChrome({ allSites: true, cookies: [sessionCookie] });
  await chrome.storage.local.set({ cnlEnabled: true });
  const ctx = load(chrome);
  const send = async () => ({ status: 200, body: '{}' });
  await vm.runInContext('signInWith', ctx)(patreon, { send });
  const back = plain(removed).flatMap((p) => [...(p.permissions ?? []), ...(p.origins ?? [])]);
  if (back.join() !== 'cookies,https://patreon.com/*,https://*.patreon.com/*') fail(`with Click'n'Load on, the hand-over gave back ${back}, want the cookies and the site's origins`);
}

// 3. Nothing goes out after a refusal or without a sign-in.
{
  const { chrome, order } = makeChrome({ grant: false, cookies: [sessionCookie] });
  const ctx = load(chrome);
  let called = false;
  const got = plain(await vm.runInContext('signInWith', ctx)(patreon, { send: async () => { called = true; } }));
  if (got.outcome !== 'refused' || called || order.includes('cookies')) fail(`after a refusal: ${JSON.stringify(got)}, sent ${called}, ${order}`);
}
{
  const { chrome } = makeChrome({ cookies: [strayCookie] });
  const ctx = load(chrome);
  let called = false;
  const got = plain(await vm.runInContext('signInWith', ctx)(patreon, { send: async () => { called = true; } }));
  if (got.outcome !== 'noSession' || called) fail(`without a sign-in: ${JSON.stringify(got)}, sent ${called}`);
}
{
  const { chrome } = makeChrome({ cookies: [sessionCookie] });
  const ctx = load(chrome);
  const got = plain(await vm.runInContext('signInWith', ctx)(patreon, { send: async () => ({ status: 502, body: 'down' }) }));
  if (got.outcome !== 'failed') fail(`an instance that did not take it: ${JSON.stringify(got)}, want failed`);
}

// 4. What the popup offers.
{
  const ctx = load(makeChrome().chrome);
  const siblings = [{ instanceId: 'a', name: 'attic' }, { instanceId: 'b', name: 'cellar' }];
  const lists = {
    a: [patreon, { ...patreon, id: 'youtube', name: 'YouTube', domains: ['youtube.com'], waiting: false }],
    b: [{ ...patreon, id: 'wide', domains: ['com'] }, { ...patreon, id: 'all', domains: ['*'] }, { id: 7 }],
  };
  const call = async (id, method, path) => {
    if (method !== 'GET' || path !== '/api/sitelogins/handover') return { status: 404, body: '' };
    return { status: 200, body: JSON.stringify(lists[id]) };
  };
  const got = plain(await vm.runInContext('waitingSignIns', ctx)(siblings, call));
  const ids = got.map((w) => `${w.instance.instanceId}:${w.site.id}`);
  if (ids.join() !== 'a:patreon') fail(`waitingSignIns = ${ids}, want only the site attic waits for`);
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: a site sign-in reads one site, asks first and sends only its session');
