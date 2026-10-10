// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { AddLinksForm } from './AddLinksForm';
import { useAddLinks } from './AddLinksAction';
import { PageActions } from './PageActions';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let switched: unknown[];
let sent: { links: string }[];

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

/** Nothing listens, so no broadcast ever comes. */
class QuietSocket {
  readyState = 0;
  send() {}
  close() {}
}

const cnl = { id: 'cnl', verdict: 'shipped', page: 'collector', enabled: false, switch: 'runtime', parked: false };

beforeEach(() => {
  switched = [];
  sent = [];
  vi.stubGlobal('WebSocket', QuietSocket);
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/features') return reply({ pages: [], modules: [cnl] });
      if (url === '/api/features/cnl') {
        switched.push(JSON.parse(String(init?.body)));
        return reply({ pages: [], modules: [{ ...cnl, enabled: true }] });
      }
      if (url === '/api/links' && init?.method === 'POST') {
        sent.push(JSON.parse(String(init.body)));
        return reply([{ id: 't1', url: 'https://example.com/a.bin', status: 'collected' }]);
      }
      if (url.startsWith('/api/uistate')) return reply({});
      return reply([]);
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

const words = (within: ParentNode) =>
  [...within.querySelectorAll('button')].map((b) => b.textContent).filter(Boolean);
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].reverse().find((b) => b.textContent === text)!;
const toggle = (name: string) => document.querySelector<HTMLButtonElement>(`[role="switch"][aria-label="${name}"]`);

function Card() {
  return (
    <I18nProvider>
      <ToastProvider>
        <AddLinksForm pkg="" onPkgChange={() => {}} onStaged={() => {}} onChooseFile={() => {}} onFilesDropped={() => {}} />
      </ToastProvider>
    </I18nProvider>
  );
}

it('keeps Options, the file picker and Add on the card and the two standing ways in under Options', async () => {
  await act(async () => root.render(<Card />));
  // jsdom has no clipboard to read, which is where the paste button stays out.
  expect(words(host)).toEqual(['Options', 'Choose a file', 'Add links']);
  expect(toggle('Watch the clipboard')).toBeNull();

  await act(async () => button('Options').click());
  expect(toggle("Click'n'Load")?.getAttribute('aria-checked')).toBe('false');
  expect(toggle('Watch the clipboard')).not.toBeNull();

  await act(async () => toggle("Click'n'Load")!.click());
  expect(switched).toEqual([{ enabled: true }]);
  expect(toggle("Click'n'Load")?.getAttribute('aria-checked')).toBe('true');

  // Folded again, since the interface remembers it open for the next test.
  await act(async () => button('Options').click());
});

function Page() {
  const addLinks = useAddLinks();
  return (
    <I18nProvider>
      <ToastProvider>
        <PageActions>{addLinks.action}</PageActions>
        {addLinks.dialog}
      </ToastProvider>
    </I18nProvider>
  );
}

it('opens the same form in a window from the floating Add links, and closes it once links are sent', async () => {
  await act(async () => root.render(<Page />));
  expect(document.querySelector('[role="dialog"]')).toBeNull();

  await act(async () => button('Add links').click());
  const dialog = document.querySelector('[role="dialog"]')!;
  expect(dialog.querySelector('h2')?.textContent).toContain('Add links');
  expect(words(dialog)).toEqual(['Options', 'Choose a file', 'Add links', 'Close']);

  const box = dialog.querySelector('textarea')!;
  Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(box, 'https://example.com/a.bin');
  act(() => box.dispatchEvent(new Event('input', { bubbles: true })));
  await act(async () => button('Add links').click());

  expect(sent.map((s) => s.links)).toEqual(['https://example.com/a.bin']);
  expect(document.querySelector('[role="dialog"]')).toBeNull();
});
