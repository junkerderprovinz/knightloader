// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
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
  const selector = (label: string, variant: 'default' | 'well' = 'well', links = false) => (
    <Tabs
      key={label}
      variant={variant}
      label={label}
      active="dark"
      onSelect={() => {}}
      items={[
        { id: 'dark', label: 'Dark', href: links ? '/dark' : undefined },
        { id: 'light', label: 'Light', href: links ? '/light' : undefined },
      ]}
    />
  );
  /** Draws, and lets the rainbow engine count the selectors out. */
  const draw = (page: ReactNode) => act(async () => root.render(page));
  const strip = (label: string) => document.querySelector<HTMLElement>(`[aria-label="${label}"]`)!;
  const position = (label: string) => strip(label).style.getPropertyValue('--item-hue');

  it.each(['well', 'default'] as const)('owns one palette position as a %s, which its segments share', async (variant) => {
    await draw(selector('Theme', variant));
    expect(position('Theme')).toBe('var(--rb-0)');
    for (const segment of strip('Theme').querySelectorAll<HTMLElement>('[role="tab"]')) {
      expect(segment.classList.contains('glim-hue')).toBe(true);
      expect(segment.style.getPropertyValue('--item-hue')).toBe('');
    }
    expect(strip('Theme').querySelector('[aria-selected="true"]')!.className).toContain('bg-accent');
  });

  it('counts the selectors through the palette in the order they stand', async () => {
    await draw([selector('Theme'), selector('Corners'), selector('Motion'), selector('Row height')]);
    expect(['Theme', 'Corners', 'Motion', 'Row height'].map(position)).toEqual([
      'var(--rb-0)',
      'var(--rb-1)',
      'var(--rb-2)',
      'var(--rb-3)',
    ]);
  });

  it('counts again when a selector appears between two others', async () => {
    await draw([selector('Theme'), selector('Motion')]);
    expect(position('Motion')).toBe('var(--rb-1)');
    await draw([selector('Theme'), selector('Corners'), selector('Motion')]);
    expect(['Theme', 'Corners', 'Motion'].map(position)).toEqual(['var(--rb-0)', 'var(--rb-1)', 'var(--rb-2)']);
    await draw([selector('Theme'), selector('Motion')]);
    expect(position('Motion')).toBe('var(--rb-1)');
  });

  it('counts the selectors of a window on their own', async () => {
    await draw(
      <>
        {selector('Theme')}
        <div aria-modal="true">
          {selector('Category')}
          {selector('Priority')}
        </div>
        {selector('Corners')}
      </>,
    );
    expect(['Theme', 'Corners'].map(position)).toEqual(['var(--rb-0)', 'var(--rb-1)']);
    expect(['Category', 'Priority'].map(position)).toEqual(['var(--rb-0)', 'var(--rb-1)']);
  });

  it('leaves a strip that chooses a page its position per tab, and out of the count', async () => {
    await draw([selector('Pages', 'default', true), selector('Theme')]);
    const [first, second] = strip('Pages').querySelectorAll<HTMLElement>('[role="tab"]');
    expect(first.classList.contains('glim-hue')).toBe(true);
    expect(first.style.getPropertyValue('--item-hue')).not.toBe(second.style.getPropertyValue('--item-hue'));
    expect(position('Pages')).toBe('');
    expect(position('Theme')).toBe('var(--rb-0)');
  });
});
