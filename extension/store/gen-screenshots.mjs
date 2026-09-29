/**
 * Generates the store screenshots in screenshots/, 1280x800 each: the popup or
 * the options page, rendered from src/ with made-up instances, inside a drawn
 * browser window next to a caption.
 *
 * The pages run in a plain tab rather than as an installed extension, so the
 * chrome.* calls they make are answered by a stub and group.js's withGroup is
 * swapped for one that returns two instances without touching the relay.
 * Everything else, from the cards to the translations, is the shipped code, so
 * a run after a release shows the current extension.
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

const require = createRequire(import.meta.url);
const { chromium } = require(`${execSync("npm root -g").toString().trim()}/playwright-core`);

const __dir = dirname(fileURLToPath(import.meta.url));
const EXT = join(__dir, "..");
const OUT = join(__dir, "screenshots");

// The content area of the drawn window. The popup keeps its real 420px width.
const VIEW_W = 820, VIEW_H = 530;

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

const TAB = { id: 1, title: "Open Movies", url: "https://openmovies.example/films" };
const TRAILER = "https://download.blender.org/durian/trailer/sintel_trailer-480p.mp4";

const SHOTS = [
  {
    file: "1-send-page.png",
    caption: "Send the page you are on to <em>any of your own instances</em>",
  },
  {
    file: "2-send-link.png",
    caption: "Right-click a link, image or selection and <em>choose where it goes</em>",
    pending: { origin: "", payload: { kind: "link", url: TRAILER, title: "Sintel (trailer)" } },
  },
  {
    file: "3-clicknload.png",
    caption: "Click'n'Load buttons are caught and sent to <em>your own server</em>",
    pending: { origin: "cnl", payload: { title: "Open Movies", url: TAB.url } },
  },
  {
    file: "4-connect.png",
    caption: "Connect with the <em>twelve-word phrase</em> your instances share",
    options: true,
  },
];

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
const MIME = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".woff2": "font/woff2" };

async function serveExtension(ctx) {
  await ctx.route(`${BASE}/**`, (route) => {
    const path = normalize(join(EXT, decodeURIComponent(new URL(route.request().url()).pathname)));
    if (!path.startsWith(EXT) || !existsSync(path)) return route.fulfill({ status: 404 });
    let body = readFileSync(path);
    if (path === join(EXT, "src", "group.js")) body = body.toString() + fakeGroup;
    return route.fulfill({ contentType: MIME[extname(path)] ?? "application/octet-stream", body });
  });
}

async function extensionPage(browser, page, { width, height, pending }) {
  const ctx = await browser.newContext({ viewport: { width, height }, colorScheme: "dark" });
  await ctx.addInitScript(stubChrome, {
    local: { phrase: PHRASE, defaultInstance: "a1", language: "en" },
    session: pending ? { pendingSend: { ...pending, siblings: SIBLINGS, defaultName: "a1" } } : {},
    manifest,
    tab: TAB,
  });
  await serveExtension(ctx);
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

/** The test page, restyled so the popup has room and the colours match it. */
async function pageShot(browser) {
  const ctx = await browser.newContext({ viewport: { width: VIEW_W, height: VIEW_H }, colorScheme: "dark" });
  await ctx.route("http://127.0.0.1:9666/**", (route) => route.abort());
  await serveExtension(ctx);
  const tab = await ctx.newPage();
  await tab.goto(`${BASE}/store/test-page/index.html`);
  await tab.addStyleTag({
    content: `
      body { background: #262626; color: #ececec; }
      main { margin: 40px 0 0 36px; max-width: 300px; padding: 0; }
      h1 { font-size: 27px; }
      h2, p.lead, li span { color: #a3a3a3; }
      ul { border-color: #3a3a3a; } li { border-color: #3a3a3a; grid-template-columns: 1fr 64px; }
      li span:first-of-type { display: none; }`,
  });
  const png = await tab.screenshot();
  await ctx.close();
  return png;
}

