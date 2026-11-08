/**
 * The download button on a video's player (src/video.js, src/video-worker.js,
 * src/video-button.js, src/popup-video.js), run against a stubbed `chrome` and
 * a small stand-in for the page.
 *
 *   1. Every form a video's page comes in sends the one address the video
 *      goes by, the playlist only when asked, and a page that plays no one
 *      video sends nothing.
 *   2. The sites are a short list of https addresses inside the optional
 *      access, none of them a static content script, and each is a site
 *      yt-dlp on the instance knows.
 *   3. The worker registers the button only where it runs, and sends only the
 *      video of the page that asked, with a format only from the menu's list,
 *      and tells the button what became of it.
 *   4. The popup's switch and the page's button share one stored setting:
 *      switching off takes the button out of an open tab at once, without a
 *      reload, and switching on brings it back. The button follows the page
 *      to another video and away from one, sits in the element shown in
 *      fullscreen, picks the video that is on screen, and moves down past
 *      the controls a player keeps along the top of its video.
 *
 * Run by CI and by hand: `node extension/check-video.mjs`
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const manifest = JSON.parse(read('src', 'manifest.json'));
const problems = [];
const fail = (why) => problems.push(why);
const settle = () => new Promise((r) => setTimeout(r, 30));

function event() {
  const listeners = [];
  return { addListener: (f) => listeners.push(f), listeners };
}

/** One store with chrome.storage's change events, shared by every context
 *  that uses it, as the extension's pages and content scripts share theirs. */
function makeStore(initial = {}) {
  const data = { ...initial };
  const onChanged = event();
  return {
    data,
    onChanged,
    local: {
      get: async (keys) => {
        const list = keys == null ? Object.keys(data) : Array.isArray(keys) ? keys : [keys];
        return Object.fromEntries(list.filter((k) => k in data).map((k) => [k, data[k]]));
      },
      set: async (o) => {
        const changes = Object.fromEntries(Object.entries(o).map(([k, v]) => [k, { oldValue: data[k], newValue: v }]));
        Object.assign(data, o);
        for (const f of onChanged.listeners) await f(changes, 'local');
      },
      remove: async () => {},
    },
  };
}

function makeChrome(store, { granted = ['<all_urls>'] } = {}) {
  const calls = { registered: [], unregistered: [], injected: [], tabs: [] };
  const live = new Map();
  const chrome = {
    runtime: {
      onInstalled: event(), onMessage: event(), onStartup: event(),
      openOptionsPage() {}, getManifest: () => manifest,
      getURL: (p) => `chrome-extension://test/${p}`,
      sendMessage: async () => undefined,
    },
    storage: {
      onChanged: store.onChanged,
      local: store.local,
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
    },
    permissions: {
      onAdded: event(), onRemoved: event(),
      contains: async (p) => (p.origins ?? []).every((o) => granted.includes('<all_urls>') || granted.includes(o)) && (p.permissions ?? []).every((x) => granted.includes(x)),
      request: async () => false,
      remove: async () => true,
    },
    action: { getUserSettings: async () => ({ isOnToolbar: true }), setBadgeText() {}, setBadgeBackgroundColor() {}, setTitle() {}, openPopup: async () => {} },
    scripting: {
      getRegisteredContentScripts: async () => [...live.values()],
      registerContentScripts: async (s) => {
        for (const x of s) {
          if (live.has(x.id)) throw new Error(`Duplicate script ID '${x.id}'`);
          live.set(x.id, x);
          calls.registered.push(x);
        }
      },
      unregisterContentScripts: async ({ ids }) => { for (const id of ids) live.delete(id); calls.unregistered.push(...ids); },
      executeScript: async (o) => {
        if (o.func) return [{ frameId: 0, result: false }];
        calls.injected.push(o);
        return [];
      },
    },
    declarativeNetRequest: { updateEnabledRulesets: async () => {} },
    tabs: {
      query: async ({ url }) => [{ id: 7, url: url[0] }],
      create: async (o) => { calls.tabs.push(o.url); },
      onRemoved: event(),
    },
  };
  return { chrome, calls, live };
}

