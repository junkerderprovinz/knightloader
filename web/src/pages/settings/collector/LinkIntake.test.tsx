// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { ApiError } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { ToastProvider } from '../../../lib/toast';
import { SettingsProvider, type SettingsDraft } from '../context';
import type { Feature } from '../features';
import { switchRefusal } from '../tx';
import { LinkIntakeCard } from './LinkIntake';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
let fetched: string[];

beforeEach(() => {
  fetched = [];
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      fetched.push(String(url));
      const body = String(url).includes('/api/cnl/takeover') ? { modules: [], pages: [] } : [];
      return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
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

const cnl = (fields: Partial<Feature>): Feature =>
  ({ id: 'cnl', verdict: 'shipped', switch: 'setting', enabled: false, page: 'collector', ...fields }) as Feature;

async function mount(row: Feature) {
  const draft = {
    cfg: {},
    patch: () => {},
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  const features = { features: { modules: [row], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <MemoryRouter>
        <I18nProvider>
          <ToastProvider>
            <SettingsProvider draft={draft} features={features}>
              <LinkIntakeCard hue={0} />
            </SettingsProvider>
          </ToastProvider>
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
}

const takeOver = () =>
  [...host.querySelectorAll('button')].find((b) => b.textContent?.includes(en['settings.linkIntake.cnlTakeOver']));

it('names the KnightLoader holding the port and takes it over on a press', async () => {
  await mount(cnl({ detailCode: 'cnlHeldBy', detailArgs: { name: 'Desk', address: '127.0.0.1:9666' } }));
  expect(host.textContent).toContain('Held by KnightLoader Desk on this computer.');
  await act(async () => takeOver()!.click());
  expect(fetched).toContain('/api/cnl/takeover');
});

it('offers no take-over when another program holds the port', async () => {
  await mount(cnl({ detailCode: 'cnlHeldByOther', detailArgs: { address: '127.0.0.1:9666' } }));
  expect(takeOver()).toBeUndefined();
});

it('words a refused take-over like the row', () => {
  const t = (key: string, vars?: Record<string, string>) =>
    (en as Record<string, string>)[key].replace(/\{(\w+)\}/g, (_, k: string) => vars?.[k] ?? '');
  const e = new ApiError('refused', 'cnlHolderOutsideGroup', { name: 'Desk' }, 400);
  expect(switchRefusal(t as never, e)).toBe(
    'KnightLoader Desk is not in this group, so it cannot be asked. Switch Click\'n\'Load off there.',
  );
});
