// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task, TorrentFileView } from '../lib/api';
import { ACCENTS, RAINBOW } from '../lib/appearance';
import { contrastRatio, oklabOf, srgbOf } from '../lib/contrast';
import { rowBands, type ListRow } from './listRows';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;

const OPEN = JSON.stringify({ 'list.expanded.downloads': ['Films', 'Music'] });

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (url: string) =>
        new Response(url.includes('/api/uistate') ? OPEN : '[]', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    ),
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

const task = (id: string, pkg: string): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.zip`,
  package: pkg,
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'queued',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
});

async function show(groups: [string, Task[]][]): Promise<void> {
  const { TaskListCard } = await import('./TaskList');
  await act(async () => root.render(<TaskListCard groups={groups} base="/api" title="Downloads" />));
}

const FILMS = [task('a', 'Films'), task('b', 'Films'), task('c', 'Films')];
const MUSIC = [task('d', 'Music'), task('e', 'Music')];

const band = (el: Element | null) => el?.getAttribute('data-band');
const link = (id: string) => host.querySelector(`[data-task-id="${id}"]`);
const folder = (name: string) => host.querySelector(`[data-package-row="${name}"]`);
const tree = () => host.querySelector('[role="tree"]')!;

describe('the shaded list', () => {
  it('heads neighbouring packages in two shades, so folded packages stand apart', async () => {
    await show([
      ['Films', FILMS],
      ['Music', MUSIC],
    ]);
    expect(band(folder('Films'))).toBe('head');
    expect(band(folder('Music'))).toBe('head-alt');
  });

  it('alternates the links of a package by their place in it', async () => {
    await show([['Films', FILMS]]);
    expect(['a', 'b', 'c'].map((id) => band(link(id)))).toEqual(['odd', 'even', 'odd']);
  });

  it('starts every package on the same shade, whatever the one above ended on', async () => {
    await show([
      ['Films', FILMS],
      ['Music', MUSIC],
    ]);
    expect(['d', 'e'].map((id) => band(link(id)))).toEqual(['odd', 'even']);
  });

  it('runs one package straight into the next, with no strip of ground between them', async () => {
    await show([
      ['Films', FILMS],
      ['Music', MUSIC],
    ]);
    expect(folder('Music')!.className).not.toContain('kl-band-seam');
  });

  it('marks the rows a switched-off link greys, and keeps their place in the alternation', async () => {
    const off = (t: Task): Task => ({ ...t, enabled: false });
    await show([
      ['Films', FILMS.map(off)],
      ['Music', [MUSIC[0], off(MUSIC[1])]],
    ]);
    const parked = (el: Element | null) => el?.hasAttribute('data-parked');
    expect([folder('Films'), link('a'), link('b'), link('c')].map(parked)).toEqual([true, true, true, true]);
    expect([folder('Music'), link('d'), link('e')].map(parked)).toEqual([false, false, true]);
    expect(band(link('e'))).toBe('even');
  });

  it('parks the files of a switched-off torrent with it, greyed like its other rows', async () => {
    const torrent = (id: string, enabled: boolean): Task => ({ ...task(id, 'Films'), torrentFileCount: 2, enabled });
    const files: TorrentFileView[] = [
      { path: 'one.mkv', size: 10, selected: true },
      { path: 'two.mkv', size: 10, selected: false },
    ];
    const state = JSON.stringify({ 'list.expanded.downloads': ['Films'], 'list.openTorrents.downloads': ['t', 'u'] });
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async (url: string) =>
          new Response(url.includes('/api/uistate') ? state : url.includes('/torrent-files') ? JSON.stringify(files) : '[]', {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
    );
    await show([['Films', [torrent('t', false), torrent('u', true)]]]);
    await act(async () => {});
    /** Per file row of a torrent: whether it sinks, and whether its text greys. */
    const parked = (id: string) =>
      [...host.querySelectorAll(`[data-file-of="${id}"]`)].map((r) => [r.hasAttribute('data-parked'), r.querySelector('.kl-parked') !== null]);
    expect(parked('t')).toEqual([
      [true, true],
      [true, true],
    ]);
    expect(parked('u')).toEqual([
      [false, false],
      [false, false],
    ]);
  });

  it('shades the list moderately until another level is picked', async () => {
    await show([['Films', FILMS]]);
    expect(tree().getAttribute('data-shade')).toBe('moderate');
    const { setListShade } = await import('../lib/listShade');
    await act(async () => setListShade('off'));
    expect(tree().hasAttribute('data-shade')).toBe(false);
    await act(async () => setListShade('strong'));
    expect(tree().getAttribute('data-shade')).toBe('strong');
    await act(async () => setListShade('moderate'));
    expect(tree().getAttribute('data-shade')).toBe('moderate');
  });
});

/** The one call of node:fs used here; the web build carries no Node types. */
interface NodeFs {
  readFileSync(path: string, encoding: 'utf8'): string;
}

describe('the shades', () => {
  // Vitest hands a test every stylesheet empty, even as ?raw, so the tokens
  // are read off the disk, from web/ where it runs.
  const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule(
    'node:fs',
  );
  const css = fs.readFileSync('src/index.css', 'utf8');
  const strongBlock = /:where\(\[data-shade='strong'\]\) \{([^}]*)\}/.exec(css)?.[1] ?? '';
  /** How much of the text colour a band mixes in, in percent. A clear band mixes none. */
  const share = (band: string, from = css) => {
    const value = new RegExp(`--list-band-${band}:\\s*([^;]+);`).exec(from)?.[1].trim();
    if (value === 'transparent') return 0;
    const pct = /var\(--carbon-text\)\s+([\d.]+)%/.exec(value ?? '')?.[1];
    if (pct === undefined) throw new Error(`--list-band-${band} is no mix of the text colour: ${value}`);
    return Number(pct);
  };

  it('alternates the files barely, closer to a quiet zebra than to bands', () => {
    expect(share('even') - share('odd')).toBeGreaterThan(0);
    expect(share('even') - share('odd')).toBeLessThanOrEqual(2.5);
  });

  it('sets the package header only a hair above its files', () => {
    expect(share('head')).toBeGreaterThan(share('even'));
    expect(share('head') - share('even')).toBeLessThanOrEqual(2.5);
  });

  it('tells a folded package from the one beside it', () => {
    expect(share('head-alt')).toBeGreaterThan(share('head'));
  });

  it('shades every band harder at the high-contrast level', () => {
    for (const band of ['even', 'head', 'head-alt']) expect(share(band, strongBlock)).toBeGreaterThan(share(band));
  });

  it('lights a hovered row in the accent over every shade', () => {
    expect(css).toMatch(/:where\(\[data-shade\]\) > \[data-band\]:is\(:hover, :has\(:focus-visible\)\) \{\s*background-color: var\(--hover\);/);
  });

  it('leaves no gap between packages', () => {
    expect(css).not.toContain('--list-band-gap');
    expect(css).not.toContain('kl-band-seam');
  });

  // A parked row sinks: a see-through shade over whatever ground it shows,
  // toward the text colour on a light theme and toward the page on a dark one.
  const sunk = (from = css) => {
    const value = /--list-band-sunk:\s*([^;]+);/.exec(from)?.[1] ?? '';
    const light = /var\(--carbon-text\)\s+([\d.]+)%/.exec(value)?.[1];
    const dark = /var\(--carbon-bg\)\s+([\d.]+)%/.exec(value)?.[1];
    if (light === undefined || dark === undefined) throw new Error(`--list-band-sunk is no light-dark sink: ${value}`);
    return { light: Number(light), dark: Number(dark) };
  };

  it('lays the sunk shade over a parked row at every level, so its band still shows through', () => {
    const body = /^\[data-parked\]\s*\{([^}]*)\}/m.exec(css)?.[1] ?? '';
    expect(body).toMatch(/background-image:\s*linear-gradient\(var\(--list-band-sunk\), var\(--list-band-sunk\)\);/);
    expect(body).not.toMatch(/background-color/);
  });

  it('sinks a parked row further than the files alternate, and harder at the high-contrast level', () => {
    expect(sunk().light).toBeGreaterThan(share('even') - share('odd'));
    expect(sunk(strongBlock).light).toBeGreaterThan(share('even', strongBlock));
    expect(sunk(strongBlock).light).toBeGreaterThan(sunk().light);
    expect(sunk(strongBlock).dark).toBeGreaterThan(sunk().dark);
  });

  it('keeps the greyed text of a parked row at least as readable as before it sank', () => {
    const ink = /--parked-ink:\s*light-dark\(color-mix\(in srgb, var\(--carbon-text-muted\), var\(--carbon-text\) ([\d.]+)%\), var\(--carbon-text-muted\)\);/.exec(
      css,
    )?.[1];
    expect(ink, 'no --parked-ink stepping the muted text toward the text colour').toBeDefined();
    const parkedBlock = /\.kl-parked\s*\{([^}]*)\}/.exec(css)?.[1] ?? '';
    expect(parkedBlock).not.toContain('var(--carbon-text-muted)');
    expect(parkedBlock).toMatch(/--carbon-text-muted:\s*var\(--parked-ink\);/);
    expect(parkedBlock).toMatch(/--color-carbon-textMuted:\s*var\(--parked-ink\);/);

    const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
    /** `top` at `share` percent over `base`, as color-mix with transparent paints it. */
    const over = (top: string, share: number, base: string) => {
      const [t, b] = [rgb(top), rgb(base)];
      return `#${b.map((v, i) => Math.round(t[i] * (share / 100) + v * (1 - share / 100)).toString(16).padStart(2, '0')).join('')}`;
    };
    const themes = [
      { name: 'light', surface: '#ffffff', text: '#161616', sink: '#161616', hover: 0.861, muted: ['#6f6f6f', '#525252'] },
      { name: 'dark', surface: '#262626', text: '#f4f4f4', sink: '#161616', hover: 0.432, muted: ['#8d8d8d', '#c6c6c6'] },
      { name: 'OLED', surface: '#161616', text: '#f4f4f4', sink: '#000000', hover: 0.338, muted: ['#8d8d8d', '#c6c6c6'] },
    ];
    const levels = [
      { name: 'off', bands: [0], sink: sunk() },
      { name: 'moderate', bands: ['odd', 'even', 'head', 'head-alt'].map((b) => share(b)), sink: sunk() },
      { name: 'strong', bands: [0, ...['even', 'head', 'head-alt'].map((b) => share(b, strongBlock))], sink: sunk(strongBlock) },
    ];
    const misses: string[] = [];
    for (const theme of themes) {
      const light = theme.name === 'light';
      for (const muted of theme.muted) {
        const parked = light ? over(theme.text, Number(ink), muted) : muted;
        for (const level of levels) {
          const grounds = level.bands.map((b) => [`band ${b}%`, over(theme.text, b, theme.surface)]);
          for (const accent of [...ACCENTS.map((a) => a.hex), ...RAINBOW]) {
            const [, a, b] = oklabOf(accent);
            grounds.push([`hover ${accent}`, srgbOf([theme.hover, a * 0.3, b * 0.3])]);
            grounds.push([`selected ${accent}`, over(accent, 22, theme.surface)]);
          }
          for (const [what, ground] of grounds) {
            const before = contrastRatio(muted, ground);
            const after = contrastRatio(parked, over(theme.sink, light ? level.sink.light : level.sink.dark, ground));
            if (after < before - 0.01) misses.push(`${theme.name} ${muted} ${level.name} ${what}: ${before.toFixed(2)} to ${after.toFixed(2)}`);
          }
        }
      }
    }
    expect(misses).toEqual([]);
  });
});

