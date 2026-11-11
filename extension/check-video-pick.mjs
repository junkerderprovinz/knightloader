/**
 * Where the download button on a video's player sends to, run against the
 * background scripts with a stubbed `chrome`.
 *
 *   1. With one instance online the button's click goes straight to it, as
 *      before, and the popup stays shut.
 *   2. With more than one, the send is parked for the popup with the video's
 *      address, pick, playlist and name, the popup opens over the instances
 *      the worker found, and the button hears it is waiting.
 *   3. The instance picked in the popup gets the same link and choice the
 *      straight send carries, and the button hears what became of it.
 *   4. A popup closed without a pick calls the button off, and one closed
 *      after a pick does not.
 *   5. Where the browser will not open the popup by itself (Firefox before
 *      149), the toolbar icon says a send is waiting and the send stays
 *      parked for the click on it. A Click'n'Load batch parked over it calls
 *      the button off and still goes through the popup as before.
 *   6. The popup tells the worker when it closes, shows the video's name and
 *      the parts in one line, and names the send button for a video.
 *
 * Run by CI and by hand: `node extension/check-video-pick.mjs`
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

function area(data = {}) {
  return {
    data,
    get: async (keys) => {
      const list = keys == null ? Object.keys(data) : Array.isArray(keys) ? keys : [keys];
      return Object.fromEntries(list.filter((k) => k in data).map((k) => [k, data[k]]));
    },
    set: async (o) => void Object.assign(data, o),
    remove: async (keys) => {
      for (const k of Array.isArray(keys) ? keys : [keys]) delete data[k];
    },
  };
}

function makeChrome({ popupOpens = true } = {}) {
  const seen = { popups: 0, badge: [], told: [] };
  const chrome = {
    runtime: {
      onInstalled: event(), onMessage: event(), onStartup: event(), onConnect: event(),
      openOptionsPage() {}, getManifest: () => manifest,
      getURL: (p) => `chrome-extension://test/${p}`,
      sendMessage: async () => undefined,
    },
    storage: { onChanged: event(), local: area(), session: area() },
    permissions: { onAdded: event(), onRemoved: event(), contains: async () => true },
    action: {
      getUserSettings: async () => ({ isOnToolbar: true }),
      setBadgeText: ({ text }) => seen.badge.push(text),
      setBadgeBackgroundColor() {}, setTitle() {},
      openPopup: async () => {
        if (!popupOpens) throw new Error('openPopup requires a user gesture');
        seen.popups++;
      },
    },
    windows: {
      getAll: async () => [{ id: 1, type: 'normal', tabs: [{ active: true, url: 'https://www.youtube.com/watch?v=aqz-KE-bpKQ' }] }],
      update: async () => {},
    },
    scripting: {
      getRegisteredContentScripts: async () => [],
      registerContentScripts: async () => {},
      unregisterContentScripts: async () => {},
      executeScript: async () => [],
    },
    declarativeNetRequest: { updateEnabledRulesets: async () => {} },
    tabs: {
      query: async () => [],
      onRemoved: event(),
      sendMessage: async (tabId, msg, opts) => void seen.told.push({ tabId, frameId: opts?.frameId, msg }),
    },
  };
  return { chrome, seen };
}

/** Loads the background scripts as Firefox does, with a group of `ids`. */
function loadBackground(chrome, ids) {
  const ctx = vm.createContext({
    chrome, console: { log() {}, warn() {}, error() {} }, setTimeout, clearTimeout,
    crypto: globalThis.crypto, TextEncoder, TextDecoder, URL, Intl, navigator: { language: 'en-US', languages: ['en-US'], userAgent: 'test' },
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'), atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    WebSocket: class {}, indexedDB: { open: () => ({}) },
    fetch: async () => ({ text: async () => '' }),
  });
  for (const f of manifest.background.scripts) vm.runInContext(read('src', f), ctx, { filename: f });
  const posted = [];
  ctx.withGroup = async (work) => work({
    siblings: ids.map((instanceId) => ({ instanceId, name: instanceId })),
    call: async (target, method, path, body) => {
      posted.push({ target, method, path, body: body ? JSON.parse(body) : undefined });
      return { status: 200, body: '{"tasks":[{"id":"a"}],"listed":[]}' };
    },
  });
  return { ctx, posted };
}

/** Hands a message to every listener the way the browser does, with the
 *  first answer as the reply. */
async function message(chrome, msg, sender) {
  let reply;
  const answered = new Promise((resolve) => {
    for (const f of chrome.runtime.onMessage.listeners) f(msg, sender, (r) => { reply ??= r; resolve(); });
    setTimeout(resolve, 200);
  });
  await answered;
  await settle();
  return reply;
}

/** A popup's port to the worker, which the test closes like a window. */
function connect(chrome, name) {
  const onMessage = event();
  const onDisconnect = event();
  const port = { name, sender: { url: 'chrome-extension://test/popup.html' }, onMessage, onDisconnect };
  for (const f of chrome.runtime.onConnect.listeners) f(port);
  return {
    post: (m) => onMessage.listeners.forEach((f) => f(m, port)),
    close: () => onDisconnect.listeners.forEach((f) => f(port)),
  };
}

