// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../lib/i18n';
import { IconLink, IconPaste, IconPhone } from '../../lib/icons';
import { WaysIn, type Way } from './WaysIn';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

function reducedMotion(reduced: boolean) {
  vi.stubGlobal('matchMedia', () => ({ matches: reduced, addEventListener() {}, removeEventListener() {} }));
}

beforeEach(() => {
  reducedMotion(true);
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
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

const ways: Way[] = [
  { id: 'paste', name: 'Add links', glyph: IconPaste, state: 'on' },
  { id: 'cnl', name: "Click'n'Load", glyph: IconLink, state: 'off' },
  { id: 'phone', name: 'Phone app', glyph: IconPhone, state: 'unset' },
  { id: 'watch', name: 'Watch folder', glyph: IconLink, state: 'on', detail: '/watch' },
];

describe('the picture of the ways into the collector', () => {
  const draw = (list = ways, onPick = (_id: string) => {}) =>
    act(async () =>
      root.render(
        <I18nProvider>
          <WaysIn ways={list} label="Link intake" onPick={onPick} />
        </I18nProvider>,
      ),
    );
  const way = (id: string) => host.querySelector(`[data-way="${id}"]`)!;
  const line = (id: string) => way(id).querySelector('path')!;

  it('draws a way that is open in colour and one that is not in grey, with the reason', async () => {
    await draw();
    expect(line('paste').getAttribute('stroke')).toBe('var(--accent-glyph)');
    expect(line('paste').getAttribute('stroke-dasharray')).toBeNull();
    expect(way('watch').textContent).toBe('Watch folder/watch');

    for (const [id, word] of [
      ['cnl', 'Off'],
      ['phone', 'Not set up'],
    ]) {
      expect(line(id).getAttribute('stroke')).toBe('var(--carbon-text-muted)');
      expect(line(id).getAttribute('stroke-dasharray')).toBe('2 7');
      expect(way(id).textContent).toContain(word);
    }
  });

  it('runs every line into the collector', async () => {
    await draw();
    const svg = host.querySelector('svg')!;
    expect(svg.textContent).toContain('Link collector');
    // Each line starts at the rim of the hub, and the hub stands where they meet.
    const hub = svg.querySelector('circle[fill="var(--accent)"]')!;
    const starts = ways.map((w) => /^M([\d.]+) ([\d.]+)/.exec(line(w.id).getAttribute('d')!)!.slice(1).map(Number));
    for (const [x, y] of starts) {
      const away = Math.hypot(x - Number(hub.getAttribute('cx')), y - Number(hub.getAttribute('cy')));
      expect(away).toBeCloseTo(35, 0);
    }
  });

  it('says in words which ways are open', async () => {
    await draw();
    expect(host.querySelector('svg')!.getAttribute('aria-label')).toBe(
      "Link intake: Add links, Click'n'Load (Off), Phone app (Not set up), Watch folder",
    );
  });

  it("goes to a way's setting on a click and on the keyboard", async () => {
    const picked: string[] = [];
    await draw(ways, (id) => picked.push(id));
    const figure = way('cnl').querySelector<SVGGElement>('[role="button"]')!;
    expect(figure.getAttribute('aria-label')).toBe("Click'n'Load (Off)");
    await act(async () => figure.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    await act(async () => figure.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true })));
    expect(picked).toEqual(['cnl', 'cnl']);
  });

  it('lays the wide picture out at the width of its card, so it fills the card at one unit to the pixel', async () => {
    const width = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(1200);
    await draw();
    const [wide, narrow] = [...host.querySelectorAll('[data-new="ways-in"] > svg')];
    expect(wide.getAttribute('viewBox')).toMatch(/^0 0 1200 /);
    expect(wide.getAttribute('class')).not.toContain('max-w');
    // The narrow one keeps its own size, for a card too narrow for both sides.
    expect(narrow.getAttribute('viewBox')).toMatch(/^0 0 360 /);
    width.mockRestore();
  });

  it('carries packets on the open ways only, and on none when motion is off', async () => {
    await draw();
    expect(host.querySelectorAll('animateMotion')).toHaveLength(0);

    reducedMotion(false);
    // jsdom has no SMIL clock.
    Object.assign(SVGSVGElement.prototype, { pauseAnimations() {}, unpauseAnimations() {}, setCurrentTime() {} });
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observe() {}
        disconnect() {}
      },
    );
    act(() => root.unmount());
    root = createRoot(host);
    await draw();
    expect(way('paste').querySelector('[data-stream] animateMotion')).not.toBeNull();
    expect(way('watch').querySelector('[data-stream]')).not.toBeNull();
    expect(way('cnl').querySelector('[data-stream]')).toBeNull();
    expect(way('phone').querySelector('[data-stream]')).toBeNull();
  });
});
