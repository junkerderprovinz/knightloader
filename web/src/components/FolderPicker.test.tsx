// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { FolderPicker } from './FolderPicker';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let asked: string[];

const shares = {
  path: '/mnt/user',
  tail: '',
  exists: true,
  listed: '/mnt/user',
  parent: '/mnt',
  roots: ['/'],
  entries: [
    { name: 'appdata', path: '/mnt/user/appdata', target: '/mnt/cache/appdata' },
    { name: 'media', path: '/mnt/user/media' },
  ],
  truncated: false,
};

const appdata = {
  ...shares,
  path: '/mnt/user/appdata',
  exists: false,
  link: { path: '/mnt/user/appdata', target: '/mnt/cache/appdata', missing: '/mnt/cache' },
};

/** Answers every listing with the one for the path asked, as the server would. */
function serve() {
  asked = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      const path = new URL(url, 'http://kl').searchParams.get('path') ?? '';
      asked.push(path);
      const body = path === '/mnt/user/appdata' ? appdata : shares;
      return Promise.resolve(
        new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }),
      );
    }),
  );
}

async function open(value: string, missingHint?: string) {
  await act(async () =>
    root.render(
      <I18nProvider>
        <FolderPicker value={value} missingHint={missingHint} onPick={() => {}} onClose={() => {}} />
      </I18nProvider>,
    ),
  );
}

const text = () => document.body.textContent ?? '';
const button = (label: string) =>
  [...document.querySelectorAll('button')].find((b) => b.textContent?.includes(label));

beforeEach(() => {
  serve();
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

it('explains a folder that is a link the server cannot follow', async () => {
  await open('/mnt/user/appdata');
  expect(text()).toContain('a link to /mnt/cache/appdata, which KnightLoader cannot see');
  expect(text()).toContain('Add /mnt/cache as a path');
  expect(text()).not.toContain('does not exist yet');
});

it('explains the link in place of a field’s own hint for a missing folder', async () => {
  await open('/mnt/user/appdata', 'There is no folder here.');
  expect(text()).toContain('KnightLoader cannot see');
  expect(text()).not.toContain('There is no folder here.');
});

it('offers no new folder behind such a link', async () => {
  await open('/mnt/user/appdata');
  expect(button('New folder')?.disabled).toBe(true);
});

it('does not take a folder behind such a link', async () => {
  await open('/mnt/user/appdata');
  expect(button('Use this folder')?.disabled).toBe(true);
});

it('lists a share behind such a link with where it leads, and opens the explanation on a click', async () => {
  await open('/mnt/user');
  const entry = button('appdata');
  expect(entry?.textContent).toContain('/mnt/cache/appdata');
  expect(text()).not.toContain('KnightLoader cannot see');

  await act(async () => entry!.click());
  expect(asked[asked.length - 1]).toBe('/mnt/user/appdata');
  expect(text()).toContain('KnightLoader cannot see');
});
