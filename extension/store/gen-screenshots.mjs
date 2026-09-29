/**
 * Generates the store screenshots in screenshots/, 1280x800 each: the popup or
 * the options page, rendered from src/ with made-up instances, inside a drawn
 * browser window next to a caption. The page behind the popup is the film page
 * from demo-page.mjs. The two promo tiles reuse the first screenshot's window.
 *
 * The pages run in a plain tab rather than as an installed extension, so the
 * chrome.* calls they make are answered by a stub and group.js's withGroup is
 * swapped for one that returns two instances without touching the relay.
 * Everything else, from the cards to the translations, is the shipped code, so
 * a run after a release shows the current extension.
 *
 * Every layer is rendered at twice the size and drawn at half, which keeps the
 * text sharp through the window's perspective.
 *
 * As in gen-store-assets.mjs, no browser is named: Edge's policy 1.1.2 rejects a
 * listing that references another one, and the window has no brand marks.
 *
 * Deps (global): playwright-core with its Chromium installed.
 * Run: node extension/store/gen-screenshots.mjs
 */
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { join, dirname, extname, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { execSync } from "node:child_process";
import { demoPage, FILMS } from "./demo-page.mjs";

const require = createRequire(import.meta.url);
const { chromium } = require(`${execSync("npm root -g").toString().trim()}/playwright-core`);

const __dir = dirname(fileURLToPath(import.meta.url));
const EXT = join(__dir, "..");
const OUT = join(__dir, "screenshots");

// The content area of the drawn window. The popup keeps its real 420px width.
const VIEW_W = 860, VIEW_H = 566, BAR_H = 44;
const SCALE = 2;

const manifest = JSON.parse(readFileSync(join(EXT, "src", "manifest.json"), "utf8"));

const SIBLINGS = [
  { instanceId: "a1", name: "Home NAS", deployment: "container" },
  { instanceId: "b2", name: "Workstation", deployment: "desktop" },
];
const GiB = 1024 ** 3, MiB = 1024 ** 2;
const API = {
  a1: {
    "/api/queue": { halted: false, running: 2 },
    "/api/queue/counters": { files: 3, running: 2, remaining: 2.4 * GiB, speed: 38 * MiB },
    "/api/remote-access": { addresses: [{ url: "https://nas.example" }] },
  },
  b2: {
    "/api/queue": { halted: true, running: 0 },
    "/api/queue/counters": { files: 1, running: 0 },
    "/api/remote-access": { addresses: [{ url: "https://pc.example" }] },
  },
};

// BIP39's all-zero test vector: a valid phrase that opens no real group. The
// options page shows it masked.
const PHRASE = "abandon ".repeat(11) + "about";

const TAB = { id: 1, title: "Big Buck Bunny · Open Movies", url: "https://openmovies.example/films/big-buck-bunny" };

const SHOTS = [
  {
    file: "1-send-page.png",
    caption: "Send the page you are on to <em>any of your own instances</em>",
  },
  {
    file: "2-send-link.png",
    caption: "Right-click a link, image or selection and <em>choose where it goes</em>",
    pending: { origin: "", payload: { kind: "link", url: FILMS.trailer[3], title: "Big Buck Bunny trailer" } },
    hover: "trailer",
  },
  {
    file: "3-clicknload.png",
    caption: "Click'n'Load buttons are caught and sent to <em>your own server</em>",
    pending: { origin: "cnl", payload: { title: TAB.title, url: TAB.url } },
    hover: "cnl",
  },
  {
    file: "4-connect.png",
    caption: "Connect with the <em>twelve-word phrase</em> your instances share",
    options: true,
  },
];

async function cached(file, url) {
  const path = join(tmpdir(), file);
  if (!existsSync(path)) {
    const res = await fetch(url, { headers: { "user-agent": "knightloader-store-assets" } });
    if (!res.ok) throw new Error(`${file}: fetch ${res.status}`);
    writeFileSync(path, Buffer.from(await res.arrayBuffer()));
  }
  return readFileSync(path);
}
const IMAGES = {
  "/poster.jpg": await cached("KnightLoader-bbb-poster.jpg", "https://upload.wikimedia.org/wikipedia/commons/thumb/c/c5/Big_buck_bunny_poster_big.jpg/960px-Big_buck_bunny_poster_big.jpg"),
  "/still.jpg": await cached("KnightLoader-bbb-forest.jpg", "https://upload.wikimedia.org/wikipedia/commons/6/69/Big_Buck_Bunny_-_forest.jpg"),
};
const bree = (await cached("KnightLoader-BreeSerif-Regular.ttf", "https://github.com/google/fonts/raw/main/ofl/breeserif/BreeSerif-Regular.ttf")).toString("base64");
const lato = (await cached("KnightLoader-Lato-Regular.ttf", "https://github.com/google/fonts/raw/main/ofl/lato/Lato-Regular.ttf")).toString("base64");
const logo = readFileSync(join(EXT, "src", "logo.svg")).toString("base64");

/** Runs in the page before its own scripts. */
function stubChrome({ local, session, manifest, tab }) {
  const area = (mem) => ({
    async get(keys) {
      if (keys == null) return { ...mem };
      const list = typeof keys === "string" ? [keys] : Array.isArray(keys) ? keys : Object.keys(keys);
      const out = typeof keys === "object" && !Array.isArray(keys) ? { ...keys } : {};
      for (const k of list) if (k in mem) out[k] = mem[k];
      return out;
    },
    async set(items) { Object.assign(mem, items); },
    async remove(keys) { for (const k of [].concat(keys)) delete mem[k]; },
    onChanged: { addListener() {}, removeListener() {} },
  });
  const event = { addListener() {}, removeListener() {}, hasListener: () => false };
  const noop = () => {};
  window.chrome = {
    storage: { local: area(local), session: area(session), onChanged: event },
    runtime: { id: "knightloader", getManifest: () => manifest, getURL: (p) => p, sendMessage: async () => ({}), openOptionsPage: noop, onMessage: event },
    tabs: { query: async () => [tab], create: noop },
    permissions: { contains: async () => true, request: async () => true, remove: async () => true, onAdded: event, onRemoved: event },
    action: { setBadgeText: noop, setTitle: noop },
    scripting: { getRegisteredContentScripts: async () => [] },
    declarativeNetRequest: { getEnabledRulesets: async () => [] },
  };
  // The popup closes itself after a send, and the countdown would send.
  window.close = noop;
}

const fakeGroup = `
withGroup = async (work) => work({
  siblings: ${JSON.stringify(SIBLINGS)},
  call: async (id, method, path) => ({ status: 200, body: JSON.stringify((${JSON.stringify(API)})[id][path]) }),
});`;

// The pages are served from disk by request interception under a made-up
// origin, so no port is opened.
const BASE = "https://store.knightloader.invalid";
const MIME = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".woff2": "font/woff2" };

