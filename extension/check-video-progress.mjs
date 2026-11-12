/**
 * The download button following what it sent (src/video.js,
 * src/video-worker.js, src/video-button.js), run against a stubbed `chrome`, a
 * small stand-in for the page and a clock the checks move by hand.
 *
 *   1. Only the rows a send created and started are followed, with the
 *      other rows of their links; the share downloaded is summed over them,
 *      unknown while a row has no size, and a row that fails for good ends it.
 *   2. The worker hands the button what to follow, asks the instance for
 *      those rows alone through the relay, and opens the instance's web
 *      interface on its downloads.
 *   3. On the page a ring runs around the mark while the instance downloads,
 *      spins where the size is unknown (and stands still with reduced
 *      motion), "Done" follows and opens the download, a failure shows as one
 *      with its reason, and the button stops asking once the rows are done,
 *      failed or gone, or the page moves on to another video.
 *
 * Run by CI and by hand: `node extension/check-video-progress.mjs`
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
const settle = () => new Promise((r) => setTimeout(r, 20));

function event() {
  const listeners = [];
  return { addListener: (f) => listeners.push(f), listeners };
}

function makeChrome() {
  const data = {};
  const onChanged = event();
  const tabs = [];
  const chrome = {
    runtime: {
      onInstalled: event(), onMessage: event(), onStartup: event(), onConnect: event(),
      openOptionsPage() {}, getManifest: () => manifest,
      getURL: (p) => `chrome-extension://test/${p}`,
      sendMessage: async () => undefined,
    },
    storage: {
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
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
    },
    permissions: { onAdded: event(), onRemoved: event(), contains: async () => true, request: async () => false, remove: async () => true },
    action: { getUserSettings: async () => ({ isOnToolbar: true }), setBadgeText() {}, setBadgeBackgroundColor() {}, setTitle() {}, openPopup: async () => {} },
    scripting: {
      getRegisteredContentScripts: async () => [],
      registerContentScripts: async () => {},
      unregisterContentScripts: async () => {},
      executeScript: async () => [],
    },
    declarativeNetRequest: { updateEnabledRulesets: async () => {} },
    tabs: { query: async () => [], create: async (o) => { tabs.push(o.url); }, onRemoved: event() },
  };
  return { chrome, tabs };
}

// 1. What a send's rows come to.
{
  const ctx = vm.createContext({ URL, chrome: {} });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  const watchIds = vm.runInContext('videoWatchIds', ctx);
  const progress = vm.runInContext('videoProgress', ctx);
  const ids = (answer) => JSON.stringify(watchIds(answer));

  if (ids({ tasks: [{ id: 'a1', status: 'queued' }, { id: 'a2', status: 'running' }], listed: [] }) !== '["a1","a2"]') fail(`a send that started two rows follows ${ids({ tasks: [{ id: 'a1', status: 'queued' }, { id: 'a2', status: 'running' }], listed: [] })}`);
  for (const [answer, why] of [
    [{ tasks: [{ id: 'a', status: 'collected', skipped: true }], listed: [] }, 'a row the filter held back'],
    [{ tasks: [{ id: 'a', status: 'collected' }], listed: [] }, 'a row left in the collector'],
    [{ tasks: [{ id: 'a', status: 'collected', variantOff: true }], listed: [] }, 'a row its preset set aside'],
    [{ tasks: [], listed: [{ id: 'old', status: 'running' }] }, 'a video already in the list'],
    [{ tasks: [], listed: [] }, 'a send that created nothing'],
    [{ tasks: [{ id: '../settings', status: 'queued' }], listed: [] }, 'an id that is no task id'],
    [{ tasks: Array.from({ length: 300 }, (_, i) => ({ id: `r${i}`, status: 'queued' })), listed: [] }, 'a playlist of three hundred rows'],
  ]) {
    if (watchIds(answer).length) fail(`${why} is followed`);
  }

  const at = '2026-11-11T10:00:00Z';
  const later = '2026-11-11T10:05:00Z';
  const video = 'https://www.youtube.com/watch?v=aqz-KE-bpKQ';
  const row = (o) => ({ url: video, createdAt: at, status: 'running', size: 0, loaded: 0, ...o });
  const state = (tasks, named = ['v']) => JSON.stringify(progress(tasks, named));
  // The video row and its subtitle row, staged together, and the same video
  // sent again later, which is another send's.
  const summed = [row({ id: 'v', size: 300, loaded: 100 }), row({ id: 's', size: 100, loaded: 100, status: 'done' }), row({ id: 'again', createdAt: later, size: 100, loaded: 0 })];
  if (state(summed) !== '{"state":"running","share":0.5}') fail(`two rows at 100 of 300 and 100 of 100 stand at ${state(summed)}, want running at half, without the later send's row`);
  const unknown = [row({ id: 'v', size: 0, loaded: 5000 }), row({ id: 's', size: 100, loaded: 50 })];
  if (state(unknown) !== '{"state":"running","share":null}') fail(`a row without a size stands at ${state(unknown)}, want running with the share unknown`);
  const aside = [row({ id: 'v', size: 100, loaded: 20 }), row({ id: 'th', status: 'collected', variantOff: true })];
  if (state(aside) !== '{"state":"running","share":0.2}') fail(`a row set aside counts: ${state(aside)}`);
  const done = [row({ id: 'v', status: 'done', size: 100, loaded: 100 }), row({ id: 's', status: 'done', size: 0, loaded: 40 })];
  if (state(done) !== '{"state":"done"}') fail(`every row finished stands at ${state(done)}, want done`);
  const failed = [row({ id: 'v', status: 'error', error: 'HTTP 403 from the host', nextTry: '0001-01-01T00:00:00Z' }), row({ id: 's', status: 'done' })];
  if (state(failed) !== '{"state":"failed","reason":"HTTP 403 from the host"}') fail(`a failed row stands at ${state(failed)}, want failed with its reason`);
  const retrying = [row({ id: 'v', status: 'error', error: 'timeout', nextTry: '2099-01-01T00:00:00Z', size: 100, loaded: 30 })];
  if (JSON.parse(state(retrying)).state !== 'running') fail(`a row waiting for its next try stands at ${state(retrying)}, want running`);
  if (state([]) !== '{"state":"gone"}') fail(`rows removed from the list stand at ${state([])}, want gone`);
  if (state([row({ id: 'other', url: 'https://vimeo.com/1' })]) !== '{"state":"gone"}') fail('a row of another link counts as one of the send');
  if (JSON.parse(state(summed.concat(row({ id: 'x', url: 'https://vimeo.com/1', size: 1000 })))).share !== 0.5) fail('an instance answering with its whole list counts rows the send did not create');
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

// 2. The worker.
{
  const { chrome, tabs } = makeChrome();
  await chrome.storage.local.set({ videoButton: true });
  const ctx = loadBackground(chrome);
  const calls = [];
  const answers = {};
  ctx.withGroup = async (work) => work({
    siblings: [{ instanceId: 'inst-1' }],
    call: async (target, method, path) => {
      calls.push({ target, method, path });
      return answers[path.split('?')[0]] ?? { status: 404, body: '' };
    },
  });
  const sendVideo = vm.runInContext('sendVideo', ctx);
  const page = 'https://www.youtube.com/watch?v=aqz-KE-bpKQ';

  answers['/api/links'] = { status: 200, body: JSON.stringify({ tasks: [{ id: 'a1', status: 'queued' }, { id: 'a2', status: 'collected', variantOff: true }], listed: [] }) };
  let r = await sendVideo(page, { href: page });
  if (r.result !== 'sent' || JSON.stringify(r.watch) !== '{"instance":"inst-1","ids":["a1"]}') fail(`a started send hands the button ${JSON.stringify(r)}, want the instance and the one started row to follow`);
  answers['/api/links'] = { status: 200, body: JSON.stringify({ tasks: [{ id: 'a1', status: 'collected', skipped: true }], listed: [] }) };
  r = await sendVideo(page, { href: page });
  if (r.watch) fail('a send held back hands the button something to follow');
  answers['/api/links'] = { status: 200, body: JSON.stringify({ tasks: [], listed: [{ id: 'old', status: 'running' }] }) };
  r = await sendVideo(page, { href: page });
  if (r.watch) fail('a video already there hands the button the older rows to follow');

  const listener = chrome.runtime.onMessage.listeners.find((f) => String(f).includes('knightloader-video-send'));
  const ask = (msg, url = page) => new Promise((resolve) => {
    if (listener(msg, { tab: { id: 3 }, url }, resolve) !== true) resolve(undefined);
  });
  answers['/api/tasks'] = { status: 200, body: JSON.stringify([{ id: 'a1', url: page, createdAt: 'x', status: 'running', size: 200, loaded: 50 }]) };
  calls.length = 0;
  const p = await ask({ type: 'knightloader-video-progress', instance: 'inst-1', ids: ['a1', 'b2'] });
  if (calls.at(-1)?.method !== 'GET' || calls.at(-1)?.path !== '/api/tasks?ids=a1,b2') fail(`the button's rows are asked for as ${calls.at(-1)?.method} ${calls.at(-1)?.path}, want GET /api/tasks?ids=a1,b2`);
  if (JSON.stringify(p) !== '{"state":"running","share":0.25}') fail(`the worker answers the button ${JSON.stringify(p)}, want running at a quarter`);
  calls.length = 0;
  for (const [msg, why] of [
    [{ type: 'knightloader-video-progress', instance: 'inst-1', ids: ['a1', 'x&files=1'] }, 'an id that is no task id'],
    [{ type: 'knightloader-video-progress', instance: 'inst-9', ids: ['a1'] }, 'an instance outside the group'],
    [{ type: 'knightloader-video-progress', instance: 'inst-1', ids: [] }, 'no ids'],
  ]) {
    const got = await ask(msg);
    if (got != null || calls.length) fail(`asked with ${why}, the worker calls ${calls.map((c) => c.path).join(', ')} and answers ${JSON.stringify(got)}`);
  }
  if ((await ask({ type: 'knightloader-video-progress', instance: 'inst-1', ids: ['a1'] }, 'chrome-extension://test/options.html')) !== undefined) fail('a frame that is no web page is answered');
  answers['/api/tasks'] = { status: 502, body: '' };
  if ((await ask({ type: 'knightloader-video-progress', instance: 'inst-1', ids: ['a1'] })) !== null) fail('an instance that does not answer is read as a state');

  answers['/api/remote-access'] = { status: 200, body: JSON.stringify({ addresses: [{ url: 'http://127.0.0.1:8749', loopback: true }, { url: 'https://kl.example.com/kl', domain: true }] }) };
  await ask({ type: 'knightloader-video-open', instance: 'inst-1' });
  await settle();
  if (tabs.at(-1) !== 'https://kl.example.com/kl/downloads') fail(`"Done" opens ${tabs.at(-1)}, want the instance's downloads at https://kl.example.com/kl/downloads`);
  const opened = tabs.length;
  await ask({ type: 'knightloader-video-open', instance: 'inst-9' });
  await settle();
  if (tabs.length !== opened) fail('the button can open an instance outside the group');

  const labels = await vm.runInContext('videoLabels', ctx)();
  for (const k of ['done', 'doneTip', 'loadFailed', 'downloading']) if (!labels[k]) fail(`the button gets no "${k}" label`);
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
    this.offsetLeft = 0;
    this.offsetTop = 0;
  }
  get parentElement() { return this.parentNode; }
  get isConnected() {
    let n = this;
    while (n.parentNode) n = n.parentNode;
    return n === this.doc.documentElement;
  }
  getRootNode() {
    let n = this;
    while (n.parentNode) n = n.parentNode;
    return n === this.doc.documentElement ? this.doc : n;
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
  matches(selector) { return selector.split(',').some((part) => part.trim() === this.tagName); }
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
  fire(type, e = {}) { for (const f of this.listeners[type] ?? []) f({ isTrusted: true, stopPropagation() {}, preventDefault() {}, composedPath: () => [], ...e }); }
  setPointerCapture() {}
  animate() { return { finished: Promise.resolve(), cancel() {} }; }
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
  doc.elementsFromPoint = () => doc.documentElement.querySelectorAll('video');
  doc.addEventListener = (type, f) => (doc.listeners[type] ??= []).push(f);
  doc.dispatch = (type, e = {}) => (doc.listeners[type] ?? []).forEach((f) => f({ composedPath: () => [], ...e }));
  return doc;
}

/** A clock the page's timers run on, moved forward by hand. */
function makeClock() {
  const timers = new Map();
  let now = 0;
  let next = 1;
  return {
    now: () => now,
    setTimeout: (f, ms = 0) => {
      timers.set(next, { at: now + ms, f });
      return next++;
    },
    clearTimeout: (id) => timers.delete(id),
    async advance(ms) {
      const until = now + ms;
      for (;;) {
        const due = [...timers.entries()].filter(([, t]) => t.at <= until).sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        timers.delete(due[0]);
        now = due[1].at;
        due[1].f();
        await settle();
      }
      now = until;
    },
  };
}

