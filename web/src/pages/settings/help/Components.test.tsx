// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { DeploymentInfo, Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { SettingsProvider, type SettingsDraft } from '../context';
import { ComponentsCard } from './Components';
import { VersionCard } from './VersionCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const open = vi.hoisted(() => ({ urls: [] as string[], news: 0 }));
vi.mock('../../../lib/external', async (original) => ({
  ...(await original<typeof import('../../../lib/external')>()),
  openExternal: (url: string) => open.urls.push(url),
}));
vi.mock('../../../lib/useWhatsNew', async (original) => ({
  ...(await original<typeof import('../../../lib/useWhatsNew')>()),
  openWhatsNew: () => {
    open.news++;
  },
}));

let root: Root;
let host: HTMLDivElement;

/** What the server answers, by route; "POST route" answers a POST apart from a GET. A test changes a route to change the answer. */
let answers: Record<string, unknown>;
/** Routes the server refuses, with the status and body it refuses with. */
let refusals: Record<string, { status: number; body: unknown }>;
/** Every request, as "GET route" or "POST route". */
let asked: string[];

const UPDATE = '/api/components/update';
const QBIT = '/api/components/qbittorrent';

const CONTAINER: DeploymentInfo = { deployment: 'container', kind: 'container', canQuit: false, canRestart: false, note: '', engines: [] };
const DESKTOP: DeploymentInfo = { ...CONTAINER, deployment: 'desktop', kind: 'desktop', installer: '1.2.5' };
const FLATPAK: DeploymentInfo = { ...DESKTOP, updatesFromStore: true };

/** The qBittorrent row's answer, with the version table the server ships. */
const qbit = (over: Record<string, unknown>) => ({
  chosen: '4.5.5',
  default: '4.5.5',
  versions: [
    { version: '4.5.5', libtorrent: '1.2.19', whitelists: ['OPS'], note: '' },
    { version: '4.6.7', libtorrent: '1.2.19', whitelists: [], note: '' },
  ],
  ...over,
});

