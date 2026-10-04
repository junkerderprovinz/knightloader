// Draws the README pictures of the web UI and the browser extension, each in
// a drawn window beside a caption on the dark relief background of the store
// pictures. The web UI runs under Vite, driven by Playwright, with every API
// answer and the live socket's frames read from fixtures/. The extension's
// popup comes from extension/src over the film page of its store pictures.
// The data is made up, so nothing of a real instance can end up in a picture,
// and the clock is fixed, so a second run draws the same pictures.
//
//   cd scripts/screenshots
//   npm install && npx playwright install chromium
//   node shoot.mjs                     every picture
//   node shoot.mjs downloads desktop   only these
//
// Vite and the React plugin come from web/node_modules, which has to be
// installed. CHROME_PATH runs a Chromium of your own instead of Playwright's.
// The pictures land in .github/assets/screenshots/, quantised with pngquant or
// ImageMagick and recompressed with oxipng where those are on the PATH. The
// Android picture comes from mobile/store/render.mjs.

import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, extname, join, normalize, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { demoPage } from '../../extension/store/demo-page.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..', '..');
const web = join(root, 'web');
const extension = join(root, 'extension');
const fixtures = join(here, 'fixtures');
const out = join(root, '.github', 'assets', 'screenshots');

// Every timestamp in the fixtures lies shortly before this moment.
const NOW = new Date('2026-09-24T14:30:00Z');

// The address the pictures show. The browser resolves it to Vite on this
// machine; home.arpa is the name space reserved for home networks.
const HOST = 'keep.home.arpa';
const PORT = 8749;

const SUB = 'KnightLoader on your server or your desktop';

/**
 * The pages of the web UI, one picture each. `ready` is a selector that is on
 * screen once the page has its data, and `act` sets the scene up after that.
 */
const SCENES = [
  { n: 1, name: 'overview', path: '/', ready: 'text=bbb_sunflower', caption: 'What’s running, <em>at a glance</em>' },
  {
    n: 2,
    name: 'downloads',
    path: '/downloads',
    ready: 'text=bbb_sunflower',
    caption: 'Every file live. <em>Staring won’t speed it up.</em>',
  },
  {
    n: 3,
    name: 'quick-settings',
    path: '/downloads',
    ready: 'text=bbb_sunflower',
    caption: 'The limits you change most, <em>one click away</em>',
    act: (page) => page.getByRole('button', { name: 'Quick settings' }).click(),
  },
  { n: 4, name: 'collector', path: '/collector', ready: 'text=Sprite Fright', caption: 'One link, <em>every version of it</em>' },
  { n: 5, name: 'appearance', path: '/settings/appearance', ready: 'text=Rainbow mode', caption: 'Corners and colours <em>are your call</em>' },
  {
    n: 6,
    name: 'accounts',
    path: '/accounts',
    ready: 'text=Real-Debrid',
    caption: 'Bring the debrid account <em>you already pay for</em>',
  },
  {
    n: 7,
    name: 'rules',
    path: '/settings/rules',
    ready: 'text=Open movies',
    caption: 'Rules sort your links <em>as they come in</em>',
    // The sample matches fixtures/rules/preview.json. The page then scrolls to
    // the rule list, so the test box and the categories fit below it.
    async act(page) {
      await page.getByPlaceholder('Link URL').fill('https://files.example/d/7f3k2/tears_of_steel_1080p.mkv');
      const name = page.getByPlaceholder('File name');
      await name.fill('tears_of_steel_1080p.mkv');
      await name.blur();
      await page.getByText('Rules, in the order they run').evaluate((el) => el.scrollIntoView({ block: 'start' }));
    },
  },
  { n: 8, name: 'app', path: '/settings/browsertools', ready: 'text=Firefox', caption: 'Send links from <em>wherever you are</em>' },
  { n: 9, name: 'instances', path: '/instances', ready: 'text=Watchtower', caption: 'All your KnightLoaders <em>in one place</em>' },
];