// 1. The address each form of a page sends.
{
  const ctx = vm.createContext({ URL, chrome: {} });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  const page = vm.runInContext('videoPage', ctx);
  const ID = 'aqz-KE-bpKQ';
  const cases = [
    [`https://www.youtube.com/watch?v=${ID}&t=42s`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://m.youtube.com/watch?feature=share&v=${ID}`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube.com/shorts/${ID}`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube.com/live/${ID}?si=x`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube-nocookie.com/embed/${ID}?autoplay=1`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube.com/watch?v=${ID}&list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB&index=3`, `https://www.youtube.com/watch?v=${ID}`, 'https://www.youtube.com/playlist?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB'],
    [`https://www.youtube.com/watch?v=${ID}&list=WL`, `https://www.youtube.com/watch?v=${ID}`, ''],
    ['https://vimeo.com/1084537', 'https://vimeo.com/1084537', ''],
    ['https://vimeo.com/1084537/8a2b3c4d5e', 'https://vimeo.com/1084537/8a2b3c4d5e', ''],
    ['https://vimeo.com/channels/staffpicks/1084537', 'https://vimeo.com/1084537', ''],
    ['https://www.dailymotion.com/video/x8abcd1?playlist=x6hynp', 'https://www.dailymotion.com/video/x8abcd1', ''],
    ['https://www.twitch.tv/videos/2012345678?t=1h', 'https://www.twitch.tv/videos/2012345678', ''],
    ['https://www.twitch.tv/somechannel/clip/FunnyClipSlug-AbC_123', 'https://clips.twitch.tv/FunnyClipSlug-AbC_123', ''],
    ['https://clips.twitch.tv/FunnyClipSlug-AbC_123', 'https://clips.twitch.tv/FunnyClipSlug-AbC_123', ''],
    ['https://www.bilibili.com/video/BV1GJ411x7h7/?spm_id_from=333&p=1', 'https://www.bilibili.com/video/BV1GJ411x7h7', ''],
    ['https://www.bilibili.com/video/BV1GJ411x7h7?p=3', 'https://www.bilibili.com/video/BV1GJ411x7h7?p=3', ''],
  ];
  for (const [href, url, playlist] of cases) {
    const got = page(href);
    if (got?.url !== url) fail(`${href} sends ${got?.url}, want ${url}`);
    else if (got.playlist !== playlist) fail(`${href} offers the playlist ${got.playlist || 'none'}, want ${playlist || 'none'}`);
  }
  for (const href of [
    'https://www.youtube.com/',
    'https://www.youtube.com/results?search_query=blender',
    'https://www.youtube.com/watch?v=short',
    'https://www.youtube.com/@BlenderOfficial',
    'http://www.youtube.com/watch?v=aqz-KE-bpKQ',
    'https://www.youtube.com.example.org/watch?v=aqz-KE-bpKQ',
    'https://music.youtube.com/watch?v=aqz-KE-bpKQ',
    'https://vimeo.com/channels/staffpicks',
    'https://www.twitch.tv/somechannel',
    'https://clips.twitch.tv/embed',
    'https://www.bilibili.com/',
    'not a url',
  ]) {
    if (page(href)) fail(`${href} sends ${page(href).url}, want nothing`);
  }
}

// 2. The sites.
{
  const ctx = vm.createContext({ URL, chrome: {} });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  const sites = vm.runInContext('VIDEO_SITES', ctx);
  const hostsGo = read('..', 'internal', 'resolver', 'ytdlp', 'hosts.go');
  const optional = manifest.optional_host_permissions ?? [];
  if (sites.length > 8) fail(`the button runs on ${sites.length} sites, want a short list`);
  for (const s of sites) {
    for (const o of s.origins) {
      if (!/^https:\/\/[a-z0-9.-]+\/\*$/.test(o)) fail(`${s.id} asks for ${o}, want one https host`);
      if (!optional.includes('<all_urls>') && !optional.includes(o)) fail(`${o} is outside the manifest's optional access`);
      const domain = new URL(o.slice(0, -1)).hostname.split('.').slice(-2).join('.');
      if (!hostsGo.includes(`"${domain}"`)) fail(`${s.id} (${domain}) is not a site internal/resolver/ytdlp/hosts.go knows`);
    }
  }
  const staticScripts = (manifest.content_scripts ?? []).flatMap((c) => c.js ?? []);
  if (staticScripts.some((f) => f.startsWith('video'))) fail('the button is a static content script, which would need its access at install');
  if ((manifest.host_permissions ?? []).length) fail(`the manifest asks for host access at install: ${manifest.host_permissions.join(', ')}`);
}

/** Loads the background scripts as Firefox does (manifest background.scripts). */
function loadBackground(chrome) {
  const ctx = vm.createContext({
    chrome, console: { log() {}, warn() {}, error() {} }, setTimeout, clearTimeout,
    crypto: globalThis.crypto, TextEncoder, TextDecoder, URL, navigator: { language: 'en-US', languages: ['en-US'], userAgent: 'test' },
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'), atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    WebSocket: class {}, indexedDB: { open: () => ({}) },
  });
  for (const f of manifest.background.scripts) vm.runInContext(read('src', f), ctx, { filename: f });
  return ctx;
}

// 3. The worker.
{
  const store = makeStore();
  const { chrome, calls, live } = makeChrome(store);
  const ctx = loadBackground(chrome);
  await vm.runInContext('applyVideoButton', ctx)();
  const want = vm.runInContext('VIDEO_SITES', ctx).flatMap((s) => s.origins);
  const reg = live.get('video-button');
  if (!reg) fail('with the access granted and nothing stored, the button is not registered');
  else {
    if (reg.matches.join('|') !== want.join('|')) fail(`the button is registered on ${reg.matches.join(', ')}, want the sites' own addresses`);
    if (!reg.allFrames) fail('the button is not registered in frames, so an embedded player never gets it');
  }
  if (!calls.injected.some((o) => o.files?.includes('video-button.js'))) fail('a tab that was open before the button went on does not get it');

  await store.local.set({ videoButtonHidden: ['vimeo'] });
  await settle();
  if (live.get('video-button')?.matches.some((m) => m.includes('vimeo'))) fail('the button stays registered on a site it was hidden on');
  await store.local.set({ videoButton: false });
  await settle();
  if (live.has('video-button')) fail('switched off, the button stays registered');
  await store.local.set({ videoButton: true, videoButtonHidden: [] });
  await settle();

  const sent = [];
  let answer = { status: 200, body: '[{"id":"a"}]' };
  ctx.withGroup = async (work) => work({
    siblings: [{ instanceId: 'inst-1' }],
    call: async (target, method, path, body) => { sent.push({ target, method, path, body: JSON.parse(body) }); return answer; },
  });
  const sendVideo = vm.runInContext('sendVideo', ctx);
  const embed = 'https://www.youtube-nocookie.com/embed/aqz-KE-bpKQ?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB';

  let r = await sendVideo(embed, { href: embed, format: 'audio' });
  const last = () => sent.at(-1)?.body;
  if (r.result !== 'sent') fail(`a send the instance took answers ${r.result}, want sent`);
  if (last()?.links !== 'https://www.youtube.com/watch?v=aqz-KE-bpKQ') fail(`an embedded player sends ${last()?.links}`);
  if (last()?.format !== 'audio') fail(`a format from the menu arrives as ${last()?.format}`);
  if (sent.at(-1)?.path !== '/api/links' || sent.at(-1)?.method !== 'POST') fail('the video is not sent as POST /api/links');

  await sendVideo(embed, { href: embed, playlist: true, format: 'everything' });
  if (last()?.links !== 'https://www.youtube.com/playlist?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB') fail(`asked for the playlist, the button sends ${last()?.links}`);
  if ('format' in last()) fail(`a format outside the menu's list was sent: ${last().format}`);

  const before = sent.length;
  r = await sendVideo('https://www.youtube.com/watch?v=aqz-KE-bpKQ', { href: 'https://vimeo.com/1084537' });
  if (r.result !== 'refused' || sent.length !== before) fail('a frame on one site could send a video of another');
  await store.local.set({ videoButtonHidden: ['youtube'] });
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'refused' || sent.length !== before) fail('a site the button is hidden on can still send');
  await store.local.set({ videoButtonHidden: [] });

  for (const [res, want] of [
    [{ status: 200, body: '[]' }, 'already'],
    [{ status: 200, body: '[{"id":"a","skipped":true}]' }, 'held'],
    [{ status: 403, body: '{"code":"memberPending"}' }, 'refused'],
  ]) {
    answer = res;
    r = await sendVideo(embed, { href: embed });
    if (r.result !== want) fail(`the instance answering ${res.status} ${res.body} shows ${r.result}, want ${want}`);
  }
  ctx.withGroup = async () => { const e = new Error('no-phrase'); e.code = 'no-phrase'; throw e; };
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'unpaired') fail(`without a group the button shows ${r.result}, want unpaired`);

  const listener = chrome.runtime.onMessage.listeners.at(-1);
  let answered = false;
  listener({ type: 'knightloader-video-send', href: embed }, { tab: { id: 1 }, url: 'https://example.org/' }, () => { answered = true; });
  await settle();
  if (answered) fail('a content script on a site without the button got an answer to a send');
}