beforeEach(() => {
  answers = {
    '/api/health': { status: 'ok', version: 'v1.8.4' },
    '/api/mediatools': {
      ytdlp: { found: true, version: '2026.08.19', source: 'path' },
      ffmpeg: { found: true, version: '8.1.2' },
      ffprobe: { found: true, version: '8.1.2' },
      deno: { found: true, version: '2.9.8' },
    },
    '/api/resolvers/jd': { configured: true, reachable: true, version: 50123 },
    [QBIT]: qbit({ state: 'running', version: '4.5.5' }),
    '/api/system/update-check': { checked: true, available: false, current: 'v1.8.4' },
    '/api/system/update-install': { state: '' },
    [UPDATE]: { running: false },
    [`POST ${UPDATE}`]: {
      running: false,
      results: {
        'yt-dlp': { state: 'current', version: '2026.08.19' },
        ffmpeg: { state: 'skipped' },
        deno: { state: 'skipped' },
        jdownloader: { state: 'current' },
      },
    },
  };
  refusals = {};
  asked = [];
  open.urls = [];
  open.news = 0;
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET';
      asked.push(`${method} ${url}`);
      const own = `${method} ${url}`;
      const key = method !== 'GET' && (own in answers || own in refusals) ? own : url;
      const refused = refusals[key];
      if (refused) return Promise.resolve(new Response(JSON.stringify(refused.body), { status: refused.status }));
      return Promise.resolve(
        key in answers
          ? new Response(JSON.stringify(answers[key]), { headers: { 'Content-Type': 'application/json' } })
          : new Response('not here', { status: 404 }),
      );
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const BASE = {
  autoUpdate: false,
  componentsAutoUpdate: true,
} as Settings;

async function draw(cfg: Partial<Settings> = {}, deployment: DeploymentInfo = CONTAINER) {
  const patch = vi.fn();
  const draft = { cfg: { ...BASE, ...cfg }, patch, patchNow: patch } as unknown as SettingsDraft;
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
            <VersionCard deployment={deployment} hue={0} />
            <ComponentsCard deployment={deployment} hue={1} />
          </SettingsProvider>
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
  return patch;
}

const wait = (ms: number) => act(() => new Promise((resolve) => setTimeout(resolve, ms)));

/** The card titled `title`. */
const card = (title: string) =>
  [...host.querySelectorAll<HTMLElement>('.glim-card')].find((c) => c.textContent?.startsWith(title))!;
/** A button by its name, which a glyph button carries as its label. */
const button = (within: Element, name: string) =>
  [...within.querySelectorAll('button')].find(
    (b) => b.getAttribute('aria-label')?.includes(name) || b.textContent?.includes(name),
  )!;
/** The buttons at the end of a card's last row, which leave its switch and its (i) aside. */
const actions = (within: Element) => within.querySelector('.glim-card > .glim-version-actions, .glim-version-foot .glim-version-actions')!;
const buttons = (within: Element) => [...actions(within).querySelectorAll('button')].map((b) => b.textContent?.trim());
const switches = (within: Element) =>
  [...within.querySelectorAll('[role="switch"]')].map((s) => s.getAttribute('aria-label'));

/** The Version card's last row: the line and the note after it. */
const line = () => card('Version').querySelector('.glim-version-line')?.textContent ?? '';
const note = () => card('Version').querySelector('.glim-version-note')?.textContent ?? '';

/** The row of a component, by its name. */
const row = (name: string) =>
  [...host.querySelectorAll('.glim-version-row')].find((r) => r.querySelector('.glim-version-part')?.textContent === name)!;
const names = () => [...host.querySelectorAll('.glim-version-row .glim-version-part')].map((n) => n.textContent);
/** The short status of a row and the sentence behind its (i). */
const status = (name: string) => row(name).querySelector('.glim-version-status')?.textContent ?? '';
const detail = (name: string) =>
  row(name).querySelector('.glim-version-status [role="note"]')?.getAttribute('aria-label') ?? '';
/** What the (i) beside a row's name says. */
const about = (name: string) => row(name).querySelector('.glim-version-name [role="note"]')?.getAttribute('aria-label') ?? '';
/** The version number of a row, and where it leads. */
const version = (name: string) => row(name).querySelector('.glim-version-number')!;
const href = (name: string) => version(name).getAttribute('href');

const updateComponents = () => act(async () => button(actions(card('Components')), 'Update components').click());
const checkNow = () => act(async () => button(actions(card('Version')), 'Check for updates').click());

describe('the Version card', () => {
  it('shows KnightLoader with its number, which opens the newest release', async () => {
    await draw();
    const head = card('Version').querySelector('.glim-version-head')!;
    expect(head.querySelector('.glim-version-app')!.textContent).toBe('KnightLoader');
    const number = head.querySelector<HTMLAnchorElement>('a.glim-version-number')!;
    expect(number.textContent).toBe('1.8.4');
    expect(number.getAttribute('href')).toBe('https://github.com/junkerderprovinz/knightloader/releases/latest');
  });

  it('says a stamp is no release and links it nowhere', async () => {
    answers['/api/health'] = { status: 'ok', version: 'dev' };
    await draw();
    const number = card('Version').querySelector('.glim-version-head .glim-version-number')!;
    expect(number.tagName).toBe('SPAN');
    expect(number.textContent).toBe('dev, not a released build');
  });

  it('asks nobody on opening while its switch is off, and once while it is on', async () => {
    await draw();
    expect(asked).not.toContain('GET /api/system/update-check');
    expect(line()).toBe('');
    expect(buttons(card('Version'))).toEqual(["What's new", 'Check for updates']);

    act(() => root.unmount());
    root = createRoot(host);
    await draw({ autoUpdate: true });
    expect(asked.filter((a) => a === 'GET /api/system/update-check')).toHaveLength(1);
    expect(line()).toBe('Up to date');
    expect(note()).toBe('Last checked now');
  });

  it('says a check that failed and keeps offering it', async () => {
    answers['/api/system/update-check'] = { checked: false, available: false, current: 'v1.8.4' };
    await draw();
    await checkNow();
    expect(line()).toBe('Check failed');
    expect(card('Version').querySelector('.glim-version-state')!.getAttribute('data-tone')).toBe('fail');
    expect(button(actions(card('Version')), 'Check for updates')).toBeDefined();
  });

  it('shows a container how to update once a newer release is out, and opens that release for What is new', async () => {
    answers['/api/system/update-check'] = {
      checked: true,
      available: true,
      current: 'v1.8.4',
      latest: 'v1.9.0',
      url: 'https://github.com/junkerderprovinz/knightloader/releases/tag/v1.9.0',
    };
    await draw();
    await checkNow();
    expect(line()).toBe('Update to 1.9.0 available');
    expect(buttons(card('Version'))).toEqual(["What's new", 'How to update']);
    await act(async () => button(card('Version'), 'How to update').click());
    expect(open.urls).toEqual(['https://junkerderprovinz.github.io/knightloader/installing/#updating-the-container']);
    await act(async () => button(card('Version'), "What's new").click());
    expect(open.urls[1]).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.9.0');
    expect(open.news).toBe(0);
  });

  it('opens the window of changes for What is new while nothing newer is out', async () => {
    await draw();
    await act(async () => button(card('Version'), "What's new").click());
    expect(open.news).toBe(1);
  });

  it('gives a container the switch that only looks, with its own sentence', async () => {
    const patch = await draw();
    expect(switches(card('Version'))).toEqual(['Check for updates automatically']);
    const tip = card('Version').querySelector('[data-glim-label="Check for updates automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateContainerHint']);
    await act(async () => card('Version').querySelector<HTMLButtonElement>('[role="switch"]')!.click());
    expect(patch.mock.calls).toEqual([[{ autoUpdate: true }]]);
  });

  it('leaves a Flatpak to Flatpak and leads to how it is updated', async () => {
    answers['/api/system/update-check'] = { checked: true, available: true, current: 'v1.8.4', latest: 'v1.9.0' };
    await draw({ autoUpdate: true }, FLATPAK);
    expect(asked).not.toContain('GET /api/system/update-install');
    expect(switches(card('Version'))).toEqual(['Check for updates automatically']);
    const tip = card('Version').querySelector('[data-glim-label="Check for updates automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateStoreHint']);
    await act(async () => button(card('Version'), 'How to update').click());
    expect(open.urls).toEqual(['https://junkerderprovinz.github.io/knightloader/installing/#the-flatpak']);
  });
});

