// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { PageBoundary } from './PageBoundary';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function Broken(): never {
  throw new Error('picture failed');
}

it('shows a card with a reload button in place of a page that throws', async () => {
  const quiet = vi.spyOn(console, 'error').mockImplementation(() => {});
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  await act(async () =>
    root.render(
      <I18nProvider>
        <p>sidebar</p>
        <PageBoundary>
          <Broken />
        </PageBoundary>
      </I18nProvider>,
    ),
  );
  expect(host.textContent).toContain('sidebar');
  expect(host.textContent).toContain('This page ran into an error');
  expect(host.querySelector('button')!.textContent).toBe('Reload');
  act(() => root.unmount());
  host.remove();
  quiet.mockRestore();
});
