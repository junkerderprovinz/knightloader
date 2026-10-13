// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../lib/i18n';
import { closeWhatsNew, useWhatsNewOpen, type WhatsNew } from '../../lib/useWhatsNew';
import { Help } from './Help';

/** The running version's record the Version card reads, null until a test sets one. */
const record = vi.hoisted(() => ({ news: null as WhatsNew | null }));
vi.mock('../../lib/useWhatsNew', async (original) => ({
  ...(await original<typeof import('../../lib/useWhatsNew')>()),
  useWhatsNew: () => record.news,
}));

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

describe('the Info tile', () => {
  it('holds three cards: Version, then Help, then About', async () => {
    await draw();
    const titles = [...host.querySelectorAll('.glim-section-badge')].map((b) => b.textContent);
    expect(titles).toEqual(['Version', 'Help', 'About KnightLoader']);
  });

  it('lists every version the app is made of and links a number to its release', async () => {
    await draw();
    const link = (name: string) => row(name).querySelector('a')?.getAttribute('href');
    expect(row('KnightLoader').textContent).toContain('1.8.4');
    expect(link('KnightLoader')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.8.4');
    expect(link('GlimStone')).toMatch(/^https:\/\/github\.com\/junkerderprovinz\/glimstone\/releases\/tag\/v\d+\.\d+\.\d+$/);
  });

  it('leaves the programs KnightLoader starts to the General page', async () => {
    await draw();
    const names = [...card('Version').querySelectorAll('li')].map((li) => li.firstElementChild?.firstElementChild?.textContent);
    expect(names).toEqual(['KnightLoader', 'GlimStone']);
    expect(asked).not.toContain('/api/mediatools');
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

  it('only shows the versions and leads to the updates on the General page', async () => {
    await draw();
    expect(button(card('Version'), 'Check for updates')).toBeUndefined();
    expect(card('Version').querySelector('a[href="/settings/look"]')).not.toBeNull();
    expect(asked).not.toContain('/api/system/update-check');
    expect(asked).not.toContain('/api/mediatools/ytdlp/latest');
  });

  /** Draws the page with a probe on the window's tab, and returns a reader for it. */
  async function drawWithProbe() {
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
    return () => open;
  }

  const news = (changes: number, unseen: number) =>
    ({
      changes: Array.from({ length: changes }, (_, i) => ({ id: `c${i}` })),
      unseen: Array.from({ length: unseen }, (_, i) => ({ id: `c${i}` })),
    }) as unknown as WhatsNew;

  it('has one button for what this version brought, opening on the changes while any is unseen', async () => {
    record.news = news(5, 2);
    const open = await drawWithProbe();
    const version = card('Version');
    const labels = [...version.querySelectorAll('button')].map((b) => b.textContent).filter((w) => /New|Release notes/.test(w ?? ''));
    expect(labels).toEqual(['New · 2']);
    await act(async () => button(version, 'New · 2').click());
    expect(open()).toBe('changes');
    act(() => closeWhatsNew());
    record.news = null;
  });

  it('opens on the release notes once every change is seen, and counts them all', async () => {
    record.news = news(5, 0);
    const open = await drawWithProbe();
    await act(async () => button(card('Version'), 'New · 5').click());
    expect(open()).toBe('notes');
    act(() => closeWhatsNew());
    record.news = null;
  });

  it('offers the release notes alone for a version that marked no change', async () => {
    const open = await drawWithProbe();
    expect(open()).toBeNull();
    await act(async () => button(card('Version'), 'Release notes').click());
    expect(open()).toBe('notes');
    act(() => closeWhatsNew());
  });

  it('offers the manual as a link to the documentation', async () => {
    await draw();
    const manual = [...card('Help').querySelectorAll('a')].find((a) => a.textContent?.includes('Manual'))!;
    expect(manual.getAttribute('href')).toBe('https://junkerderprovinz.github.io/knightloader/');
    expect(manual.textContent).toContain('Open');
  });

  it('shows the manual as a closed book with an open one for the pointer, as the envelope has', async () => {
    await draw();
    const pair = (name: string) =>
      [...card('Help').querySelectorAll('.glim-readme-btn')]
        .find((b) => b.textContent?.includes(name))!
        .querySelectorAll('.glim-readme-btn-mark svg');
    for (const name of ['Manual', 'Email']) {
      expect([...pair(name)].map((svg) => svg.getAttribute('class'))).toEqual(['glim-mark-rest', 'glim-mark-hover']);
    }
  });

  it("wears the README's docs button on the manual: its yellow and its closed book", async () => {
    await draw();
    const manual = [...card('Help').querySelectorAll('a')].find((a) => a.textContent?.includes('Manual'))!;
    expect(manual.closest('.glim-tile-docs')).not.toBeNull();
    expect(manual.querySelector('.glim-readme-btn-mark')!.classList.contains('glim-docs-mark')).toBe(true);
    const book = manual.querySelector('svg.glim-mark-rest')!;
    expect(book.getAttribute('viewBox')).toBe('-32 0 512 512');
    expect(book.querySelector('path')!.getAttribute('d')).toMatch(/^M96 0C43 0 0 43 0 96/);
  });

  it('draws the marks of the Help card at one size, each across its whole box', async () => {
    await draw();
    const sides = [...card('Help').querySelectorAll('.glim-readme-btn-mark svg')].map((svg) => {
      const [, , width, height] = svg.getAttribute('viewBox')!.split(' ').map(Number);
      expect(height).toBe(width);
      return width;
    });
    // GitHub, the envelope shut and open, the book shut and open, the bug: each
    // box is the longer side of its ink. An ordinary glyph keeps a quarter of
    // its box as air, so GitHub's 24 would stand in a box of 32.
    expect(sides).toEqual([24, 14, 20, 512, 14, 13.9]);
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