describe('updating the desktop build from the Version card', () => {
  beforeEach(() => {
    answers['/api/system/update-check'] = { checked: true, available: true, current: 'v1.8.4', latest: 'v1.9.0' };
  });

  it('gives it the switch that installs, and Update now once a newer release is found', async () => {
    await draw({}, DESKTOP);
    expect(switches(card('Version'))).toEqual(['Update automatically']);
    const tip = card('Version').querySelector('[data-glim-label="Update automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toContain('Once a day KnightLoader looks for a new version');
    expect(button(card('Version'), 'Update now')).toBeUndefined();
    await checkNow();
    expect(line()).toBe('Update to 1.9.0 available');
    // Nothing is left to look for once a newer release is found.
    expect(buttons(card('Version'))).toEqual(["What's new", 'Update now']);
    expect(button(card('Version'), 'Update now').className).toContain('bg-accent');
  });

  it('shows the download with its bar, then the installer taking over', async () => {
    await draw({}, DESKTOP);
    await checkNow();
    answers['/api/system/update-install'] = { state: 'downloading', version: '1.9.0', done: 40, total: 100 };
    await act(async () => button(card('Version'), 'Update now').click());
    expect(line()).toBe('Downloading … 40%');
    expect(card('Version').querySelector('[role="progressbar"]')!.getAttribute('aria-valuenow')).toBe('40');
    expect(button(card('Version'), 'Update now')).toBeUndefined();

    answers['/api/system/update-install'] = { state: 'started', version: '1.9.0' };
    await wait(600);
    expect(line()).toBe('Downloading … 100%');
  });

  it('says a downloaded version waits for the next start', async () => {
    answers['/api/system/update-check'] = { checked: true, available: true, current: 'v1.8.4', latest: 'v1.9.0', ready: 'v1.9.0' };
    await draw({ autoUpdate: true }, DESKTOP);
    expect(line()).toBe('1.9.0 is ready');
    expect(buttons(card('Version'))).toEqual(["What's new"]);
  });

  it('picks up an update that was started before the page opened', async () => {
    answers['/api/system/update-install'] = { state: 'downloading', version: '1.9.0', done: 1, total: 4 };
    await draw({}, DESKTOP);
    expect(line()).toBe('Downloading … 25%');
  });

  it('says why an update did not work, and a refused one in the same place', async () => {
    await draw({}, DESKTOP);
    await checkNow();
    answers['/api/system/update-install'] = { state: 'failed', error: 'permission to run it was refused' };
    await act(async () => button(card('Version'), 'Update now').click());
    expect(line()).toBe('Check failed');
    expect(note()).toBe('permission to run it was refused');

    act(() => root.unmount());
    root = createRoot(host);
    refusals['POST /api/system/update-install'] = { status: 409, body: { status: 'an update is already under way' } };
    answers['/api/system/update-install'] = { state: '' };
    await draw({}, DESKTOP);
    await checkNow();
    await act(async () => button(card('Version'), 'Update now').click());
    expect(line()).toBe('Check failed');
    expect(note()).not.toBe('');
  });
});

describe('the Components card', () => {
  it('lists the programs by name, then GlimStone', async () => {
    await draw();
    expect(names()).toEqual(['Deno', 'FFmpeg', 'JDownloader backend', 'qBittorrent', 'yt-dlp', 'GlimStone']);
    expect(version('yt-dlp').textContent).toBe('2026.08.19');
    expect(version('FFmpeg').textContent).toBe('8.1.2');
    expect(version('Deno').textContent).toBe('2.9.8');
    expect(version('JDownloader backend').textContent).toBe('50123');
  });

  it('ends the desktop build with its installer', async () => {
    await draw({}, DESKTOP);
    expect(names().slice(-1)).toEqual(['PommelPorter']);
    expect(about('PommelPorter')).toBe('Installer');
    expect(version('PommelPorter').textContent).toBe('1.2.5');
    expect(href('PommelPorter')).toBe('https://github.com/junkerderprovinz/pommelporter/releases/latest');
  });

  it('says what each component is in the (i) beside its name', async () => {
    await draw();
    expect(about('yt-dlp')).toBe(en['componentsCard.about.ytdlp']);
    expect(about('GlimStone')).toBe('Design system');
    expect(card('Components').querySelector('.glim-version-name .glim-version-sub')).toBeNull();
  });

  it('leads each number to its newest release or its project page', async () => {
    await draw();
    expect(href('GlimStone')).toBe('https://github.com/junkerderprovinz/glimstone/releases/latest');
    expect(href('yt-dlp')).toBe('https://github.com/yt-dlp/yt-dlp/releases/latest');
    expect(href('FFmpeg')).toBe('https://ffmpeg.org/download.html');
    expect(href('Deno')).toBe('https://github.com/denoland/deno/releases/latest');
    expect(href('JDownloader backend')).toBe('https://jdownloader.org/');
    expect(href('qBittorrent')).toBe('https://www.qbittorrent.org/news');
  });

  it('gives the components one switch and writes it to their setting', async () => {
    const patch = await draw({ componentsAutoUpdate: true });
    expect(switches(card('Components'))).toEqual(['Update components automatically']);
    const toggle = card('Components').querySelector<HTMLButtonElement>('[role="switch"]')!;
    expect(toggle.getAttribute('aria-checked')).toBe('true');
    await act(async () => toggle.click());
    expect(patch.mock.calls).toEqual([[{ componentsAutoUpdate: false }]]);
  });

  it('updates every component with the one button at its foot, and leaves KnightLoader to its own card', async () => {
    await draw();
    await updateComponents();
    expect(asked).toContain(`POST ${UPDATE}`);
    expect(asked).not.toContain('GET /api/system/update-check');
    for (const name of ['yt-dlp', 'FFmpeg', 'Deno', 'JDownloader backend']) expect(status(name)).toBe('');
  });

  it('says on each row what its update came to', async () => {
    answers[`POST ${UPDATE}`] = {
      running: false,
      results: {
        'yt-dlp': { state: 'updated', version: '2026.09.30' },
        ffmpeg: { state: 'failed', detail: 'the archive holds no bin/ffprobe' },
        deno: { state: 'current', version: '2.9.8' },
        jdownloader: { state: 'waits' },
      },
    };
    await draw();
    await updateComponents();
    expect(status('yt-dlp')).toBe('Done');
    expect(status('FFmpeg')).toBe('Failed');
    expect(detail('FFmpeg')).toBe('Nothing was replaced. the archive holds no bin/ffprobe');
    expect(row('FFmpeg').querySelector('.glim-version-status')!.getAttribute('data-tone')).toBe('fail');
    expect(status('Deno')).toBe('');
    expect(status('JDownloader backend')).toBe(en['settings.info.statusWaits']);
    expect(detail('JDownloader backend')).toBe(en['settings.info.statusWaitsHint']);
  });

  it('follows an update until every component has come to something', async () => {
    answers[`POST ${UPDATE}`] = { running: true, results: { 'yt-dlp': { state: 'checking' } } };
    await draw();
    answers[UPDATE] = { running: true, results: { 'yt-dlp': { state: 'checking' } } };
    await updateComponents();
    expect(status('yt-dlp')).toBe('Checking …');
    expect(button(actions(card('Components')), 'Update components').disabled).toBe(true);

    answers[UPDATE] = { running: false, results: { 'yt-dlp': { state: 'updated', version: '2026.09.30' } } };
    answers['/api/mediatools'] = { ytdlp: { found: true, version: '2026.09.30', source: 'managed' }, ffmpeg: { found: true }, ffprobe: { found: true } };
    await wait(1100);
    expect(status('yt-dlp')).toBe('Done');
    expect(version('yt-dlp').textContent).toBe('2026.09.30');
    expect(button(actions(card('Components')), 'Update components').disabled).toBe(false);
  });

  it('picks up an update that was started before the page opened', async () => {
    answers[UPDATE] = { running: true, results: { ffmpeg: { state: 'checking' } } };
    await draw();
    expect(status('FFmpeg')).toBe('Checking …');
    expect(button(actions(card('Components')), 'Update components').disabled).toBe(true);
    expect(asked).not.toContain(`POST ${UPDATE}`);
  });

  it('says an update the server refused above the rows', async () => {
    refusals[`POST ${UPDATE}`] = { status: 500, body: { error: 'nope' } };
    await draw();
    await updateComponents();
    expect(card('Components').querySelector('.glim-version-state')!.textContent).toBe('Failed');
  });

  it('waits for JDownloader to come back after its update and shows its new revision', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'Date'] });
    answers[`POST ${UPDATE}`] = { running: false, results: { jdownloader: { state: 'updated' } } };
    answers['/api/resolvers/jd'] = { configured: true, reachable: false };
    await draw();
    await updateComponents();
    expect(status('JDownloader backend')).toBe('Done');

    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50200 };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(version('JDownloader backend').textContent).toBe('50200');
  });

  it('says where a JDownloader backend that is not set up stands, with nothing to press', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false };
    await draw();
    expect(status('JDownloader backend')).toBe('Not set up');
    expect(row('JDownloader backend').querySelector('button')).toBeNull();
  });

  it('shows an update the server found for JDownloader on its own', async () => {
    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50123, updateWaits: true };
    await draw();
    expect(status('JDownloader backend')).toBe('Updates: 1');
  });

  it('says a tool that is nowhere to be found', async () => {
    answers['/api/mediatools'] = {
      ytdlp: { found: true, version: '2026.08.19', source: 'path' },
      ffmpeg: { found: false },
      ffprobe: { found: false },
      deno: { found: false },
    };
    await draw();
    expect(status('FFmpeg')).toBe('Not found');
    expect(status('Deno')).toBe('Not found');
  });

  it('says the other yt-dlp took over when the fetched copy does not start, with nothing to press', async () => {
    answers['/api/mediatools'] = {
      ytdlp: { found: true, version: '2026.08.19', source: 'path' },
      managed: { version: '2026.09.30' },
      managedPath: '/config/bin/yt-dlp',
      ffmpeg: { found: true, version: '8.1.2' },
      ffprobe: { found: true, version: '8.1.2' },
    };
    await draw();
    expect(status('yt-dlp')).toBe(en['settings.resolvers.toolsFellBack']);
    expect(detail('yt-dlp')).toContain('/config/bin/yt-dlp');
    expect(row('yt-dlp').querySelector('button')).toBeNull();
  });
});

