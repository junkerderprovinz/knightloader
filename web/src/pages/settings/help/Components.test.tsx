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

/** What the server answers, by route; a test changes a route to change the answer. */
let answers: Record<string, unknown>;
/** Routes the server refuses, with the status and body it refuses with. */
let refusals: Record<string, { status: number; body: unknown }>;
let asked: string[];

beforeEach(() => {
  answers = {
    '/api/health': { status: 'ok', version: 'v1.8.4' },
    '/api/mediatools': {
      ytdlp: { found: true, version: '2026.08.19', source: 'path' },
      ffmpeg: { found: true, version: '8.1.2' },
      ffprobe: { found: true, version: '8.1.2' },
    },
    '/api/system/deployment': {
      deployment: 'container',
      canQuit: false,
      canRestart: false,
      note: '',
      engines: [
        { path: 'github.com/GopeedLab/gopeed', version: 'v0.0.0-20261005140626-5111a4c57695', fork: true },
        { path: 'modernc.org/sqlite', version: 'v1.60.1' },
      ],
    },
    '/api/resolvers/jd': { configured: true, reachable: true, version: 50123 },
    '/api/system/update-check': { checked: true, available: false, current: 'v1.8.4' },
    '/api/mediatools/ytdlp/latest': { checked: true, tag: '2026.08.19', compare: 'same' },
    '/api/jd/update': { checked: true, available: false },
  };
  refusals = {};
  asked = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      asked.push(url);
      const refused = refusals[url];
      if (refused) return Promise.resolve(new Response(JSON.stringify(refused.body), { status: refused.status }));
      return Promise.resolve(
        url in answers
          ? new Response(JSON.stringify(answers[url]), { headers: { 'Content-Type': 'application/json' } })
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
});

const BASE = {
  autoUpdateCheck: false,
  autoUpdate: false,
  ytdlpVersionCheck: false,
  ytdlpAutoUpdate: false,
  jdAutoUpdate: false,
  jdVersionCheck: false,
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

/** The row whose switches stand under its name. */
const settingsOf = (name: string): HTMLElement => group(name);
const switches = (within: HTMLElement) =>
  [...within.querySelectorAll('[role="switch"]')].map((s) => s.getAttribute('aria-label'));

/** The one button at the card's foot that checks every component, by what it says now. */
const checkButton = (face: string) => button(card('Components'), face);
const checkAll = () => act(async () => checkButton('Check all').click());

/** The card titled `title`. */
const card = (title: string) =>
  [...host.querySelectorAll<HTMLElement>('.glim-card')].find((c) => c.textContent?.startsWith(title))!;

describe('the Components card', () => {
  it('lists every component, each with its version', async () => {
    await draw();
    const rows = [...host.querySelectorAll('section')].map((s) => s.querySelector('h3')?.textContent);
    expect(rows).toEqual([
      'KnightLoader',
      'yt-dlp',
      'JDownloader backend',
      'GlimStone',
    ]);
    expect(version('KnightLoader').textContent).toBe('1.8.4');
    expect(version('yt-dlp').textContent).toBe('2026.08.19');
    expect(version('JDownloader backend').textContent).toBe('50123');
  });

  it('shows a row as its name and its version, which leads to the release or project page', async () => {
    await draw();
    const href = (name: string) => version(name).querySelector('a')?.getAttribute('href');
    expect(href('KnightLoader')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4');
    expect(href('GlimStone')).toMatch(/^https:\/\/github\.com\/junkerderprovinz\/glimstone\/releases\/tag\/v\d+\.\d+\.\d+$/);
    expect(href('yt-dlp')).toBe('https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.19');
    expect(href('JDownloader backend')).toBe('https://jdownloader.org/');
  });

  it('links a branch build to the release it was cut from', async () => {
    answers['/api/health'] = { status: 'ok', version: 'v1.8.4+redesign.59b73a6' };
    await draw();
    expect(version('KnightLoader').textContent).toBe('1.8.4+redesign.59b73a6');
    expect(version('KnightLoader').querySelector('a')?.getAttribute('href')).toBe(
      'https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4',
    );
  });

  it('leads a stamp without a release to the repository', async () => {
    answers['/api/health'] = { status: 'ok', version: 'preview-59b73a6' };
    await draw();
    expect(version('KnightLoader').textContent).toBe('preview-59b73a6');
    expect(version('KnightLoader').querySelector('a')?.getAttribute('href')).toBe('https://github.com/junkerderprovinz/knightloader');
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
    expect(rowsWith('[role="switch"]')).toEqual(['KnightLoader', 'yt-dlp', 'JDownloader backend']);
  });

  it('checks every component with the one button at its foot', async () => {
    await draw();
    await checkAll();
    expect(asked).toEqual(
      expect.arrayContaining(['/api/system/update-check', '/api/mediatools/ytdlp/latest', '/api/jd/update']),
    );
    expect(checkButton('Up to date').className).toContain('bg-statusOkSolid');
  });

  it('says where a JDownloader backend that is not set up stands, with nothing to press', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false };
    await draw();
    expect(status('JDownloader backend')).toBe('Not set up');
    expect(group('JDownloader backend').querySelector('button')).toBeNull();
  });

  it('offers a JDownloader update once a check found one', async () => {
    answers['/api/jd/update'] = { available: true, checked: true };
    await draw();
    expect(button(group('JDownloader backend'), 'Update now')).toBeUndefined();
    await checkAll();
    expect(checkButton('Updates: 1').className).toContain('bg-statusWarnSolid');
    expect(status('JDownloader backend')).toBe('Updates: 1');
    expect(button(group('JDownloader backend'), 'Update now')).toBeDefined();
  });

  it('says JDownloader is up to date when it has nothing waiting', async () => {
    answers['/api/jd/update'] = { available: false, checked: true };
    await draw();
    await checkAll();
    expect(checkButton('Up to date').className).toContain('bg-statusOkSolid');
    expect(button(group('JDownloader backend'), 'Update now')).toBeUndefined();
  });

  it('says why JDownloader could not be asked', async () => {
    refusals['/api/jd/update'] = { status: 409, body: { error: 'jd unreachable', code: 'jdUnreachable' } };
    await draw();
    await checkAll();
    expect(checkButton('Check failed').className).toContain('bg-statusFailSolid');
    expect(status('JDownloader backend')).toBe('Failed');
    expect(detail('JDownloader backend')).toBe(en['health.remedy.jd.unreachable']);
  });

  it('waits for JDownloader to come back after the update and shows its new revision', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'Date'] });
    answers['/api/jd/update'] = { available: true, checked: true };
    await draw();
    await checkAll();

    answers['/api/jd/update'] = { started: true };
    await act(async () => button(group('JDownloader backend'), 'Update now').click());
    expect(asked.filter((u) => u === '/api/jd/update')).toHaveLength(2);
    expect(status('JDownloader backend')).toBe(en['settings.look.jdUpdating']);
    expect(button(group('JDownloader backend'), 'Updating').disabled).toBe(true);

    // Still the old one, before it goes down.
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(status('JDownloader backend')).toBe(en['settings.look.jdUpdating']);

    answers['/api/resolvers/jd'] = { configured: true, reachable: false };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(status('JDownloader backend')).toBe(en['settings.look.jdUpdating']);

    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50200 };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(status('JDownloader backend')).toBe('Done');
    expect(detail('JDownloader backend')).toBe('');
    expect(version('JDownloader backend').textContent).toBe('50200');
    expect(button(group('JDownloader backend'), 'Update now')).toBeUndefined();
    vi.useRealTimers();
  });

  it('names the update switches the same for every component, with one for the app itself', async () => {
    await draw();
    expect(switches(settingsOf('KnightLoader'))).toEqual(['Update automatically']);
    expect(switches(settingsOf('yt-dlp'))).toEqual(['Check automatically', 'Update automatically']);
    expect(switches(settingsOf('JDownloader backend'))).toEqual(['Check automatically', 'Update automatically']);
  });

  it('puts the switches under the name, with no window of their own', async () => {
    await draw();
    const row = group('yt-dlp');
    expect(row.querySelector('h3')!.compareDocumentPosition(row.querySelector('[role="switch"]')!)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(button(row, 'Update settings')).toBeUndefined();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  it('puts every version last in its row, so they line up at the right edge', async () => {
    await draw();
    for (const name of ['KnightLoader', 'yt-dlp', 'JDownloader backend', 'GlimStone']) {
      const cells = [...group(name).children].filter((c) => c.tagName !== 'DIV');
      expect(cells[cells.length - 1]).toBe(version(name));
    }
  });

  it("writes the JDownloader backend's switches to its own settings", async () => {
    const patch = await draw();
    const [check, update] = settingsOf('JDownloader backend').querySelectorAll<HTMLButtonElement>('[role="switch"]');
    await act(async () => check.click());
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ jdVersionCheck: true }], [{ jdAutoUpdate: true }]]);
  });

  it('shows an update the server found for JDownloader on its own, ready to install', async () => {
    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50123, updateWaits: true };
    await draw();
    expect(status('JDownloader backend')).toBe('Updates: 1');
    expect(button(group('JDownloader backend'), 'Update now')).toBeDefined();
  });

  it("gives a container one switch, which looks for a newer release and leaves the update to the container's manager", async () => {
    const patch = await draw({ autoUpdateCheck: false, autoUpdate: true });
    const panel = settingsOf('KnightLoader');
    const [update, ...rest] = panel.querySelectorAll<HTMLButtonElement>('[role="switch"]');
    expect(rest).toEqual([]);
    expect(update.getAttribute('aria-checked')).toBe('false');
    expect(update.parentElement!.className).not.toContain('opacity-40');
    const tip = panel.querySelector('[data-glim-label="Update automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateContainerHint']);
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ autoUpdateCheck: true }]]);
  });

  it("writes yt-dlp's switches to yt-dlp's own settings", async () => {
    const patch = await draw();
    const panel = settingsOf('yt-dlp');
    const [check, update] = panel.querySelectorAll<HTMLButtonElement>('[role="switch"]');
    await act(async () => check.click());
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ ytdlpVersionCheck: true }], [{ ytdlpAutoUpdate: true }]]);
  });

  it('says nothing on a row whose component is current', async () => {
    await draw();
    await checkAll();
    expect(status('KnightLoader')).toBe('');
    expect(status('yt-dlp')).toBe('');
    expect(status('JDownloader backend')).toBe('');
  });

  it('turns the check red when GitHub cannot be asked', async () => {
    answers['/api/system/update-check'] = { checked: false, available: false, current: 'v1.8.4' };
    await draw();
    await checkAll();
    expect(checkButton('Check failed').className).toContain('bg-statusFailSolid');
    expect(status('KnightLoader')).toBe('Check failed');
  });

  it('offers a newer yt-dlp to fetch once a check found one', async () => {
    answers['/api/mediatools/ytdlp/latest'] = { checked: true, tag: '2026.09.30', compare: 'newer' };
    await draw();
    expect(button(group('yt-dlp'), 'Fetch this release')).toBeUndefined();
    await checkAll();
    expect(checkButton('Updates: 1').className).toContain('bg-statusWarnSolid');
    expect(status('yt-dlp')).toBe('2026.09.30 is available');
    expect(detail('yt-dlp')).toBe('');
    expect(button(group('yt-dlp'), 'Fetch this release')).toBeDefined();
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
    expect(button(group('yt-dlp'), 'Back to the system copy')).toBeUndefined();
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

  it('asks GitHub on opening only while the check switches are on', async () => {
    await draw();
    expect(asked).not.toContain('/api/health/detail');
    expect(asked).not.toContain('/api/mediatools/ytdlp/latest');

    act(() => root.unmount());
    root = createRoot(host);
    await draw({ autoUpdateCheck: true, ytdlpVersionCheck: true });
    expect(asked).toContain('/api/mediatools/ytdlp/latest');
  });

  it("shows a newer release the daily check found on KnightLoader's row without asking GitHub again", async () => {
    answers['/api/health/detail'] = {
      status: 'ok',
      subsystems: [],
      volumes: [],
      release: { version: 'v2.8.0', url: 'https://github.com/junkerderprovinz/knightloader/releases/tag/v2.8.0' },
    };
    await draw({ autoUpdateCheck: true });
    expect(status('KnightLoader')).toBe('KnightLoader v2.8.0 is out');
    expect(asked).not.toContain('/api/system/update-check');
  });

  it("says nothing of the daily check's find while the switch is off", async () => {
    answers['/api/health/detail'] = {
      status: 'ok',
      subsystems: [],
      volumes: [],
      release: { version: 'v2.8.0', url: 'https://github.com/junkerderprovinz/knightloader/releases/tag/v2.8.0' },
    };
    await draw({ autoUpdateCheck: false });
    expect(status('KnightLoader')).toBe('');
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
    await checkAll();
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
    await checkAll();
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
    await checkAll();
    answers['/api/system/update-install'] = { state: 'failed', error: 'permission to run it was refused' };
    await act(async () => button(group('KnightLoader'), 'Install now').click());
    expect(status('KnightLoader')).toBe('Failed');
    expect(detail('KnightLoader')).toBe('The update did not work: permission to run it was refused');
    expect(group('KnightLoader').querySelector('[role="status"]')!.className).toContain('text-statusFail');
    expect(button(group('KnightLoader'), 'Install now').disabled).toBe(false);
  });

  it('says a refused request in the same place', async () => {
    desktop();
    refusals['/api/system/update-install'] = { status: 409, body: { status: 'an update is already under way' } };
    await draw();
    await checkAll();
    await act(async () => button(group('KnightLoader'), 'Install now').click());
    expect(detail('KnightLoader')).toContain('The update did not work:');
  });

  it('gives the desktop build one switch, which updates it in the background', async () => {
    desktop();
    const patch = await draw({ autoUpdateCheck: true, autoUpdate: false });
    const panel = settingsOf('KnightLoader');
    const [update, ...rest] = panel.querySelectorAll<HTMLButtonElement>('[role="switch"]');
    expect(rest).toEqual([]);
    expect(update.getAttribute('aria-checked')).toBe('false');
    const tip = panel.querySelector('[data-glim-label="Update automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateHint']);
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ autoUpdate: true }]]);
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
    expect(asked).not.toContain('/api/system/update-install');
    await checkAll();
    expect(status('KnightLoader')).toBe('v1.9.0 is available');
    expect(button(group('KnightLoader'), 'Install now')).toBeUndefined();

    const panel = settingsOf('KnightLoader');
    const [update, ...rest] = panel.querySelectorAll<HTMLButtonElement>('[role="switch"]');
    expect(rest).toEqual([]);
    expect(update.getAttribute('aria-checked')).toBe('false');
    expect(update.parentElement!.className).not.toContain('opacity-40');
    const tip = panel.querySelector('[data-glim-label="Update automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoUpdateStoreHint']);
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ autoUpdateCheck: true }]]);
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
    answers['/api/resolvers/jd/setup'] = { configured: false, reachable: false, setup: 'starting' };
    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50123 };
    await act(async () => button(group('JDownloader backend'), 'Try again').click());
    expect(asked).toContain('/api/resolvers/jd/setup');
    expect(status('JDownloader backend')).toBe('Being set up');
    await act(() => new Promise((resolve) => setTimeout(resolve, 1100)));
    expect(version('JDownloader backend').textContent).toBe('50123');
  });

  it('keeps the Java runtime it fetched off the card, where the JDownloader row speaks for it', async () => {
    answers['/api/resolvers/jd'] = {
      configured: true,
      reachable: true,
      version: 50123,
      runtime: { vendor: 'Eclipse Temurin', version: '21.0.13+9' },
    };
    await draw();
    const names = [...host.querySelectorAll('section h3')].map((h) => h.textContent);
    expect(names.some((n) => n?.startsWith('Java'))).toBe(false);
  });
});
