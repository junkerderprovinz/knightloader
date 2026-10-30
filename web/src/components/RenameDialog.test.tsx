// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { RenameLinkDialog } from './RenameDialog';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const NAME = 'Die.Chronik.der.Burg.S01E01.Der.Anfang.German.DL.1080p.WEB.h264-BURGHOF.mkv';

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

async function open() {
  const task = { id: 't1', name: NAME, url: 'https://example.org/f' } as Task;
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <RenameLinkDialog task={task} base="/api" onClose={() => {}} />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  return document.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!;
}

it('shows a long name whole in a wide window', async () => {
  const field = await open();
  expect(field.value).toBe(NAME);
  expect(document.querySelector('[role="dialog"]')!.className).toContain('max-w-3xl');
});

it('selects the name without its extension', async () => {
  const field = await open();
  expect(field.selectionStart).toBe(0);
  expect(field.selectionEnd).toBe(NAME.lastIndexOf('.'));
});

it('keeps the name on one line when a pasted name brings a line break', async () => {
  const field = await open();
  const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
  await act(async () => {
    set.call(field, 'Die Chronik\nder Burg.mkv');
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  expect(field.value).toBe('Die Chronikder Burg.mkv');
});
