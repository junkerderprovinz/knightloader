// Hovers neighbouring buttons with rainbow on and checks that each lights up
// in a colour of its own (GlimStone rule 40), which jsdom cannot tell since it
// paints nothing. A pair of surface or bare buttons that share a parent is
// checked, and a link badge beside the button before or after it in the page's
// order, which counts links among the buttons. With rainbow off they all take
// the accent again.
//
//   cd scripts/e2e
//   npm install && npx playwright install chromium
//   node rainbow-hover.mjs
//
// Like column-drag.mjs it drives web/dist and answers the API from the README
// pictures' fixtures, with rainbow switched on in the settings.

import { existsSync, readFileSync } from 'node:fs';
import { dirname, extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..', '..');
const dist = join(root, 'web', 'dist');
const fixtures = join(root, 'scripts', 'screenshots', 'fixtures');

function fixture(url) {
  const path = url.pathname.replace(/^\/api\//, '');
  const narrow = [...url.searchParams.values()][0];
  for (const name of narrow ? [join(path, narrow), path] : [path]) {
    const file = join(fixtures, `${name}.json`);
    if (existsSync(file)) return readFileSync(file, 'utf8');
  }
  return null;
}

const json = (name) => JSON.parse(readFileSync(join(fixtures, `${name}.json`), 'utf8'));

const BASE = 'http://knightloader.invalid';
const MIME = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.woff2': 'font/woff2',
  '.json': 'application/json',
  '.webmanifest': 'application/manifest+json',
};

/**
 * A file of web/dist, or index.html for a route of the app. The page asks for
 * its assets relative to itself, so a page two levels deep asks for them under
 * its own path.
 */
function serveDist(route, path) {
  const at = path.indexOf('/assets/');
  const file = normalize(join(dist, decodeURIComponent(at > 0 ? path.slice(at) : path)));
  if (file.startsWith(dist) && existsSync(file) && extname(file)) {
    return route.fulfill({ contentType: MIME[extname(file)] ?? 'application/octet-stream', body: readFileSync(file) });
  }
  return route.fulfill({ contentType: 'text/html', body: readFileSync(join(dist, 'index.html')) });
}

let failed = 0;
function check(name, ok, detail) {
  if (ok) {
    console.log(`ok    ${name}`);
    return;
  }
  failed++;
  console.error(`FAIL  ${name}${detail ? `\n        ${detail}` : ''}`);
}

const TINTED = '.glim-btn:is(.glim-btn-subtle, .glim-btn-bare):not(.glim-hue, .glim-tint-badge, :disabled)';

/**
 * Marks a pair to hover and says whether one was found: two tinted buttons
 * that share a parent, or with `link` a link badge and its neighbour in the
 * page's order of buttons.
 */
function markPair(page, link) {
  return page.evaluate(
    ([sel, link]) => {
      const shown = (el) => {
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.height > 0 && (link || (r.top >= 0 && r.bottom <= innerHeight));
      };
      const mark = (a, b) => {
        a.setAttribute('data-pair', 'a');
        b.setAttribute('data-pair', 'b');
        return true;
      };
      if (link) {
        const all = [...document.querySelectorAll('.glim-btn')];
        for (const [i, a] of all.entries()) {
          if (a.tagName !== 'A' || !shown(a)) continue;
          const b = [all[i - 1], all[i + 1]].find((c) => c?.tagName === 'BUTTON' && c.matches(sel) && shown(c));
          if (b) return mark(a, b);
        }
        return false;
      }
      for (const a of document.querySelectorAll(sel)) {
        const b = [...a.parentElement.children].find((c) => c !== a && c.matches(sel));
        if (b && shown(a) && shown(b)) return mark(a, b);
      }
      return false;
    },
    [TINTED, link],
  );
}

/** The hovered glyph's colour, or the button's own where it shows words alone. */
async function lit(page, which) {
  const el = page.locator(`[data-pair="${which}"]`);
  await el.hover();
  await page.waitForTimeout(250);
  return el.evaluate((b) => getComputedStyle(b.querySelector('.glim-btn-glyph') ?? b).color);
}

async function scenario(browser, path, rainbow, link = false) {
  const label = `${path}, rainbow ${rainbow ? 'on' : 'off'}`;
  const what = link ? 'a link badge and the button beside it' : 'two neighbouring buttons';
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    locale: 'en-US',
    timezoneId: 'UTC',
    colorScheme: 'dark',
    reducedMotion: 'reduce',
  });
  await context.addInitScript(() => {
    localStorage.setItem('kl-theme', 'dark');
    localStorage.setItem('kl-lang', 'en');
  });
  await context.route(`${BASE}/**`, (route) => serveDist(route, new URL(route.request().url()).pathname));
  await context.route('**/api/**', (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === '/api/settings' && request.method() === 'GET') {
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ...json('settings'), rainbow }) });
    }
    const body = fixture(url);
    if (body !== null) return route.fulfill({ contentType: 'application/json', body });
    return route.fulfill({ status: request.method() === 'GET' ? 404 : 204 });
  });
  await context.routeWebSocket('**/api/ws', (ws) => {
    ws.send(JSON.stringify({ type: 'snapshot', data: json('tasks') }));
    for (const frame of json('ws')) ws.send(JSON.stringify(frame));
  });

  const page = await context.newPage();
  page.on('pageerror', (e) => console.error('page error:', e.message));
  await page.goto(`${BASE}${path}`);
  await page.waitForFunction((on) => (document.documentElement.getAttribute('data-rainbow') !== null) === on, rainbow);
  await page.evaluate(() => document.fonts.ready);

  // The page fills in from the fixtures after it first paints.
  let found = false;
  for (let i = 0; i < 40 && !found; i++) {
    found = await markPair(page, link);
    if (!found) await page.waitForTimeout(250);
  }
  check(`${label}: ${what} are on the page`, found);
  if (!found) return context.close();
  const a = await lit(page, 'a');
  const b = await lit(page, 'b');
  if (rainbow) check(`${label}: ${what} light up in different colours`, a !== b, `both ${a}`);
  else check(`${label}: ${what} take the same accent`, a === b, `${a} and ${b}`);

  await context.close();
}

const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || undefined });
try {
  for (const path of ['/settings/help', '/downloads']) {
    await scenario(browser, path, true);
    await scenario(browser, path, false);
  }
  await scenario(browser, '/settings/access', true, true);
  await scenario(browser, '/settings/access', false, true);
} finally {
  await browser.close();
}
if (failed) {
  console.error(`${failed} failed`);
  process.exit(1);
}