async function serve(ctx, pages = {}) {
  await ctx.route(`${BASE}/**`, (route) => {
    const url = new URL(route.request().url());
    if (url.pathname in pages) return route.fulfill({ contentType: "text/html", body: pages[url.pathname] });
    if (url.pathname in IMAGES) return route.fulfill({ contentType: "image/jpeg", body: IMAGES[url.pathname] });
    const path = normalize(join(EXT, decodeURIComponent(url.pathname)));
    if (!path.startsWith(EXT) || !existsSync(path)) return route.fulfill({ status: 404 });
    let body = readFileSync(path);
    if (path === join(EXT, "src", "group.js")) body = body.toString() + fakeGroup;
    return route.fulfill({ contentType: MIME[extname(path)] ?? "application/octet-stream", body });
  });
}

async function extensionPage(browser, page, { width, height, pending }) {
  const ctx = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: SCALE, colorScheme: "dark" });
  await ctx.addInitScript(stubChrome, {
    local: { phrase: PHRASE, defaultInstance: "a1", language: "en" },
    session: pending ? { pendingSend: { ...pending, siblings: SIBLINGS, defaultName: "a1" } } : {},
    manifest,
    tab: TAB,
  });
  await serve(ctx);
  const tab = await ctx.newPage();
  await tab.goto(`${BASE}/src/${page}`);
  await tab.evaluate(() => document.fonts.ready);
  return { ctx, tab };
}

async function popupShot(browser, pending) {
  const { ctx, tab } = await extensionPage(browser, "popup.html", { width: 420, height: 800, pending });
  await tab.locator(".glim-instance .glim-status").first().waitFor();
  // Past the cards' fade-in, before a Click'n'Load countdown runs out.
  await tab.waitForTimeout(900);
  const png = await tab.locator("body").screenshot({ animations: "disabled" });
  await ctx.close();
  return png;
}

async function optionsShot(browser) {
  const { ctx, tab } = await extensionPage(browser, "options.html", { width: VIEW_W, height: VIEW_H });
  await tab.waitForTimeout(900);
  const png = await tab.screenshot({ animations: "disabled" });
  await ctx.close();
  return png;
}

