/**
 * Taking downloads over from the browser and finding media (src/capture.js,
 * src/takeover.js, src/media.js), run against a stubbed `chrome`.
 *
 *   1. The rules: which downloads go to KnightLoader and which stay, and which
 *      responses count as media.
 *   2. The headers a link is handed over with: the cookies for its own
 *      address, the page and the user agent, nothing empty.
 *   3. A download is cancelled only once the instance has it. A hand-over that
 *      fails lets it carry on, in Chromium (held while its name is decided)
 *      and in Firefox (paused and resumed).
 *   4. Only an extension page can send a stream, and switching one feature off
 *      leaves the access another one still runs on.
 *
 * Run by CI and by hand: `node extension/check-capture.mjs`
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const manifest = JSON.parse(readFileSync(join(here, 'src', 'manifest.json'), 'utf8'));
const problems = [];
const fail = (why) => problems.push(why);
const settle = () => new Promise((r) => setTimeout(r, 30));

function event() {
  const listeners = [];
  return { addListener: (f) => listeners.push(f), listeners };
}

function makeChrome({ chromium = true, granted = true } = {}) {
  const calls = { cancelled: [], erased: [], paused: [], resumed: [], removed: [], notified: [], badge: [] };
  const store = { takeoverEnabled: true, mediaEnabled: true };
  const session = {};
  const access = { granted, permissions: new Set(['downloads', 'cookies', 'notifications', 'webRequest']), origins: new Set(['<all_urls>']) };
  const downloads = {
    onCreated: event(),
    pause: async (id) => { calls.paused.push(id); },
    resume: async (id) => { calls.resumed.push(id); },
    cancel: async (id) => { calls.cancelled.push(id); },
    erase: async (q) => { calls.erased.push(q.id); },
  };
  if (chromium) downloads.onDeterminingFilename = event();
  const chrome = {
    runtime: {
      onInstalled: event(), onMessage: event(), onStartup: event(),
      openOptionsPage() {}, getManifest: () => manifest,
      getURL: (p) => `chrome-extension://test/${p}`,
    },
    storage: {
      onChanged: event(),
      local: {
        get: async (keys) => {
          const list = keys == null ? Object.keys(store) : Array.isArray(keys) ? keys : [keys];
          return Object.fromEntries(list.filter((k) => k in store).map((k) => [k, store[k]]));
        },
        set: async (o) => { Object.assign(store, o); },
        remove: async () => {},
      },
      session: {
        get: async (k) => (k == null ? { ...session } : k in session ? { [k]: session[k] } : {}),
        set: async (o) => { Object.assign(session, o); },
        remove: async (k) => { for (const x of [].concat(k)) delete session[x]; },
      },
    },
    permissions: {
      onAdded: event(), onRemoved: event(),
      contains: async (p) => access.granted && (p.permissions ?? []).every((x) => access.permissions.has(x)) && (p.origins ?? []).every((x) => access.origins.has(x)),
      remove: async (p) => {
        calls.removed.push(...(p.permissions ?? []), ...(p.origins ?? []));
        for (const x of p.permissions ?? []) access.permissions.delete(x);
        for (const x of p.origins ?? []) access.origins.delete(x);
        return true;
      },
    },
    action: {
      getUserSettings: async () => ({ isOnToolbar: true }),
      setBadgeText: ({ text }) => { if (text) calls.badge.push(text); }, setBadgeBackgroundColor() {}, setTitle() {}, openPopup: async () => {},
    },
    scripting: {
      getRegisteredContentScripts: async () => [],
      registerContentScripts: async () => {},
      unregisterContentScripts: async () => {},
    },
    declarativeNetRequest: { updateEnabledRulesets: async () => {} },
    downloads,
    cookies: {
      getAll: async ({ url }) => (new URL(url).hostname === 'files.example' ? [{ name: 'session', value: 'abc' }, { name: 'lang', value: 'de' }] : []),
    },
    notifications: { create: async (id, o) => { calls.notified.push(o.message); }, clear: async () => {} },
    webRequest: { onResponseStarted: event() },
    tabs: { onRemoved: event() },
  };
  return { chrome, calls, store, session, access };
}

function load(chrome) {
  const ctx = vm.createContext({
    chrome, console: { log() {}, warn() {}, error() {} }, setTimeout, clearTimeout,
    crypto: globalThis.crypto, TextEncoder, TextDecoder, URL,
    navigator: { language: 'en-US', userAgent: 'Mozilla/5.0 (Test)' },
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'), atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    WebSocket: class {}, indexedDB: {},
  });
  for (const f of manifest.background.scripts) {
    vm.runInContext(readFileSync(join(here, 'src', f), 'utf8'), ctx, { filename: f });
  }
  return ctx;
}

// 1. The rules.
{
  const ctx = load(makeChrome().chrome);
  const verdict = vm.runInContext('takeoverVerdict', ctx);
  const rules = { types: 'zip, .MKV', minSizeMb: 10, skip: 'skip.example', bypassKey: 'alt' };
  const big = 50 * 1024 * 1024;
  const base = { url: 'https://files.example/get/film.mkv', referrer: 'https://files.example/thread', filename: '/home/me/film.mkv', totalBytes: big };
  const cases = [
    [base, true, ''],
    [{ ...base, filename: '', url: 'https://files.example/a.zip' }, true, 'the address names the type before the browser does'],
    [{ ...base, filename: 'C:\\Users\\me\\film.MKV' }, true, 'the type list ignores case and dots'],
    [{ ...base, filename: 'notes.pdf' }, false, 'type'],
    [{ ...base, totalBytes: 5 * 1024 * 1024 }, false, 'size'],
    [{ ...base, totalBytes: -1, fileSize: 0 }, false, 'size'],
    [{ ...base, url: 'https://cdn.skip.example/film.mkv' }, false, 'skipped'],
    [{ ...base, referrer: 'https://skip.example/page' }, false, 'skipped'],
    [{ ...base, url: 'http://192.168.1.20:8080/api/tasks/1/file' }, false, 'local'],
    [{ ...base, url: 'http://nas/film.mkv' }, false, 'local'],
    [{ ...base, referrer: 'http://knightloader.local/' }, false, 'local'],
    [{ ...base, url: 'blob:https://files.example/123' }, false, 'scheme'],
    [{ ...base, incognito: true }, false, 'private'],
    [{ ...base, byExtensionId: 'other' }, false, 'extension'],
  ];
  for (const [item, take, why] of cases) {
    const got = verdict(item, rules, 1000, 0);
    if (got.take !== take || (!take && got.why !== why)) fail(`takeoverVerdict(${JSON.stringify(item)}) = ${JSON.stringify(got)}, want take ${take}${take ? '' : ` because ${why}`}`);
  }
  if (verdict(base, rules, 1000, 2000).take) fail('a download right after a click with the bypass key held was taken over');
  if (!verdict(base, { ...rules, types: '', minSizeMb: 0 }, 1000, 0).take) fail('with no type and no size rule a download was not taken');

  const kind = vm.runInContext('mediaKind', ctx);
  const kinds = [
    ['https://cdn.example/live/index.m3u8?token=1', '', 'xmlhttprequest', 'hls'],
    ['https://cdn.example/play', 'application/vnd.apple.mpegURL; charset=utf-8', 'xmlhttprequest', 'hls'],
    ['https://cdn.example/manifest.mpd', '', 'other', 'dash'],
    ['https://cdn.example/film.mp4', 'video/mp4', 'media', 'video'],
    ['https://cdn.example/song', 'audio/mpeg', 'media', 'audio'],
    ['https://cdn.example/seg-12.ts', 'video/mp2t', 'media', ''],
    ['https://cdn.example/chunk-3.m4s', 'video/mp4', 'media', ''],
    ['https://cdn.example/range.mp4', 'video/mp4', 'xmlhttprequest', ''],
    ['https://cdn.example/app.js', 'text/javascript', 'other', ''],
  ];
  for (const [url, mime, type, want] of kinds) {
    const got = kind(url, mime, type);
    if (got !== want) fail(`mediaKind(${url}, ${mime}, ${type}) = '${got}', want '${want}'`);
  }
}

// 2. The headers.
{
  const ctx = load(makeChrome().chrome);
  const headers = vm.runInContext('handedHeaders', ctx);
  const cookie = vm.runInContext('cookieHeader', ctx);
  const got = headers({ cookie: cookie([{ name: 'a', value: '1' }, { name: 'b', value: '2' }, { name: 'bad', value: 'x\r\nY: z' }]), referrer: 'https://files.example/thread', userAgent: 'UA' });
  if (JSON.stringify(got) !== JSON.stringify({ Cookie: 'a=1; b=2', Referer: 'https://files.example/thread', 'User-Agent': 'UA' })) fail(`handedHeaders gave ${JSON.stringify(got)}`);
  const bare = headers({ cookie: '', referrer: 'chrome://newtab/', userAgent: '' });
  if (Object.keys(bare).length) fail(`empty values and a non-web referrer still went out: ${JSON.stringify(bare)}`);
}

/** Starts one download through the listener the browser would call. */
async function startDownload(chromium, arrives) {
  const { chrome, calls } = makeChrome({ chromium });
  const ctx = load(chrome);
  let sent = null;
  ctx.handLinkOver = async (link) => {
    sent = link;
    return arrives ? { instanceId: 'a'.repeat(40), name: 'NAS' } : null;
  };
  const item = { id: 7, url: 'https://files.example/get/film.mkv', referrer: 'https://files.example/thread', filename: 'film.mkv', totalBytes: 50 * 1024 * 1024 };
  let suggested = false;
  if (chromium) {
    const held = chrome.downloads.onDeterminingFilename.listeners[0]?.(item, () => { suggested = true; });
    if (held !== true) fail('the Chromium listener does not hold the download while it is handed over');
  } else {
    chrome.downloads.onCreated.listeners[0]?.(item);
  }
  await settle();
  return { calls, sent, suggested };
}