const PAGE_LABELS = {
  send: 'Download', sendTip: 'Send', more: 'More', sending: 'Sending', sent: 'Sent', already: 'Already there', held: 'Held back',
  done: 'Done', doneTip: 'Done, click to see it', loadFailed: 'Download failed', downloading: 'Downloading',
  mark: '<svg viewBox="0 0 587 901.8"></svg>', dir: 'ltr', lang: 'en',
};

/** The page with the button on it, and what it asked the worker. */
async function loadPage({ reduced = false } = {}) {
  const { chrome } = makeChrome();
  const clock = makeClock();
  const asked = [];
  const page = { send: { result: 'sent' }, progress: null };
  chrome.runtime.sendMessage = async (msg) => {
    asked.push(msg);
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-send') return page.send;
    if (msg.type === 'knightloader-video-progress') return page.progress;
    return undefined;
  };
  const doc = makeDocument();
  const location = { href: 'https://www.youtube.com/watch?v=aqz-KE-bpKQ' };
  const ctx = vm.createContext({
    chrome, document: doc, location, URL, console, Intl,
    setTimeout: clock.setTimeout, clearTimeout: clock.clearTimeout,
    setInterval: () => 0, requestAnimationFrame: () => 0, performance: { now: clock.now },
    matchMedia: (q) => ({ matches: reduced && q.includes('reduce'), addEventListener() {} }),
    getComputedStyle: () => ({ overflow: 'visible', visibility: 'visible', opacity: '1' }),
    window: { addEventListener() {} }, innerWidth: 1280, innerHeight: 800,
    navigator: { userAgent: 'test' }, CSS: { supports: () => false },
    DOMParser: class { parseFromString() { return { documentElement: doc.createElementNS('', 'svg') }; } },
  });
  ctx.globalThis = ctx;
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  const v = doc.createElement('video');
  v.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };
  player.append(v);
  for (const f of ['appearance.js', 'video.js', 'video-button.js']) vm.runInContext(read('src', f), ctx, { filename: f });
  await settle();
  const shadow = doc.documentElement.querySelectorAll('knightloader-video-button')[0].shadow;
  const send = shadow.querySelectorAll('button').find((b) => b.className === 'send');
  const frame = shadow.querySelectorAll('div').find((d) => d.className === 'kl');
  const arc = () => shadow.querySelectorAll('circle').find((c) => c.attrs.class === 'arc');
  const label = () => send.children.find((c) => c.tagName === 'span')?.textContent;
  const polls = () => asked.filter((m) => m.type === 'knightloader-video-progress').length;
  const style = shadow.querySelectorAll('style')[0]?.textContent ?? '';
  return { doc, location, clock, asked, page, send, frame, arc, label, polls, style };
}

