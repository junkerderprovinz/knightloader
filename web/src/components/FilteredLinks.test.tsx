// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { FilteredLinks } from './FilteredLinks';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })),
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

const held = (i: number) =>
  ({
    id: `t${i}`,
    url: `https://rapidgator.example/file/${i}`,
    name: `Film.Part${i}.rar`,
    skipped: true,
    skipCode: 'downloaded',
    skipParams: { name: `Film.Part${i}.rar`, finished: '2026-10-30T12:00:00Z' },
    origin: 'cnl',
    createdAt: '2026-10-30T12:00:00Z',
  }) as unknown as Task;

it('lists every link its title counts', async () => {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <FilteredLinks held={[held(1), held(2), held(3)]} />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  expect(host.querySelector('h3')!.textContent).toContain('3');
  expect(host.querySelectorAll('.glim-row')).toHaveLength(3);
  const foot = host.querySelector('footer')!;
  expect([...foot.querySelectorAll('button')].map((b) => b.textContent)).not.toContain('Show');
});

const renderHeld = (rows: Task[]) =>
  act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <FilteredLinks held={rows} />
        </ToastProvider>
      </I18nProvider>,
    ),
  );

const repeat = (id: string, variant?: string) =>
  ({
    id,
    url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL1',
    name: 'Never Gonna Give You Up',
    variant,
    skipped: true,
    skipCode: 'downloaded',
    skipParams: { name: 'Clip [1080p].mp4', finished: '2026-10-03T18:15:00Z', folder: '/media/Rick Astley' },
    origin: 'feed',
    createdAt: '2026-11-01T08:00:00Z',
  }) as unknown as Task;

it('marks a repeat as already downloaded with its folder and offers to download it anyway', async () => {
  await renderHeld([repeat('v')]);
  const row = host.querySelector('.glim-row')!;
  expect(row.textContent).toContain('already downloaded as "Clip [1080p].mp4"');
  expect(row.textContent).toContain('/media/Rick Astley');
  expect(row.textContent).toContain('subscription');
  expect([...row.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Download anyway']);
});

it('keeps Restore for a link a rule rejected', async () => {
  await renderHeld([{ ...repeat('r'), skipCode: 'filterRule', skipParams: { rule: 'no samples' } } as Task]);
  const row = host.querySelector('.glim-row')!;
  expect([...row.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Restore']);
});

it('shows a held video once for its whole family', async () => {
  await renderHeld([repeat('thumb', 'thumbnail'), repeat('video', 'video'), repeat('audio', 'audio')]);
  expect(host.querySelectorAll('.glim-row')).toHaveLength(1);
  expect(host.querySelector('h3')!.textContent).toContain('1');

  await act(async () => host.querySelector<HTMLButtonElement>('.glim-row button')!.click());
  const call = vi.mocked(fetch).mock.calls.find(([url]) => url === '/api/collector/filtered/restore')!;
  expect(JSON.parse(String(call[1]!.body))).toEqual({ ids: ['video'] });
});
