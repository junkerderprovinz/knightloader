// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { SettingsProvider, type SettingsDraft } from '../context';
import { ComponentsCard, releaseTag } from './Components';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

/** What the server answers, by route; "POST route" answers a POST apart from a GET. A test changes a route to change the answer. */
let answers: Record<string, unknown>;
/** Routes the server refuses, with the status and body it refuses with. */
let refusals: Record<string, { status: number; body: unknown }>;
/** Every request, as "GET route" or "POST route". */
let asked: string[];

const UPDATE = '/api/components/update';

beforeEach(() => {
  answers = {
    '/api/health': { status: 'ok', version: 'v1.8.4' },
    '/api/mediatools': {
      ytdlp: { found: true, version: '2026.08.19', source: 'path' },
      ffmpeg: { found: true, version: '8.1.2' },
      ffprobe: { found: true, version: '8.1.2' },
      deno: { found: true, version: '2.9.8' },
    },
    '/api/system/deployment': {
      deployment: 'container',
      canQuit: false,
      canRestart: false,
      note: '',
      engines: [],
    },
    '/api/resolvers/jd': { configured: true, reachable: true, version: 50123 },
    '/api/system/update-check': { checked: true, available: false, current: 'v1.8.4' },
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

async function draw(cfg: Partial<Settings> = {}) {
  const patch = vi.fn();
  const draft = { cfg: { ...BASE, ...cfg }, patch } as unknown as SettingsDraft;
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
            <ComponentsCard hue={0} />
          </SettingsProvider>
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
  return patch;
}

/** The row of a component, by the name in its heading. */
const group = (name: string) =>
  [...host.querySelectorAll('section')].find((s) => s.querySelector('h3')?.textContent === name)!;
/** A button by its name, which a glyph button carries as its label. */
const button = (within: Element, name: string) =>
  [...within.querySelectorAll('button')].find(
    (b) => b.getAttribute('aria-label')?.includes(name) || b.textContent?.includes(name),
  )!;
/** The short status of a row and the sentence behind its (i). */
const status = (name: string) => group(name).querySelector('[role="status"]')?.textContent ?? '';
const detail = (name: string) => group(name).querySelector('[role="status"] [role="note"]')?.getAttribute('aria-label') ?? '';
/** The version number of a row, and where it leads. */
const version = (name: string) => group(name).querySelector('.glim-num')!;
const href = (name: string) => version(name).querySelector('a')?.getAttribute('href');
const names = () => [...host.querySelectorAll('section h3')].map((h) => h.textContent);

const switches = (within: Element) =>
  [...within.querySelectorAll('[role="switch"]')].map((s) => s.getAttribute('aria-label'));

/** The card titled `title`. */
const card = (title: string) =>
  [...host.querySelectorAll<HTMLElement>('.glim-card')].find((c) => c.textContent?.startsWith(title))!;

/** The one button at the card's foot that updates every component, by what it says now. */
const updateButton = (face: string) => button(card('Components'), face);
const updateNow = () => act(async () => updateButton('Update now').click());

describe('the Components card', () => {
  it('lists every component, each with its version', async () => {
    await draw();
    expect(names()).toEqual(['KnightLoader', 'yt-dlp', 'FFmpeg', 'Deno', 'JDownloader backend', 'GlimStone']);
    expect(version('KnightLoader').textContent).toBe('1.8.4');
    expect(version('yt-dlp').textContent).toBe('2026.08.19');
    expect(version('FFmpeg').textContent).toBe('8.1.2');
    expect(version('Deno').textContent).toBe('2.9.8');
    expect(version('JDownloader backend').textContent).toBe('50123');
  });

  it('leads each version to its release or project page', async () => {
    await draw();
    expect(href('KnightLoader')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4');
    expect(href('GlimStone')).toBe('https://github.com/junkerderprovinz/glimstone/releases/latest');
    expect(href('yt-dlp')).toBe('https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.19');
    expect(href('FFmpeg')).toBe('https://ffmpeg.org/');
    expect(href('Deno')).toBe('https://github.com/denoland/deno/releases/tag/v2.9.8');
    expect(href('JDownloader backend')).toBe('https://jdownloader.org/');
  });

  it('links a branch build to the release it was cut from', async () => {
    answers['/api/health'] = { status: 'ok', version: 'v1.8.4+redesign.59b73a6' };
    await draw();
    expect(version('KnightLoader').textContent).toBe('1.8.4+redesign.59b73a6');
    expect(href('KnightLoader')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4');
  });

  it('leads a stamp without a release to the repository', async () => {
    answers['/api/health'] = { status: 'ok', version: 'preview-59b73a6' };
    await draw();
    expect(version('KnightLoader').textContent).toBe('preview-59b73a6');
    expect(href('KnightLoader')).toBe('https://github.com/junkerderprovinz/knightloader');
  });

  it('shows each name alone, with no (i) beside it', async () => {
    await draw();
    expect(card('Components').querySelector('h3 [role="note"]')).toBeNull();
  });

  it('keeps buttons off the rows until there is something to do there', async () => {
    await draw();
    const rowsWith = (what: string) =>
      [...host.querySelectorAll('section')].filter((s) => s.querySelector(what)).map((s) => s.querySelector('h3')?.textContent);
    expect(rowsWith('section > span button')).toEqual([]);
  });

  it('gives the components one switch, below them, and KnightLoader its own', async () => {
    await draw();
    expect(switches(group('KnightLoader'))).toEqual(['Automatically update']);
    for (const name of ['yt-dlp', 'FFmpeg', 'Deno', 'JDownloader backend']) expect(switches(group(name))).toEqual([]);
    const all = switches(card('Components'));
    expect(all.filter((s) => s === 'Update components automatically')).toHaveLength(1);
    expect(all).not.toContain('Check for updates');
  });

  it('writes the one switch to the components setting', async () => {
    const patch = await draw({ componentsAutoUpdate: true });
    const toggle = card('Components').querySelector<HTMLButtonElement>('[role="switch"][aria-label="Update components automatically"]')!;
    expect(toggle.getAttribute('aria-checked')).toBe('true');
    await act(async () => toggle.click());
    expect(patch.mock.calls).toEqual([[{ componentsAutoUpdate: false }]]);
  });

  it('puts every version last in its row, so they line up at the right edge', async () => {
    await draw();
    for (const name of names()) {
      const cells = [...group(name!).children].filter((c) => c.tagName !== 'DIV');
      expect(cells[cells.length - 1]).toBe(version(name!));
    }
  });

  it('updates every component and asks after KnightLoader with the one button at its foot', async () => {
    await draw();
    await updateNow();
    expect(asked).toEqual(expect.arrayContaining([`POST ${UPDATE}`, 'GET /api/system/update-check']));
    expect(updateButton('Up to date').className).toContain('bg-statusOkSolid');
    for (const name of ['yt-dlp', 'FFmpeg', 'Deno', 'JDownloader backend']) expect(status(name)).toBe('');
  });

  it('asks nobody on opening while KnightLoader checks nothing on its own', async () => {
    await draw();
    expect(asked).not.toContain(`POST ${UPDATE}`);
    expect(asked).not.toContain('GET /api/system/update-check');

    act(() => root.unmount());
    root = createRoot(host);
    await draw({ autoUpdate: true });
    expect(asked).toContain('GET /api/system/update-check');
    expect(asked).not.toContain(`POST ${UPDATE}`);
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
    await updateNow();
    expect(status('yt-dlp')).toBe('Done');
    expect(status('FFmpeg')).toBe('Failed');
    expect(detail('FFmpeg')).toBe('Nothing was replaced. the archive holds no bin/ffprobe');
    expect(status('Deno')).toBe('');
    expect(status('JDownloader backend')).toBe(en['settings.info.statusWaits']);
    expect(detail('JDownloader backend')).toBe(en['settings.info.statusWaitsHint']);
    expect(updateButton(en['settings.info.updateFailed']).className).toContain('bg-statusFailSolid');
  });

  it('counts an update that waits for downloads, and a newer KnightLoader', async () => {
    answers['/api/system/update-check'] = { checked: true, available: true, current: 'v1.8.4', latest: 'v1.9.0' };
    answers[`POST ${UPDATE}`] = { running: false, results: { jdownloader: { state: 'waits' } } };
    await draw();
    await updateNow();
    expect(updateButton('Updates: 2').className).toContain('bg-statusWarnSolid');
    expect(status('KnightLoader')).toBe('v1.9.0 is available');
  });

  it('follows an update until every component has come to something', async () => {
    answers[`POST ${UPDATE}`] = { running: true, results: { 'yt-dlp': { state: 'checking' } } };
    await draw();
    answers[UPDATE] = { running: true, results: { 'yt-dlp': { state: 'checking' } } };
    await updateNow();
    expect(status('yt-dlp')).toBe(en['settings.look.updatesChecking']);
    expect(updateButton(en['settings.info.updating']).disabled).toBe(true);

    answers[UPDATE] = { running: false, results: { 'yt-dlp': { state: 'updated', version: '2026.09.30' } } };
    answers['/api/mediatools'] = { ytdlp: { found: true, version: '2026.09.30', source: 'managed' }, ffmpeg: { found: true }, ffprobe: { found: true } };
    await act(() => new Promise((resolve) => setTimeout(resolve, 1100)));
    expect(status('yt-dlp')).toBe('Done');
    expect(version('yt-dlp').textContent).toBe('2026.09.30');
    expect(updateButton('Up to date').disabled).toBe(false);
  });

  it('picks up an update that was started before the page opened', async () => {
    answers[UPDATE] = { running: true, results: { ffmpeg: { state: 'checking' } } };
    await draw();
    expect(status('FFmpeg')).toBe(en['settings.look.updatesChecking']);
    expect(updateButton(en['settings.info.updating']).disabled).toBe(true);
    expect(asked).not.toContain(`POST ${UPDATE}`);
  });

  it('turns the button red when KnightLoader could not be asked', async () => {
    answers['/api/system/update-check'] = { checked: false, available: false, current: 'v1.8.4' };
    await draw();
    await updateNow();
    expect(updateButton(en['settings.info.updateFailed']).className).toContain('bg-statusFailSolid');
    expect(status('KnightLoader')).toBe('Check failed');
  });

  it('waits for JDownloader to come back after its update and shows its new revision', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'Date'] });
    answers[`POST ${UPDATE}`] = { running: false, results: { jdownloader: { state: 'updated' } } };
    answers['/api/resolvers/jd'] = { configured: true, reachable: false };
    await draw();
    await updateNow();
    expect(status('JDownloader backend')).toBe('Done');

    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50200 };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(version('JDownloader backend').textContent).toBe('50200');
  });

  it('says where a JDownloader backend that is not set up stands, with nothing to press', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false };
    await draw();
    expect(status('JDownloader backend')).toBe('Not set up');
    expect(group('JDownloader backend').querySelector('button')).toBeNull();
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
    expect(group('yt-dlp').querySelector('button')).toBeNull();
  });

  it('says when the server could not tell how this instance runs, and asks again on request', async () => {
    const deployment = answers['/api/system/deployment'];
    delete answers['/api/system/deployment'];
    await draw();
    expect(card('Components').textContent).toContain('Could not load this.');

    answers['/api/system/deployment'] = deployment;
    await act(async () => button(card('Components'), 'Try again').click());
    expect(card('Components').textContent).not.toContain('Could not load this.');
    expect(version('KnightLoader').textContent).toBe('1.8.4');
  });

  it('gives a container the one switch, which looks for a release and says how the container is updated', async () => {
    const patch = await draw({ autoUpdate: false });
    const panel = group('KnightLoader');
    expect(switches(panel)).toEqual(['Automatically update']);
    const update = panel.querySelector<HTMLButtonElement>('[role="switch"]')!;
    expect(update.getAttribute('aria-checked')).toBe('false');
    expect(update.parentElement!.className).not.toContain('opacity-40');
    const tip = panel.querySelector('[data-glim-label="Automatically update"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateContainerHint']);
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ autoUpdate: true }]]);
  });
});