const watch = 'https://www.youtube.com/watch?v=aqz-KE-bpKQ&list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB';
const page = { tab: { id: 7, url: watch, title: 'Big Buck Bunny - YouTube' }, frameId: 0, url: watch };
const embed = 'https://www.youtube-nocookie.com/embed/aqz-KE-bpKQ';
const framed = { tab: { id: 8, url: 'https://blog.example/post', title: 'A blog' }, frameId: 3, url: embed };
const pick = { parts: ['video', 'subtitle', 'chapters'], quality: '1080p', audio: 'best', subLangs: ['de'] };
const popupPage = { url: 'chrome-extension://test/popup.html' };

// 1. One instance: straight there, as before.
let straight;
{
  const { chrome, seen } = makeChrome();
  const { posted } = loadBackground(chrome, ['inst-1']);
  const reply = await message(chrome, { type: 'knightloader-video-send', href: watch, pick, playlist: true }, page);
  straight = posted.at(-1);
  if (reply?.result !== 'sent') fail(`with one instance a click answers ${JSON.stringify(reply)}, want sent`);
  if (posted.length !== 1 || straight?.target !== 'inst-1') fail(`with one instance the video went to ${JSON.stringify(posted.map((p) => p.target))}, want inst-1 once`);
  if (seen.popups !== 0 || chrome.storage.session.data.pendingSend) fail('with one instance the click still asks in the popup');
}

// 2. to 4. Two instances: the popup asks, its pick sends, a close calls off.
{
  const { chrome, seen } = makeChrome();
  const { posted } = loadBackground(chrome, ['inst-1', 'inst-2']);
  await chrome.storage.local.set({ defaultInstance: 'inst-1' });
  const reply = await message(chrome, { type: 'knightloader-video-send', href: watch, pick, playlist: true }, page);
  const parked = chrome.storage.session.data.pendingSend;
  const v = parked?.payload;
  if (reply?.result !== 'asking') fail(`with two instances a click answers ${JSON.stringify(reply)}, want asking`);
  if (posted.length) fail('with two instances the click sent before the popup was asked');
  if (seen.popups !== 1) fail(`with two instances the popup opened ${seen.popups} times, want once`);
  if (!v) fail('with two instances nothing is parked for the popup');
  else {
    if (v.kind !== 'video' || v.href !== watch || v.playlist !== true || v.tabId !== 7 || v.frameId !== 0) {
      fail(`the parked video reads ${JSON.stringify(v)}, want its address, the playlist and the frame that asked`);
    }
    if (JSON.stringify(v.pick?.parts) !== '["video","subtitle"]') fail(`the parked pick holds ${JSON.stringify(v.pick)}, want it cleaned to video and subtitles`);
    if (v.title !== 'Big Buck Bunny - YouTube') fail(`the parked video is called "${v.title}", want the tab's title`);
    if (v.detail !== 'Whole playlist · Video, Subtitles') fail(`the parked video's parts read "${v.detail}", want "Whole playlist · Video, Subtitles"`);
    if (parked.origin === 'cnl') fail('a video send is parked as a Click\'n\'Load batch, which counts down');
    if (parked.siblings?.map((s) => s.instanceId).join() !== 'inst-1,inst-2' || parked.defaultName !== 'inst-1') fail('the popup does not get the instances the worker found, preset to the default');
  }

  // The popup reads the send and hands the pick over, as for every other one.
  await chrome.storage.session.remove('pendingSend');
  const port = connect(chrome, 'knightloader-video-pick');
  port.post(v);
  const handed = await message(chrome, { type: 'knightloader-send-to', target: 'inst-2', payload: v }, popupPage);
  port.close();
  await settle();
  const sent = posted.at(-1);
  if (!handed?.accepted) fail('the popup\'s pick of a video is not accepted');
  if (posted.length !== 1 || sent?.target !== 'inst-2') fail(`the popup's pick went to ${JSON.stringify(posted.map((p) => p.target))}, want inst-2 once`);
  if (JSON.stringify(sent?.body) !== JSON.stringify(straight?.body)) fail(`the picked instance gets ${JSON.stringify(sent?.body)}, want what a straight send carries: ${JSON.stringify(straight?.body)}`);
  const told = seen.told.filter((x) => x.msg?.type === 'knightloader-video-result');
  if (told.length !== 1 || told[0].tabId !== 7 || told[0].frameId !== 0 || told[0].msg.answer?.result !== 'sent') {
    fail(`after the pick the button hears ${JSON.stringify(told)}, want one "sent" for tab 7, frame 0`);
  }

  // A content script cannot make the pick.
  await message(chrome, { type: 'knightloader-send-to', target: 'inst-2', payload: v }, page);
  if (posted.length !== 1) fail('a content script could pick where a parked video goes');

  // An embedded player has no title of its own, and a popup closed without a
  // pick calls the button off.
  seen.told.length = 0;
  await message(chrome, { type: 'knightloader-video-send', href: embed, pick }, framed);
  const fromEmbed = chrome.storage.session.data.pendingSend?.payload;
  if (fromEmbed?.title !== 'youtube.com · aqz-KE-bpKQ') fail(`an embedded player's video is called "${fromEmbed?.title}", want its host and id`);
  if (fromEmbed?.detail !== 'Video, Subtitles') fail(`an embedded player's parts read "${fromEmbed?.detail}"`);
  const shut = connect(chrome, 'knightloader-video-pick');
  shut.post(fromEmbed);
  shut.close();
  await settle();
  const off = seen.told.filter((x) => x.msg?.type === 'knightloader-video-result');
  if (off.length !== 1 || off[0].tabId !== 8 || off[0].frameId !== 3 || off[0].msg.answer !== null) {
    fail(`a popup closed without a pick tells the button ${JSON.stringify(off)}, want one call-off for tab 8, frame 3`);
  }
  if (posted.length !== 1) fail('a popup closed without a pick still sent the video');

  // Only the popup's own port is listened to.
  seen.told.length = 0;
  const onMessage = event();
  const onDisconnect = event();
  const foreign = { name: 'knightloader-video-pick', sender: { url: watch, tab: page.tab }, onMessage, onDisconnect };
  for (const f of chrome.runtime.onConnect.listeners) f(foreign);
  onMessage.listeners.forEach((f) => f(fromEmbed, foreign));
  onDisconnect.listeners.forEach((f) => f(foreign));
  await settle();
  if (seen.told.length) fail('a port from a content script could call a button off');
}

