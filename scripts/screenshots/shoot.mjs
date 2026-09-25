// Draws the README screenshots: the web UI under Vite, driven by Playwright,
// with every API answer and the live socket's frames read from fixtures/.
// The data there is made up, so nothing of a real instance can end up in a
// picture, and the clock is fixed, so a second run draws the same pictures.
//
//   cd scripts/screenshots
//   npm install && npx playwright install chromium
//   node shoot.mjs                 every scene in both themes
//   node shoot.mjs downloads app   only these scenes
//
// Vite and the React plugin come from web/node_modules, which has to be
// installed. CHROME_PATH runs a Chromium of your own instead of Playwright's.
// The pictures land in .github/assets/screenshots/, quantised with pngquant or
// ImageMagick and recompressed with oxipng where those are on the PATH.

import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, realpathSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..', '..');
const web = join(root, 'web');
const fixtures = join(here, 'fixtures');
const out = join(root, '.github', 'assets', 'screenshots');

// The size the README's pictures have always had, one device pixel per CSS
// pixel, so text is drawn at the size a browser draws it.
const VIEWPORT = { width: 1500, height: 940 };

// Every timestamp in the fixtures lies shortly before this moment.
const NOW = new Date('2026-09-24T14:30:00Z');

const THEMES = ['dark', 'light'];

// The address the pictures show. The browser resolves it to Vite on this
// machine; home.arpa is the name space reserved for home networks.
const HOST = 'keep.home.arpa';

/**
 * One picture per theme. `ready` is a selector that is on screen once the
 * page has its data, and `act` sets the scene up after that.
 */
const SCENES = [
  { n: 1, name: 'overview', path: '/', ready: 'text=bbb_sunflower' },
  { n: 2, name: 'downloads', path: '/downloads', ready: 'text=bbb_sunflower' },
  {
    n: 3,
    name: 'quick-settings',
    path: '/downloads',
    ready: 'text=bbb_sunflower',
    act: (page) => page.getByRole('button', { name: 'Quick settings' }).click(),
  },
  { n: 4, name: 'collector', path: '/collector', ready: 'text=Sprite Fright' },
  { n: 5, name: 'appearance', path: '/settings/appearance', ready: 'text=Rainbow mode' },
  { n: 6, name: 'accounts', path: '/accounts', ready: 'text=Real-Debrid' },
  {
    n: 7,
    name: 'rules',
    path: '/settings/rules',
    ready: 'text=Open movies',
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
  { n: 8, name: 'app', path: '/settings/browsertools', ready: 'text=Firefox' },
  { n: 9, name: 'instances', path: '/instances', ready: 'text=Watchtower' },
];

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

async function answer(route, missing) {
  const request = route.request();
  const url = new URL(request.url());
  // No favicons: every host draws its monogram, and no hoster's mark ends up
  // in a picture.
  if (url.pathname === '/api/hosters/icon') return route.fulfill({ status: 404 });
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
      port: 8749,
      allowedHosts: [HOST],
      // web/node_modules can be a link to an install elsewhere, and Vite
      // serves nothing outside the folders it is allowed.
      fs: { allow: [web, realpathSync(join(web, 'node_modules'))] },
    },
  });
  await server.listen();
  return server;
}

async function shoot(browser, base, theme, scene, missing) {
  const context = await browser.newContext({
    viewport: VIEWPORT,
    deviceScaleFactor: 1,
    locale: 'en-US',
    timezoneId: 'UTC',
    colorScheme: theme,
    // The pictures show where each page settles, not a transition halfway.
    reducedMotion: 'reduce',
  });
  await context.clock.setFixedTime(NOW);
  await context.addInitScript((t) => {
    localStorage.setItem('kl-theme', t);
    localStorage.setItem('kl-lang', 'en');
  }, theme);
  await context.route('**/api/**', (route) => answer(route, missing));
  await context.routeWebSocket('**/api/ws', socket);

  const page = await context.newPage();
  await page.goto(base + scene.path);
  await page.locator(scene.ready).first().waitFor();
  if (scene.act) await scene.act(page);
  await page.evaluate(() => document.fonts.ready);
  // Two ticks of the speed curve, so the live end has joined the history.
  await page.waitForTimeout(2500);

  const file = join(out, `knightloader-${scene.n}-${scene.name}-${theme}.png`);
  await page.screenshot({ path: file });
  await context.close();
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
// palette. ImageMagick's dithering speckles the blurred backdrop behind a
// dialog, so it is left off.
function optimise(files) {
  let quantise = null;
  if (onPath('pngquant')) quantise = (f) => run('pngquant', ['--force', '--ext', '.png', '--strip', '256', f]);
  else if (onPath('magick')) quantise = (f) => run('magick', [f, '-strip', '-dither', 'None', '-colors', '256', `PNG8:${f}`]);
  else console.warn('neither pngquant nor ImageMagick found, the pictures keep their full colour');
  const recompress = onPath('oxipng');
  if (!recompress) console.warn('oxipng not found, the pictures are not recompressed');
  for (const f of files) {
    quantise?.(f);
    if (recompress) run('oxipng', ['--quiet', '--opt', 'max', '--strip', 'safe', f]);
  }
}

const wanted = process.argv.slice(2);
const unknown = wanted.filter((w) => !SCENES.some((s) => s.name === w));
if (unknown.length) {
  console.error(`no such scene: ${unknown.join(', ')}; there are ${SCENES.map((s) => s.name).join(', ')}`);
  process.exit(2);
}
const scenes = wanted.length ? SCENES.filter((s) => wanted.includes(s.name)) : SCENES;

const vite = await startVite();
const browser = await chromium.launch({
  executablePath: process.env.CHROME_PATH || undefined,
  args: [`--host-resolver-rules=MAP ${HOST} 127.0.0.1`],
});
const missing = new Set();
const files = [];
try {
  const base = `http://${HOST}:${vite.httpServer.address().port}`;
  for (const theme of THEMES) {
    for (const scene of scenes) {
      files.push(await shoot(browser, base, theme, scene, missing));
      console.log(relative(root, files.at(-1)));
    }
  }
} finally {
  await browser.close();
  await vite.close();
}
if (missing.size) console.warn(`answered 404, no fixture: ${[...missing].sort().join(', ')}`);
optimise(files);