// 3. Cancelled only once the instance has it.
for (const chromium of [true, false]) {
  const where = chromium ? 'Chromium' : 'Firefox';
  {
    const { calls, sent, suggested } = await startDownload(chromium, true);
    if (!sent) { fail(`${where}: a matching download was never handed over`); continue; }
    if (sent.headers.Cookie !== 'session=abc; lang=de') fail(`${where}: the cookies for the download's host did not go along: ${JSON.stringify(sent.headers)}`);
    if (sent.headers.Referer !== 'https://files.example/thread' || sent.headers['User-Agent'] !== 'Mozilla/5.0 (Test)') fail(`${where}: Referer or User-Agent missing: ${JSON.stringify(sent.headers)}`);
    if (!calls.cancelled.includes(7) || !calls.erased.includes(7)) fail(`${where}: a download the instance took is still in the browser`);
    if (!calls.notified.some((m) => m.includes('film.mkv') && m.includes('NAS'))) fail(`${where}: the note does not name the file and the instance: ${JSON.stringify(calls.notified)}`);
    if (chromium && !suggested) fail('Chromium: the download was never released from the name step');
  }
  {
    const { calls, suggested } = await startDownload(chromium, false);
    if (calls.cancelled.length || calls.erased.length) fail(`${where}: a download the instance did not take was cancelled, so it is lost`);
    if (chromium && !suggested) fail('Chromium: after a failed hand-over the download stays held instead of carrying on');
    if (!chromium && !calls.resumed.includes(7)) fail('Firefox: after a failed hand-over the paused download was not resumed');
    if (!calls.badge.includes('!')) fail(`${where}: a failed hand-over says nothing`);
  }
}

