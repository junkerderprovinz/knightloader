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
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response('{}', { headers: { 'Content-Type': 'application/json' } }))),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const task = { id: 't1', name: 'film.mkv', loaded: 4096 } as Task;

function Harness({ withFiles }: { withFiles: boolean }) {
  const removal = useRemoval({ all: [task], selected: new Set([task.id]), base: '/api', onDone: () => {} });
  return (
    <>
      <button type="button" data-ask onClick={() => removal.ask([task.id], withFiles)} />
      {removal.dialog}
    </>
  );
}

async function openDialog(withFiles = false) {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <Harness withFiles={withFiles} />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  await act(async () => host.querySelector<HTMLButtonElement>('[data-ask]')!.click());
  return document.body.querySelector('[role="dialog"]')!;
}

const buttons = (el: Element) => [...el.querySelectorAll('button')].map((b) => b.textContent);

const filesSwitch = (el: Element) => el.querySelector<HTMLButtonElement>('[role="switch"]')!;

describe('the removal dialog', () => {
  it('keeps the files unless the switch is turned on', async () => {
    const dialog = await openDialog();
    expect(filesSwitch(dialog).getAttribute('aria-checked')).toBe('false');
    expect(buttons(dialog)).toContain('Remove from the list');
    expect(buttons(dialog)).not.toContain('Remove and delete the files');
    expect(dialog.textContent).not.toContain('cannot be undone');
  });

  it('deletes the files once the switch is on, and says how much', async () => {
    const dialog = await openDialog();
    await act(async () => filesSwitch(dialog).click());
    expect(buttons(dialog)).toContain('Remove and delete the files');
    expect(dialog.textContent).toContain('This also erases 1 file(s)');
  });

  it('starts with the switch on for Shift+Del', async () => {
    const dialog = await openDialog(true);
    expect(filesSwitch(dialog).getAttribute('aria-checked')).toBe('true');
  });
});
