// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { SettingsProvider, type SettingsDraft } from '../context';
import { UpdatesCard, moduleVersion } from './Updates';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

/** What the server answers, by route; a test changes a route to change the answer. */
let answers: Record<string, unknown>;
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
  asked = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      asked.push(url);
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

const BASE = { autoUpdateCheck: false, autoUpdate: false, ytdlpVersionCheck: false, ytdlpAutoUpdate: false } as Settings;

async function draw(cfg: Partial<Settings> = {}) {
  const patch = vi.fn();
  const draft = { cfg: { ...BASE, ...cfg }, patch } as unknown as SettingsDraft;
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
            <UpdatesCard hue={0} />
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

describe('the Updates card', () => {
  it('lists every tool KnightLoader brings, each with its version and where it comes from', async () => {
    await draw();
    const rows = [...host.querySelectorAll('section')].map((s) => s.querySelector('h3')?.textContent);
    expect(rows).toEqual(['KnightLoader', 'yt-dlp', 'ffmpeg', 'ffprobe', 'JDownloader backend', 'Gopeed', 'SQLite']);
    expect(group('KnightLoader').textContent).toContain('1.8.4');
    expect(group('yt-dlp').textContent).toContain('2026.08.19');
    expect(group('ffmpeg').textContent).toContain('8.1.2');
    expect(group('ffmpeg').textContent).toContain('Comes with the container image');
    expect(group('JDownloader backend').textContent).toContain('50123');
    expect(group('JDownloader backend').textContent).toContain('Updates itself');
    expect(group('Gopeed').textContent).toContain('5111a4c');
    expect(group('Gopeed').textContent).toContain('Built into KnightLoader from its own fork');
    expect(group('SQLite').textContent).toContain('1.60.1');
  });

  it('offers update controls only for the tools KnightLoader updates', async () => {
    await draw();
    for (const name of ['ffmpeg', 'ffprobe', 'JDownloader backend', 'Gopeed', 'SQLite']) {
      expect(group(name).querySelector('button, [role="switch"]'), name).toBeNull();
    }
  });

  it('says where a JDownloader backend that is not set up stands', async () => {
    answers['/api/resolvers/jd'] = { configured: false, reachable: false };
    await draw();
    expect(group('JDownloader backend').textContent).toContain('Not set up');
  });

  it('names the same two switches the same for both', async () => {
    await draw();
    expect(switches(group('KnightLoader'))).toEqual(['Check automatically', 'Update automatically']);
    expect(switches(group('yt-dlp'))).toEqual(['Check automatically', 'Update automatically']);
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

  it('asks GitHub on opening only where its switch is on', async () => {
    await draw();
    expect(asked).not.toContain('/api/system/update-check');
    expect(asked).not.toContain('/api/mediatools/ytdlp/latest');

    act(() => root.unmount());
    root = createRoot(host);
    await draw({ autoUpdateCheck: true, ytdlpVersionCheck: true });
    expect(asked).toContain('/api/system/update-check');
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