// Switched off, nothing is touched.
{
  const { chrome, calls, store } = makeChrome();
  store.takeoverEnabled = false;
  const ctx = load(chrome);
  let sent = false;
  ctx.handLinkOver = async () => { sent = true; return null; };
  let suggested = false;
  chrome.downloads.onDeterminingFilename.listeners[0]({ id: 1, url: 'https://files.example/a.zip', totalBytes: 1 << 30 }, () => { suggested = true; });
  await settle();
  if (sent || calls.cancelled.length || !suggested) fail('with taking over switched off a download was held, sent or cancelled');
}

// 4. Only an extension page sends a stream, with the page as its Referer.
{
  const { chrome } = makeChrome();
  const ctx = load(chrome);
  let sent = null;
  ctx.handLinkOver = async (link) => { sent = link; return { instanceId: 'b'.repeat(40) }; };
  const msg = { type: 'knightloader-send-media', url: 'https://files.example/live/index.m3u8', page: 'https://site.example/watch/7', title: 'Watch', target: 'b'.repeat(40) };
  for (const f of chrome.runtime.onMessage.listeners) f(msg, { url: 'https://evil.example/', tab: { id: 1 } }, () => {});
  await settle();
  if (sent) fail('a content script on a website sent a stream');
  for (const f of chrome.runtime.onMessage.listeners) f(msg, { url: 'chrome-extension://test/popup.html' }, () => {});
  await settle();
  if (!sent) fail('the popup could not send a stream');
  else {
    if (sent.headers.Referer !== msg.page || sent.source !== msg.page) fail(`a stream went without its page: ${JSON.stringify(sent)}`);
    if (sent.headers.Cookie !== 'session=abc; lang=de') fail(`a stream went without the cookies for its own address: ${JSON.stringify(sent.headers)}`);
    if (sent.target !== msg.target) fail('a stream did not go to the instance the popup picked');
  }
  sent = null;
  for (const f of chrome.runtime.onMessage.listeners) f({ ...msg, incognito: true }, { url: 'chrome-extension://test/popup.html' }, () => {});
  await settle();
  if (sent?.headers.Cookie) fail("a private window's cookies were sent");
}