// 5. Without openPopup outside a click, the icon waits for one.
{
  const { chrome, seen } = makeChrome({ popupOpens: false });
  const { posted } = loadBackground(chrome, ['inst-1', 'inst-2']);
  const reply = await message(chrome, { type: 'knightloader-video-send', href: watch, pick }, page);
  if (reply?.result !== 'asking') fail(`where the popup cannot open by itself a click answers ${JSON.stringify(reply)}, want asking`);
  if (seen.badge.at(-1) !== '…') fail(`where the popup cannot open by itself the icon shows ${JSON.stringify(seen.badge.at(-1))}, want "…"`);
  if (chrome.storage.session.data.pendingSend?.payload?.kind !== 'video') fail('where the popup cannot open by itself the video is not kept for the click on the icon');

  // A Click'n'Load batch arriving meanwhile takes the place, the button is
  // called off, and the batch goes on as it always did.
  seen.told.length = 0;
  await chrome.storage.local.set({ cnlEnabled: true });
  await message(chrome, { type: 'knightloader-cnl', fields: { urls: 'https://h.example/a.rar\r\nhttps://h.example/b.rar', package: 'Pack' }, pageUrl: 'https://h.example/' }, { tab: { id: 9 }, url: 'https://h.example/' });
  const batch = chrome.storage.session.data.pendingSend;
  if (batch?.origin !== 'cnl' || batch.payload?.text !== 'https://h.example/a.rar\nhttps://h.example/b.rar' || batch.payload?.title !== 'Pack') {
    fail(`a Click'n'Load batch over a waiting video is parked as ${JSON.stringify(batch)}`);
  }
  const off = seen.told.filter((x) => x.msg?.type === 'knightloader-video-result');
  if (off.length !== 1 || off[0].tabId !== 7 || off[0].msg.answer !== null) fail(`a batch parked over a waiting video tells the button ${JSON.stringify(off)}, want one call-off`);
  await message(chrome, { type: 'knightloader-send-to', target: 'inst-2', payload: batch.payload }, popupPage);
  const links = posted.at(-1);
  if (links?.target !== 'inst-2' || links.body?.links !== 'https://h.example/a.rar\nhttps://h.example/b.rar' || links.body?.package !== 'Pack' || links.body?.origin !== 'cnl') {
    fail(`a Click'n'Load batch picked in the popup goes out as ${JSON.stringify(links)}`);
  }
  if (seen.told.length !== 1) fail('a Click\'n\'Load batch told a video button about its send');
}

// 6. The popup's side.
{
  const popup = read('src', 'popup.js');
  if (!/chrome\.runtime\.connect\(\s*\{\s*name:\s*'knightloader-video-pick'\s*\}\s*\)/.test(popup)) fail('src/popup.js: does not tell the worker when a popup with a video closes');
  if (!/payload\??\.detail/.test(popup)) fail('src/popup.js: does not show which parts a video send downloads');
  const ctx = vm.createContext({});
  vm.runInContext(read('src', 'shared.js'), ctx);
  const key = vm.runInContext('sendLabelKey', ctx)({ origin: '', payload: { kind: 'video' } });
  if (key !== 'popup.sendVideo') fail(`the popup's button for a video reads ${key}, want popup.sendVideo`);
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: one instance takes a video at once, more ask in the popup, the pick goes where it was made with the same choice, a closed popup calls the button off, and the icon waits where the popup cannot open');
