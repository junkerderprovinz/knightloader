// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Tabs } from './Tabs';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom lays nothing out, so the strip's box and each segment's one-line width
// come from here: `room` for the track, `widths` by segment id.
let room = 0;
let widths: Record<string, number> = {};
let resized: (() => void)[] = [];

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  room = 0;
  widths = {};
  resized = [];
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(cb: () => void) {
        resized.push(cb);
      }
      observe() {}
      disconnect() {}
    },
  );
  const real = window.getComputedStyle.bind(window);
  vi.spyOn(window, 'getComputedStyle').mockImplementation((el: Element) => {
    const style = real(el);
    const id = (el as HTMLElement).dataset?.tabId;
    const own: Record<string, string> = { paddingLeft: '0px', paddingRight: '0px', columnGap: '4px' };
    if (id) own.width = `${widths[id] ?? 0}px`;
    return new Proxy(style, {
      get: (target, key) => (typeof key === 'string' && key in own ? own[key] : Reflect.get(target, key, target)),
    });
  });
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return this.hasAttribute('data-strip') ? room : 0;
  });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const draw = (strip: ReactNode) => act(() => root.render(strip));
const resize = (to: number) => {
  room = to;
  act(() => resized.forEach((cb) => cb()));
};
const segments = () => [...host.querySelectorAll<HTMLElement>('[data-tab-id]')];
const dropdown = () => host.querySelector<HTMLButtonElement>('button[aria-haspopup]');

const Glyph = () => <svg viewBox="0 0 20 20" />;

function collision(onSelect: (id: string) => void = () => {}) {
  widths = { ask: 120, rename: 180, replace: 140 };
  return (
    <Tabs
      variant="well"
      size="sm"
      inline
      hug
      label="If the file exists"
      active="rename"
      onSelect={onSelect}
      items={[
        { id: 'ask', label: 'Ask every time' },
        { id: 'rename', label: 'Keep both under a new name' },
        { id: 'replace', label: 'Replace the old file' },
      ]}
    />
  );
}

describe('a strip that picks one value', () => {
  it('keeps its segments on one row while its labels fit', () => {
    room = 600;
    draw(collision());
    expect(dropdown()).toBeNull();
    expect(host.querySelector('[role="tablist"]')).not.toBeNull();
    // The three labels, two gaps and the pixel against rounding.
    expect(host.querySelector<HTMLElement>('[data-strip]')!.style.width).toBe(`${120 + 180 + 140 + 8 + 1}px`);
  });

  it('turns into a dropdown showing the chosen value where its labels do not fit', () => {
    room = 300;
    draw(collision());
    expect(dropdown()!.textContent).toContain('Keep both under a new name');
    expect(host.querySelector('[role="tablist"]')).toBeNull();
    for (const segment of segments()) {
      expect(segment.getAttribute('aria-hidden')).toBe('true');
      expect(segment.tabIndex).toBe(-1);
    }
  });

  it('chooses through the dropdown as through a segment', () => {
    const onSelect = vi.fn();
    room = 300;
    draw(collision(onSelect));
    act(() => dropdown()!.click());
    const row = [...document.querySelectorAll<HTMLElement>('[role="menuitemradio"], [role="menuitem"]')].find((r) =>
      r.textContent?.includes('Replace the old file'),
    )!;
    act(() => row.click());
    expect(onSelect).toHaveBeenCalledWith('replace');
  });

  it('turns back into segments once the row has room again', () => {
    room = 300;
    draw(collision());
    expect(dropdown()).not.toBeNull();
    resize(600);
    expect(dropdown()).toBeNull();
    expect(host.querySelector('[role="tablist"]')).not.toBeNull();
  });
});

describe('a strip with a glyph on every segment', () => {
  it('shows the glyphs alone before it turns into a dropdown, each label kept as its name', () => {
    widths = { light: 120, dark: 120, system: 160 };
    room = 300;
    draw(
      <Tabs
        variant="well"
        size="sm"
        inline
        hug
        labelled
        label="Theme"
        active="dark"
        onSelect={() => {}}
        items={[
          { id: 'light', label: 'Light', icon: <Glyph /> },
          { id: 'dark', label: 'Dark', icon: <Glyph /> },
          { id: 'system', label: 'Follow the system', icon: <Glyph /> },
        ]}
      />,
    );
    expect(dropdown()).toBeNull();
    for (const segment of segments()) {
      expect(segment.querySelector('[data-tab-label]')!.classList.contains('sr-only')).toBe(true);
    }
    expect(segments()[2].textContent).toBe('Follow the system');
  });
});

describe('a strip that toggles several values', () => {
  it('cuts its long labels short instead of turning into a dropdown', () => {
    widths = { running: 90, waiting: 220, failed: 80 };
    room = 300;
    draw(
      <Tabs
        select="many"
        size="sm"
        inline
        label="Show"
        active={new Set(['running'])}
        onSelect={() => {}}
        items={[
          { id: 'running', label: 'Running' },
          { id: 'waiting', label: 'Waiting for a free slot on the hoster' },
          { id: 'failed', label: 'Failed' },
        ]}
      />,
    );
    expect(dropdown()).toBeNull();
    expect(host.querySelector('[role="group"]')).not.toBeNull();
    for (const segment of segments()) expect(segment.style.flex).toBe('1 1 0px');
  });
});
