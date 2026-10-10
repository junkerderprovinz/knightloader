// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../lib/i18n';
import { closeWhatsNew, useWhatsNewOpen } from '../../lib/useWhatsNew';
import { Help } from './Help';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom lays nothing out and has no ResizeObserver, which the README buttons
// use to fit their words.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;

/** What the server answers, by route; a test changes a route to change the answer. */
let answers: Record<string, unknown>;
let asked: string[];

beforeEach(() => {
  answers = {
    '/api/health': { status: 'ok', version: 'v1.8.4' },
    '/api/mediatools': {
      ytdlp: { found: true, version: '2026.08.19' },
      ffmpeg: { found: true, version: '8.1.2' },
      ffprobe: { found: true, version: '8.1.2' },
    },
    '/api/system/deployment': { deployment: 'container', canQuit: false, canRestart: false, note: '' },
    '/api/system/update-check': { checked: true, available: false, current: 'v1.8.4' },
    '/api/mediatools/ytdlp/latest': { checked: true, tag: '2026.08.19', compare: 'same' },
    '/api/diagnostics': { version: 'v1.8.4', logLines: [] },
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
  vi.restoreAllMocks();
});

async function draw() {
  await act(async () =>
    root.render(
      // The provider fills the placeholders, which the bare context does not.
      <I18nProvider>
        <MemoryRouter>
          <Help />
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
}

/** The card under a title badge. */
function card(title: string): HTMLElement {
  const badge = [...host.querySelectorAll('.glim-section-badge')].find((b) => b.textContent === title)!;
  return badge.closest<HTMLElement>('.glim-card')!;
}

/** A version row by the name at its start. */
function row(name: string): HTMLElement {
  return [...card('Version').querySelectorAll('li')].find((li) => li.firstElementChild?.firstElementChild?.textContent === name)!;
}

const button = (within: HTMLElement, words: string) =>
  [...within.querySelectorAll('button')].find((b) => b.textContent?.includes(words))!;

async function check() {
  await act(async () => button(card('Version'), 'Check for updates').click());
  return card('Version').querySelector<HTMLButtonElement>('div.justify-end > button:last-of-type')!;
}

describe('the Info tile', () => {
  it('opens with About, Version and Help and keeps the help topics under them', async () => {
    await draw();
    const titles = [...host.querySelectorAll('.glim-section-badge')].map((b) => b.textContent);
    expect(titles.slice(0, 3)).toEqual(['About KnightLoader', 'Version', 'Help']);
    expect(titles).toContain('Adding downloads');
    expect(titles).toHaveLength(14);
  });

  it('lists every version the app is made of and links a number to its release', async () => {
    await draw();
    const link = (name: string) => row(name).querySelector('a')?.getAttribute('href');
    expect(row('KnightLoader').textContent).toContain('1.8.4');
    expect(link('KnightLoader')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4');
    expect(link('GlimStone')).toMatch(/^https:\/\/github\.com\/junkerderprovinz\/glimstone\/releases\/tag\/v\d+\.\d+\.\d+$/);
    expect(link('yt-dlp')).toBe('https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.19');
    expect(row('ffmpeg').textContent).toContain('8.1.2');
    expect(row('ffmpeg').querySelector('a')).toBeNull();
  });

  it('says a media tool is not found instead of leaving its number empty', async () => {
    answers['/api/mediatools'] = { ytdlp: { found: false }, ffmpeg: { found: false }, ffprobe: { found: false } };
    await draw();
    expect(row('yt-dlp').textContent).toContain('not found');
    expect(row('ffmpeg').textContent).toContain('not found');
  });

  it('links a branch build to the release it was cut from', async () => {
    answers['/api/health'] = { status: 'ok', version: 'v1.8.4+redesign.59b73a6' };
    await draw();
    expect(row('KnightLoader').textContent).toContain('1.8.4+redesign.59b73a6');
    expect(row('KnightLoader').querySelector('a')?.getAttribute('href')).toBe(
      'https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4',
    );
  });

  it('shows a stamp without a release as written, with no link', async () => {
    answers['/api/health'] = { status: 'ok', version: 'preview-59b73a6' };
    await draw();
    expect(row('KnightLoader').textContent).toContain('preview-59b73a6');
    expect(row('KnightLoader').querySelector('a')).toBeNull();
  });

  it('turns the update check green when nothing is newer', async () => {
    await draw();
    const verdict = await check();
    expect(verdict.textContent).toBe('Up to date');
    expect(verdict.className).toContain('bg-statusOkSolid');
    expect(asked).toContain('/api/mediatools/ytdlp/latest');
  });

  it('counts a newer KnightLoader and a newer yt-dlp and leads to both', async () => {
    answers['/api/system/update-check'] = {
      checked: true,
      available: true,
      current: 'v1.8.4',
      latest: 'v1.9.0',
      url: 'https://github.com/junkerderprovinz/knightloader/releases/tag/v1.9.0',
    };
    answers['/api/mediatools/ytdlp/latest'] = { checked: true, tag: '2026.09.30', compare: 'newer' };
    await draw();
    const verdict = await check();
    expect(verdict.textContent).toBe('Updates: 2');
    expect(verdict.className).toContain('bg-statusWarnSolid');

    expect(row('KnightLoader').textContent).toContain('v1.9.0 is available');
    const hrefs = (name: string) => [...row(name).querySelectorAll('a')].map((a) => a.getAttribute('href'));
    expect(hrefs('KnightLoader')).toContain('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.9.0');
    expect(row('yt-dlp').textContent).toContain('2026.09.30 is available');
    expect(hrefs('yt-dlp')).toContain('/settings/resolvers');
  });

  it('opens the release notes of the running version from the Version card', async () => {
    let open: string | null = null;
    function Probe() {
      open = useWhatsNewOpen();
      return null;
    }
    await act(async () =>
      root.render(
        <I18nProvider>
          <MemoryRouter>
            <Help />
            <Probe />
          </MemoryRouter>
        </I18nProvider>,
      ),
    );
    expect(open).toBeNull();
    await act(async () => button(card('Version'), 'Release notes').click());
    expect(open).toBe('notes');
    act(() => closeWhatsNew());
  });

  it('turns the update check red when GitHub cannot be asked', async () => {
    answers['/api/system/update-check'] = { checked: false, available: false, current: 'v1.8.4' };
    await draw();
    const verdict = await check();
    expect(verdict.textContent).toBe('Check failed');
    expect(verdict.className).toContain('bg-statusFailSolid');
  });

  it('does not ask about yt-dlp where none runs', async () => {
    answers['/api/mediatools'] = { ytdlp: { found: false }, ffmpeg: { found: true, version: '8.1.2' }, ffprobe: { found: true } };
    await draw();
    await check();
    expect(asked).not.toContain('/api/mediatools/ytdlp/latest');
  });

  it('offers the manual as a link to the documentation', async () => {
    await draw();
    const manual = [...card('Help').querySelectorAll('a')].find((a) => a.textContent?.includes('Manual'))!;
    expect(manual.getAttribute('href')).toBe('https://junkerderprovinz.github.io/knightloader/');
    expect(manual.textContent).toContain('Open');
  });

  it('downloads the diagnostics bundle as the bug report', async () => {
    const saved: string[] = [];
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:report', revokeObjectURL: () => {} }));
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(this.download);
    });
    await draw();
    await act(async () => button(card('Help'), 'Bug report').click());
    expect(asked).toContain('/api/diagnostics');
    expect(saved).toHaveLength(1);
    expect(saved[0]).toMatch(/^knightloader-diagnostics-\d{8}T\d{6}Z\.json$/);
  });
});