/** A small stand-in for the DOM, enough for the button and the popup. */
class Node {
  constructor(tag, doc) {
    this.tagName = tag;
    this.doc = doc;
    this.children = [];
    this.parentNode = null;
    this.listeners = {};
    this.attrs = {};
    this.dataset = {};
    this.style = { setProperty: (k, v) => { this.style[k] = v; } };
    const classes = new Set();
    this.classList = {
      add: (...c) => c.forEach((x) => classes.add(x)),
      remove: (...c) => c.forEach((x) => classes.delete(x)),
      toggle: (c, on) => (on ?? !classes.has(c) ? classes.add(c) : classes.delete(c)),
      contains: (c) => classes.has(c),
    };
    this.rect = { left: 0, top: 0, width: 0, height: 0, right: 0, bottom: 0 };
    this.offsetWidth = 0;
  }
  get parentElement() { return this.parentNode; }
  get isConnected() {
    let n = this;
    while (n.parentNode) n = n.parentNode;
    return n === this.doc.documentElement;
  }
  append(...nodes) {
    for (const n of nodes) {
      n.parentNode?.children.splice(n.parentNode.children.indexOf(n), 1);
      n.parentNode = this;
      this.children.push(n);
    }
  }
  replaceChildren(...nodes) {
    for (const n of this.children) n.parentNode = null;
    this.children = [];
    this.append(...nodes);
  }
  remove() {
    if (!this.parentNode) return;
    this.parentNode.children.splice(this.parentNode.children.indexOf(this), 1);
    this.parentNode = null;
  }
  matches(selector) {
    return selector.split(',').some((part) => part.trim() === this.tagName);
  }
  closest(selector) {
    for (let n = this; n; n = n.parentNode) if (n.matches?.(selector)) return n;
    return null;
  }
  contains(n) {
    for (; n; n = n.parentNode) if (n === this) return true;
    return false;
  }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k] ?? null; }
  removeAttribute(k) { delete this.attrs[k]; }
  addEventListener(type, f) { (this.listeners[type] ??= []).push(f); }
  dispatch(type, e = {}) { for (const f of this.listeners[type] ?? []) f({ isTrusted: true, stopPropagation() {}, preventDefault() {}, composedPath: () => [], ...e }); }
  attachShadow() { return new Node('#shadow', this.doc); }
  getBoundingClientRect() { return this.rect; }
  querySelectorAll(tag) {
    const out = [];
    const walk = (n) => n.children.forEach((c) => { if (c.tagName === tag) out.push(c); walk(c); });
    walk(this);
    return out;
  }
  focus() {}
}

