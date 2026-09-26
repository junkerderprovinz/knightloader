// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { ToastProvider } from '../../../lib/toast';
import { SettingsProvider, type SettingsDraft } from '../context';
import type { Feature } from '../features';
import { KeepAwakeRow } from './KeepAwake';

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

const keepAwake = (verdict: 'shipped' | 'desktop'): Feature => ({
  id: 'keepawake',
  verdict,
  page: 'automation',
  enabled: verdict === 'shipped',
  switch: verdict === 'shipped' ? 'setting' : 'none',
  parked: false,
});

function renderRow(m: Feature) {
  const toggle = vi.fn(async () => {});
  const patch = vi.fn();
  const draft = { cfg: { keepAwake: true } as Settings, patch } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <SettingsProvider draft={draft} features={{ features: { modules: [m], pages: [] }, toggle }}>
              <KeepAwakeRow />
            </SettingsProvider>
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  return { toggle, patch };
}

const modulesLink = () => host.querySelector('a[href="/settings/modules"]');

it('switches keep awake through the module registry, the same switch as its row on the Modules page', async () => {
  const { toggle, patch } = renderRow(keepAwake('shipped'));
  await act(async () => host.querySelector<HTMLButtonElement>('[role="switch"]')!.click());
  expect(toggle).toHaveBeenCalledWith('keepawake', false);
  expect(patch).not.toHaveBeenCalled();
  expect(modulesLink()).not.toBeNull();
});

it('leads to the Modules page from the disabled switch a container shows', async () => {
  const { toggle } = renderRow(keepAwake('desktop'));
  await act(async () => host.querySelector<HTMLButtonElement>('[role="switch"]')!.click());
  expect(toggle).not.toHaveBeenCalled();
  expect(modulesLink()).not.toBeNull();
});
