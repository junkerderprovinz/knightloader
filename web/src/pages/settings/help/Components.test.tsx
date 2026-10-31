// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { SettingsProvider, type SettingsDraft } from '../context';
import { ComponentsCard, moduleVersion, releaseTag } from './Components';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom lays nothing out and has no ResizeObserver, which the settings window
// uses to place itself.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

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

/** Opens a row's switches and returns the window they stand in. */
async function settingsOf(name: string): Promise<HTMLElement> {
  await act(async () => button(group(name), 'Update settings').click());
  return document.querySelector<HTMLElement>('[role="dialog"]')!;
}
const switches = (within: HTMLElement) =>
  [...within.querySelectorAll('[role="switch"]')].map((s) => s.getAttribute('aria-label'));

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
      'ffmpeg',
      'ffprobe',
      'UnRAR',
      'Gopeed',
      'SQLite',
    ]);
    expect(version('KnightLoader').textContent).toBe('1.8.4');
    expect(version('yt-dlp').textContent).toBe('2026.08.19');
    expect(version('JDownloader backend').textContent).toBe('50123');
    expect(version('ffmpeg').textContent).toBe('8.1.2');
    expect(version('Gopeed').textContent).toBe('5111a4c');
    expect(version('SQLite').textContent).toBe('1.60.1');
    expect(status('UnRAR')).toBe('Not found');
    expect(detail('UnRAR')).toBe(en['settings.resolvers.toolsUnrarMissingHint']);
  });

  it('shows a row as its name and its version, which leads to the release or project page', async () => {
    await draw();
    expect(group('Gopeed').textContent).toBe('Gopeed5111a4c');
    const href = (name: string) => version(name).querySelector('a')?.getAttribute('href');
    expect(href('KnightLoader')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4');
    expect(href('GlimStone')).toMatch(/^https:\/\/github\.com\/junkerderprovinz\/glimstone\/releases\/tag\/v\d+\.\d+\.\d+$/);
    expect(href('yt-dlp')).toBe('https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.19');
    expect(href('JDownloader backend')).toBe('https://jdownloader.org/');
    expect(href('ffmpeg')).toBe('https://ffmpeg.org/');
    expect(href('SQLite')).toBe('https://pkg.go.dev/modernc.org/sqlite@v1.60.1');
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

  it('shows UnRAR with its version and no licence text', async () => {
    answers['/api/mediatools'] = {
      ...(answers['/api/mediatools'] as object),
      unrar: { found: true, version: '7.12', source: 'bundled' },
    };
    await draw();
    expect(group('UnRAR').textContent).toBe('UnRAR7.12');
    expect(version('UnRAR').querySelector('a')?.getAttribute('href')).toBe('https://www.rarlab.com/');
    expect(card('Components').querySelector('p')).toBeNull();
  });

  it('says what each tool does behind an (i) beside its name', async () => {
    await draw();
    const tip = (name: string) => group(name).querySelector('h3 [role="note"]')?.getAttribute('aria-label');
    expect(tip('yt-dlp')).toContain('Reads video pages');
    expect(tip('JDownloader backend')).toContain('JDownloader also updates itself');
    expect(tip('ffmpeg')).toBe('Joins picture and sound');
    expect(tip('SQLite')).toBe('Keeps the download list');
    expect(group('ffmpeg').textContent).not.toContain('Joins picture and sound');
  });

  it('gives only what updates on its own buttons, and keeps them to glyphs', async () => {
    await draw();
    const pressable = [...host.querySelectorAll('section')]
      .filter((s) => s.querySelector('button'))
      .map((s) => s.querySelector('h3')?.textContent);
    expect(pressable).toEqual(['KnightLoader', 'yt-dlp', 'JDownloader backend']);
    for (const b of card('Components').querySelectorAll('section button')) expect(b.textContent).toBe('');
    expect(card('Components').querySelector('[role="switch"]')).toBeNull();
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
    await act(async () => button(group('JDownloader backend'), 'Check for updates').click());
    expect(button(group('JDownloader backend'), 'Updates: 1').className).toContain('bg-statusWarnSolid');
    expect(button(group('JDownloader backend'), 'Update now')).toBeDefined();
  });

  it('says JDownloader is up to date when it has nothing waiting', async () => {
    answers['/api/jd/update'] = { available: false, checked: true };
    await draw();
    await act(async () => button(group('JDownloader backend'), 'Check for updates').click());
    expect(button(group('JDownloader backend'), 'Up to date').className).toContain('bg-statusOkSolid');
    expect(button(group('JDownloader backend'), 'Update now')).toBeUndefined();
  });

  it('says why JDownloader could not be asked', async () => {
    refusals['/api/jd/update'] = { status: 409, body: { error: 'jd unreachable', code: 'jdUnreachable' } };
    await draw();
    await act(async () => button(group('JDownloader backend'), 'Check for updates').click());
    expect(button(group('JDownloader backend'), 'Check failed').className).toContain('bg-statusFailSolid');
    expect(status('JDownloader backend')).toBe('Failed');
    expect(detail('JDownloader backend')).toBe(en['health.remedy.jd.unreachable']);
  });

  it('waits for JDownloader to come back after the update and shows its new revision', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'Date'] });
    answers['/api/jd/update'] = { available: true, checked: true };
    await draw();
    await act(async () => button(group('JDownloader backend'), 'Check for updates').click());

    answers['/api/jd/update'] = { started: true };
    await act(async () => button(group('JDownloader backend'), 'Update now').click());
    expect(asked.filter((u) => u === '/api/jd/update')).toHaveLength(2);
    expect(detail('JDownloader backend')).toBe(en['settings.look.jdRestarting']);
    expect(button(group('JDownloader backend'), 'Updating').disabled).toBe(true);

    // Still the old one, before it goes down.
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(detail('JDownloader backend')).toBe(en['settings.look.jdRestarting']);

    answers['/api/resolvers/jd'] = { configured: true, reachable: false };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(detail('JDownloader backend')).toBe(en['settings.look.jdRestarting']);

    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50200 };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(status('JDownloader backend')).toBe('Done');
    expect(detail('JDownloader backend')).toBe('JDownloader is running again, at revision 50200.');
    expect(version('JDownloader backend').textContent).toBe('50200');
    expect(button(group('JDownloader backend'), 'Update now')).toBeUndefined();
    vi.useRealTimers();
  });

  it('names the update switches the same for every component', async () => {
    await draw();
    expect(switches(await settingsOf('KnightLoader'))).toEqual(['Check automatically', 'Update automatically']);
    await act(async () => button(document.querySelector('[role="dialog"]')!, 'Close').click());
    expect(switches(await settingsOf('yt-dlp'))).toEqual(['Check automatically', 'Update automatically']);
    await act(async () => button(document.querySelector('[role="dialog"]')!, 'Close').click());
    expect(switches(await settingsOf('JDownloader backend'))).toEqual(['Update automatically']);
  });

  it('opens the switches under their gear', async () => {
    await draw();
    const panel = await settingsOf('yt-dlp');
    expect(panel.getAttribute('aria-label')).toBe('yt-dlp');
  });

  it("writes the JDownloader backend's switch to its own setting", async () => {
    const patch = await draw();
    const panel = await settingsOf('JDownloader backend');
    await act(async () => panel.querySelector<HTMLButtonElement>('[role="switch"]')!.click());
    expect(patch.mock.calls).toEqual([[{ jdAutoUpdate: true }]]);
  });

  it('greys out both KnightLoader switches in a container and says the container manager updates it', async () => {
    const patch = await draw({ autoUpdateCheck: true, autoUpdate: true });
    const panel = await settingsOf('KnightLoader');
    const rows = [...panel.querySelectorAll<HTMLButtonElement>('[role="switch"]')];
    expect(rows.map((r) => r.getAttribute('aria-checked'))).toEqual(['false', 'false']);
    expect(rows.map((r) => r.parentElement!.className)).toEqual([
      expect.stringContaining('opacity-40'),
      expect.stringContaining('opacity-40'),
    ]);
    const tip = panel.querySelector('[data-glim-label="Check automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoContainerHint']);
    await act(async () => rows[0].click());
    expect(patch).not.toHaveBeenCalled();
    expect(button(group('KnightLoader'), 'Check for updates').disabled).toBe(false);
  });

  it("writes yt-dlp's switches to yt-dlp's own settings", async () => {
    const patch = await draw();
    const panel = await settingsOf('yt-dlp');
    const [check, update] = panel.querySelectorAll<HTMLButtonElement>('[role="switch"]');
    await act(async () => check.click());
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ ytdlpVersionCheck: true }], [{ ytdlpAutoUpdate: true }]]);
  });

  it('turns the check green when nothing is newer', async () => {
    await draw();
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
    expect(button(group('KnightLoader'), 'Up to date').className).toContain('bg-statusOkSolid');
    expect(status('KnightLoader')).toBe('');
  });

  it('lets the check button alone say that yt-dlp is current', async () => {
    await draw();
    await act(async () => button(group('yt-dlp'), 'Check for updates').click());
    expect(button(group('yt-dlp'), 'Up to date').className).toContain('bg-statusOkSolid');
    expect(status('yt-dlp')).toBe('');
  });

  it('turns the check red when GitHub cannot be asked', async () => {
    answers['/api/system/update-check'] = { checked: false, available: false, current: 'v1.8.4' };
    await draw();
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
    expect(button(group('KnightLoader'), 'Check failed').className).toContain('bg-statusFailSolid');
    expect(status('KnightLoader')).toBe('Check failed');
  });

  it('offers a newer yt-dlp to fetch once a check found one', async () => {
    answers['/api/mediatools/ytdlp/latest'] = { checked: true, tag: '2026.09.30', compare: 'newer' };
    await draw();
    expect(button(group('yt-dlp'), 'Fetch this release')).toBeUndefined();
    await act(async () => button(group('yt-dlp'), 'Check for updates').click());
    expect(button(group('yt-dlp'), 'Updates: 1').className).toContain('bg-statusWarnSolid');
    expect(status('yt-dlp')).toBe('2026.09.30 is available');
    expect(detail('yt-dlp')).toBe('2026.09.30 is published, 2026.08.19 is installed.');
    expect(button(group('yt-dlp'), 'Fetch this release')).toBeDefined();
  });

  it('says the fetched copy is gone after a revert that leaves no yt-dlp behind', async () => {
    answers['/api/mediatools'] = {
      ytdlp: { found: true, version: '2026.09.30', source: 'managed', path: '/config/bin/yt-dlp' },
      managed: { version: '2026.09.30' },
      ffmpeg: { found: true, version: '8.1.2' },
      ffprobe: { found: true, version: '8.1.2' },
    };
    answers['/api/mediatools/ytdlp/revert'] = {
      ytdlp: { found: false, source: 'none' },
      ffmpeg: { found: true, version: '8.1.2' },
      ffprobe: { found: true, version: '8.1.2' },
    };
    await draw();
    await act(async () => button(group('yt-dlp'), 'Back to the system copy').click());
    expect(detail('yt-dlp')).toBe(en['settings.resolvers.toolsRevertedNone']);
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

  it('asks GitHub on opening only for yt-dlp, and only while its switch is on', async () => {
    await draw();
    expect(asked).not.toContain('/api/system/update-check');
    expect(asked).not.toContain('/api/mediatools/ytdlp/latest');

    act(() => root.unmount());
    root = createRoot(host);
    await draw({ autoUpdateCheck: true, ytdlpVersionCheck: true });
    expect(asked).not.toContain('/api/system/update-check');
    expect(asked).toContain('/api/mediatools/ytdlp/latest');
  });
});

