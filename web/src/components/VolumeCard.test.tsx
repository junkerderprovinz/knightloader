// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
let asked: string[];

function curve() {
  return {
    unit: 'day',
    buckets: [{ key: '2026-09-24', bytes: 5e9, count: 2, unsized: 0, byHost: null, byResolver: null }],
    timeZone: 'UTC',
    trimmed: false,
  };
}

beforeEach(async () => {
  vi.resetModules();
  asked = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string) => {
      const url = new URL(input, 'http://localhost');
      if (url.pathname === '/api/stats/volume') {
        const span = url.searchParams.get('span') ?? '';
        asked.push(span);
        return Promise.resolve(new Response(JSON.stringify(curve()), { status: 200 }));
      }
      if (url.pathname === '/api/uistate') return Promise.resolve(new Response('{}', { status: 200 }));
      return new Promise(() => {});
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  const { VolumeCard } = await import('./VolumeCard');
  await act(async () => root.render(<VolumeCard />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function chosenStep(): string | null {
  const strip = document.querySelector('[aria-label="Time span"]')!;
  return strip.querySelector('[aria-selected="true"]')?.textContent ?? null;
}

function step(name: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find((b) => b.textContent === name)!;
}

function field() {
  return document.querySelector<HTMLInputElement>('input[inputmode="numeric"]')!;
}

async function type(text: string) {
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  await act(async () => {
    setValue.call(field(), text);
    field().dispatchEvent(new Event('input', { bubbles: true }));
  });
}

async function leave() {
  await act(async () => {
    field().dispatchEvent(new FocusEvent('focusout', { bubbles: true }));
  });
}

describe('VolumeCard', () => {
  it('opens on thirty days', () => {
    expect(chosenStep()).toBe('30 days');
    expect(asked).toEqual(['30d']);
  });

  it('draws the steps under the chart', () => {
    const chart = document.querySelector('svg[role="img"]')!;
    const strip = document.querySelector('[aria-label="Time span"]')!;
    expect(chart.compareDocumentPosition(strip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('takes the choice from the steps once a valid count is typed', async () => {
    await type('45');
    expect(chosenStep()).toBeNull();
    expect(asked[asked.length - 1]).toBe('45d');
    expect(field().className).toContain('border-accent');
  });

  it('hands the choice back to a step the typed count equals', async () => {
    await type('90');
    expect(chosenStep()).toBeNull();
    await leave();
    expect(chosenStep()).toBe('90 days');
    expect(field().value).toBe('');
  });

  it('empties the field when a step is picked', async () => {
    await type('45');
    await act(async () => step('7 days').click());
    expect(field().value).toBe('');
    expect(chosenStep()).toBe('7 days');
    expect(asked[asked.length - 1]).toBe('7d');
  });

  it('goes back to the step chosen before once the field is emptied', async () => {
    await act(async () => step('12 months').click());
    await type('45');
    await type('');
    expect(chosenStep()).toBe('12 months');
    expect(asked[asked.length - 1]).toBe('12m');
  });

  it('refuses text that is no count and asks for nothing', async () => {
    await type('800');
    await leave();
    expect(asked).toEqual(['30d']);
    expect(field().getAttribute('aria-invalid')).toBe('true');
  });

  it('steps the count with the arrow keys', async () => {
    await type('45');
    await act(async () => {
      field().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
    });
    expect(field().value).toBe('46');
    await act(async () => {
      field().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', shiftKey: true, bubbles: true }));
    });
    expect(field().value).toBe('36');
    expect(asked[asked.length - 1]).toBe('36d');
  });
});