// The wide pictures, one per way to run KnightLoader. `scene` names the page
// of SCENES they show.
const WIDE = [
  {
    name: 'desktop',
    scene: 'downloads',
    frame: 'app',
    caption: 'Downloads everything. <em>Yes, everything.</em>',
    sub: 'KnightLoader for Windows, macOS and Linux',
  },
  {
    name: 'container',
    scene: 'overview',
    frame: 'browser',
    caption: 'Runs on your server, <em>day and night</em>',
    sub: 'KnightLoader in Docker, on Unraid or any other host',
  },
  {
    name: 'extension',
    caption: 'Send the page you are on <em>to your own downloader</em>',
    sub: 'KnightLoader for your browser',
  },
];

// Every picture has the same canvas and the window in the same place, so the
// windows line up as the README scrolls. The window shows a page laid out
// 1440 wide, which keeps every column of the download list, at 1270/1440 of
// its size. The page pictures keep twice the pixels, to be read when opened.
const CANVAS = [1920, 1000];
const WINDOW = { x: 600, y: 96, w: 1270, h: 760 };
const PAGE = [1440, Math.round((1440 * WINDOW.h) / WINDOW.w)];

// Logos of the services the pictures show, keyed by host: from Dashboard Icons
// where it has one, else the site's own icon. A made-up host such as
// files.example has none and keeps its monogram.
const icons = join(fixtures, 'icons');

/**
 * fixture reads the answer to one request: fixtures/<path>.json for
 * /api/<path>. A query can pick a narrower file, so ?host=youtube.com on
 * ytdlp/formats reads ytdlp/formats/youtube.com.json when that exists.
 */
function fixture(url) {
  const path = url.pathname.replace(/^\/api\//, '');
  const narrow = [...url.searchParams.values()][0];
  for (const name of narrow ? [join(path, narrow), path] : [path]) {
    const file = join(fixtures, `${name}.json`);
    if (existsSync(file)) return readFileSync(file, 'utf8');
  }
  return null;
}

function json(name) {
  return JSON.parse(readFileSync(join(fixtures, `${name}.json`), 'utf8'));
}

/** hosterIcon answers /api/hosters/icon with the logo of the host or of a domain above it. */
function hosterIcon(route, host) {
  for (let h = host; h.includes('.'); h = h.slice(h.indexOf('.') + 1)) {
    for (const [ext, contentType] of [['svg', 'image/svg+xml'], ['png', 'image/png']]) {
      const file = join(icons, `${h}.${ext}`);
      if (existsSync(file)) return route.fulfill({ contentType, body: readFileSync(file) });
    }
  }
  return route.fulfill({ status: 204 });
}

async function answer(route, missing) {
  const request = route.request();
  const url = new URL(request.url());
  if (url.pathname === '/api/hosters/icon') return hosterIcon(route, url.searchParams.get('host') ?? '');
  const body = fixture(url);
  if (body !== null) return route.fulfill({ contentType: 'application/json', body });
  // A save the page makes on its own succeeds without changing anything.
  if (request.method() !== 'GET') return route.fulfill({ status: 204 });
  missing.add(url.pathname);
  return route.fulfill({ status: 404 });
}

// The socket opens with the task list and the idle activity counters, as the
// server's does; everything it would send later is left out.
function socket(ws) {
  ws.send(JSON.stringify({ type: 'snapshot', data: json('tasks') }));
  for (const frame of json('ws')) ws.send(JSON.stringify(frame));
}

async function startVite() {
  // Tailwind looks for class names below the working directory.
  process.chdir(web);
  const { createServer } = createRequire(join(web, 'package.json'))('vite');
  const server = await createServer({
    configFile: join(web, 'vite.config.ts'),
    root: web,
    logLevel: 'warn',
    // Kept out of web/node_modules, which several checkouts may share.
    cacheDir: join(tmpdir(), 'knightloader-screenshots'),
    server: {
      host: '127.0.0.1',
      // KnightLoader's own port where it is free, since the Instances page
      // prints the address.
      port: PORT,
      allowedHosts: [HOST],
      // web/node_modules can be a link to an install elsewhere, and Vite
      // serves nothing outside the folders it is allowed.
      fs: { allow: [web, realpathSync(join(web, 'node_modules'))] },
    },
  });
  await server.listen();
  return server;
}

/** A page of the web UI in the dark theme, laid out at `view` and taken at twice the pixels. */
async function webPage(browser, base, scene, [width, height], missing) {
  const context = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: 2,
    locale: 'en-US',
    timezoneId: 'UTC',
    colorScheme: 'dark',
    // The pictures show where each page settles, not a transition halfway.
    reducedMotion: 'reduce',
  });
  await context.clock.setFixedTime(NOW);
  await context.addInitScript(() => {
    localStorage.setItem('kl-theme', 'dark');
    localStorage.setItem('kl-lang', 'en');
  });
  await context.route('**/api/**', (route) => answer(route, missing));
  await context.routeWebSocket('**/api/ws', socket);

  const page = await context.newPage();
  await page.goto(base + scene.path);
  await page.locator(scene.ready).first().waitFor();
  if (scene.act) await scene.act(page);
  await page.evaluate(() => document.fonts.ready);
  // Two ticks of the speed curve, so the live end has joined the history.
  await page.waitForTimeout(2500);
  const png = await page.screenshot();
  await context.close();
  return png;
}

