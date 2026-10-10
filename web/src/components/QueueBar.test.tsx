// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { InstanceProvider } from '../lib/instance';
import { QueueBar } from './QueueBar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let halted: boolean;
let writes: boolean[];

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  writes = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url !== '/api/queue') return new Response('not here', { status: 404 });
      if (init?.method && init.method !== 'GET') {
        halted = (JSON.parse(String(init.body)) as { halted: boolean }).halted;
        writes.push(halted);
      }
      return reply({ halted, running: 1 });
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
});

async function show(): Promise<HTMLButtonElement[]> {
  await act(async () => {
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <InstanceProvider>
            <QueueBar />
          </InstanceProvider>
        </MemoryRouter>
      </I18nProvider>,
    );
  });
  return [...host.querySelectorAll('button')];
}

const filled = (b: HTMLButtonElement) => b.className.split(/\s+/).includes('bg-accent');

it('fills Play with the accent while the queue runs', async () => {
  halted = false;
  const [play, pause, stop] = await show();
  expect(play.getAttribute('aria-pressed')).toBe('true');
  expect(pause.getAttribute('aria-pressed')).toBe('false');
  expect([play, pause, stop].map(filled)).toEqual([true, false, false]);
});

it('shows a halted queue without the accent', async () => {
  halted = true;
  const [play, pause, stop] = await show();
  expect(pause.getAttribute('aria-pressed')).toBe('true');
  expect([play, pause, stop].map(filled)).toEqual([false, false, false]);
});

it('writes nothing when the state in force is pressed again', async () => {
  halted = true;
  const [play, pause] = await show();
  await act(async () => pause.click());
  expect(writes).toEqual([]);
  await act(async () => play.click());
  expect(writes).toEqual([false]);
});