describe('the release tag of a version', () => {
  it('adds the v a stamp lacks and drops build metadata', () => {
    expect(releaseTag('1.8.4')).toBe('v1.8.4');
    expect(releaseTag('v1.8.4+redesign.59b73a6')).toBe('v1.8.4');
  });
});

describe('the version of a built-in library', () => {
  it('drops the v of a release', () => {
    expect(moduleVersion('v1.60.1')).toBe('1.60.1');
  });

  it('names the commit a module was taken at without a release', () => {
    expect(moduleVersion('v0.0.0-20261009133625-a4bedf5f6000')).toBe('a4bedf5');
    expect(moduleVersion('v1.61.1-0.20260823090931-70072f1a7460')).toBe('70072f1');
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
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
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
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
    answers['/api/system/update-install'] = { state: 'downloading', version: '1.9.0', done: 40, total: 100 };
    await act(async () => button(group('KnightLoader'), 'Install now').click());
    expect(status('KnightLoader')).toBe('Downloading the update: 40%');
    expect(button(group('KnightLoader'), 'Installing…').disabled).toBe(true);

    answers['/api/system/update-install'] = { state: 'started', version: '1.9.0' };
    await wait(600);
    expect(detail('KnightLoader')).toContain('The installer is running.');
    expect(button(group('KnightLoader'), 'Install now')).toBeUndefined();
  });

  it('says why an update did not work and lets it be tried again', async () => {
    desktop();
    await draw();
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
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
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
    await act(async () => button(group('KnightLoader'), 'Install now').click());
    expect(detail('KnightLoader')).toContain('The update did not work:');
  });
});

describe('the JDownloader backend the desktop build sets up', () => {
  it('shows the Java runtime arriving while JDownloader is being set up', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false, setup: 'starting', setupDone: 30, setupTotal: 120 };
    await draw();
    expect(status('JDownloader backend')).toBe('Being set up');
    expect(detail('JDownloader backend')).toBe('Downloading the Java runtime for JDownloader: 25%');
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

  it('lists the Java runtime it fetched beside the other built-in parts', async () => {
    answers['/api/resolvers/jd'] = {
      configured: true,
      reachable: true,
      version: 50123,
      runtime: { vendor: 'Eclipse Temurin', version: '21.0.13+9' },
    };
    await draw();
    expect(version('Java (Eclipse Temurin)').textContent).toBe('21.0.13+9');
  });
});