// 3. The ring, "Done" and a failure on the page.
{
  const p = await loadPage();
  p.page.send = { result: 'sent', watch: { instance: 'inst-1', ids: ['a1'] } };
  p.send.fire('click');
  await settle();
  if (p.frame.dataset.ring) fail('the ring runs before the instance has said anything');
  p.page.progress = { state: 'running', share: 0.25 };
  await p.clock.advance(3000);
  const poll = p.asked.findLast((m) => m.type === 'knightloader-video-progress');
  if (p.polls() !== 1 || poll?.instance !== 'inst-1' || JSON.stringify(poll?.ids) !== '["a1"]') fail(`three seconds after the send the button asked ${p.polls()} times about ${JSON.stringify(poll)}, want once about a1 on inst-1`);
  if (p.frame.dataset.ring !== 'share' || p.arc()?.attrs['stroke-dasharray'] !== '25 100') fail(`a quarter downloaded draws the ring as ${p.frame.dataset.ring} ${p.arc()?.attrs['stroke-dasharray']}, want a quarter of it`);
  // The ring starts under "Sent", and the label is the button's own once that
  // has gone.
  await p.clock.advance(1500);
  if (!p.send.title.startsWith('Downloading') || !p.send.title.includes('25')) fail(`while the ring runs the button says "${p.send.title}", want that it is downloading and how far`);
  if (p.label() !== 'Download') fail(`while the ring runs the label reads "${p.label()}", want the button's own`);
  p.page.progress = { state: 'running', share: null };
  await p.clock.advance(1500);
  if (p.frame.dataset.ring !== 'unknown') fail(`with the size unknown the ring is ${p.frame.dataset.ring}, want it spinning without a share`);
  if (!/\.kl\[data-ring="unknown"\] \.ring \{[^}]*animation:/.test(p.style)) fail('the ring for an unknown size does not spin');

  // Longer downloads are asked about less often.
  await p.clock.advance(60000 - 6000 + 1);
  const atMinute = p.polls();
  await p.clock.advance(30000);
  if (p.polls() - atMinute !== 3) fail(`after a minute the button asks ${p.polls() - atMinute} times in thirty seconds, want every ten seconds`);

  p.page.progress = { state: 'done' };
  await p.clock.advance(10000);
  const finished = p.polls();
  if (p.label() !== 'Done' || p.frame.dataset.state !== 'done' || p.frame.dataset.ring) fail(`a finished download shows "${p.label()}" ${p.frame.dataset.state} with ring ${p.frame.dataset.ring}, want "Done" without the ring`);
  if (p.send.title !== 'Done, click to see it') fail(`"Done" says "${p.send.title}", want that a click shows it`);
  await p.clock.advance(11000);
  if (p.polls() !== finished) fail('the button keeps asking after the download finished');
  const sends = p.asked.filter((m) => m.type === 'knightloader-video-send').length;
  p.send.fire('click');
  await settle();
  if (p.asked.at(-1)?.type !== 'knightloader-video-open' || p.asked.at(-1)?.instance !== 'inst-1') fail(`a click on "Done" asks ${JSON.stringify(p.asked.at(-1))}, want the instance's web interface opened`);
  if (p.asked.filter((m) => m.type === 'knightloader-video-send').length !== sends) fail('a click on "Done" sends the video again');
  await p.clock.advance(30000);
  if (p.label() !== 'Download') fail(`"Done" stays as "${p.label()}" for good`);

  // A row that fails ends it, with the instance's reason.
  p.page.send = { result: 'sent', watch: { instance: 'inst-1', ids: ['b2'] } };
  p.send.fire('click');
  await settle();
  p.page.progress = { state: 'failed', reason: 'HTTP 403 from the host' };
  await p.clock.advance(3000);
  const failedAt = p.polls();
  if (p.frame.dataset.state !== 'loadFailed' || p.label() !== 'Download failed' || p.send.title !== 'HTTP 403 from the host') fail(`a failed row shows ${p.frame.dataset.state} "${p.label()}" "${p.send.title}", want the failure with the host's reason`);
  if (!/\.kl\[data-state="loadFailed"\] \.send/.test(p.style)) fail('a failed download does not take the colour of a refusal');
  await p.clock.advance(15000);
  if (p.polls() !== failedAt) fail('the button keeps asking after a row failed');
  await p.clock.advance(30000);

  // Rows taken out of the list end it without a word.
  p.send.fire('click');
  await settle();
  p.page.progress = { state: 'running', share: 0.5 };
  await p.clock.advance(3000);
  p.page.progress = { state: 'gone' };
  await p.clock.advance(3000);
  const goneAt = p.polls();
  if (p.frame.dataset.ring || p.frame.dataset.state) fail(`rows removed from the list leave the ring ${p.frame.dataset.ring} and the state ${p.frame.dataset.state}`);
  await p.clock.advance(15000);
  if (p.polls() !== goneAt) fail('the button keeps asking about rows that are gone');

  // Another video drops the ring; the old download goes on without it.
  p.send.fire('click');
  await settle();
  p.page.progress = { state: 'running', share: 0.5 };
  await p.clock.advance(3000);
  p.location.href = 'https://www.youtube.com/watch?v=dQw4w9WgXcQ';
  p.doc.dispatch('yt-navigate-finish');
  const movedAt = p.polls();
  if (p.frame.dataset.ring) fail('on the next video the ring of the last one stays');
  await p.clock.advance(15000);
  if (p.polls() !== movedAt) fail('on the next video the button keeps asking about the last one');

  // Nothing to follow, nothing asked.
  for (const answer of [{ result: 'already' }, { result: 'held' }, { result: 'sent' }]) {
    p.page.send = answer;
    p.send.fire('click');
    await settle();
    const before = p.polls();
    await p.clock.advance(10000);
    if (p.polls() !== before || p.frame.dataset.ring) fail(`a send answered ${answer.result} without rows to follow runs the ring`);
  }

  // The ring is a thin line in the accent, and stands still with reduced
  // motion.
  if (!/\.ring \.arc \{[^}]*stroke: var\(--accent\)/.test(p.style) || !/\.ring circle \{[^}]*stroke-width: 2/.test(p.style)) fail('the ring is not a 2px line in the accent colour');
  const reduce = p.style.slice(p.style.indexOf('prefers-reduced-motion: reduce'));
  if (!/\.kl\[data-ring="unknown"\] \.ring[^{]*\{[^}]*animation: none/.test(reduce)) fail('with reduced motion the ring still spins');
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: the button follows only the rows a send created and started, sums what they downloaded, spins where the size is unknown, says "Done" and opens the download, shows a failure with its reason, and stops asking once the rows are done, failed or gone, or the page moves on');