function makeDocument() {
  const doc = { listeners: {}, fullscreenElement: null };
  doc.documentElement = new Node('html', doc);
  doc.createElement = (tag) => new Node(tag, doc);
  doc.createElementNS = (_, tag) => new Node(tag, doc);
  doc.querySelectorAll = (tag) => doc.documentElement.querySelectorAll(tag);
  doc.getElementById = (id) => doc.documentElement.querySelectorAll('*').find((n) => n.attrs.id === id) ?? doc.byId?.[id];
  doc.elementsFromPoint = () => doc.documentElement.querySelectorAll('video');
  doc.addEventListener = (type, f) => (doc.listeners[type] ??= []).push(f);
  doc.dispatch = (type) => (doc.listeners[type] ?? []).forEach((f) => f({}));
  return doc;
}

function video(doc, parent, rect) {
  const v = doc.createElement('video');
  v.rect = { ...rect, right: rect.left + rect.width, bottom: rect.top + rect.height };
  parent.append(v);
  return v;
}

/** The page's context: a site with the button's content scripts in it. */
function loadPage(chrome, href) {
  const doc = makeDocument();
  const location = { href };
  const ctx = vm.createContext({
    chrome, document: doc, location, URL, console, setTimeout, clearTimeout,
    setInterval: () => 0, requestAnimationFrame: () => 0, performance,
    matchMedia: () => ({ matches: false, addEventListener() {} }),
    window: { addEventListener() {} }, innerWidth: 1280, innerHeight: 800,
    navigator: { userAgent: 'test' }, CSS: { supports: () => false },
  });
  ctx.globalThis = ctx;
  return { ctx, doc, location, run: () => {
    for (const f of ['appearance.js', 'video.js', 'video-button.js']) vm.runInContext(read('src', f), ctx, { filename: f });
  } };
}

