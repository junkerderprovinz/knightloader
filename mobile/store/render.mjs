// Builds the Play and F-Droid images in mobile/fastlane/metadata/android from
// the captures in mobile/store/captures/<locale>/: five phone screenshots at
// 1080x1920 and the 1024x500 feature graphic. Each capture sits in a drawn
// phone on the dark relief background, under a caption.
//
// The captures are plain screenshots of the app in dark mode, 1080x1920, with
// the status bar in demo mode. Name them after the SHOTS below.
//
// Every page is rendered at twice the size and scaled down in a second page,
// which keeps the text sharp through the phone's tilt.
//
// Deps (global): playwright-core with its Chromium installed.
// Run from mobile/: `node store/render.mjs`
import { execSync } from 'node:child_process';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const { chromium } = require(`${execSync('npm root -g').toString().trim()}/playwright-core`);

const here = dirname(fileURLToPath(import.meta.url));
const mobile = dirname(here);
const metadata = join(mobile, 'fastlane', 'metadata', 'android');

const CAPTIONS = {
  'de-DE': {
    sub: 'KnightLoader für Android',
    tagline: 'Holt alles.<br>Kniet vor nichts.',
    connections: 'Alle deine Burgen <em>auf einen Blick</em>',
    downloads: 'Die Beute rollt <em>live herein</em>',
    add: 'Ein Link genügt. <em>Der Ritter erledigt den Rest.</em>',
    connect: 'Zwölf Wörter. <em>Kein Konto.</em>',
    settings: 'Deine Farben, <em>dein Wappen</em>',
  },
  'en-US': {
    sub: 'KnightLoader for Android',
    tagline: 'Grabs everything.<br>Kneels to nothing.',
    connections: 'Every castle <em>at a glance</em>',
    downloads: 'Watch the loot <em>roll in</em>',
    add: 'Drop a link. <em>The knight does the rest.</em>',
    connect: 'Twelve words. <em>No account.</em>',
    settings: 'Your colors, <em>your coat of arms</em>',
  },
};

// The order on the store page. `lift` is a card of the capture, in capture
// pixels, drawn floating in front of the phone.
const SHOTS = [
  { name: 'connections', lift: { x: 42, y: 305, w: 996, h: 403 } },
  { name: 'downloads' },
  { name: 'add' },
  { name: 'connect' },
  { name: 'settings' },
];

// Height of the status bar in a capture, which the phone redraws with room
// for its rounded corners.
const STATUS_H = 63;

