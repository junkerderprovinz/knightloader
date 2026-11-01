// Drags a column head of the download list across two columns in a real
// browser and checks what jsdom cannot: the columns move while the button is
// still down, head and cells together, the carried one stays under the
// pointer, the passed ones slide unless the system asks for less motion, and
// the order is written once the button comes up and not before.
//
//   cd scripts/e2e
//   npm install && npx playwright install chromium
//   node column-drag.mjs
//
// It drives the built web UI in web/dist, so build that first, and answers
// every API call from the README pictures' fixtures (scripts/screenshots/
// fixtures). Nothing listens on a port: the page and the API are served by
// request interception under a made-up origin. CHROME_PATH runs a Chromium of
// your own.

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

/** A file of web/dist, or index.html for a route of the app, as the server answers. */
function serveDist(route, path) {
  const file = normalize(join(dist, decodeURIComponent(path)));
  if (file.startsWith(dist) && existsSync(file) && extname(file)) {
    return route.fulfill({ contentType: MIME[extname(file)] ?? 'application/octet-stream', body: readFileSync(file) });
  }
  return route.fulfill({ contentType: 'text/html', body: readFileSync(join(dist, 'index.html')) });
}

let failed = 0;
function check(name, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a === b) {
    console.log(`ok    ${name}`);
    return;
  }
  failed++;
  console.error(`FAIL  ${name}\n        got:  ${a}\n        want: ${b}`);
}

const TABLE = '[data-new="list-heads"]';

/** The order of the heads and of the first row's cells, and each head's box. */
function read(page) {
  return page.evaluate((sel) => {
    const head = document.querySelector(sel);
    const table = head.parentElement;
    const ids = (row) => [...row.querySelectorAll(':scope > [data-cell]')].map((c) => c.dataset.cell);
    const row = table.querySelector('[data-row-kind]');
    const boxes = Object.fromEntries(
      [...head.querySelectorAll(':scope > [data-cell]')].map((c) => {
        const r = c.getBoundingClientRect();
        return [c.dataset.cell, { x: r.x, w: r.width, y: r.y, h: r.height }];
      }),
    );
    return { heads: ids(head), cells: ids(row), boxes };
  }, TABLE);
}

/** The translate a column's head and its first row cell are painted with. */
function slid(page, id) {
  return page.evaluate(
    ([sel, id]) => {
      const table = document.querySelector(sel).parentElement;
      return [...table.querySelectorAll(`[data-cell="${id}"]`)].slice(0, 2).map((c) => parseFloat(getComputedStyle(c).translate) || 0);
    },
    [TABLE, id],
  );
}

async function scenario(browser, reducedMotion) {
  const label = reducedMotion === 'reduce' ? 'reduced motion' : 'full motion';
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    locale: 'en-US',
    timezoneId: 'UTC',
    colorScheme: 'dark',
    reducedMotion,
  });
  await context.addInitScript(() => {
    localStorage.setItem('kl-theme', 'dark');
    localStorage.setItem('kl-lang', 'en');
    localStorage.setItem('kl-motion', 'wild');
  });
  const writes = [];
  // The later route is asked first.
  await context.route(`${BASE}/**`, (route) => serveDist(route, new URL(route.request().url()).pathname));
  await context.route('**/api/**', (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === '/api/uistate' && request.method() === 'PUT') {
      const body = request.postData() ?? '';
      if (body.includes('list.columns.downloads')) writes.push(JSON.parse(body));
      return route.fulfill({ status: 204 });
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
  await page.goto(`${BASE}/downloads`);
  await page.locator('[data-row-kind]').first().waitFor();
  await page.evaluate(() => document.fonts.ready);

  const start = await read(page);
  check(`${label}: the first row's cells follow the heads at rest`, start.cells, start.heads);
  // The last column, carried two places towards the start.
  const k = start.heads.length - 1;
  const held = start.heads[k];
  const passed = start.heads[k - 1];
  const target = start.heads[k - 2];
  const want = [...start.heads.slice(0, k - 2), held, target, passed];
  const y = start.boxes[held].y + start.boxes[held].h / 2;
  const centre = (id) => start.boxes[id].x + start.boxes[id].w / 2;

  await page.mouse.move(centre(held), y);
  await page.mouse.down();
  await page.mouse.move(centre(held) - 10, y, { steps: 3 });
  await page.mouse.move(centre(passed) + 4, y, { steps: 8 });
  check(`${label}: nothing moves before the carried column passes a middle`, (await read(page)).heads, start.heads);

  // One step over the passed column's middle: it changes places at once and,
  // with motion on, slides there rather than jumping.
  await page.mouse.move(centre(passed) - 4, y);
  const sliding = await slid(page, passed);
  if (reducedMotion === 'reduce') check(`${label}: the passed column jumps`, sliding, [0, 0]);
  else check(`${label}: the passed column slides, head and cell alike`, sliding[0] !== 0 && sliding[0] === sliding[1], true);

  await page.mouse.move(centre(target) - 4, y, { steps: 8 });
  await page.waitForTimeout(400);
  const mid = await read(page);
  check(`${label}: mid-drag, the heads show the live order`, mid.heads, want);
  check(`${label}: mid-drag, the cells move with their heads`, mid.cells, want);
  const under = mid.boxes[held].x + mid.boxes[held].w / 2;
  check(`${label}: the carried head stays under the pointer`, Math.abs(under - (centre(target) - 4)) < 2, true);
  check(`${label}: nothing is written while the button is down`, writes.length, 0);

  await page.mouse.up();
  await page.waitForFunction(() => !document.querySelector('[data-col-drag]'));
  const after = await read(page);
  check(`${label}: after the drop, the heads keep the order`, after.heads, want);
  check(`${label}: after the drop, the cells keep the order`, after.cells, want);
  // The store writes on a 600ms debounce.
  await page.waitForTimeout(1200);
  check(`${label}: the drop is written once`, writes.length, 1);
  const saved = writes.at(-1)?.['list.columns.downloads']?.order ?? [];
  check(`${label}: the written order is the one shown`, saved.filter((id) => want.includes(id)), want);

  // Escape halfway through a second drag puts everything back.
  const now = await read(page);
  const c = (id) => now.boxes[id].x + now.boxes[id].w / 2;
  const first = now.heads[now.heads.length - 1];
  await page.mouse.move(c(first), y);
  await page.mouse.down();
  await page.mouse.move(c(first) - 10, y, { steps: 3 });
  await page.mouse.move(c(now.heads[now.heads.length - 3]) - 4, y, { steps: 10 });
  check(`${label}: a second drag moves the columns again`, (await read(page)).heads.join() !== now.heads.join(), true);
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await page.waitForFunction(() => !document.querySelector('[data-col-drag]'));
  const back = await read(page);
  check(`${label}: Escape puts the heads back`, back.heads, now.heads);
  check(`${label}: Escape puts the cells back`, back.cells, now.heads);
  await page.waitForTimeout(1200);
  check(`${label}: Escape writes nothing`, writes.length, 1);

  await context.close();
}

const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || undefined });
try {
  await scenario(browser, 'no-preference');
  await scenario(browser, 'reduce');
} finally {
  await browser.close();
}
if (failed) {
  console.error(`${failed} failed`);
  process.exit(1);
}