// 4. The switch, the page and the button.
{
  const store = makeStore();
  const { chrome } = makeChrome(store);
  chrome.runtime.sendMessage = async (msg) => (msg.type === 'knightloader-video-labels' ? { send: 'Download', sendTip: 'Send', more: 'More', dir: 'ltr', lang: 'en' } : undefined);

  const { doc, location, run } = loadPage(chrome, 'https://www.youtube.com/watch?v=aqz-KE-bpKQ');
  const player = doc.createElement('div');
  player.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };
  doc.documentElement.append(player);
  const main = video(doc, player, { left: 0, top: 56, width: 1280, height: 720 });
  run();
  await settle();
  const button = () => doc.documentElement.querySelectorAll('knightloader-video-button')[0];
  const where = () => button()?.parentNode;
  if (!button()) fail('on a watch page the button does not appear');
  else if (button().style.left !== '640px') fail(`the button sits at ${button().style.left}, want the middle of the video's top edge`);

  // The popup in its own context, on the same storage.
  const popupDoc = makeDocument();
  const row = popupDoc.createElement('div');
  const label = popupDoc.createElement('span');
  const toggle = popupDoc.createElement('button');
  popupDoc.byId = { videoRow: row, videoLabel: label, videoEnabled: toggle };
  const popup = vm.createContext({ chrome, document: popupDoc, URL, console, window: { close() {} }, t: (k) => k, loadLanguage: async () => 'en' });
  for (const f of ['video.js', 'popup-video.js']) vm.runInContext(read('src', f), popup, { filename: f });
  await settle();
  if (toggle.getAttribute('aria-checked') !== 'true') fail('the popup does not show the button as on');

  toggle.dispatch('click');
  await settle();
  if (store.data.videoButton !== false) fail('the popup switch does not store the button as off');
  if (button()) fail('switched off in the popup, the button stays on the open page until a reload');
  if (toggle.getAttribute('aria-checked') !== 'false') fail('switched off, the popup still shows it on');
  toggle.dispatch('click');
  await settle();
  if (!button()) fail('switched on again in the popup, the button does not come back to the open page');

  await store.local.set({ videoButtonHidden: ['youtube'] });
  if (button()) fail('hidden on this site, the button stays');
  await store.local.set({ videoButtonHidden: [] });
  if (!button()) fail('shown on this site again, the button does not come back');

  location.href = 'https://www.youtube.com/feed/subscriptions';
  doc.dispatch('yt-navigate-finish');
  if (button()) fail('on a page that plays no one video the button stays');
  location.href = 'https://www.youtube.com/watch?v=dQw4w9WgXcQ';
  doc.dispatch('yt-navigate-finish');
  if (!button()) fail('on the next video the button does not come back');

  doc.fullscreenElement = player;
  doc.dispatch('fullscreenchange');
  if (where() !== player) fail('in fullscreen the button is not inside the element shown, so it is not drawn');
  doc.fullscreenElement = main;
  doc.dispatch('fullscreenchange');
  if (button()) fail('with the video element itself in fullscreen the button stays where nothing draws it');
  doc.fullscreenElement = null;
  doc.dispatch('fullscreenchange');
  if (where() !== doc.documentElement) fail('out of fullscreen the button does not return to the page');

  // Shorts keep the next video loaded below the screen.
  location.href = 'https://www.youtube.com/shorts/aqz-KE-bpKQ';
  main.remove();
  video(doc, player, { left: 400, top: 1000, width: 480, height: 850 });
  video(doc, player, { left: 500, top: 0, width: 400, height: 710 });
  doc.dispatch('yt-navigate-finish');
  if (button()?.style.left !== '700px') fail(`on Shorts the button sits at ${button()?.style.left}, want over the video on screen at 700px`);
}

// An embedded player keeps its title, a link, along the top of the video.
{
  const store = makeStore();
  const { chrome } = makeChrome(store);
  chrome.runtime.sendMessage = async (msg) => (msg.type === 'knightloader-video-labels' ? { send: 'Download', sendTip: 'Send', more: 'More', dir: 'ltr', lang: 'en' } : undefined);
  const { doc, run } = loadPage(chrome, 'https://www.youtube-nocookie.com/embed/aqz-KE-bpKQ');
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  const v = video(doc, player, { left: 0, top: 0, width: 800, height: 450 });
  const title = doc.createElement('a');
  player.append(title);
  doc.elementsFromPoint = (x, y) => (y < 60 ? [title, v] : [v]);
  run();
  await settle();
  const top = parseInt(doc.documentElement.querySelectorAll('knightloader-video-button')[0]?.style.top ?? '0', 10);
  if (top < 60) fail(`in an embedded player the button sits at ${top}px, over the title link along the top`);
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: every form of a video page sends its one address, the sites are a short list yt-dlp knows inside the optional access, the worker sends only the asking page\'s video and says what became of it, and the popup switch takes the button off an open page at once and back, while it follows navigation and fullscreen');
