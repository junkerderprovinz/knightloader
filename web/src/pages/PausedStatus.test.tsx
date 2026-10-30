// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { AccountPause } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { PausedStatus } from './Accounts';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.body.innerHTML = '';
});

async function show(pauses: AccountPause[]) {
  await act(async () =>
    root.render(
      <I18nProvider>
        <PausedStatus pauses={pauses} />
      </I18nProvider>,
    ),
  );
  await act(async () => host.querySelector('[role="note"]')!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
  return { word: host.textContent ?? '', bubble: document.querySelector('[role="tooltip"]')?.textContent ?? '' };
}

it('names a used-up volume, how long it lasts and how to try again', async () => {
  const { word, bubble } = await show([{ volume: true, until: '2026-10-30T12:00:00Z' }]);
  expect(word).toContain('Download volume used up');
  expect(bubble).toMatch(/The download volume is used up until .+\./);
  expect(bubble).toContain('Refresh tries the account again right away.');
});

it('says a hoster volume leaves the other hosters on the account', async () => {
  const { word, bubble } = await show([{ volume: true, host: 'rapidgator.net', until: '2026-10-30T12:00:00Z' }]);
  expect(word).toContain('Paused for some hosters');
  expect(bubble).toContain('The download volume for rapidgator.net is used up until');
  expect(bubble).toContain('Other hosters keep using this account.');
});