// The extension's pages run in a plain tab rather than as an installed
// extension: the chrome.* calls are answered by a stub, and group.js's
// withGroup is swapped for one that returns two instances without the relay.

const SIBLINGS = [
  { instanceId: 'a1', name: 'Keep', deployment: 'container' },
  { instanceId: 'b2', name: 'Gatehouse', deployment: 'desktop' },
];
const GiB = 1024 ** 3;
const MiB = 1024 ** 2;
const SIBLING_API = {
  a1: {
    '/api/queue': { halted: false, running: 2 },
    '/api/queue/counters': { files: 3, running: 2, remaining: 2.4 * GiB, speed: 38 * MiB },
    '/api/remote-access': { addresses: [{ url: `http://${HOST}:${PORT}` }] },
  },
  b2: {
    '/api/queue': { halted: true, running: 0 },
    '/api/queue/counters': { files: 1, running: 0 },
    '/api/remote-access': { addresses: [{ url: `http://gatehouse.home.arpa:${PORT}` }] },
  },
};
// BIP39's all-zero test vector: a valid phrase that opens no real group.
const PHRASE = `${'abandon '.repeat(11)}about`;
const FILM = { id: 1, title: 'Big Buck Bunny · Open Movies', url: 'https://openmovies.example/films/big-buck-bunny' };
// The film page is made for a narrow window, so it is laid out narrower than
// PAGE and drawn larger, the popup with it.
const FILM_VIEW = [1000, Math.round((1000 * WINDOW.h) / WINDOW.w)];

function stubChrome({ local, manifest, tab }) {
  const area = (mem) => ({
    async get(keys) {
      if (keys == null) return { ...mem };
      const list = typeof keys === 'string' ? [keys] : Array.isArray(keys) ? keys : Object.keys(keys);
      const got = typeof keys === 'object' && !Array.isArray(keys) ? { ...keys } : {};
      for (const k of list) if (k in mem) got[k] = mem[k];
      return got;
    },
    async set(items) {
      Object.assign(mem, items);
    },
    async remove(keys) {
      for (const k of [].concat(keys)) delete mem[k];
    },
    onChanged: { addListener() {}, removeListener() {} },
  });
  const event = { addListener() {}, removeListener() {}, hasListener: () => false };
  const noop = () => {};
  window.chrome = {
    storage: { local: area(local), session: area({}), onChanged: event },
    runtime: { id: 'knightloader', getManifest: () => manifest, getURL: (p) => p, sendMessage: async () => ({}), openOptionsPage: noop, onMessage: event },
    tabs: { query: async () => [tab], create: noop },
    permissions: { contains: async () => true, request: async () => true, remove: async () => true, onAdded: event, onRemoved: event },
    action: { setBadgeText: noop, setTitle: noop },
    scripting: { getRegisteredContentScripts: async () => [] },
    declarativeNetRequest: { getEnabledRulesets: async () => [] },
  };
}