describe('the JDownloader backend the desktop build sets up', () => {
  it('shows in the status how far the Java runtime has come while JDownloader is being set up', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false, setup: 'starting', setupDone: 30, setupTotal: 120 };
    await draw();
    expect(status('JDownloader backend')).toBe('Being set up · 25%');
    answers['/api/resolvers/jd'] = { configured: false, reachable: false, setup: 'starting', setupDone: 90, setupTotal: 120 };
    await wait(1100);
    expect(status('JDownloader backend')).toBe('Being set up · 75%');
  });

  it('says why the setup failed and tries again on request', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false, setup: 'failed', detail: 'downloading the Java runtime: no network' };
    await draw();
    expect(status('JDownloader backend')).toBe('Setup failed');
    expect(detail('JDownloader backend')).toBe('KnightLoader could not set up its JDownloader: downloading the Java runtime: no network');
    answers['POST /api/resolvers/jd/setup'] = { configured: false, reachable: false, setup: 'starting' };
    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50123 };
    await act(async () => button(row('JDownloader backend'), 'Try again').click());
    expect(asked).toContain('POST /api/resolvers/jd/setup');
    expect(status('JDownloader backend')).toBe('Being set up');
    await wait(1100);
    expect(version('JDownloader backend').textContent).toBe('50123');
  });

  it('shows the Java runtime it fetched, with a newer one that waits', async () => {
    answers['/api/resolvers/jd'] = {
      configured: true,
      reachable: true,
      version: 50123,
      runtime: { vendor: 'Eclipse Temurin', version: '21.0.13+9' },
      runtimeUpdate: '21.0.14+7',
    };
    await draw();
    expect(names()).toContain('Java');
    expect(version('Java').textContent).toBe('21.0.13+9');
    expect(href('Java')).toBe('https://adoptium.net/temurin/releases/');
    expect(status('Java')).toBe('21.0.14+7 is available');
  });

  it('leaves Java off the card where KnightLoader runs no JDownloader of its own', async () => {
    await draw();
    expect(names()).not.toContain('Java');
  });
});

