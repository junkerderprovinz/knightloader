// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';

// WATCH_SUPPORTED is read when the module loads, so the clipboard has to be
// there before the import.
vi.hoisted(() => {
  Object.defineProperty(navigator, 'clipboard', { value: { readText: async () => '' }, configurable: true });
});

import { I18nProvider } from '../lib/i18n';
import { GlobalIntake } from './GlobalIntake';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it('takes the lease only with the instance the stored target names', async () => {
  localStorage.setItem('knightloader.clipboardWatch', 'on');
  localStorage.setItem('knightloader.clipboardWatcher', 'tab-1');
  const fetches: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET';
      fetches.push(`${method} ${url}`);
      if (url.startsWith('/api/uistate')) return Response.json({ clipboardWatchTarget: 'nas' });
      return new Response(method === 'PUT' ? '{"stop":false}' : null, { status: method === 'PUT' ? 200 : 204 });
    }),
  );
  const root = createRoot(document.createElement('div'));
  await act(async () =>
    root.render(
      <I18nProvider>
        <GlobalIntake />
      </I18nProvider>,
    ),
  );
  await act(async () => {});

  expect(fetches.filter((f) => f.includes('/clipboard-watchers/'))).toEqual([
    'PUT /api/instances/nas/clipboard-watchers/tab-1',
  ]);
  act(() => root.unmount());
  vi.unstubAllGlobals();
});
