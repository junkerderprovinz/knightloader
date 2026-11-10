/**
 * The download button on a video's player (src/video.js, src/video-worker.js,
 * src/video-button.js), run against a stubbed `chrome` and a small stand-in
 * for the page.
 *
 *   1. Every form a video's page comes in sends the one address the video
 *      goes by, the playlist only when asked, and a page that plays no one
 *      video sends nothing.
 *   2. The sites are a short list of https addresses inside the optional
 *      access, none of them a static content script, and each is a site
 *      yt-dlp on the instance knows.
 *   3. A site's stored defaults become the choice a click sends, cleaned of
 *      anything the instance would refuse. A dragged button is stored as
 *      shares of its player, kept inside it, so it keeps its place when the
 *      player changes size. Auto-hide shows the button only with the pointer
 *      over the video or while it is in use.
 *   4. The worker registers the button only where it runs, sends only the
 *      video of the page that asked, to start at once with its choice, and
 *      tells the button whether it started, was held back or was there
 *      already. The menu lists what the instance says the video has, and the
 *      resolutions found are kept for the site.
 *   5. On the page, a click sends the site's defaults and the menu a pick of
 *      its own, a drag places the button and sends nothing, auto-hide and the
 *      keyboard decide when it shows, and the switches take it off an open
 *      tab at once and back. The button follows the page to another video
 *      and away from one, asks the instance once per video, sits in the
 *      element shown in fullscreen, picks the video that is on screen, and
 *      moves down past the controls a player keeps along the top of its
 *      video.
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
    fetch: async (url) => ({ text: async () => (String(url).endsWith('logo.svg') ? '<svg viewBox="0 0 587 901.8"></svg>' : '') }),
  });
  for (const f of manifest.background.scripts) vm.runInContext(read('src', f), ctx, { filename: f });
  return ctx;
}

// 3. What a click downloads, where the button sits and when it shows.
{
  const ctx = vm.createContext({ URL, chrome: {} });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  const run = (src) => JSON.parse(JSON.stringify(vm.runInContext(src, ctx)));
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

  ctx.stored = {
    videoDefaults: {
      youtube: { parts: ['subtitle', 'video', 'chapters'], quality: '720p', audio: 'opus' },
      vimeo: { parts: [], quality: '1080p' },
      twitch: { parts: ['audio'], quality: '8k', audio: 'm4b' },
    },
  };
  const choice = (site) => run(`videoChoice(videoDefaultsFor(stored, '${site}'))`);
  const best = { variants: ['video'], quality: 'best', audioFormat: 'best' };
  if (!same(choice('youtube'), { variants: ['video', 'subtitle'], quality: '720p', audioFormat: 'opus' })) fail(`YouTube's defaults send ${JSON.stringify(choice('youtube'))}, want the video at 720p with its subtitles, and opus kept for the audio`);
  if (!same(choice('vimeo'), best)) fail(`a site set to download no part sends ${JSON.stringify(choice('vimeo'))}, want the best video`);
  if (!same(choice('dailymotion'), best)) fail(`a site with nothing set sends ${JSON.stringify(choice('dailymotion'))}, want the best video`);
  if (!same(choice('twitch'), { variants: ['audio'], quality: 'best', audioFormat: 'best' })) fail(`a quality and an audio format the instance has not got are sent as ${JSON.stringify(choice('twitch'))}, want the best`);

  ctx.stored = { videoQualities: { youtube: ['480p', '2160p', '9999p'] } };
  if (run(`videoSiteQualities(stored, 'youtube')`).join() !== '2160p,480p') fail('the options offer a site other resolutions than those the instance found there');
  if (run(`videoSiteQualities(stored, 'vimeo')`).join() !== run('VIDEO_CAPS').join()) fail('for a site the instance has found nothing on, the options do not offer every resolution it knows');

  const rect = { left: 100, top: 50, width: 800, height: 450 };
  const size = { width: 160, height: 32 };
  ctx.rect = rect;
  ctx.size = size;
  if (!same(run('videoSpotOf({ x: 500, y: 275 }, rect, size)'), { x: 0.5, y: 0.5 })) fail('a button dropped in the middle of the player is not stored as half its width and height');
  const corner = run('videoSpotOf({ x: 0, y: 1000 }, rect, size)');
  if (!same(corner, { x: 0.1, y: 0.9644 })) fail(`a button dropped past the player's corner is stored at ${JSON.stringify(corner)}, want it kept inside at 0.1, 0.9644`);
  ctx.spot = corner;
  ctx.full = { left: 0, top: 0, width: 1600, height: 900 };
  const scaled = run('videoSpotPoint(spot, full, size)');
  if (Math.round(scaled.x) !== 160 || Math.round(scaled.y) !== 868) fail(`in a player twice the size the button sits at ${scaled.x}, ${scaled.y}, want the same share of it at 160, 868`);
  if (!same(run('videoSpotPoint({ x: 0, y: 0 }, rect, size)'), { x: 180, y: 66 })) fail('a spot at the edge puts part of the button outside the player');
  if (!same(run(`videoSpotPoint({ x: 0.9, y: 0.1 }, { left: 0, top: 0, width: 100, height: 20 }, size)`), { x: 50, y: 10 })) fail('a player smaller than the button does not get it in its middle');
  for (const bad of [{ x: 2, y: 0.5 }, { x: '0.5', y: 0.5 }, { y: 0.5 }, null]) {
    ctx.stored = { videoButtonSpot: { youtube: bad } };
    if (run(`videoSpotFor(stored, 'youtube')`) !== null) fail(`a stored spot of ${JSON.stringify(bad)} is used`);
  }

  const shows = (s) => vm.runInContext('videoButtonShows', ctx)(s);
  if (!shows({ autoHide: false })) fail('without auto-hide the button does not show on its own');
  if (shows({ autoHide: true })) fail('with auto-hide the button shows with the pointer away from the video');
  if (!shows({ autoHide: true, pointerOver: true })) fail('with auto-hide the button does not show with the pointer over the video');
  for (const k of ['hovering', 'focused', 'menuOpen', 'dragging', 'busy']) {
    if (!shows({ autoHide: true, [k]: true })) fail(`with auto-hide the button hides while ${k}`);
  }
}

// 4. The worker.
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

  const labels = await vm.runInContext('videoLabels', ctx)();
  if (!labels.mark?.startsWith('<svg')) fail('the button gets no mark to show');
  if (Object.keys(labels.parts ?? {}).join() !== 'video,audio,subtitle,thumbnail,description') fail('the button gets no name for every part');

  const sent = [];
  let answer = { status: 200, body: '{"tasks":[{"id":"a"}],"listed":[]}' };
  const withGroup = ctx.withGroup;
  ctx.withGroup = async (work) => work({
    siblings: [{ instanceId: 'inst-1' }],
    call: async (target, method, path, body) => { sent.push({ target, method, path, body: body ? JSON.parse(body) : undefined }); return answer; },
  });
  const sendVideo = vm.runInContext('sendVideo', ctx);
  const embed = 'https://www.youtube-nocookie.com/embed/aqz-KE-bpKQ?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB';

  let r = await sendVideo(embed, { href: embed, pick: { parts: ['audio', 'video', 'chapters'], quality: '1080p', audio: 'mp3' } });
  const last = () => sent.at(-1)?.body;
  if (r.result !== 'started') fail(`a send the instance started answers ${r.result}, want started`);
  if (last()?.links !== 'https://www.youtube.com/watch?v=aqz-KE-bpKQ') fail(`an embedded player sends ${last()?.links}`);
  if (JSON.stringify(last()?.choice) !== '{"variants":["video","audio"],"quality":"1080p","audioFormat":"mp3"}') fail(`a pick arrives as ${JSON.stringify(last()?.choice)}, want the video at 1080p and the audio as mp3`);
  if (last()?.start !== true || last()?.report !== true) fail('the video is not sent to start at once and to report a link already there');
  if ('format' in last()) fail('the send still names a format beside its choice');
  if (sent.at(-1)?.path !== '/api/links' || sent.at(-1)?.method !== 'POST') fail('the video is not sent as POST /api/links');

  await sendVideo(embed, { href: embed, playlist: true, pick: { parts: ['everything'], quality: '8k' } });
  if (last()?.links !== 'https://www.youtube.com/playlist?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB') fail(`asked for the playlist, the button sends ${last()?.links}`);
  if (JSON.stringify(last()?.choice) !== '{"variants":["video"],"quality":"best","audioFormat":"best"}') fail(`a pick the instance would refuse arrives as ${JSON.stringify(last()?.choice)}, want the best video`);

  // The menu offers what the instance says the video has, and the
  // resolutions found are kept for the site.
  const videoFormats = vm.runInContext('videoFormats', ctx);
  answer = { status: 200, body: '{"qualities":["720p","480p","9999p"],"audio":true}' };
  let menu = await videoFormats(embed, { href: embed });
  const asked = sent.at(-1);
  if (asked?.method !== 'GET' || asked.path !== `/api/ytdlp/link?url=${encodeURIComponent('https://www.youtube.com/watch?v=aqz-KE-bpKQ')}`) {
    fail(`the menu asks ${asked?.method} ${asked?.path}, want GET /api/ytdlp/link for the video's address`);
  }
  if (menu.qualities?.join() !== '720p,480p' || menu.audio !== true || menu.unknown) fail(`the menu offers ${JSON.stringify(menu)}, want the two resolutions the video has and its sound`);
  answer = { status: 200, body: '{"qualities":["1080p","720p"],"audio":false}' };
  await videoFormats(embed, { href: embed });
  if (store.data.videoQualities?.youtube?.join() !== '1080p,720p,480p') fail(`the resolutions kept for YouTube are ${store.data.videoQualities?.youtube}, want every one found there`);
  answer = { status: 502, body: 'ERROR: [youtube] aqz-KE-bpKQ: Private video' };
  menu = await videoFormats(embed, { href: embed });
  if (menu.qualities?.length || !menu.unknown) fail(`a probe that failed offers ${JSON.stringify(menu)}, want nothing known`);
  const asking = sent.length;
  menu = await videoFormats('https://www.youtube.com/watch?v=aqz-KE-bpKQ', { href: 'https://vimeo.com/1084537' });
  if (sent.length !== asking || !menu.unknown) fail('a frame on one site could ask about a video of another');
  answer = { status: 200, body: '{"tasks":[{"id":"a"}],"listed":[]}' };

  const before = sent.length;
  r = await sendVideo('https://www.youtube.com/watch?v=aqz-KE-bpKQ', { href: 'https://vimeo.com/1084537' });
  if (r.result !== 'refused' || sent.length !== before) fail('a frame on one site could send a video of another');
  await store.local.set({ videoButtonHidden: ['youtube'] });
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'refused' || sent.length !== before) fail('a site the button is hidden on can still send');
  await store.local.set({ videoButtonHidden: [] });

  for (const [res, want] of [
    [{ status: 200, body: '{"tasks":[],"listed":[{"id":"a"}]}' }, 'already'],
    [{ status: 200, body: '{"tasks":[{"id":"a","skipped":true}],"listed":[]}' }, 'held'],
    [{ status: 200, body: '{"tasks":[],"listed":[]}' }, 'refused'],
    // An instance from before report answers with the bare list.
    [{ status: 200, body: '[]' }, 'already'],
    [{ status: 200, body: '[{"id":"a"}]' }, 'started'],
    [{ status: 403, body: '{"code":"memberPending"}' }, 'refused'],
  ]) {
    answer = res;
    r = await sendVideo(embed, { href: embed });
    if (r.result !== want) fail(`the instance answering ${res.status} ${res.body} shows ${r.result}, want ${want}`);
  }
  ctx.withGroup = async () => { const e = new Error('no-phrase'); e.code = 'no-phrase'; throw e; };
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'unpaired') fail(`without a group the button shows ${r.result}, want unpaired`);

  // A relay that cannot be reached, through the real session.
  const t = vm.runInContext('t', ctx);
  ctx.withGroup = withGroup;
  ctx.readPhrase = async () => 'words';
  ctx.keysFromPhrase = async () => ({ key: 'k', frameKey: new Uint8Array(32) });
  ctx.readMember = async () => null;
  ctx.WebSocket = class {
    constructor() { setTimeout(() => this.onerror?.(), 0); }
    close() {}
  };
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'failed' || r.reason !== t('send.relayFailed')) fail(`with the relay out of reach the button shows ${r.result} "${r.reason ?? ''}", want failed "${t('send.relayFailed')}"`);
  ctx.withGroup = async () => { throw vm.runInContext("Object.assign(new Error('relay: the instance did not answer'), { code: 'no-answer' })", ctx); };
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'failed' || r.reason !== t('options.followFailed')) fail(`with the instance not answering the button shows ${r.result} "${r.reason ?? ''}", want failed "${t('options.followFailed')}"`);

  const listener = chrome.runtime.onMessage.listeners.at(-1);
  let answered = false;
  listener({ type: 'knightloader-video-send', href: embed }, { tab: { id: 1 }, url: 'https://example.org/' }, () => { answered = true; });
  await settle();
  if (answered) fail('a content script on a site without the button got an answer to a send');
}

/** A small stand-in for the DOM, enough for the button. */
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
    this.offsetHeight = 0;
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
  prepend(n) {
    this.append(n);
    this.children.unshift(this.children.pop());
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
  addEventListener(type, f, opts) { (this.listeners[type] ??= []).push({ f, capture: opts === true || opts?.capture === true }); }
  dispatch(type, e = {}) { for (const l of this.listeners[type] ?? []) l.f({ isTrusted: true, stopPropagation() {}, preventDefault() {}, composedPath: () => [], ...e }); }
  /** Dispatches as the browser does: capturing listeners from the top down,
   *  then the bubbling ones back up, until one stops it. */
  fire(type, e = {}) {
    const path = [];
    for (let n = this; n; n = n.parentNode) path.unshift(n);
    let stopped = false;
    const ev = { isTrusted: true, preventDefault() {}, composedPath: () => [], ...e, stopPropagation() { stopped = true; } };
    for (const n of path) {
      for (const l of n.listeners[type] ?? []) if (l.capture || n === this) l.f(ev);
      if (stopped) return;
    }
    for (const n of path.slice(0, -1).reverse()) {
      for (const l of n.listeners[type] ?? []) if (!l.capture) l.f(ev);
      if (stopped) return;
    }
  }
  setPointerCapture() {}
  attachShadow() {
    this.shadow = new Node('#shadow', this.doc);
    return this.shadow;
  }
  get textContent() { return this.text ?? ''; }
  set textContent(s) { this.text = s; }
  getBoundingClientRect() { return this.rect; }
  querySelectorAll(tag) {
    const out = [];
    const walk = (n) => n.children.forEach((c) => { if (c.tagName === tag || tag === '*') out.push(c); walk(c); });
    walk(this);
    return out;
  }
  querySelector(selector) {
    const cls = selector.startsWith('.') ? selector.slice(1) : null;
    return this.querySelectorAll('*').find((n) => (cls ? n.attrs.class === cls || n.className === cls : n.tagName === selector)) ?? null;
  }
  focus() {}
}

