// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

// WATCH_SUPPORTED is read when the module loads, so the clipboard has to be
// there before the import.
vi.hoisted(() => {
  Object.defineProperty(navigator, 'clipboard', { value: { readText: async () => '' }, configurable: true });
});

import { I18nProvider, useT } from '../lib/i18n';
import { GlobalIntake } from './GlobalIntake';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let fetches: string[];
let i18n: ReturnType<typeof useT>;

function LangSwitch() {
  i18n = useT();
  return null;
}

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem('knightloader.clipboardWatch', 'on');
  fetches = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      fetches.push(`${init?.method ?? 'GET'} ${url}`);
      return new Response(init?.method === 'PUT' ? '{"stop":false}' : null, { status: init?.method === 'PUT' ? 200 : 204 });
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

it('keeps the clipboard watch listed while the translations arrive', async () => {
  await act(async () =>
    root.render(
      <I18nProvider>
        <LangSwitch />
        <GlobalIntake />
      </I18nProvider>,
    ),
  );
  const english = i18n.t;
  await act(async () => i18n.setLang('de'));
  await vi.waitFor(() => expect(i18n.t).not.toBe(english));
  await act(async () => {});

  const lease = fetches.filter((f) => f.includes('/api/clipboard-watchers/'));
  expect(lease.filter((f) => f.startsWith('DELETE'))).toEqual([]);
  expect(lease.filter((f) => f.startsWith('PUT'))).toHaveLength(1);
});