const fakeGroup = `
withGroup = async (work) => work({
  siblings: ${JSON.stringify(SIBLINGS)},
  call: async (id, method, path) => ({ status: 200, body: JSON.stringify((${JSON.stringify(SIBLING_API)})[id][path]) }),
});`;

async function cached(file, url) {
  const path = join(tmpdir(), `KnightLoader-${file}`);
  if (!existsSync(path)) {
    const res = await fetch(url, { headers: { 'user-agent': 'knightloader-readme-pictures' } });
    if (!res.ok) throw new Error(`${file}: fetch ${res.status}`);
    writeFileSync(path, Buffer.from(await res.arrayBuffer()));
  }
  return readFileSync(path);
}

// The extension's files and the film page are served by request interception
// under a made-up origin, so no port is opened for them.
const EXT_BASE = 'https://readme.knightloader.invalid';
const MIME = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2' };

async function serveExtension(context, images) {
  await context.route(`${EXT_BASE}/**`, (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/film') return route.fulfill({ contentType: 'text/html', body: demoPage({ poster: '/poster.jpg', still: '/still.jpg' }) });
    if (path in images) return route.fulfill({ contentType: 'image/jpeg', body: images[path] });
    const file = normalize(join(extension, decodeURIComponent(path)));
    if (!file.startsWith(extension) || !existsSync(file)) return route.fulfill({ status: 404 });
    let body = readFileSync(file);
    if (file === join(extension, 'src', 'group.js')) body = body.toString() + fakeGroup;
    return route.fulfill({ contentType: MIME[extname(file)] ?? 'application/octet-stream', body });
  });
}

async function extensionShots(browser) {
  const images = {
    '/poster.jpg': await cached(
      'bbb-poster.jpg',
      'https://upload.wikimedia.org/wikipedia/commons/thumb/c/c5/Big_buck_bunny_poster_big.jpg/960px-Big_buck_bunny_poster_big.jpg',
    ),
    '/still.jpg': await cached('bbb-forest.jpg', 'https://upload.wikimedia.org/wikipedia/commons/6/69/Big_Buck_Bunny_-_forest.jpg'),
  };
  const manifest = JSON.parse(readFileSync(join(extension, 'src', 'manifest.json'), 'utf8'));

  const popupContext = await browser.newContext({ viewport: { width: 420, height: 800 }, deviceScaleFactor: 2, colorScheme: 'dark' });
  await popupContext.addInitScript(stubChrome, { local: { phrase: PHRASE, defaultInstance: 'a1', language: 'en' }, manifest, tab: FILM });
  await serveExtension(popupContext, images);
  const popupTab = await popupContext.newPage();
  await popupTab.goto(`${EXT_BASE}/src/popup.html`);
  await popupTab.evaluate(() => document.fonts.ready);
  await popupTab.locator('.glim-instance .glim-status').first().waitFor();
  // Past the cards' fade-in.
  await popupTab.waitForTimeout(900);
  const popup = await popupTab.locator('body').screenshot({ animations: 'disabled' });
  await popupContext.close();

  const [width, height] = FILM_VIEW;
  const pageContext = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: 2, colorScheme: 'dark' });
  await serveExtension(pageContext, images);
  const pageTab = await pageContext.newPage();
  await pageTab.goto(`${EXT_BASE}/film`);
  await pageTab.evaluate(() => document.fonts.ready);
  const page = await pageTab.screenshot();
  await pageContext.close();
  return { page, popup };
}