describe('the release tag of a version', () => {
  it('adds the v a stamp lacks and drops build metadata', () => {
    expect(releaseTag('1.8.4')).toBe('v1.8.4');
    expect(releaseTag('v1.8.4+redesign.59b73a6')).toBe('v1.8.4');
  });
});

describe('installing an update on the desktop build', () => {
  const desktop = () => {
    answers['/api/system/deployment'] = { deployment: 'desktop', canQuit: false, canRestart: false, note: '', engines: [] };
    answers['/api/system/update-check'] = { checked: true, available: true, current: 'v1.8.4', latest: 'v1.9.0' };
  };
  const wait = (ms: number) => act(() => new Promise((resolve) => setTimeout(resolve, ms)));

  it('offers Install only once a check found a newer release', async () => {
    desktop();
    await draw();
    expect(button(group('KnightLoader'), 'Install now')).toBeUndefined();
    await updateNow();
    expect(status('KnightLoader')).toBe('v1.9.0 is available');
    expect(button(group('KnightLoader'), 'Install now')).toBeDefined();
  });

  it('picks up an update that was started before the page opened', async () => {
    desktop();
    answers['/api/system/update-install'] = { state: 'downloading', version: '1.9.0', done: 1, total: 4 };
    await draw();
    expect(status('KnightLoader')).toBe('Downloading the update: 25%');
  });

  it('shows the download, then the installer taking over', async () => {
    desktop();
    await draw();
    await updateNow();
    answers['/api/system/update-install'] = { state: 'downloading', version: '1.9.0', done: 40, total: 100 };
    await act(async () => button(group('KnightLoader'), 'Install now').click());
    expect(status('KnightLoader')).toBe('Downloading the update: 40%');
    expect(button(group('KnightLoader'), 'Installing…').disabled).toBe(true);

    answers['/api/system/update-install'] = { state: 'started', version: '1.9.0' };
    await wait(600);
    expect(status('KnightLoader')).toBe(en['settings.look.updatesInstalling']);
    expect(button(group('KnightLoader'), 'Install now')).toBeUndefined();
  });

  it('says why an update did not work and lets it be tried again', async () => {
    desktop();
    await draw();
    await updateNow();
    answers['/api/system/update-install'] = { state: 'failed', error: 'permission to run it was refused' };
    await act(async () => button(group('KnightLoader'), 'Install now').click());
    expect(status('KnightLoader')).toBe('Failed');
    expect(detail('KnightLoader')).toBe('The update did not work: permission to run it was refused');
    expect(group('KnightLoader').querySelector('[role="status"]')!.className).toContain('text-statusFail');
    expect(button(group('KnightLoader'), 'Install now').disabled).toBe(false);
  });

  it('says a refused request in the same place', async () => {
    desktop();
    refusals['POST /api/system/update-install'] = { status: 409, body: { status: 'an update is already under way' } };
    await draw();
    await updateNow();
    await act(async () => button(group('KnightLoader'), 'Install now').click());
    expect(detail('KnightLoader')).toContain('The update did not work:');
  });

  it('leaves a Flatpak to Flatpak and lets it check on its own', async () => {
    desktop();
    answers['/api/system/deployment'] = {
      deployment: 'desktop',
      canQuit: false,
      canRestart: false,
      note: '',
      engines: [],
      updatesFromStore: true,
    };
    const patch = await draw({ autoUpdate: true });
    expect(asked).not.toContain('GET /api/system/update-install');
    await updateNow();
    expect(status('KnightLoader')).toBe('v1.9.0 is available');
    expect(button(group('KnightLoader'), 'Install now')).toBeUndefined();

    const panel = group('KnightLoader');
    expect(switches(panel)).toEqual(['Automatically update']);
    const update = panel.querySelector<HTMLButtonElement>('[role="switch"]')!;
    expect(update.getAttribute('aria-checked')).toBe('true');
    expect(update.parentElement!.className).not.toContain('opacity-40');
    const tip = panel.querySelector('[data-glim-label="Automatically update"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateStoreHint']);
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ autoUpdate: false }]]);
  });

  it('gives the desktop build the one switch, which looks for updates and installs them', async () => {
    desktop();
    const patch = await draw({ autoUpdate: true });
    const panel = group('KnightLoader');
    expect(switches(panel)).toEqual(['Automatically update']);
    const tip = panel.querySelector('[data-glim-label="Automatically update"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateHint']);
    await act(async () => panel.querySelector<HTMLButtonElement>('[role="switch"]')!.click());
    expect(patch.mock.calls).toEqual([[{ autoUpdate: false }]]);
  });
});

