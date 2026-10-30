// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { Notice, NoticeRow } from './Notice';
import { Button } from './ui';

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

async function show() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <Notice id="filtered" title="12 links rejected" actions={<Button kind="secondary">Restore all</Button>}>
          <NoticeRow reason="The link filter: too big" name="https://example.org/a.rar" at="30.10.26" />
        </Notice>
      </I18nProvider>,
    ),
  );
}

it('puts its own actions in one row at its foot, after the box that switches it off', async () => {
  await show();
  const foot = host.querySelector('footer')!;
  expect(foot.querySelector('input[type="checkbox"]')).not.toBeNull();
  expect(foot.lastElementChild!.textContent).toBe('Restore all');
});

it('goes away once its box is ticked', async () => {
  await show();
  await act(async () => host.querySelector<HTMLInputElement>('footer input[type="checkbox"]')!.click());
  expect(host.querySelector('section')).toBeNull();
});
