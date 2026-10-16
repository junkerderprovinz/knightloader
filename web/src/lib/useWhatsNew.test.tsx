// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { WhatsNewEntry } from '../components/whatsnew/WhatsNew';
import { I18nProvider } from './i18n';
import { flushUIState } from './uistate';
import { closeWhatsNew, startWhatsNew, useWhatsNew, useWhatsNewOpen, type WhatsNew } from './useWhatsNew';
import { CHANGES } from './whatsNew';

// The update below lands on the oldest release the list knows, so its changes are all there is to mark.
const upToTwoZero = CHANGES.filter((c) => c.since === '2.0.0');

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let written: unknown[];
let news: WhatsNew | null;
let open: boolean;

function Probe() {
  news = useWhatsNew();
  open = useWhatsNewOpen();
  return null;
}

beforeEach(() => {
  written = [];
  // An instance that finished its first-run tour under a release that kept no record.
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') written.push(JSON.parse(String(init.body)));
      const answer = url === '/api/health' ? { status: 'ok', version: 'v2.0.0' } : { 'onboarding.done': true };
      return Promise.resolve(new Response(JSON.stringify(answer), { headers: { 'Content-Type': 'application/json' } }));
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

it('marks an update, opens the window once and remembers what was seen', async () => {
  await act(async () => root.render(<Probe />));
  expect(news).toBeNull();

  await act(async () => {
    await startWhatsNew();
  });
  expect(open).toBe(true);
  expect(news!.version).toBe('v2.0.0');
  expect(news!.unseen).toEqual(upToTwoZero);
  expect(news!.releases).toEqual(['2.0.0']);

  await act(async () => news!.markSeen(['seed-ring', 'week-band']));
  expect(news!.seen).toEqual(new Set(['seed-ring', 'week-band']));
  expect(news!.unseen).toHaveLength(upToTwoZero.length - 2);

  await act(async () => {
    await flushUIState();
  });
  expect(written[written.length - 1]).toMatchObject({
    'onboarding.done': true,
    whatsNew: { version: 'v2.0.0', from: '', seen: ['seed-ring', 'week-band'], notes: true },
  });
});

it('leads from the event list to the window, with the number still unseen', async () => {
  const onOpen = vi.fn();
  await act(async () => {
    await startWhatsNew();
  });
  await act(async () =>
    root.render(
      <I18nProvider>
        <Probe />
        <WhatsNewEntry onOpen={onOpen} />
      </I18nProvider>,
    ),
  );
  act(() => closeWhatsNew());
  const entry = host.querySelector('button')!;
  expect(entry.textContent).toBe(`New in 2.0.0${news!.unseen.length}`);

  await act(async () => entry.click());
  expect(onOpen).toHaveBeenCalled();
  expect(open).toBe(true);
});
