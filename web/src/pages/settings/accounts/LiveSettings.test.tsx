// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { ToastProvider } from '../../../lib/toast';
import { useDraft } from '../context';
import { LiveSettings } from './LiveSettings';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

class QuietSocket {
  readyState = 0;
  send() {}
  close() {}
}

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

let send: (fields: Partial<Settings>) => Promise<void> = async () => {};

function Limit() {
  const draft = useDraft();
  send = draft.patchNow;
  return <output>{String(draft.cfg.speedLimit)}</output>;
}

it('keeps a newer edit when the reply to an older save comes last', async () => {
  const replies: ((s: Partial<Settings>) => void)[] = [];
  vi.stubGlobal('WebSocket', QuietSocket);
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/features') return reply({ pages: [], modules: [] });
      if (url === '/api/settings' && init?.method === 'PATCH') {
        return new Promise<Response>((done) => replies.push((s) => done(reply(s))));
      }
      if (url === '/api/settings') return reply({ speedLimit: 0 });
      return reply({});
    }),
  );
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <LiveSettings>
            <Limit />
          </LiveSettings>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  let first: Promise<void> = Promise.resolve();
  let second: Promise<void> = Promise.resolve();
  await act(async () => {
    first = send({ speedLimit: 1 });
  });
  await act(async () => {
    second = send({ speedLimit: 2 });
  });
  await act(async () => {
    replies[1]({ speedLimit: 2 });
    await second;
  });
  await act(async () => {
    replies[0]({ speedLimit: 1 });
    await first;
  });
  expect(host.querySelector('output')!.textContent).toBe('2');
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});