function makeDocument() {
  const doc = { listeners: {}, fullscreenElement: null };
  doc.documentElement = new Node('html', doc);
  doc.createElement = (tag) => new Node(tag, doc);
  doc.createElementNS = (_, tag) => new Node(tag, doc);
  doc.importNode = (n) => n;
  doc.querySelectorAll = (tag) => doc.documentElement.querySelectorAll(tag);
  doc.getElementById = (id) => doc.documentElement.querySelectorAll('*').find((n) => n.attrs.id === id) ?? doc.byId?.[id];
  doc.elementsFromPoint = () => doc.documentElement.querySelectorAll('video');
  doc.addEventListener = (type, f) => (doc.listeners[type] ??= []).push(f);
  doc.dispatch = (type, e = {}) => (doc.listeners[type] ?? []).forEach((f) => f({ composedPath: () => [], ...e }));
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
    DOMParser: class { parseFromString() { return { documentElement: doc.createElementNS('', 'svg') }; } },
  });
  ctx.globalThis = ctx;
  return { ctx, doc, location, run: () => {
    for (const f of ['appearance.js', 'video.js', 'video-button.js']) vm.runInContext(read('src', f), ctx, { filename: f });
  } };
}

const PAGE_LABELS = {
  send: 'Download', sendTip: 'Send', more: 'More', best: 'Best', quality: 'Video quality', audioFormat: 'Audio format',
  parts: { video: 'Video', audio: 'Audio', subtitle: 'Subtitles', thumbnail: 'Thumbnail', description: 'Text' },
  hide: 'Hide on {site}', checking: 'Checking', unknown: 'Unknown', started: 'Started', already: 'Already there',
  mark: '<svg viewBox="0 0 587 901.8"></svg>', dir: 'ltr', lang: 'en',
};

