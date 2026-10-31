// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { MemoryRouter } from 'react-router-dom';
import { ToastProvider } from '../lib/toast';
import { AddLinksForm } from './AddLinksForm';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// The options' selectors measure their box, and jsdom lays nothing out.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

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
      <MemoryRouter>
        <ToastProvider>
          <AddLinksForm pkg="" onPkgChange={() => {}} onStaged={() => {}} onChooseFile={() => {}} onFilesDropped={() => {}} />
        </ToastProvider>
      </MemoryRouter>
    </I18nProvider>
  );
}

it('keeps the two standing ways in on the card as buttons that show their state', async () => {
  await act(async () => root.render(<Card />));
  // jsdom has no clipboard to read, which is where the paste button stays out.
  expect(words(host)).toEqual(['Options', "Click'n'Load", 'Watch the clipboard', 'Choose a file', 'Add links']);
  expect(button("Click'n'Load").getAttribute('aria-pressed')).toBe('false');
  expect(button('Watch the clipboard').getAttribute('aria-pressed')).toBe('false');

  await act(async () => button("Click'n'Load").click());
  expect(switched).toEqual([{ enabled: true }]);
  expect(button("Click'n'Load").getAttribute('aria-pressed')).toBe('true');
});

it('leaves the two standing ways in out of the options window', async () => {
  await act(async () => root.render(<Card />));
  await act(async () => button('Options').click());
  const dialog = document.querySelector('[role="dialog"]')!;
  expect(dialog.textContent).not.toContain("Click'n'Load");
  expect(dialog.textContent).not.toContain('Watch the clipboard');
});

it('asks for the options of a batch in a window and marks its button while one is set', async () => {
  await act(async () => root.render(<Card />));
  const marked = () => host.querySelector('[data-marked]') !== null;
  expect(marked()).toBe(false);

  await act(async () => button('Options').click());
  const dialog = document.querySelector('[role="dialog"]')!;
  expect(dialog.querySelector('h2')?.textContent).toContain('Options');

  await act(async () => toggle('Overrule a matching Packagizer rule')!.click());
  await act(async () => button('Close').click());
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(marked()).toBe(true);

  // Back off, since the interface remembers the switch for the next test.
  await act(async () => button('Options').click());
  await act(async () => toggle('Overrule a matching Packagizer rule')!.click());
});

it('marks the options button while a package name is set', async () => {
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <ToastProvider>
            <AddLinksForm pkg="Holiday" onPkgChange={() => {}} onStaged={() => {}} onChooseFile={() => {}} onFilesDropped={() => {}} />
          </ToastProvider>
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
  expect(host.querySelector('[data-marked]')).not.toBeNull();
});

it('sends the links in the box when Add links is pressed', async () => {
  await act(async () => root.render(<Card />));
  const box = host.querySelector('textarea')!;
  Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(box, 'https://example.com/a.bin');
  act(() => box.dispatchEvent(new Event('input', { bubbles: true })));
  await act(async () => button('Add links').click());

  expect(sent.map((s) => s.links)).toEqual(['https://example.com/a.bin']);
});
