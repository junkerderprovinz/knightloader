// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ConnectionsCard } from './Connections';
import { SettingsProvider, type SettingsDraft } from './context';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom has no ResizeObserver, which a selector in the window measures with.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// The card reads the running downloads off the live stream, which a test has none of.
vi.mock('../../lib/useTasks', () => ({ useTasks: () => ({}) }));

let root: Root;
let host: HTMLDivElement;

const proxy = { id: 'a', type: 'http', host: 'proxy.example.org', port: 8080, enabled: true, order: 0 };

beforeEach(() => {
  // The picture of the routes stands still, so a line turns with the answer.
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function mount(connections: object[] = [proxy]) {
  const draft = {
    cfg: { connections },
    patch: () => {},
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  // The card's module switch finds no module and draws nothing.
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <MemoryRouter>
        <SettingsProvider draft={draft} features={features}>
          <ConnectionsCard hue={0} />
        </SettingsProvider>
      </MemoryRouter>,
    ),
  );
}

const row = () => host.querySelector<HTMLButtonElement>('button[aria-haspopup="dialog"]')!;
const sheet = () => host.querySelector<HTMLElement>('[role="dialog"]');
const named = (text: string, within: Element = host) =>
  [...within.querySelectorAll('button')].find((b) => b.textContent === text)!;
const route = () => host.querySelector('[data-route="a"]')!;

function answer(report: { ok: boolean; detail: string }) {
  const asked = vi.fn((_url: string, _init?: RequestInit) =>
    Promise.resolve(
      new Response(JSON.stringify({ stage: 'dial', millis: 12, ...report }), {
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  );
  vi.stubGlobal('fetch', asked);
  return asked;
}

describe('a connection row', () => {
  it('opens its settings in a window over the page instead of unfolding', async () => {
    await mount();
    expect(sheet()).toBeNull();
    expect(row().hasAttribute('aria-expanded')).toBe(false);

    await act(async () => row().click());
    const win = sheet()!;
    expect(win.getAttribute('aria-modal')).toBe('true');
    // Named after the row it belongs to, with the row's fields in it.
    expect(win.querySelector('h2')!.textContent).toBe('proxy.example.org:8080');
    expect(win.querySelector<HTMLInputElement>('input[placeholder="proxy.example.org"]')!.value).toBe('proxy.example.org');

    await act(async () => named('Close').click());
    expect(sheet()).toBeNull();
  });

  it('shows what the test came to on the test button, with the reason above it', async () => {
    answer({ ok: false, detail: 'connection refused' });
    await mount();
    await act(async () => row().click());
    const test = named('Test', sheet()!);
    await act(async () => test.click());

    expect(test.textContent).toBe('Not connected');
    expect(test.className).toContain('bg-statusFailSolid');
    const reason = [...sheet()!.querySelectorAll('p')].find((p) => p.textContent === 'connection refused')!;
    expect(reason.className).not.toMatch(/text-status/);
    // The reason stands before the button in the window.
    expect(reason.compareDocumentPosition(test) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

describe('the picture of the routes', () => {
  it('draws the row as a route out of this instance', async () => {
    await mount();
    expect(route().getAttribute('data-state')).toBe('on');
    expect(route().textContent).toContain('proxy.example.org:8080');
  });

  it('dials every route in use from its one test button and turns the line with the answer', async () => {
    const asked = answer({ ok: true, detail: 'proxy.example.org:8080 answers' });
    await mount();
    const test = named('Test');
    await act(async () => test.click());

    expect(JSON.parse(String(asked.mock.calls[0][1]!.body))).toEqual({ entry: proxy, target: '' });
    expect(route().getAttribute('data-state')).toBe('ok');
    expect(test.textContent).toBe('Connected');
  });

  it('says why a route did not answer, above the button', async () => {
    answer({ ok: false, detail: 'proxy.example.org:8080 could not be reached' });
    await mount();
    const test = named('Test');
    await act(async () => test.click());

    expect(route().getAttribute('data-state')).toBe('fail');
    expect(test.textContent).toBe('Not connected');
    const reason = [...host.querySelectorAll('p')].find((p) => p.textContent === 'proxy.example.org:8080 could not be reached')!;
    expect(reason.compareDocumentPosition(test) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('drops the reason a test gives for a route changed while it ran', async () => {
    let reply = (_r: Response) => {};
    vi.stubGlobal('fetch', () => new Promise<Response>((done) => (reply = done)));
    await mount();
    await act(async () => named('Test').click());
    await mount([{ ...proxy, port: 8081 }]);
    await act(async () =>
      reply(
        new Response(JSON.stringify({ stage: 'dial', millis: 12, ok: false, detail: 'refused' }), {
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    expect([...host.querySelectorAll('p')].some((p) => p.textContent === 'refused')).toBe(false);
    expect(named('Test').textContent).toBe('Test');
  });

  it('opens a row from its route', async () => {
    await mount();
    await act(async () =>
      route().querySelector('[role="button"]')!.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    );
    expect(sheet()!.querySelector('h2')!.textContent).toBe('proxy.example.org:8080');
  });
});