const dataUrl = (buf, type) => `data:${type};base64,${buf.toString('base64')}`;
const bree = dataUrl(
  await cached('BreeSerif-Regular.ttf', 'https://github.com/google/fonts/raw/main/ofl/breeserif/BreeSerif-Regular.ttf'),
  'font/ttf',
);
const lato = dataUrl(await cached('Lato-Regular.ttf', 'https://github.com/google/fonts/raw/main/ofl/lato/Lato-Regular.ttf'), 'font/ttf');
const logo = dataUrl(readFileSync(join(root, 'docs', 'assets', 'logo.svg')), 'image/svg+xml');

const STYLE = `
@font-face { font-family: "Bree Serif"; src: url(${bree}); }
@font-face { font-family: Lato; src: url(${lato}); }
* { box-sizing: border-box; margin: 0; }
body { position: relative; overflow: hidden; font-family: Lato, sans-serif; background: #0c0c0b; }
.backdrop, .backdrop * { position: absolute; }
.backdrop { inset: 0; overflow: hidden; }
.wall { inset: 0; background: radial-gradient(55% 60% at 62% 35%, #26231d, #0f0e0c 72%); }
.mark { transform: rotate(-10deg); opacity: .32;
  filter: grayscale(1) brightness(.36) contrast(1.2) drop-shadow(-2px -2px 0 rgba(255,255,255,.16)) drop-shadow(12px 18px 26px rgba(0,0,0,.85)); }
.vignette { inset: 0; box-shadow: inset 0 0 200px rgba(0,0,0,.6); }
.copy { position: absolute; display: flex; flex-direction: column; }
h1 { font-family: "Bree Serif", serif; font-weight: 400; color: #f4f4f4; text-wrap: balance; }
h1 em { font-style: normal; color: #FCC419; }
.sub { color: #9d9481; }

.stage { position: absolute; perspective: 2400px; }
.floor { position: absolute; left: 6%; right: 6%; bottom: -34px; height: 60px; border-radius: 50%; background: rgba(0,0,0,.75); filter: blur(28px); }
.win { position: absolute; inset: 0; border-radius: 12px; background: #1c1c1c; transform-style: preserve-3d; transform: rotateY(-7deg) rotateX(3deg);
  box-shadow: 0 0 0 1px #333, 0 1px 0 1px rgba(255,255,255,.05), 0 2px 4px rgba(0,0,0,.35), 0 16px 32px rgba(0,0,0,.4), 0 48px 96px rgba(0,0,0,.5); }
.bar { display: flex; align-items: center; gap: 16px; padding: 0 16px; border-radius: 12px 12px 0 0;
  background: linear-gradient(#242424, #1c1c1c); border-bottom: 1px solid #0e0e0e; color: #b4b4b4; }
.dots { display: flex; gap: 8px; } .dots i { width: 12px; height: 12px; border-radius: 50%; background: #3d3d3d; }
.nav { display: flex; gap: 14px; color: #7c7c7c; } .nav svg { width: 16px; height: 16px; display: block; }
.url { flex: 1; height: 30px; border-radius: 15px; background: #2b2b2b; font-size: 14px; display: flex; align-items: center; gap: 8px; padding: 0 16px; }
.url svg { width: 12px; height: 12px; color: #7c7c7c; }
.ext { width: 30px; height: 30px; border-radius: 8px; display: grid; place-items: center; background: rgba(252,196,25,.14); box-shadow: inset 0 0 0 1.5px #FCC419; }
.ext img { width: 20px; height: 20px; }
.more { color: #7c7c7c; font-size: 18px; }
.title { gap: 10px; font-size: 15px; color: #d6d6d6; }
.title img { width: 20px; height: 20px; }
.ctl { position: absolute; top: 0; width: 46px; height: 100%; }
.ctl::before, .ctl::after { content: ""; position: absolute; left: 17px; top: 50%; width: 12px; height: 1.5px; background: #bdbdbd; }
.ctl.min { right: 92px; }
.ctl.max { right: 46px; }
.ctl.max::before { margin-top: -6px; height: 12px; background: none; border: 1.5px solid #bdbdbd; }
.ctl.close { right: 0; }
.ctl.close::before { transform: rotate(45deg); }
.ctl.close::after { transform: rotate(-45deg); }
.ctl.min::after, .ctl.max::after { display: none; }
.view { position: relative; transform-style: preserve-3d; }
.view > img { display: block; border-radius: 0 0 12px 12px; }
.popup { position: absolute; top: 4px; right: 14px; border-radius: 10px; overflow: hidden; transform: translateZ(60px);
  box-shadow: 0 0 0 1px #474747, 0 4px 8px rgba(0,0,0,.35), 0 20px 40px rgba(0,0,0,.5), 0 44px 80px rgba(0,0,0,.45); }
.popup img { display: block; }
`;