/** The film page, and where the pointer goes when an element is shown hovered. */
async function pageShot(browser, hover) {
  const ctx = await browser.newContext({ viewport: { width: VIEW_W, height: VIEW_H }, deviceScaleFactor: SCALE, colorScheme: "dark" });
  await serve(ctx, { "/film": demoPage({ poster: "/poster.jpg", still: "/still.jpg", hover }) });
  const tab = await ctx.newPage();
  await tab.goto(`${BASE}/film`);
  await tab.evaluate(() => document.fonts.ready);
  const box = hover ? await tab.locator(`#${hover}[data-point], #${hover} [data-point]`).boundingBox() : null;
  const png = await tab.screenshot();
  await ctx.close();
  return { png, pointer: box && { x: box.x + box.width - 14, y: box.y + box.height - 10 } };
}

const b64 = (png) => `data:image/png;base64,${png.toString("base64")}`;

const POINTER = `<svg class="pointer" viewBox="0 0 24 24" width="24" height="24"><path d="M5 2.5v16.2l4.3-4.1 2.8 6.4 2.7-1.2-2.8-6.3H18z" fill="#fff" stroke="#111" stroke-width="1.3" stroke-linejoin="round"/></svg>`;

// The logo, pressed into the dark backdrop behind the caption, gives the
// picture depth without competing with the window.
const BACKDROP = `<div class="backdrop"><div class="wall"></div><img class="mark" src="data:image/svg+xml;base64,${logo}"><div class="vignette"></div></div>`;

