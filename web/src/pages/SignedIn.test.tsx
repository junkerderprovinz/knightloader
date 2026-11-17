// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { SignedIn } from './SignedIn';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  history.replaceState(null, '', '/');
});

async function show(search: string) {
  history.replaceState(null, '', `/signedin${search}`);
  await act(async () =>
    root.render(
      <I18nProvider>
        <SignedIn />
      </I18nProvider>,
    ),
  );
}

it('says the site is signed in and the tab can go', async () => {
  await show('?site=Patreon');
  expect(host.textContent).toContain(en['signedIn.title'].replace('{site}', 'Patreon'));
  expect(host.textContent).toContain(en['signedIn.body']);
  expect(document.title).toBe(en['signedIn.title'].replace('{site}', 'Patreon'));
});

it('still reads without a site', async () => {
  await show('');
  expect(host.textContent).toContain(en['signedIn.titleBare']);
  expect(host.textContent).toContain(en['signedIn.body']);
});
