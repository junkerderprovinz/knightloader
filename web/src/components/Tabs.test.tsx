// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { setLabelMode } from '../lib/labelModes';
import { Tabs } from './Tabs';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
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

const items = [
  { id: 'running', label: 'Downloading', badge: 3 },
  { id: 'finished', label: 'Finished', badge: 5 },
];

function draw(onSelect: (id: string) => void, onClear: () => void) {
  act(() =>
    root.render(
      <Tabs
        select="many"
        size="sm"
        label="Quick filters"
        labelled
        active={new Set(['finished'])}
        onSelect={onSelect}
        items={items}
        folded={{ icon: null, more: [{ id: 'clear', label: 'Show everything', onSelect: onClear }] }}
      />,
    ),
  );
}

function open() {
  const chip = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('Quick filters'))!;
  act(() => chip.click());
}

describe('a folded chip strip', () => {
  it('is one chip that opens its chips as switches, lit ones checked', () => {
    draw(() => {}, () => {});
    expect(host.querySelectorAll('button')).toHaveLength(1);
    open();
    const entries = [...document.querySelectorAll('[role=menuitemcheckbox]')];
    expect(entries.map((e) => [e.textContent, e.getAttribute('aria-checked')])).toEqual([
      ['Downloading3', 'false'],
      ['Finished5', 'true'],
    ]);
  });

  it('follows Beschriftung like the badges beside it', () => {
    const chip = () => host.querySelector('button')!;
    const drawWith = () =>
      act(() =>
        root.render(
          <Tabs
            select="many"
            size="sm"
            label="Quick filters"
            labelled
            active={new Set()}
            onSelect={() => {}}
            items={items}
            folded={{ icon: <svg data-glyph /> }}
          />,
        ),
      );
    try {
      setLabelMode('tabs', 'glyph');
      drawWith();
      expect(chip().textContent).toBe('');
      expect(chip().getAttribute('aria-label')).toBe('Quick filters');
      expect(chip().querySelector('[data-glyph]')).not.toBeNull();

      setLabelMode('tabs', 'text');
      drawWith();
      expect(chip().textContent).toBe('Quick filters');
      expect(chip().querySelector('[data-glyph]')).toBeNull();
    } finally {
      setLabelMode('tabs', 'both');
    }
  });

  it('switches a chip from its menu and offers what stood beside the strip', () => {
    const onSelect = vi.fn();
    const onClear = vi.fn();
    draw(onSelect, onClear);
    open();
    const running = [...document.querySelectorAll<HTMLButtonElement>('[role=menuitemcheckbox]')][0];
    act(() => running.click());
    expect(onSelect).toHaveBeenCalledWith('running');

    open();
    const clear = [...document.querySelectorAll<HTMLButtonElement>('[role=menuitem]')].find(
      (e) => e.textContent === 'Show everything',
    )!;
    act(() => clear.click());
    expect(onClear).toHaveBeenCalled();
  });
});

describe('a rail on the sidebar ground', () => {
  function rail(sidebarGround: boolean) {
    act(() =>
      root.render(
        <Tabs
          orientation="vertical"
          fill
          sidebarGround={sidebarGround}
          label="Pages"
          active="general"
          onSelect={() => {}}
          items={[
            { id: 'general', label: 'General' },
            { id: 'pairing', label: 'Pairing' },
          ]}
        />,
      ),
    );
    const tab = (name: string) => [...host.querySelectorAll('[role="tab"]')].find((t) => t.textContent === name)!;
    return { on: tab('General').className, off: tab('Pairing').className };
  }

  it('gives idle tabs the sidebar colour and its row hover, and keeps the lit one the accent', () => {
    const { on, off } = rail(true);
    expect(off).toContain('bg-carbon-sidebar');
    expect(off).toContain('hover:bg-carbon-hover');
    expect(off).not.toContain('bg-carbon-surface2');
    expect(on).toContain('bg-accent');
    expect(on).not.toContain('bg-carbon-sidebar');
  });

  it('leaves other strips on their own ground', () => {
    expect(rail(false).off).toContain('bg-carbon-surface2');
  });
});

describe('a selector that sets a value', () => {
  const segments = (variant: 'default' | 'well') => {
    act(() =>
      root.render(
        <Tabs
          variant={variant}
          label="Theme"
          active="dark"
          onSelect={() => {}}
          items={[
            { id: 'dark', label: 'Dark' },
            { id: 'light', label: 'Light' },
          ]}
        />,
      ),
    );
    return [...host.querySelectorAll<HTMLElement>('[role="tab"]')];
  };

  it('owns no palette position, so the chosen segment wears the accent of its card', () => {
    for (const segment of segments('well')) {
      expect(segment.classList.contains('glim-hue')).toBe(false);
      expect(segment.style.getPropertyValue('--item-hue')).toBe('');
    }
    expect(segments('well')[0].className).toContain('bg-accent');
  });

  it('leaves a tab strip its position per tab', () => {
    const [first, second] = segments('default');
    expect(first.classList.contains('glim-hue')).toBe(true);
    expect(first.style.getPropertyValue('--item-hue')).not.toBe(second.style.getPropertyValue('--item-hue'));
  });
});