/** A canvas with `copy` on it and the window at (x, y), drawn at `scale`. */
function frame({ width = 1280, height = 800, copy, x = 384, y = (800 - VIEW_H - BAR_H) / 2, scale = 1, url, page, popup, pointer }) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>
@font-face { font-family: "Bree Serif"; src: url(data:font/ttf;base64,${bree}); }
@font-face { font-family: Lato; src: url(data:font/ttf;base64,${lato}); }
* { box-sizing: border-box; margin: 0; }
body { width: ${width}px; height: ${height}px; overflow: hidden; position: relative; font-family: Lato, sans-serif;
  background: #0c0c0b; }
.backdrop, .backdrop * { position: absolute; }
.backdrop { inset: 0; overflow: hidden; }
.wall { inset: 0; background: radial-gradient(55% 60% at 62% 35%, #26231d, #0f0e0c 72%); }
.mark { left: -9%; top: -4%; width: 46%; transform: rotate(-10deg); opacity: .45;
  filter: grayscale(1) brightness(.42) contrast(1.2) drop-shadow(-2px -2px 0 rgba(255,255,255,.22)) drop-shadow(12px 18px 26px rgba(0,0,0,.85)); }
.vignette { inset: 0; box-shadow: inset 0 0 200px rgba(0,0,0,.6); }
.copy { position: absolute; left: 64px; top: 0; bottom: 0; width: 290px; display: flex; flex-direction: column; justify-content: center; gap: 22px; }
.copy img { width: 58px; }
h1 { font: 400 38px/1.14 "Bree Serif", serif; color: #f4f4f4; }
h1 em { font-style: normal; color: #FCC419; }
.copy p { font-size: 18px; color: #9d9481; }
.name { font: 400 80px/1 "Bree Serif", serif; color: #f4f4f4; }
.stage { position: absolute; left: ${x}px; top: ${y}px; width: ${VIEW_W}px; height: ${VIEW_H + BAR_H}px; perspective: 2400px;
  transform: scale(${scale}); transform-origin: 0 0; }
.floor { position: absolute; left: 6%; right: 6%; bottom: -34px; height: 60px; border-radius: 50%; background: rgba(0,0,0,.75); filter: blur(28px); }
.win { position: absolute; inset: 0; border-radius: 12px; background: #1c1c1c; transform-style: preserve-3d;
  transform: rotateY(-7deg) rotateX(3deg);
  box-shadow: 0 0 0 1px #333, 0 1px 0 1px rgba(255,255,255,.05), 0 2px 4px rgba(0,0,0,.35), 0 16px 32px rgba(0,0,0,.4), 0 48px 96px rgba(0,0,0,.5); }
.bar { height: ${BAR_H}px; display: flex; align-items: center; gap: 14px; padding: 0 14px; border-radius: 12px 12px 0 0;
  background: linear-gradient(#242424, #1c1c1c); border-bottom: 1px solid #0e0e0e; }
.dots { display: flex; gap: 7px; } .dots i { width: 11px; height: 11px; border-radius: 50%; background: #3d3d3d; }
.nav { display: flex; gap: 12px; color: #7c7c7c; } .nav svg { width: 15px; height: 15px; display: block; }
.url { flex: 1; height: 28px; border-radius: 14px; background: #2b2b2b; color: #b4b4b4; font-size: 13px; display: flex; align-items: center; gap: 8px; padding: 0 14px; }
.url svg { width: 12px; height: 12px; color: #7c7c7c; }
.ext { width: 30px; height: 30px; border-radius: 8px; display: grid; place-items: center; background: rgba(252,196,25,.14); box-shadow: inset 0 0 0 1.5px #FCC419; }
.ext img { width: 20px; height: 20px; }
.more { color: #7c7c7c; font-size: 18px; }
.view { position: relative; height: ${VIEW_H}px; transform-style: preserve-3d; }
.view > img { display: block; border-radius: 0 0 12px 12px; }
.popup { position: absolute; top: 4px; right: 14px; border-radius: 10px; overflow: hidden; transform: translateZ(60px);
  box-shadow: 0 0 0 1px #474747, 0 4px 8px rgba(0,0,0,.35), 0 20px 40px rgba(0,0,0,.5), 0 44px 80px rgba(0,0,0,.45); }
.popup img { display: block; }
.pointer { position: absolute; filter: drop-shadow(0 2px 3px rgba(0,0,0,.5)); }
</style></head><body>
${BACKDROP}
${copy}
<div class="stage">
  <div class="floor"></div>
  <div class="win">
    <div class="bar">
      <div class="dots"><i></i><i></i><i></i></div>
      <div class="nav">
        <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M10 3 5 8l5 5"/></svg>
        <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="m6 3 5 5-5 5"/></svg>
        <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M13 8a5 5 0 1 1-1.5-3.6M13 2.5V5h-2.5"/></svg>
      </div>
      <div class="url"><svg viewBox="0 0 12 12" fill="currentColor"><rect x="2" y="5" width="8" height="6" rx="1.2"/><path d="M4 5V3.6a2 2 0 0 1 4 0V5" fill="none" stroke="currentColor" stroke-width="1.3"/></svg>${url}</div>
      <div class="ext"><img src="data:image/svg+xml;base64,${logo}"></div>
      <div class="more">&#8942;</div>
    </div>
    <div class="view">
      <img src="${b64(page)}" width="${VIEW_W}" height="${VIEW_H}">
      ${pointer ? POINTER.replace('class="pointer"', `class="pointer" style="left:${pointer.x - 5}px;top:${pointer.y - 2}px"`) : ""}
      ${popup ? `<div class="popup"><img src="${b64(popup)}" width="420"></div>` : ""}
    </div>
  </div>
</div>
</body></html>`;
}

const LOGO = `<img src="data:image/svg+xml;base64,${logo}">`;
const caption = (text) => `<div class="copy">${LOGO}<h1>${text}</h1><p>KnightLoader for your browser</p></div>`;

const TILES = [
  {
    file: "promo-marquee-1400x560.png", width: 1400, height: 560, x: 690, y: 92, scale: 0.78,
    copy: `<div class="copy" style="left:96px;width:560px;gap:24px">${LOGO.replace("<img", '<img style="width:112px"')}
      <div class="name">KnightLoader</div><p style="font-size:27px;line-height:1.35">Send links to your own<br>download manager</p></div>`,
  },
  {
    file: "promo-small-440x280.png", width: 440, height: 280, x: 92, y: 88, scale: 0.4,
    copy: `<div style="position:absolute;left:26px;top:24px;display:flex;align-items:center;gap:12px">${LOGO.replace("<img", '<img style="width:40px"')}
      <div class="name" style="font-size:30px">KnightLoader</div></div>`,
  },
];

async function render(browser, file, { width = 1280, height = 800, ...opts }) {
  const canvas = await browser.newPage({ viewport: { width, height } });
  await canvas.setContent(frame({ width, height, ...opts }));
  await canvas.evaluate(() => document.fonts.ready);
  await canvas.screenshot({ path: file });
  await canvas.close();
}

const browser = await chromium.launch();
mkdirSync(OUT, { recursive: true });
try {
  const url = TAB.url.replace("https://", "");
  let first;
  for (const shot of SHOTS) {
    let opts;
    if (shot.options) {
      opts = { copy: caption(shot.caption), url: "KnightLoader settings", page: await optionsShot(browser) };
    } else {
      const { png, pointer } = await pageShot(browser, shot.hover);
      opts = { copy: caption(shot.caption), url, page: png, pointer, popup: await popupShot(browser, shot.pending) };
      first ??= opts;
    }
    await render(browser, join(OUT, shot.file), opts);
    console.log(`wrote screenshots/${shot.file}`);
  }
  for (const { file, copy, ...place } of TILES) {
    await render(browser, join(__dir, file), { ...first, ...place, copy });
    console.log(`wrote ${file}`);
  }
} finally {
  await browser.close();
}