describe('the qBittorrent KnightLoader runs', () => {
  /** Every info bubble on the row outside its name and status, as a screen reader hears them. */
  const bubbles = () =>
    [...row('qBittorrent').querySelectorAll('.glim-version-row-actions [role="note"]')].map((n) => n.getAttribute('aria-label') ?? '').join(' ');

  it('offers the versions to hold on the row itself, the recommended one marked', async () => {
    await draw();
    expect(version('qBittorrent').textContent).toBe('4.5.5');
    expect(row('qBittorrent').querySelector('button[aria-haspopup]')!.textContent).toContain('4.5.5, recommended');
  });

  it('says in a bubble why the version stays and which trackers list it', async () => {
    await draw();
    expect(bubbles()).toContain('Private trackers let in only the qBittorrent versions on their list');
    expect(bubbles()).toContain('On the public client list of OPS.');
  });

  it('writes a version picked to the torrent settings', async () => {
    const patch = await draw({ torrent: { seedRatioTarget: 2 } } as unknown as Partial<Settings>);
    await act(async () => row('qBittorrent').querySelector<HTMLButtonElement>('button[aria-haspopup]')!.click());
    const option = [...document.querySelectorAll<HTMLElement>('[role="option"], [role="menuitemradio"]')].find(
      (o) => o.textContent?.trim() === '4.6.7',
    )!;
    await act(async () => option.click());
    expect(patch.mock.calls).toEqual([[{ torrent: { seedRatioTarget: 2, engineVersion: '4.6.7' } }]]);
  });

  it('warns about a held version no tracker list names', async () => {
    await draw({ torrent: { engineVersion: '4.6.7' } } as unknown as Partial<Settings>);
    expect(bubbles()).toContain('Not on any public client list KnightLoader knows of.');
  });

  it('says why it is not running and starts it again on request', async () => {
    answers[QBIT] = qbit({ state: 'failed', detail: 'github answered 503' });
    answers[`POST ${QBIT}/start`] = qbit({ state: 'running', version: '4.5.5' });
    await draw();
    expect(status('qBittorrent')).toContain('Not working');
    expect(detail('qBittorrent')).toContain('github answered 503');
    await act(async () => button(row('qBittorrent'), 'Try again').click());
    expect(asked).toContain(`POST ${QBIT}/start`);
    expect(version('qBittorrent').textContent).toBe('4.5.5');
  });

  it('is left out of Update components', async () => {
    await draw();
    await updateComponents();
    expect(asked.filter((a) => a.startsWith('POST') && a.includes('qbittorrent'))).toEqual([]);
  });
});