async function fontFile(file, url) {
  const path = join(tmpdir(), file);
  if (!existsSync(path)) {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${file}: font fetch ${res.status}`);
    writeFileSync(path, Buffer.from(await res.arrayBuffer()));
  }
  return readFileSync(path).toString("base64");
}
const bree = await fontFile("KnightLoader-BreeSerif-Regular.ttf", "https://github.com/google/fonts/raw/main/ofl/breeserif/BreeSerif-Regular.ttf");
const lato = await fontFile("KnightLoader-Lato-Regular.ttf", "https://github.com/google/fonts/raw/main/ofl/lato/Lato-Regular.ttf");
const logo = readFileSync(join(EXT, "src", "logo.svg")).toString("base64");

const b64 = (png) => `data:image/png;base64,${png.toString("base64")}`;

function frame({ caption, url, page, popup }) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>
@font-face { font-family: "Bree Serif"; src: url(data:font/ttf;base64,${bree}); }
@font-face { font-family: Lato; src: url(data:font/ttf;base64,${lato}); }
* { box-sizing: border-box; margin: 0; }
body { width: 1280px; height: 800px; overflow: hidden; position: relative; font-family: Lato, sans-serif;
  background: radial-gradient(900px 700px at 78% 45%, #1f1b12, #0f0f0f 70%); }
.copy { position: absolute; left: 72px; top: 0; bottom: 0; width: 310px; display: flex; flex-direction: column; justify-content: center; gap: 22px; }
.copy img { width: 60px; }
h1 { font: 400 40px/1.13 "Bree Serif", serif; color: #f4f4f4; }
h1 em { font-style: normal; color: #FCC419; }
.copy p { font-size: 18px; color: #9d9481; }
.win { position: absolute; left: 418px; top: ${(800 - VIEW_H - 44) / 2}px; width: ${VIEW_W}px; border-radius: 12px; overflow: hidden;
  background: #262626; box-shadow: 0 0 0 1px #2e2e2e, 0 40px 80px rgba(0,0,0,.55), 0 12px 24px rgba(0,0,0,.35); }
.bar { height: 44px; display: flex; align-items: center; gap: 14px; padding: 0 14px; background: #1c1c1c; border-bottom: 1px solid #0e0e0e; }
.dots { display: flex; gap: 7px; } .dots i { width: 11px; height: 11px; border-radius: 50%; background: #3d3d3d; }
.nav { display: flex; gap: 12px; color: #7c7c7c; } .nav svg { width: 15px; height: 15px; display: block; }
.url { flex: 1; height: 28px; border-radius: 14px; background: #2b2b2b; color: #b4b4b4; font-size: 13px; display: flex; align-items: center; gap: 8px; padding: 0 14px; }
.url svg { width: 12px; height: 12px; color: #7c7c7c; }
.ext { width: 30px; height: 30px; border-radius: 8px; display: grid; place-items: center; background: rgba(252,196,25,.14); box-shadow: inset 0 0 0 1.5px #FCC419; }
.ext img { width: 20px; height: 20px; }
.more { color: #7c7c7c; font-size: 18px; }
.view { position: relative; height: ${VIEW_H}px; }
.view > img { display: block; }
.popup { position: absolute; top: 4px; right: 14px; border-radius: 10px; overflow: hidden;
  box-shadow: 0 0 0 1px #404040, 0 22px 48px rgba(0,0,0,.6), 0 6px 14px rgba(0,0,0,.4); }
.popup img { display: block; }
</style></head><body>
<div class="copy">
  <img src="data:image/svg+xml;base64,${logo}">
  <h1>${caption}</h1>
  <p>KnightLoader for your browser</p>
</div>
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
    ${popup ? `<div class="popup"><img src="${b64(popup)}" width="420"></div>` : ""}
  </div>
</div>
</body></html>`;
}

const browser = await chromium.launch();
mkdirSync(OUT, { recursive: true });
try {
  const page = await pageShot(browser);
  const canvas = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  for (const shot of SHOTS) {
    const html = shot.options
      ? frame({ caption: shot.caption, url: "KnightLoader settings", page: await optionsShot(browser) })
      : frame({ caption: shot.caption, url: TAB.url.replace("https://", ""), page, popup: await popupShot(browser, shot.pending) });
    await canvas.setContent(html);
    await canvas.evaluate(() => document.fonts.ready);
    await canvas.screenshot({ path: join(OUT, shot.file) });
    console.log(`wrote screenshots/${shot.file}`);
  }
} finally {
  await browser.close();
}