const backdrop = (w, left, top) =>
  `<div class="backdrop"><div class="wall"></div><img class="mark" src="${logo}" style="width:${w};left:${left};top:${top}"><div class="vignette"></div></div>`;

const NAV = `<div class="nav">
  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M10 3 5 8l5 5"/></svg>
  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="m6 3 5 5-5 5"/></svg>
  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M13 8a5 5 0 1 1-1.5-3.6M13 2.5V5h-2.5"/></svg>
</div>`;
const LOCK = `<svg viewBox="0 0 12 12" fill="currentColor"><rect x="2" y="5" width="8" height="6" rx="1.2"/><path d="M4 5V3.6a2 2 0 0 1 4 0V5" fill="none" stroke="currentColor" stroke-width="1.3"/></svg>`;

/**
 * The window at WINDOW showing `content`: a browser with `url` in its address
 * bar, or the desktop app's own window when `url` is null. `popup` hangs from
 * the extension's button, drawn `popup.w` wide.
 */
function windowed(content, { url, popup }) {
  const { x, y, w, h } = WINDOW;
  const barH = 48;
  const bar =
    url === null
      ? `<div class="bar title" style="position:relative;height:${barH}px"><img src="${logo}"><span>KnightLoader</span><i class="ctl min"></i><i class="ctl max"></i><i class="ctl close"></i></div>`
      : `<div class="bar" style="height:${barH}px"><div class="dots"><i></i><i></i><i></i></div>${NAV}<div class="url">${url.startsWith('http://') ? '' : LOCK}${url.replace(/^https?:\/\//, '')}</div>${
          popup ? `<div class="ext"><img src="${logo}"></div>` : ''
        }<div class="more">&#8942;</div></div>`;
  const floating = popup ? `<div class="popup"><img src="${dataUrl(popup.png, 'image/png')}" width="${popup.w}"></div>` : '';
  return `<div class="stage" style="left:${x}px;top:${y}px;width:${w}px;height:${h + barH}px">
  <div class="floor"></div>
  <div class="win">${bar}<div class="view" style="height:${h}px"><img src="${dataUrl(content, 'image/png')}" width="${w}" height="${h}">${floating}</div></div>
</div>`;
}

