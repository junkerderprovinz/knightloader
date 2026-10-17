// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { SettingsProvider, type SettingsDraft } from '../context';
import { BuiltInCard, UpdatesCard, moduleVersion } from './Updates';

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
            <UpdatesCard hue={0} />
            <BuiltInCard hue={1} />
          </SettingsProvider>
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
  return patch;
}

/** The group under a component's heading. */
const group = (name: string) =>
  [...host.querySelectorAll('section')].find((s) => s.querySelector('h3')?.textContent === name)!;
const button = (within: HTMLElement, words: string) =>
  [...within.querySelectorAll('button')].find((b) => b.textContent?.includes(words))!;
const switches = (within: HTMLElement) =>
  [...within.querySelectorAll('[role="switch"]')].map((s) => s.getAttribute('aria-label'));

/** The card titled `title`. */
const card = (title: string) =>
  [...host.querySelectorAll<HTMLElement>('.glim-card')].find((c) => c.textContent?.startsWith(title))!;

describe('the Updates card', () => {
  it('lists every tool KnightLoader brings, each with its version', async () => {
    await draw();
    const rows = [...host.querySelectorAll('section')].map((s) => s.querySelector('h3')?.textContent);
    expect(rows).toEqual(['KnightLoader', 'yt-dlp', 'JDownloader backend', 'ffmpeg', 'ffprobe', 'Gopeed', 'SQLite']);
    expect(group('KnightLoader').textContent).toContain('1.8.4');
    expect(group('yt-dlp').textContent).toContain('2026.08.19');
    expect(group('JDownloader backend').textContent).toContain('50123');
    expect(group('ffmpeg').textContent).toContain('8.1.2');
    expect(group('ffmpeg').textContent).toContain('Comes with the container image');
    expect(group('Gopeed').textContent).toContain('5111a4c');
    expect(group('Gopeed').textContent).toContain('Built into KnightLoader from its own fork');
    expect(group('SQLite').textContent).toContain('1.60.1');
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

  it('holds only what updates on its own, each on a sub-card, and the rest on a card without controls', async () => {
    await draw();
    const names = (c: HTMLElement) => [...c.querySelectorAll('section')].map((s) => s.querySelector('h3')?.textContent);
    expect(names(card('Updates'))).toEqual(['KnightLoader', 'yt-dlp', 'JDownloader backend']);
    expect(card('Updates').querySelectorAll('.glim-subcard')).toHaveLength(3);
    expect(names(card('Built-in components'))).toEqual(['ffmpeg', 'ffprobe', 'Gopeed', 'SQLite']);
    expect(card('Built-in components').querySelector('button, [role="switch"]')).toBeNull();
  });

  it('says where a JDownloader backend that is not set up stands, with nothing to press', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false };
    await draw();
    expect(group('JDownloader backend').textContent).toContain('Not set up');
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
    expect(group('JDownloader backend').textContent).toContain(en['health.remedy.jd.unreachable']);
  });

  it('waits for JDownloader to come back after the update and shows its new revision', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'Date'] });
    answers['/api/jd/update'] = { available: true, checked: true };
    await draw();
    await act(async () => button(group('JDownloader backend'), 'Check for updates').click());

    answers['/api/jd/update'] = { started: true };
    await act(async () => button(group('JDownloader backend'), 'Update now').click());
    expect(asked.filter((u) => u === '/api/jd/update')).toHaveLength(2);
    expect(group('JDownloader backend').textContent).toContain(en['settings.look.jdRestarting']);
    expect(button(group('JDownloader backend'), 'Updating').disabled).toBe(true);

    // Still the old one, before it goes down.
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(group('JDownloader backend').textContent).toContain(en['settings.look.jdRestarting']);

    answers['/api/resolvers/jd'] = { configured: true, reachable: false };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(group('JDownloader backend').textContent).toContain(en['settings.look.jdRestarting']);

    answers['/api/resolvers/jd'] = { configured: true, reachable: true, version: 50200 };
    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(group('JDownloader backend').textContent).not.toContain(en['settings.look.jdRestarting']);
    expect(group('JDownloader backend').textContent).toContain('JDownloader is running again, at revision 50200.');
    expect(button(group('JDownloader backend'), 'Update now')).toBeUndefined();
    vi.useRealTimers();
  });

  it('names the update switches the same on every sub-card', async () => {
    await draw();
    expect(switches(group('KnightLoader'))).toEqual(['Check automatically', 'Update automatically']);
    expect(switches(group('yt-dlp'))).toEqual(['Check automatically', 'Update automatically']);
    expect(switches(group('JDownloader backend'))).toEqual(['Update automatically']);
  });

  it("writes the JDownloader backend's switch to its own setting", async () => {
    const patch = await draw();
    await act(async () => group('JDownloader backend').querySelector<HTMLButtonElement>('[role="switch"]')!.click());
    expect(patch.mock.calls).toEqual([[{ jdAutoUpdate: true }]]);
  });

  it('greys out both KnightLoader switches in a container and says the container manager updates it', async () => {
    const patch = await draw({ autoUpdateCheck: true, autoUpdate: true });
    const rows = [...group('KnightLoader').querySelectorAll<HTMLButtonElement>('[role="switch"]')];
    expect(rows.map((r) => r.getAttribute('aria-checked'))).toEqual(['false', 'false']);
    expect(rows.map((r) => r.parentElement!.className)).toEqual([
      expect.stringContaining('opacity-40'),
      expect.stringContaining('opacity-40'),
    ]);
    const tip = group('KnightLoader').querySelector('[data-glim-label="Check automatically"] [role="note"]');
    expect(tip?.getAttribute('aria-label')).toBe(en['settings.look.updatesAutoContainerHint']);
    await act(async () => rows[0].click());
    expect(patch).not.toHaveBeenCalled();
    expect(button(group('KnightLoader'), 'Check for updates').disabled).toBe(false);
  });

  it("writes yt-dlp's switches to yt-dlp's own settings", async () => {
    const patch = await draw();
    const [check, update] = group('yt-dlp').querySelectorAll<HTMLButtonElement>('[role="switch"]');
    await act(async () => check.click());
    await act(async () => update.click());
    expect(patch.mock.calls).toEqual([[{ ytdlpVersionCheck: true }], [{ ytdlpAutoUpdate: true }]]);
  });

  it('turns the check green when nothing is newer', async () => {
    await draw();
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
    const verdict = button(group('KnightLoader'), 'Up to date');
    expect(verdict.className).toContain('bg-statusOkSolid');
  });

  it('lets the check button alone say that yt-dlp is current', async () => {
    await draw();
    await act(async () => button(group('yt-dlp'), 'Check for updates').click());
    expect(button(group('yt-dlp'), 'Up to date').className).toContain('bg-statusOkSolid');
    expect(group('yt-dlp').querySelector('p')).toBeNull();
    expect(group('yt-dlp').textContent).not.toContain('Comes with the container image');
  });

  it('turns the check red when GitHub cannot be asked', async () => {
    answers['/api/system/update-check'] = { checked: false, available: false, current: 'v1.8.4' };
    await draw();
    await act(async () => button(group('KnightLoader'), 'Check for updates').click());
    expect(button(group('KnightLoader'), 'Check failed').className).toContain('bg-statusFailSolid');
  });

  it('offers a newer yt-dlp to fetch once a check found one', async () => {
    answers['/api/mediatools/ytdlp/latest'] = { checked: true, tag: '2026.09.30', compare: 'newer' };
    await draw();
    expect(button(group('yt-dlp'), 'Fetch this release')).toBeUndefined();
    await act(async () => button(group('yt-dlp'), 'Check for updates').click());
    expect(button(group('yt-dlp'), 'Updates: 1').className).toContain('bg-statusWarnSolid');
    expect(group('yt-dlp').textContent).toContain('2026.09.30 is published, 2026.08.19 is installed.');
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
    expect(group('yt-dlp').textContent).toContain(en['settings.resolvers.toolsRevertedNone']);
  });

  it('says when the server could not tell how this instance runs, and asks again on request', async () => {
    const deployment = answers['/api/system/deployment'];
    delete answers['/api/system/deployment'];
    await draw();
    expect(card('Updates').textContent).toContain('Could not load this.');
    expect(card('Built-in components').textContent).toContain('Could not load this.');

    answers['/api/system/deployment'] = deployment;
    await act(async () => button(card('Updates'), 'Try again').click());
    expect(card('Updates').textContent).not.toContain('Could not load this.');
    expect(group('KnightLoader').textContent).toContain('1.8.4');
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

describe('the version of a built-in library', () => {
  it('drops the v of a release', () => {
    expect(moduleVersion('v1.60.1')).toBe('1.60.1');
  });

  it('names the commit a module was taken at without a release', () => {
    expect(moduleVersion('v0.0.0-20261009133625-a4bedf5f6000')).toBe('a4bedf5');
    expect(moduleVersion('v1.61.1-0.20260823090931-70072f1a7460')).toBe('70072f1');
  });
});
