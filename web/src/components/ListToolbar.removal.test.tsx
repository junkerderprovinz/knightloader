// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { useRemoval } from './ListToolbar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const task = { id: 't1', name: 'film.mkv', loaded: 4096 } as Task;

function serve(passwordSet: boolean) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      const body = url.startsWith('/api/auth') ? { enabled: passwordSet, authenticated: true } : {};
      return Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } }));
    }),
  );
}

function Harness() {
  const removal = useRemoval({ all: [task], selected: new Set([task.id]), base: '/api', onDone: () => {} });
  return (
    <>
      <button type="button" data-ask onClick={() => removal.askWithFiles([task.id])} />
      {removal.dialog}
    </>
  );
}

async function openDialog() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <Harness />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  await act(async () => host.querySelector<HTMLButtonElement>('[data-ask]')!.click());
  return document.body.querySelector('[role="dialog"]')!;
}

const buttons = (el: Element) => [...el.querySelectorAll('button')].map((b) => b.textContent);

describe('the removal dialog', () => {
  it('offers to delete the files once a password is set', async () => {
    serve(true);
    const dialog = await openDialog();
    expect(buttons(dialog)).toContain('Remove and delete the files');
  });

  it('takes rows only, and says why, while no password is set', async () => {
    serve(false);
    const dialog = await openDialog();
    expect(buttons(dialog)).not.toContain('Remove and delete the files');
    expect(buttons(dialog)).toContain('Remove from the list');
    expect(dialog.textContent).toContain('Deleting the files needs a login password');
    expect(dialog.textContent).toContain('Settings > Security');
  });
});
