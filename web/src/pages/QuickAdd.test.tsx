// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { QuickAdd } from './QuickAdd';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let posts: { url: string; body: unknown }[];

beforeEach(() => {
  posts = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'POST') posts.push({ url, body: JSON.parse(String(init.body)) });
      const created = [{ id: 't1', name: 'a.zip', status: 'collected' }];
      return Promise.resolve(new Response(JSON.stringify(created), { headers: { 'Content-Type': 'application/json' } }));
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

async function open(query: string) {
  await act(async () =>
    root.render(
      <MemoryRouter initialEntries={[`/quickadd${query}`]}>
        <I18nProvider>
          <QuickAdd />
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
}

const addButton = () => [...host.querySelectorAll('button')].find((b) => b.textContent === 'Add')!;

describe('QuickAdd', () => {
  it('fills in what a link hands it and adds nothing until Add is pressed', async () => {
    await open('?url=https%3A%2F%2Ffiles.example%2Fa.zip&title=Holiday&to=cellar');
    expect(posts).toEqual([]);
    expect(host.querySelector('textarea')!.value).toBe('https://files.example/a.zip');
    expect(host.innerHTML).toContain('Check what came in, then press Add.');
    expect(host.textContent).toContain('Sending to “cellar”.');
    expect(document.activeElement).toBe(addButton());

    await act(async () => addButton().click());
    expect(posts).toEqual([
      { url: '/api/instances/cellar/links', body: { links: 'https://files.example/a.zip', package: 'Holiday' } },
    ]);
    expect(host.textContent).toContain('Added “a.zip” to the collector.');
  });

  it('opens on an empty box when nothing was handed over', async () => {
    await open('');
    expect(posts).toEqual([]);
    expect(host.querySelector('textarea')!.value).toBe('');
    expect(addButton().disabled).toBe(true);
  });
});
