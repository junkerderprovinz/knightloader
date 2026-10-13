// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { PieceMap } from '../../lib/api';
import { I18nProvider, type TranslationKey } from '../../lib/i18n';
import { interpolate } from '../../lib/interpolate';
import { en } from '../../lib/locales/en';
import { PieceBand, bandOf, pieceWords } from './PieceBand';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

function reducedMotion(reduced: boolean) {
  vi.stubGlobal('matchMedia', () => ({ matches: reduced, addEventListener() {}, removeEventListener() {} }));
}

beforeEach(() => {
  reducedMotion(true);
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const draw = (picture: ReactNode) => act(async () => root.render(<I18nProvider>{picture}</I18nProvider>));

const t = ((key: TranslationKey, vars?: Record<string, string | number>) => interpolate(en[key], vars)) as Parameters<
  typeof pieceWords
>[1];
const words = (map: PieceMap) => pieceWords(map, t).replace(/[⁦-⁩]/g, '');

const run = (state: string, len: number) => ({ state, len });

// Three connections on a file of 1000 bytes, the last range not begun.
const bytes: PieceMap = {
  unit: 'bytes',
  total: 1000,
  runs: [run('done', 300), run('missing', 200), run('done', 100), run('missing', 400)],
  heads: [300, 600],
};

const articles: PieceMap = {
  unit: 'articles',
  total: 10,
  runs: [run('done', 4), run('lost', 1), run('missing', 5)],
  heads: [5, 6],
};

const blocks: PieceMap = {
  unit: 'blocks',
  total: 2000,
  runs: [run('done', 500), run('lost', 2), run('done', 1498)],
};

const transforms = (selector: string) =>
  [...host.querySelectorAll<SVGElement>(selector)].map((el) => el.style.transform);

describe('the band of a file', () => {
  it('fills what is written and leaves the rest of the track open', async () => {
    await draw(<PieceBand map={bytes} label="x" />);
    // The band is 1000 wide, as this file is; the stretch at its start runs a little past the end.
    expect(transforms('[data-piece="written"]')).toEqual([
      'translateX(-2.00px) scaleX(302.00)',
      'translateX(500.00px) scaleX(100.00)',
    ]);
  });

  it('fills articles written on both sides of a lost one as stretches of their own', async () => {
    const holed: PieceMap = { unit: 'articles', total: 10, runs: [run('done', 4), run('lost', 1), run('done', 5)] };
    await draw(<PieceBand map={holed} label="x" />);
    expect(transforms('[data-piece="written"]')).toEqual([
      'translateX(-2.00px) scaleX(402.00)',
      'translateX(500.00px) scaleX(502.00)',
    ]);
  });

  it('stands a head where each connection is writing', async () => {
    await draw(<PieceBand map={bytes} label="x" />);
    expect(transforms('[data-head]')).toEqual(['translateX(300.00px)', 'translateX(600.00px)']);
  });

  it('stands a head on the middle of an article that is being fetched', async () => {
    await draw(<PieceBand map={articles} label="x" />);
    expect(transforms('[data-head]')).toEqual(['translateX(550.00px)', 'translateX(650.00px)']);
  });

  it('lights what arrived damaged or not at all, and keeps it out of the fill', async () => {
    await draw(<PieceBand map={articles} label="x" />);
    const lost = host.querySelector('[data-piece="lost"]')!;
    expect([lost.getAttribute('x'), lost.getAttribute('width'), lost.getAttribute('fill')]).toEqual([
      '400',
      '100',
      'var(--status-fail-solid)',
    ]);
    expect(transforms('[data-piece="written"]')).toEqual(['translateX(-2.00px) scaleX(402.00)']);
  });

  it('tells pieces apart by a line between them while they are few', async () => {
    await draw(<PieceBand map={articles} label="x" />);
    expect(host.querySelector('[data-piece="between"]')!.getAttribute('d')!.match(/M/g)).toHaveLength(9);
    await draw(<PieceBand map={blocks} label="x" />);
    expect(host.querySelector('[data-piece="between"]')).toBeNull();
    await draw(<PieceBand map={bytes} label="x" />);
    expect(host.querySelector('[data-piece="between"]')).toBeNull();
  });

  it('pulses the damaged blocks only while a repair replaces them and motion is allowed', async () => {
    await draw(<PieceBand map={blocks} label="x" mending />);
    expect(host.querySelectorAll('animate')).toHaveLength(0);

    reducedMotion(false);
    await draw(<PieceBand key="waiting" map={blocks} label="x" />);
    expect(host.querySelectorAll('animate')).toHaveLength(0);
    await draw(<PieceBand key="mending" map={blocks} label="x" mending />);
    expect(host.querySelectorAll('animate')).toHaveLength(1);
  });

  it('glides to the next reading, and jumps to it when motion is off', async () => {
    await draw(<PieceBand map={bytes} label="x" />);
    expect(host.querySelector<SVGElement>('[data-head]')!.style.transition).toBe('');
    reducedMotion(false);
    await draw(<PieceBand key="moving" map={bytes} label="x" />);
    expect(host.querySelector<SVGElement>('[data-head]')!.style.transition).toContain('transform');
  });

  it('carries its words as the name of the picture', async () => {
    await draw(<PieceBand map={bytes} label={words(bytes)} />);
    expect(host.querySelector('[role="img"]')!.getAttribute('aria-label')).toBe(
      '400 B of 1.0 kB written. Connections writing: 2',
    );
  });
});

describe('the words for a band', () => {
  it('count the articles written, lost and in flight', () => {
    expect(words(articles)).toBe('Articles written: 4 of 10. On no server: 1. Being fetched: 2');
  });

  it('count the blocks a check found damaged', () => {
    expect(words(blocks)).toBe('Blocks intact: 1998 of 2000. Damaged or missing: 2');
  });

  it('say nothing about a unit this build does not know', () => {
    expect(words({ ...bytes, unit: 'pieces' })).toBe('');
  });

  it('take a state this build does not know for not written', () => {
    expect(bandOf({ unit: 'bytes', total: 10, runs: [run('done', 4), run('checking', 6)] }).done).toBe(4);
  });
});