describe('rowBands', () => {
  const head = (name: string): ListRow => ({
    kind: 'package',
    key: `package:${name}`,
    name,
    items: [],
    collapsed: false,
    divider: false,
    level: 1,
    posinset: 1,
    setsize: 1,
  });
  const linkRow = (id: string, at: number): ListRow => ({
    kind: 'task',
    key: `task:${id}`,
    task: task(id, 'P'),
    index: at,
    level: 2,
    posinset: at,
    setsize: 9,
  });
  const fileRow = (id: string, at: number): ListRow => ({
    kind: 'file',
    key: `file:${id}:${at}`,
    task: task(id, 'P'),
    file: { path: `f${at}`, size: 1, selected: true } as TorrentFileView,
    index: 0,
    level: 3,
    posinset: at,
    setsize: 2,
  });

  it('counts a torrent file as a row of its package, so no two neighbours share a shade', () => {
    const rows = [head('P'), linkRow('a', 1), fileRow('a', 1), fileRow('a', 2), linkRow('b', 2)];
    expect(rowBands(rows)).toEqual(['head', 'odd', 'even', 'odd', 'even']);
  });

  it("alternates the heads of neighbouring packages and starts each one's links afresh", () => {
    const rows = [head('P'), linkRow('a', 1), head('Q'), linkRow('b', 1), head('R')];
    expect(rowBands(rows)).toEqual(['head', 'odd', 'head-alt', 'odd', 'head']);
  });

  it('goes by the drawn rows, so a link filtered out between two leaves them apart', () => {
    const rows = [head('P'), linkRow('a', 1), linkRow('c', 3)];
    expect(rowBands(rows)).toEqual(['head', 'odd', 'even']);
  });
});