// A media response lands in its tab's list once, and a new page starts afresh.
{
  const { chrome, session } = makeChrome();
  load(chrome);
  const hear = chrome.webRequest.onResponseStarted.listeners[0];
  if (!hear) fail('nothing listens for media responses once webRequest is granted');
  else {
    const hls = { tabId: 3, type: 'xmlhttprequest', url: 'https://cdn.example/a.m3u8', responseHeaders: [] };
    hear({ tabId: 3, type: 'main_frame', url: 'https://site.example/watch' });
    hear(hls);
    hear(hls);
    hear({ tabId: 3, type: 'xmlhttprequest', url: 'https://cdn.example/seg1.ts', responseHeaders: [{ name: 'Content-Type', value: 'video/mp2t' }] });
    await settle();
    const list = session['media:3'];
    if (list?.page !== 'https://site.example/watch' || list.items.length !== 1 || list.items[0].kind !== 'hls') fail(`tab 3's list is ${JSON.stringify(list)}, want the one playlist on its page`);
    hear({ tabId: 3, type: 'main_frame', url: 'https://site.example/next' });
    await settle();
    if (session['media:3']?.items.length !== 0) fail('a new page in the tab kept the last page\'s streams');
  }
}

// Switching one feature off leaves what another still runs on.
{
  const { chrome, store, calls, access } = makeChrome();
  const ctx = load(chrome);
  store.cnlEnabled = false;
  await vm.runInContext('releaseFeature', ctx)('takeoverEnabled');
  if (calls.removed.includes('cookies') || calls.removed.includes('<all_urls>')) fail(`switching taking over off took away what finding media runs on: ${calls.removed}`);
  if (!calls.removed.includes('downloads') || !calls.removed.includes('notifications')) fail(`switching taking over off kept its own permissions: ${calls.removed}`);
  await vm.runInContext('releaseFeature', ctx)('mediaEnabled');
  if (access.origins.size || access.permissions.has('cookies') || access.permissions.has('webRequest')) fail('with everything off the extension kept access');
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: downloads go over only by the rules and leave the browser only once the instance has them, with the cookies for their own host; streams go only from the popup, with their page; switching one feature off keeps what another runs on');
