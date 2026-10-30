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