// 5. The switch, the page and the button.
{
  const store = makeStore({ videoDefaults: { youtube: { parts: ['video', 'subtitle'], quality: '1080p', audio: 'best' } } });
  const { chrome } = makeChrome(store);
  let formatsAnswer;
  let formatsAsked = 0;
  const sends = [];
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-formats') {
      formatsAsked++;
      return new Promise((resolve) => { formatsAnswer = resolve; });
    }
    if (msg.type === 'knightloader-video-send') {
      sends.push(msg);
      return { result: 'already' };
    }
    return undefined;
  };

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

  const shadow = button()?.shadow;
  const buttons = () => shadow.querySelectorAll('button');
  const send = () => buttons().find((b) => b.className === 'send');
  const more = () => buttons().find((b) => b.attrs['aria-haspopup'] === 'menu');
  const frame = () => shadow.querySelectorAll('div').find((d) => d.className === 'kl');
  const bar = () => shadow.querySelectorAll('div').find((d) => d.className === 'bar');
  if (!send()?.children.some((c) => c.attrs.class === 'mark')) fail('the button shows no mark in front of its label');
  if (!frame()?.classList.contains('shown')) fail('without auto-hide the button does not show over the video');

  // A click sends the site's defaults; the instance having the video already
  // shows on the button.
  send().fire('click');
  await settle();
  if (JSON.stringify(sends.at(-1)?.pick) !== '{"parts":["video","subtitle"],"quality":"1080p","audio":"best"}') fail(`a click sends ${JSON.stringify(sends.at(-1)?.pick)}, want YouTube's defaults`);
  if (frame()?.dataset.state !== 'already' || send().children.at(-1)?.textContent !== 'Already there') fail(`a video already there shows ${frame()?.dataset.state} "${send().children.at(-1)?.textContent}", want already`);

  // The menu lists the parts with the site's defaults ticked, says it is
  // checking until the instance answers, then offers what the video has,
  // and opening it again asks nothing more.
  const menuEl = () => shadow.querySelectorAll('div').find((d) => d.attrs.role === 'menu');
  const entries = () => menuEl().querySelectorAll('*').filter((n) => n.text).map((n) => (n.attrs['aria-checked'] === 'true' ? `[${n.text}]` : n.text));
  more().fire('click');
  await settle();
  const checking = 'Video|Audio|Subtitles|Thumbnail|Text|Video quality|Checking|Download|Hide on YouTube';
  if (entries().join('|').replace(/[[\]]/g, '') !== checking) fail(`while the instance is asked the menu shows ${entries().join(', ')}, want ${checking}`);
  formatsAnswer({ qualities: ['720p'], audio: true });
  await settle();
  const listed = '[Video]|Audio|[Subtitles]|Thumbnail|Text|Video quality|Best|[1080p]|720p|Download|Hide on YouTube';
  if (entries().join('|') !== listed) fail(`the menu shows ${entries().join(', ')}, want ${listed}, with the default 1080p beside what the video has`);
  const item = (text) => menuEl().querySelectorAll('button').find((b) => b.text === text);
  item('Audio').fire('click');
  item('720p').fire('click');
  item('MP3').fire('click');
  item('Subtitles').fire('click');
  item('Download').fire('click');
  await settle();
  if (JSON.stringify(sends.at(-1)?.pick) !== '{"parts":["video","audio"],"quality":"720p","audio":"mp3"}') fail(`the menu's own pick sends ${JSON.stringify(sends.at(-1)?.pick)}, want the video at 720p and its audio as mp3`);
  more().fire('click');
  await settle();
  if (formatsAsked !== 1) fail(`opening the menu again asked the instance ${formatsAsked} times, want once`);
  if (!entries().includes('[1080p]')) fail('opened again, the menu does not start from the defaults');
  more().fire('click');

  // A drag places the button and keeps the place as shares of the player,
  // and sends nothing.
  bar().offsetWidth = 160;
  bar().offsetHeight = 32;
  button().rect = { left: 560, top: 68, width: 160, height: 32, right: 720, bottom: 100 };
  const before = sends.length;
  bar().dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 600, clientY: 80 });
  bar().dispatch('pointermove', { pointerId: 1, clientX: 602, clientY: 81 });
  if (frame()?.classList.contains('dragging')) fail('a press that barely moves already drags');
  bar().dispatch('pointermove', { pointerId: 1, clientX: 1000, clientY: 700 });
  bar().dispatch('pointermove', { pointerId: 1, clientX: 1400, clientY: 700 });
  bar().dispatch('pointerup', { pointerId: 1, clientX: 1400, clientY: 700 });
  send().fire('click');
  await settle();
  if (sends.length !== before) fail('the click that ends a drag sends the video');
  const spot = store.data.videoButtonSpot?.youtube;
  if (JSON.stringify(spot) !== '{"x":0.9375,"y":0.9}') fail(`the dragged button is stored at ${JSON.stringify(spot)}, want shares of the player kept inside it, 0.9375 and 0.9`);
  if (button().style.left !== '1200px' || button().style.top !== '704px') fail(`after the drag the button sits at ${button().style.left}, ${button().style.top}, want 1200px, 704px`);
  send().fire('click');
  await settle();
  if (sends.length !== before + 1) fail('after a drag the next click sends nothing');

  // Auto-hide: away until the pointer moves over the video, and back while
  // the keyboard is on it.
  await store.local.set({ videoButtonAutoHide: true });
  await settle();
  await new Promise((r) => setTimeout(r, 4100));
  doc.dispatch('pointermove', { clientX: 5, clientY: 5 });
  if (frame()?.classList.contains('shown')) fail('with auto-hide the button shows with the pointer away from the video');
  doc.dispatch('pointermove', { clientX: 300, clientY: 300 });
  if (!frame()?.classList.contains('shown')) fail('with auto-hide the button does not show with the pointer over the video');
  doc.dispatch('pointermove', { clientX: 5, clientY: 900 });
  if (frame()?.classList.contains('shown')) fail('with auto-hide the button stays once the pointer leaves the video');
  shadow.dispatch('focusin');
  if (!frame()?.classList.contains('shown')) fail('with auto-hide the button does not show when the keyboard reaches it');
  await store.local.set({ videoButtonAutoHide: false });

  // The switch and the site's own switch take the button off an open page
  // at once, without a reload, and bring it back.
  await store.local.set({ videoButton: false });
  if (button()) fail('switched off, the button stays on the open page until a reload');
  await store.local.set({ videoButton: true });
  if (!button()) fail('switched on again, the button does not come back to the open page');

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

  // Shorts keep the next video loaded below the screen. The place dragged to
  // goes with the site, so the reset puts the button back on the top edge.
  await store.local.set({ videoButtonSpot: undefined });
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
console.log("ok: every form of a video page sends its one address, the sites are a short list yt-dlp knows inside the optional access, a click sends the site's defaults to start at once and says when the video was there already, the menu offers what the video has, a drag keeps its place as shares of the player and sends nothing, auto-hide shows the button only over the video, and the switches take it off an open page at once and back");