async function cached(file, url) {
  const path = join(tmpdir(), `KnightLoader-${file}`);
  if (!existsSync(path)) {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${file}: fetch ${res.status}`);
    writeFileSync(path, Buffer.from(await res.arrayBuffer()));
  }
  return readFileSync(path);
}

const dataUrl = (buf, type) => `data:${type};base64,${buf.toString('base64')}`;
const bree = dataUrl(await cached('BreeSerif-Regular.ttf', 'https://github.com/google/fonts/raw/main/ofl/breeserif/BreeSerif-Regular.ttf'), 'font/ttf');
const lato = dataUrl(await cached('Lato-Regular.ttf', 'https://github.com/google/fonts/raw/main/ofl/lato/Lato-Regular.ttf'), 'font/ttf');
const logo = dataUrl(readFileSync(join(mobile, '..', 'docs', 'assets', 'logo.svg')), 'image/svg+xml');

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
h1 { font-family: "Bree Serif", serif; font-weight: 400; color: #f4f4f4; text-wrap: balance; }
h1 em { font-style: normal; color: #FCC419; }
.sub { color: #9d9481; }

.stage { position: absolute; perspective: 2400px; }
.floor { position: absolute; left: 12%; right: 12%; bottom: -4%; height: 8%; border-radius: 50%; background: rgba(0,0,0,.8); filter: blur(30px); }
.phone { position: absolute; inset: 0; transform-style: preserve-3d; transform: rotateY(-7deg) rotateX(3deg);
  background: linear-gradient(135deg, #9a9a9a 0%, #4a4a4a 14%, #2c2c2c 45%, #3a3a3a 70%, #6e6e6e 100%);
  box-shadow: -1px 1px 0 #262626, -2px 2px 0 #222, -3px 3px 0 #1e1e1e, -4px 4px 0 #1a1a1a, -5px 5px 0 #161616,
    0 2px 4px rgba(0,0,0,.35), 0 18px 36px rgba(0,0,0,.45), 0 52px 100px rgba(0,0,0,.55); }
.key { position: absolute; right: -4px; width: 5px; border-radius: 0 3px 3px 0; background: linear-gradient(90deg, #2a2a2a, #6a6a6a); }
.glass { position: absolute; background: #050505; box-shadow: inset 0 0 0 1px rgba(255,255,255,.07); }
.screen { position: absolute; overflow: hidden; background: #161616; }
.screen > img { position: absolute; left: 0; top: 0; width: 100%; }
.status { position: absolute; left: 0; right: 0; top: 0; background: #161616; }
.status i { position: absolute; top: 0; height: 100%; background-repeat: no-repeat; }
.cam { position: absolute; left: 50%; border-radius: 50%; background: radial-gradient(circle at 35% 35%, #2d3440, #050505 60%); box-shadow: 0 0 0 2px #0b0b0b; }
.glare { position: absolute; inset: 0; pointer-events: none;
  background: linear-gradient(118deg, rgba(255,255,255,.09) 0%, rgba(255,255,255,.03) 28%, rgba(255,255,255,0) 42%); }
.lift { position: absolute; overflow: hidden; transform: translateZ(70px); background-repeat: no-repeat;
  box-shadow: 0 0 0 1px #474747, 0 4px 8px rgba(0,0,0,.35), 0 22px 44px rgba(0,0,0,.5), 0 50px 90px rgba(0,0,0,.45); }
`;

const backdrop = (w, left, top) =>
  `<div class="backdrop"><div class="wall"></div><img class="mark" src="${logo}" style="width:${w};left:${left};top:${top}"><div class="vignette"></div></div>`;

/** A phone showing `capture` with its screen `sw` wide, its top left corner at (x, y). */
function phone(capture, { x, y, sw, lift }) {
  const k = sw / 1080;
  const sh = Math.round(1920 * k);
  const rim = Math.max(2, Math.round(sw * 0.01));
  const bezel = Math.round(sw * 0.024);
  const r = Math.round(sw * 0.1);
  const w = sw + 2 * (rim + bezel);
  const h = sh + 2 * (rim + bezel);
  const bar = STATUS_H * k;
  // The clock and the icons move inwards by this much, clear of the corners.
  const inset = Math.round(sw * 0.045);
  const bg = `url(${capture})`;
  const size = `${sw}px ${sh}px`;
  const cam = Math.round(sw * 0.028);

  // The lifted card is drawn a tenth larger and reaches past the phone's left edge.
  const z = k * 1.1;
  const floating = lift
    ? `<div class="lift" style="left:${rim + bezel + lift.x * k - lift.w * k * 0.09}px;top:${rim + bezel + lift.y * k}px;width:${lift.w * z}px;height:${lift.h * z}px;border-radius:${40 * z}px;
        background-image:${bg};background-size:${1080 * z}px ${1920 * z}px;background-position:${-lift.x * z}px ${-lift.y * z}px"></div>`
    : '';

  return `<div class="stage" style="left:${x}px;top:${y}px;width:${w}px;height:${h}px">
  <div class="floor"></div>
  <div class="phone" style="border-radius:${r + rim + bezel}px">
    <i class="key" style="top:${h * 0.18}px;height:${h * 0.07}px"></i>
    <i class="key" style="top:${h * 0.28}px;height:${h * 0.12}px"></i>
    <div class="glass" style="inset:${rim}px;border-radius:${r + bezel}px">
      <div class="screen" style="inset:${bezel}px;border-radius:${r}px">
        <img src="${capture}">
        <div class="status" style="height:${bar}px">
          <i style="left:${inset}px;width:${sw * 0.2}px;background-image:${bg};background-size:${size};background-position:0 0"></i>
          <i style="right:${inset}px;width:${sw * 0.2}px;background-image:${bg};background-size:${size};background-position:${-sw * 0.8}px 0"></i>
        </div>
        <div class="cam" style="top:${(bar - cam) / 2}px;width:${cam}px;height:${cam}px;margin-left:${-cam / 2}px"></div>
        <div class="glare"></div>
      </div>
    </div>
    ${floating}
  </div>
</div>`;
}

function screenshot(capture, caption, sub, lift) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>${STYLE}
body { width: 1080px; height: 1920px; }
.copy { position: absolute; left: 88px; right: 88px; top: 110px; display: flex; flex-direction: column; gap: 30px; }
.copy img { width: 92px; }
h1 { font-size: 78px; line-height: 1.12; }
.sub { font-size: 34px; }
</style></head><body>
${backdrop('78%', '-16%', '-5%')}
<div class="copy"><img src="${logo}"><h1>${caption}</h1><p class="sub">${sub}</p></div>
${phone(capture, { x: lift ? 232 : 196, y: 612, sw: 640, lift })}
</body></html>`;
}

function featureGraphic(back, front, tagline) {
  return `<!doctype html><html><head><meta charset="utf-8"><style>${STYLE}
body { width: 1024px; height: 500px; }
.copy { position: absolute; left: 70px; top: 0; bottom: 0; width: 470px; display: flex; flex-direction: column; justify-content: center; gap: 18px; }
.copy img { width: 84px; }
.name { font: 400 62px/1 "Bree Serif", serif; color: #f4f4f4; }
.sub { font-size: 25px; line-height: 1.35; }
</style></head><body>
${backdrop('46%', '-9%', '-4%')}
<div class="copy"><img src="${logo}"><div class="name">KnightLoader</div><p class="sub">${tagline}</p></div>
${phone(back, { x: 590, y: 76, sw: 196 })}
${phone(front, { x: 758, y: 40, sw: 220 })}
</body></html>`;
}

/** Renders `html` at twice `width` x `height` and writes it scaled down to `file`. */
async function render(browser, html, width, height, file) {
  const page = await browser.newPage({ viewport: { width, height }, deviceScaleFactor: 2 });
  await page.setContent(html);
  await page.evaluate(() => document.fonts.ready);
  const big = await page.screenshot();
  await page.close();

  const small = await browser.newPage({ viewport: { width, height } });
  await small.setContent(`<body style="margin:0"><img src="${dataUrl(big, 'image/png')}" style="display:block;width:${width}px;height:${height}px">`);
  await small.locator('img').evaluate((img) => img.decode());
  await small.screenshot({ path: file });
  await small.close();
}

const browser = await chromium.launch();
try {
  for (const [locale, text] of Object.entries(CAPTIONS)) {
    const capture = (name) => dataUrl(readFileSync(join(here, 'captures', locale, `${name}.png`)), 'image/png');
    const images = join(metadata, locale, 'images');
    for (const [i, { name, lift }] of SHOTS.entries()) {
      await render(browser, screenshot(capture(name), text[name], text.sub, lift), 1080, 1920, join(images, 'phoneScreenshots', `${i + 1}.png`));
      console.log(`wrote ${locale}/images/phoneScreenshots/${i + 1}.png`);
    }
    await render(browser, featureGraphic(capture('downloads'), capture('connections'), text.tagline), 1024, 500, join(images, 'featureGraphic.png'));
    console.log(`wrote ${locale}/images/featureGraphic.png`);
  }
} finally {
  await browser.close();
}