// The caption starts at the same height in every picture, as the window does,
// and in mobile/store/render.mjs's Android picture.
function picture(content, { caption, sub }, frame) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>${STYLE}
body { width: ${CANVAS[0]}px; height: ${CANVAS[1]}px; }
.copy { left: 84px; top: 210px; width: 470px; gap: 28px; }
.copy img { width: 92px; }
h1 { font-size: 64px; line-height: 1.12; }
.sub { font-size: 28px; line-height: 1.35; }
</style></head><body>
${backdrop('50%', '-9%', '-6%')}
<div class="copy"><img src="${logo}"><h1>${caption}</h1><p class="sub">${sub}</p></div>
${windowed(content, frame)}
</body></html>`;
}

/**
 * Renders `html` at twice `width` x `height` and writes it to `file`, scaled
 * down to `width` x `height` when `half` is set.
 */
async function render(browser, html, [width, height], file, half = false) {
  const page = await browser.newPage({ viewport: { width, height }, deviceScaleFactor: 2 });
  await page.setContent(html);
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => Promise.all([...document.images].map((img) => img.decode())));
  if (!half) {
    await page.screenshot({ path: file });
    await page.close();
    return file;
  }
  const big = await page.screenshot();
  await page.close();
  const small = await browser.newPage({ viewport: { width, height } });
  await small.setContent(`<body style="margin:0"><img src="${dataUrl(big, 'image/png')}" style="display:block;width:${width}px;height:${height}px">`);
  await small.locator('img').evaluate((img) => img.decode());
  await small.screenshot({ path: file });
  await small.close();
  return file;
}

function onPath(tool) {
  return !spawnSync(tool, ['--version'], { stdio: 'ignore' }).error;
}

function run(tool, args) {
  const r = spawnSync(tool, args, { stdio: 'inherit' });
  if (r.status !== 0) throw new Error(`${tool} ${args.join(' ')} exited with ${r.status}`);
}

// A palette of 256 colours takes these pictures to less than half their size
// and cannot be told apart at this resolution. pngquant picks the better
// palette and dithers the dark gradient behind the windows without banding.
function optimise(files) {
  let quantise = null;
  if (onPath('pngquant')) quantise = (f) => run('pngquant', ['--force', '--ext', '.png', '--strip', '256', f]);
  else if (onPath('magick')) quantise = (f) => run('magick', [f, '-strip', '-colors', '256', `PNG8:${f}`]);
  else console.warn('neither pngquant nor ImageMagick found, the pictures keep their full colour');
  const recompress = onPath('oxipng');
  if (!recompress) console.warn('oxipng not found, the pictures are not recompressed');
  for (const f of files) {
    quantise?.(f);
    if (recompress) run('oxipng', ['--quiet', '--opt', 'max', '--strip', 'safe', f]);
  }
}

const names = [...SCENES, ...WIDE].map((s) => s.name);
const wanted = process.argv.slice(2);
const unknown = wanted.filter((w) => !names.includes(w));
if (unknown.length) {
  console.error(`no such picture: ${unknown.join(', ')}; there are ${names.join(', ')}`);
  process.exit(2);
}
const picked = (list) => (wanted.length ? list.filter((s) => wanted.includes(s.name)) : list);

const vite = await startVite();
const browser = await chromium.launch({
  executablePath: process.env.CHROME_PATH || undefined,
  args: [`--host-resolver-rules=MAP ${HOST} 127.0.0.1`],
});
const missing = new Set();
const files = [];
try {
  const base = `http://${HOST}:${vite.httpServer.address().port}`;
  for (const scene of picked(SCENES)) {
    const png = await webPage(browser, base, scene, PAGE, missing);
    const frame = { url: `http://${HOST}:${PORT}${scene.path}` };
    files.push(await render(browser, picture(png, { caption: scene.caption, sub: SUB }, frame), CANVAS, join(out, `knightloader-${scene.n}-${scene.name}.png`)));
    console.log(relative(root, files.at(-1)));
  }
  for (const shot of picked(WIDE)) {
    const file = join(out, `${shot.name}.png`);
    if (shot.scene) {
      const scene = SCENES.find((s) => s.name === shot.scene);
      const png = await webPage(browser, base, scene, PAGE, missing);
      const url = shot.frame === 'app' ? null : `http://${HOST}:${PORT}${scene.path}`;
      await render(browser, picture(png, shot, { url }), CANVAS, file, true);
    } else {
      const { page, popup } = await extensionShots(browser);
      const frame = { url: FILM.url, popup: { png: popup, w: (420 * WINDOW.w) / FILM_VIEW[0] } };
      await render(browser, picture(page, shot, frame), CANVAS, file, true);
    }
    files.push(file);
    console.log(relative(root, file));
  }
} finally {
  await browser.close();
  await vite.close();
}
if (missing.size) console.warn(`answered 404, no fixture: ${[...missing].sort().join(', ')}`);
optimise(files);
