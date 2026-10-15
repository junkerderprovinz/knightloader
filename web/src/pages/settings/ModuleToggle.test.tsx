// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Settings } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { ToastProvider } from '../../lib/toast';
import { SettingsProvider, type SettingsDraft } from './context';
import type { Feature } from './features';
import { ModuleCard } from './ModuleToggle';

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

const row = (fields: Partial<Feature>): Feature => ({
  id: 'feeds',
  verdict: 'shipped',
  page: 'downloads',
  enabled: true,
  switch: 'parked',
  parked: false,
  ...fields,
});

function renderCard(m: Feature) {
  const toggle = vi.fn(async () => {});
  const draft = { cfg: {} as Settings, patch: vi.fn() } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <SettingsProvider draft={draft} features={{ features: { modules: [m], pages: [] }, toggle }}>
              <ModuleCard id={m.id} hue={0} titleRight={<button type="button">Add</button>}>
                <p data-testid="body">rows</p>
              </ModuleCard>
            </SettingsProvider>
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  return toggle;
}

const body = () => host.querySelector('[data-testid="body"]');
const add = () => [...host.querySelectorAll('button')].find((b) => b.textContent === 'Add');
const theSwitch = () => host.querySelector<HTMLButtonElement>('[role="switch"]')!;
const text = () => host.textContent ?? '';

it('shows a module that is on in full, with its heading action', () => {
  renderCard(row({}));
  expect(body()).not.toBeNull();
  expect(add()).toBeDefined();
  expect(theSwitch().getAttribute('aria-checked')).toBe('true');
});

it('folds a module that is off to its title, its switch and its state', () => {
  renderCard(
    row({ id: 'packagizer', switch: 'setting', enabled: false, detailCode: 'packagizerOff', detail: 'off' }),
  );
  expect(body()).toBeNull();
  expect(add()).toBeUndefined();
  expect(text()).toContain(en['settings.module.packagizer']);
  expect(text()).toContain(en['settings.modules.detail.packagizerOff']);
  expect(theSwitch().getAttribute('aria-checked')).toBe('false');
});

it('says what comes back when a parked module is switched on again', async () => {
  const toggle = renderCard(row({ enabled: false, parked: true }));
  expect(body()).toBeNull();
  expect(text()).toContain(en['settings.modules.parkedBelow']);
  await act(async () => theSwitch().click());
  expect(toggle).toHaveBeenCalledWith('feeds', true);
});

it('opens a module that was never set up when it is switched on, without asking the server', async () => {
  const toggle = renderCard(row({ enabled: false, unset: true }));
  expect(body()).toBeNull();
  expect(text()).toContain(en['settings.modules.missing.feeds']);
  await act(async () => theSwitch().click());
  expect(toggle).not.toHaveBeenCalled();
  expect(body()).not.toBeNull();
  expect(add()).toBeDefined();
  expect(text()).toContain(en['settings.modules.settingUp']);
});

it('keeps a module open that is set up but not working, since the fix is in the card', () => {
  renderCard(row({ id: 'reconnect', enabled: false }));
  expect(body()).not.toBeNull();
  expect(theSwitch().closest('.pointer-events-none')).not.toBeNull();
});

it('keeps a module without a switch open and says why under the switch', () => {
  renderCard(row({ id: 'ytdlp', switch: 'none', enabled: false, reasonCode: 'ytdlpMissing', reason: 'missing' }));
  expect(body()).not.toBeNull();
  expect(text()).toContain(en['settings.modules.reason.ytdlpMissing']);
});
