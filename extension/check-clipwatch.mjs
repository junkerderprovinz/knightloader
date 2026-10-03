/**
 * The clipboard watch (src/clipwatch.js and its part of background.js).
 *
 *   1. The link rule is the web interface's (LOOKS_LIKE_A_LINK in
 *      web/src/lib/clipboardWatch.ts), so a copied text counts as a link in
 *      both places or in neither.
 *   2. Only the links of a copied text are handed on, never the rest of it.
 *   3. The poller treats what is on the clipboard when it starts as old, sends
 *      a changed text once, ignores text without links, and stops after three
 *      failed reads in a row.
 *   4. The background sends links only from an extension page, pulls them out
 *      of the message again, sends them to the default instance, and switches
 *      the watch off when a renewal says another device asked it to stop.
 *   5. A new start of the background leaves the renewal alarm as it runs.
 *
 * Run by CI and by hand: `node extension/check-clipwatch.mjs`.
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];
const fail = (why) => problems.push(why);
const source = (f) => readFileSync(join(here, 'src', f), 'utf8');

function context(extra = {}) {
  const ctx = vm.createContext({ setTimeout, clearTimeout, console, crypto: globalThis.crypto, ...extra });
  vm.runInContext(source('clipwatch.js'), ctx, { filename: 'clipwatch.js' });
  return ctx;
}

// 1. One rule in both places.
{
  const web = readFileSync(join(here, '..', 'web', 'src', 'lib', 'clipboardWatch.ts'), 'utf8');
  const webRule = web.match(/const LOOKS_LIKE_A_LINK = (\/.*\/[a-z]*);/)?.[1];
  const extRule = source('clipwatch.js').match(/const CLIP_LINK_RULE = (\/.*\/[a-z]*);/)?.[1];
  if (!webRule || !extRule) fail('could not find the link rule in clipboardWatch.ts or clipwatch.js');
  else if (webRule !== extRule) fail(`the extension's link rule ${extRule} differs from the web interface's ${webRule}`);
}

// 2. Only the links.
{
  const ctx = context();
  const clipLinks = vm.runInContext('clipLinks', ctx);
  const cases = [
    ['my password is hunter2', ''],
    ['https://files.example/a.zip', 'https://files.example/a.zip'],
    ['see https://files.example/a.zip and\nmagnet:?xt=urn:btih:abc too', 'https://files.example/a.zip\nmagnet:?xt=urn:btih:abc'],
    ['notes ftp://host/file secret words', 'ftp://host/file'],
    ['xhttps://glued.example', ''],
  ];
  for (const [text, want] of cases) {
    const got = clipLinks(text);
    if (got !== want) fail(`clipLinks(${JSON.stringify(text)}) = ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  }
}

// 3. The poller.
{
  const ctx = context();
  const start = vm.runInContext('startClipPoller', ctx);
  const reads = ['https://old.example/x', 'https://old.example/x', 'just words', 'https://new.example/y', 'https://new.example/y'];
  const sent = [];
  let refused = false;
  const queue = [...reads];
  const stop = start({
    remembered: null,
    onLinks: (l) => sent.push(l),
    onSeen: () => {},
    onRefused: () => (refused = true),
    read: async () => {
      if (queue.length) return queue.shift();
      throw new Error('denied');
    },
  });
  // Five reads and three refusals take about ten seconds at the poller's pace.
  const deadline = Date.now() + 15000;
  while (!refused && Date.now() < deadline) await new Promise((r) => setTimeout(r, 100));
  stop();
  if (sent.join('|') !== 'https://new.example/y') fail(`the poller sent ${JSON.stringify(sent)}, want only the new link, once`);
  if (!refused) fail('three failed reads in a row did not end the watch');
}

// A remembered hash makes a text copied while the reader was gone count as new.
{
  const ctx = context();
  const start = vm.runInContext('startClipPoller', ctx);
  const hash = vm.runInContext('clipHash', ctx);
  const sent = [];
  const stop = start({
    remembered: hash('older text'),
    onLinks: (l) => sent.push(l),
    onSeen: () => {},
    onRefused: () => {},
    read: async () => 'https://while-asleep.example/z',
  });
  await new Promise((r) => setTimeout(r, 50));
  stop();
  if (sent.join('|') !== 'https://while-asleep.example/z') fail(`after a restart the poller sent ${JSON.stringify(sent)}, want the link copied in between`);
}

// 4. The background.
function event() {
  const listeners = [];
  return { addListener: (f) => listeners.push(f), listeners };
}

const TARGET = 'a'.repeat(40);

function background({ stopAnswer = false, alarms = new Map() } = {}) {
  const store = { clipWatch: true, clipWatcherId: 'ext-test' };
  const calls = [];
  const chrome = {
    runtime: { onInstalled: event(), onMessage: event(), onStartup: event(), getURL: (p) => `chrome-extension://test/${p}`, getManifest: () => ({}) },
    storage: {
      onChanged: event(),
      local: {
        get: async (keys) => {
          const list = Array.isArray(keys) ? keys : [keys];
          return Object.fromEntries(list.filter((k) => k in store).map((k) => [k, store[k]]));
        },
        set: async (o) => Object.assign(store, o),
        remove: async () => {},
      },
      session: { get: async () => ({}), set: async () => {} },
    },
    permissions: { onAdded: event(), onRemoved: event(), contains: async (p) => !!p?.permissions?.includes('clipboardRead') },
    action: { setBadgeText() {}, setBadgeBackgroundColor() {}, setTitle() {} },
    scripting: { getRegisteredContentScripts: async () => [], registerContentScripts: async () => {}, unregisterContentScripts: async () => {} },
    declarativeNetRequest: { updateEnabledRulesets: async () => {} },
    offscreen: { createDocument: async () => {}, closeDocument: async () => {} },
    alarms: {
      get: async (name) => alarms.get(name),
      create: (name, info) => alarms.set(name, { name, ...info, created: (alarms.get(name)?.created ?? 0) + 1 }),
      clear: async (name) => alarms.delete(name),
      onAlarm: event(),
    },
  };
  const ctx = vm.createContext({
    chrome, setTimeout, clearTimeout, console: { log() {}, warn() {}, error() {} }, crypto: globalThis.crypto,
    TextEncoder, TextDecoder, URL, navigator: { language: 'en', languages: ['en'], userAgent: 'Mozilla/5.0 (Windows NT 10.0) Firefox/140.0' },
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'), atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    indexedDB: {}, WebSocket: class {},
  });
  const manifest = JSON.parse(source('manifest.json'));
  for (const f of manifest.background.scripts) vm.runInContext(source(f), ctx, { filename: f });
  // The relay is replaced by one instance that records what it is asked.
  ctx.withGroup = async (work) =>
    work({
      siblings: [{ instanceId: TARGET, name: 'NAS' }],
      call: async (target, method, path, body) => {
        calls.push({ target, method, path, body });
        if (method === 'PUT') return { status: 200, body: JSON.stringify({ stop: stopAnswer }) };
        return { status: 200, body: '[]' };
      },
    });
  ctx.readDefaultTarget = async () => '';
  return { ctx, chrome, store, calls };
}

{
  const { chrome, calls } = background();
  const send = (msg, url) => {
    for (const f of chrome.runtime.onMessage.listeners) f(msg, { url }, () => {});
  };
  send({ type: 'knightloader-clip', links: 'https://files.example/a' }, 'https://evil.example/');
  await new Promise((r) => setTimeout(r, 30));
  if (calls.some((c) => c.path === '/api/links')) fail('a website could make the extension send links as if they were copied');

  send({ type: 'knightloader-clip', links: 'top secret https://files.example/a more secrets' }, 'chrome-extension://test/offscreen.html');
  await new Promise((r) => setTimeout(r, 30));
  const sent = calls.find((c) => c.path === '/api/links');
  if (!sent) fail('links from the offscreen document were not sent');
  else {
    if (sent.target !== TARGET) fail(`copied links went to ${sent.target}, want the default instance`);
    const body = JSON.parse(sent.body);
    if (body.links !== 'https://files.example/a') fail(`the instance received ${JSON.stringify(body.links)}, want the link alone`);
  }
}

{
  const { ctx, store, calls } = background({ stopAnswer: true });
  await vm.runInContext('renewClipLease', ctx)();
  const put = calls.find((c) => c.method === 'PUT');
  if (!put || put.path !== '/api/clipboard-watchers/ext-test') fail(`the lease was renewed at ${put?.path}, want /api/clipboard-watchers/ext-test`);
  else if (JSON.parse(put.body).kind !== 'extension' || JSON.parse(put.body).name !== 'Firefox, Windows') fail(`the lease says ${put.body}, want kind extension and name "Firefox, Windows"`);
  if (store.clipWatch !== false || store.clipNotice !== 'stopped') fail('a renewal answered with stop left the watch on or did not say why');
}


// The browser starts the background again and again, often more than once a
// minute. Each start must leave the renewal alarm running, not set it back.
{
  const alarms = new Map();
  for (let start = 0; start < 3; start++) {
    background({ alarms });
    await new Promise((r) => setTimeout(r, 30));
  }
  const made = alarms.get('knightloader-clip')?.created ?? 0;
  if (made !== 1) fail(`three starts of the background created the renewal alarm ${made} times, want once`);
}

if (problems.length) {
  for (const p of problems) console.error(`- ${p}`);
  console.error(`${problems.length} problem(s)`);
  process.exit(1);
}
console.log('ok: one link rule in both places, only links leave, the poller sends each new link once and gives up after refusals, only extension pages can send, a stop from elsewhere switches the watch off, a restart of the background keeps the renewal alarm');