describe('the JDownloader backend the desktop build sets up', () => {
  it('shows in the status how far the Java runtime has come while JDownloader is being set up', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false, setup: 'starting', setupDone: 30, setupTotal: 120 };
    await draw();
    expect(status('JDownloader backend')).toBe('Being set up · 25%');
    expect(detail('JDownloader backend')).toBe('');
    answers['/api/resolvers/jd'] = { configured: false, reachable: false, setup: 'starting', setupDone: 90, setupTotal: 120 };
    await act(() => new Promise((resolve) => setTimeout(resolve, 1100)));
    expect(status('JDownloader backend')).toBe('Being set up · 75%');
  });

  it('says why the setup failed and tries again on request', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false, setup: 'failed', detail: 'downloading the Java runtime: no network' };
    await draw();
    expect(status('JDownloader backend')).toBe('Setup failed');
    expect(detail('JDownloader backend')).toBe('KnightLoader could not set up its JDownloader: downloading the Java runtime: no network');
    answers['POST /api/resolvers/jd/setup'] = { configured: false, reachable: false, setup: 'starting' };
    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50123 };
    await act(async () => button(group('JDownloader backend'), 'Try again').click());
    expect(asked).toContain('POST /api/resolvers/jd/setup');
    expect(status('JDownloader backend')).toBe('Being set up');
    await act(() => new Promise((resolve) => setTimeout(resolve, 1100)));
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
    expect(href('Java')).toBe('https://adoptium.net/temurin/');
    expect(status('Java')).toBe('21.0.14+7 is available');
  });

  it('leaves Java off the card where KnightLoader runs no JDownloader of its own', async () => {
    await draw();
    expect(names()).not.toContain('Java');
  });
});
