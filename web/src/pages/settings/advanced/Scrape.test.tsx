// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { ToastProvider } from '../../../lib/toast';
import { SettingsProvider, type SettingsDraft } from '../context';
import { ScrapeCard } from './Scrape';

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
});

it('copies the metrics address on a page opened over plain http, where the browser has no clipboard api', async () => {
  let copied = '';
  Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
  document.execCommand = vi.fn(() => {
    copied = document.body.querySelector<HTMLTextAreaElement>(':scope > textarea')?.value ?? '';
    return true;
  });
  const draft = { cfg: {} as Settings, patch: vi.fn() } as unknown as SettingsDraft;
  const metrics = { id: 'metrics', verdict: 'shipped', page: 'settings', enabled: true, switch: 'setting', parked: false };
  act(() =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <SettingsProvider
              draft={draft}
              features={{ features: { modules: [metrics], pages: [] }, toggle: vi.fn(async () => {}) } as never}
            >
              <ScrapeCard hue={0} />
            </SettingsProvider>
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  const copy = [...host.querySelectorAll('button')].find((b) => b.textContent === en['settings.health.scrapeCopy']);
  expect(copy).toBeDefined();
  await act(async () => copy!.click());
  expect(copied).toMatch(/\/api\/metrics$/);
  expect(host.textContent).toContain(en['settings.health.scrapeCopied']);
});
