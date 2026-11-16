// Opens the accounts page in a real browser at the widths people use it at
// and checks what jsdom cannot: no account table scrolls sideways, every row
// button lies inside its card, the rows stay 44px, and on a screen with a
// pointer the buttons only come in under it.
//
//   cd scripts/e2e
//   npm install && npx playwright install chromium
//   node account-tables.mjs
//
// It drives the built web UI in web/dist, so build that first, and answers
// the API from the README pictures' fixtures (scripts/screenshots/fixtures),
// with a hoster login and two site sign-ins on top, so the hoster card carries
// its longest rows. CHROME_PATH runs a Chromium of your own.

import { existsSync, readFileSync } from 'node:fs';
import { dirname, extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..', '..');
const dist = join(root, 'web', 'dist');
const fixtures = join(root, 'scripts', 'screenshots', 'fixtures');

const json = (name) => JSON.parse(readFileSync(join(fixtures, `${name}.json`), 'utf8'));

// Rows with every button an account row can carry: a sign-in on the site's
// own page, the switch, Edit and Remove.
const extra = {
  '/api/hosterauth/logins': [
    ...json('hosterauth/logins'),
    {
      host: 'rapidgator.net',
      username: 'reader@mail.example',
      status: 'rejected',
      code: 'ownActivate',
      enabled: true,
      signInSite: 'rapidgator',
      twoFactor: true,
    },
  ],
  '/api/sitelogins': [
    {
      id: 'patreon',
      name: 'Patreon',
      host: 'patreon.com',
      loginUrl: 'https://www.patreon.com/login',
      domains: ['patreon.com'],
      state: 'signedIn',
      source: 'browser',
      browser: 'Firefox',
      unreadable: [],
      readsBrowsers: true,
      disconnected: false,
      expires: '2027-02-01T00:00:00Z',
    },
    {
      id: 'youtube',
      name: 'YouTube',
      host: 'youtube.com',
      loginUrl: 'https://accounts.google.com',
      domains: ['youtube.com'],
      state: 'signedIn',
      source: 'extension',
      unreadable: [],
      readsBrowsers: true,
      disconnected: false,
    },
  ],
  '/api/signinbrowser': { available: true, browser: 'Chromium', site: '' },
};

function fixture(url) {
  if (url.pathname in extra) return JSON.stringify(extra[url.pathname]);
  const path = url.pathname.replace(/^\/api\//, '');
  const narrow = [...url.searchParams.values()][0];
  for (const name of narrow ? [join(path, narrow), path] : [path]) {
    const file = join(fixtures, `${name}.json`);
    if (existsSync(file)) return readFileSync(file, 'utf8');
  }
  return null;
}

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
function check(name, ok, detail) {
  if (ok) {
    console.log(`ok    ${name}`);
    return;
  }
  failed++;
  console.error(`FAIL  ${name}\n        ${detail}`);
}

/** Each account table's well and rows, measured where they are drawn. */
function measure(page) {
  return page.evaluate(() =>
    [...document.querySelectorAll('[data-new="account-rows"]')].map((well) => {
      const edge = well.getBoundingClientRect().right;
      return {
        name: well.querySelector('table').getAttribute('aria-label'),
        client: well.clientWidth,
        scroll: well.scrollWidth,
        rows: [...well.querySelectorAll('tbody tr')].map((tr) => {
          const buttons = [...tr.querySelectorAll('.glim-row-actions button')];
          return {
            name: tr.querySelector('td .truncate').textContent.trim(),
            height: tr.getBoundingClientRect().height,
            buttons: buttons.length,
            outside: buttons.filter((b) => b.getBoundingClientRect().right > edge + 0.5).length,
            shown: getComputedStyle(tr.querySelector('.glim-row-actions')).opacity,
          };
        }),
      };
    }),
  );
}

async function scenario(browser, { label, viewport, touch, lang }) {
  const context = await browser.newContext({
    viewport,
    hasTouch: touch,
    isMobile: touch,
    locale: lang === 'de' ? 'de-DE' : 'en-US',
    timezoneId: 'UTC',
    colorScheme: 'dark',
  });
  await context.addInitScript((lang) => {
    localStorage.setItem('kl-theme', 'dark');
    localStorage.setItem('kl-lang', lang);
  }, lang);
  // The later route is asked first.
  await context.route(`${BASE}/**`, (route) => serveDist(route, new URL(route.request().url()).pathname));
  await context.route('**/api/**', (route) => {
    const request = route.request();
    const body = fixture(new URL(request.url()));
    if (body !== null) return route.fulfill({ contentType: 'application/json', body });
    return route.fulfill({ status: request.method() === 'GET' ? 404 : 204 });
  });
  await context.routeWebSocket('**/api/ws', (ws) => ws.send(JSON.stringify({ type: 'snapshot', data: json('tasks') })));

  const page = await context.newPage();
  page.on('pageerror', (e) => console.error('page error:', e.message));
  await page.goto(`${BASE}/accounts`);
  await page.locator('[data-new="account-rows"] tbody tr').first().waitFor();
  await page.waitForFunction(() => document.querySelectorAll('[data-new="account-rows"] tbody tr').length >= 6);
  await page.evaluate(() => document.fonts.ready);
  if (!touch) await page.mouse.move(1, viewport.height - 1);

  const tables = await measure(page);
  check(`${label}: the page shows the account tables`, tables.length >= 3, `${tables.length} tables`);
  for (const t of tables) {
    check(`${label}: ${t.name} does not scroll sideways`, t.scroll <= t.client, `scrollWidth ${t.scroll}, clientWidth ${t.client}`);
    for (const r of t.rows) {
      check(`${label}: ${t.name}, ${r.name}: every button lies inside the card`, r.outside === 0, `${r.outside} of ${r.buttons} outside`);
      check(`${label}: ${t.name}, ${r.name}: the row is 44px`, Math.round(r.height) === 44, `${r.height}px`);
      check(`${label}: ${t.name}, ${r.name}: the buttons ${touch ? 'stay' : 'wait for the pointer'}`, r.shown === (touch ? '1' : '0'), `opacity ${r.shown}`);
    }
  }
  const hoster = tables.find((t) => t.rows.some((r) => r.name.includes('rapidgator')));
  check(`${label}: the hoster card has its sign-in rows`, !!hoster && hoster.rows.length >= 4, JSON.stringify(hoster?.rows.map((r) => r.name)));

  if (!touch) {
    // Pointing at a row brings its buttons in without moving anything.
    const row = page.locator('[data-new="account-rows"] tbody tr', { hasText: 'rapidgator' });
    const cells = () =>
      row.evaluate((tr) => [...tr.children].map((td) => `${Math.round(td.getBoundingClientRect().x)}+${Math.round(td.getBoundingClientRect().width)}`).join(' '));
    await row.scrollIntoViewIfNeeded();
    const before = await cells();
    await row.hover();
    await page.waitForTimeout(300);
    const after = await cells();
    const shown = await row.locator('.glim-row-actions').evaluate((el) => getComputedStyle(el).opacity);
    check(`${label}: a row pointed at shows its buttons`, shown === '1', `opacity ${shown}`);
    check(`${label}: and no cell moves`, before === after, `${before} then ${after}`);
    const again = await measure(page);
    for (const t of again) check(`${label}: with buttons shown, ${t.name} does not scroll sideways`, t.scroll <= t.client, `scrollWidth ${t.scroll}, clientWidth ${t.client}`);
  }
  await context.close();
}

const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || undefined });
try {
  for (const lang of ['en', 'de']) {
    // 1080 leaves the cards about 700px wide.
    for (const width of [1080, 1280, 1620]) {
      await scenario(browser, { label: `${width} ${lang}`, viewport: { width, height: 1000 }, touch: false, lang });
    }
    await scenario(browser, { label: `tablet ${lang}`, viewport: { width: 820, height: 1180 }, touch: true, lang });
    await scenario(browser, { label: `phone ${lang}`, viewport: { width: 390, height: 844 }, touch: true, lang });
  }
} finally {
  await browser.close();
}
if (failed) {
  console.error(`${failed} failed`);
  process.exit(1);
}
