// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Settings } from '../../../lib/api';
import type { WindowPrefs } from '../../../lib/desktop';
import { I18nProvider } from '../../../lib/i18n';
import { ToastProvider } from '../../../lib/toast';
import { SettingsProvider, type SettingsDraft } from '../context';
import type { Feature } from '../features';
import { BrowserWindowCard, WindowCard } from './WindowCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let saved: WindowPrefs[];

const PREFS: WindowPrefs = {
  startHidden: false,
  onClose: 'exit',
  onMinimize: 'taskbar',
  raiseOnAttention: 'off',
  trayAvailable: true,
};

/** The desktop app's side of the bound calls, main.Tray.Prefs and main.Tray.SetPrefs. */
function app(prefs: WindowPrefs) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url !== '/wails/runtime') return new Response('not here', { status: 404 });
      const { args } = JSON.parse(String(init?.body)) as { args: { methodName: string; args: WindowPrefs[] } };
      let answer = prefs;
      if (args.methodName === 'main.Tray.SetPrefs') {
        answer = args.args[0];
        saved.push(answer);
      }
      return new Response(JSON.stringify(answer), { headers: { 'Content-Type': 'application/json' } });
    }),
  );
}

beforeEach(() => {
  saved = [];
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const keepAwake = (verdict: 'shipped' | 'desktop'): Feature => ({
  id: 'keepawake',
  verdict,
  page: 'appearance',
  enabled: verdict === 'shipped',
  switch: verdict === 'shipped' ? 'setting' : 'none',
  parked: false,
});

async function draw(card = <WindowCard hue={0} />, modules: Feature[] = [keepAwake('shipped')]) {
  const draft = { cfg: {} as Settings, patch: () => {} } as unknown as SettingsDraft;
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <SettingsProvider draft={draft} features={{ features: { modules, pages: [] }, toggle: async () => {} }}>
              {card}
            </SettingsProvider>
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
}

const toggle = (label: string) => host.querySelector<HTMLButtonElement>(`[role="switch"][aria-label="${label}"]`)!;

describe('the window card on the Appearance page', () => {
  it("offers the tray menu's choices under the tray menu's names", async () => {
    app(PREFS);
    await draw();
    for (const label of ['Start hidden', 'Close to the notification area', 'Minimise to the notification area']) {
      expect(toggle(label).getAttribute('aria-checked')).toBe('false');
    }
    expect(host.textContent).toContain('When a captcha needs you');
  });

  it('holds the switch that keeps the computer awake, the module of the desktop build', async () => {
    app(PREFS);
    await draw();
    expect(toggle('Keep the computer awake while downloading').getAttribute('aria-checked')).toBe('true');
  });

  it('says in a browser tab where the window is set, and shows the keep awake switch a container cannot use', async () => {
    await draw(<BrowserWindowCard hue={0} />, [keepAwake('desktop')]);
    expect(host.textContent).toContain('In the desktop app, its window');
    expect(toggle('Keep the computer awake while downloading').closest('.pointer-events-none')).not.toBeNull();
    expect(toggle('Start hidden')).toBeNull();
  });

  it('hands a change to the desktop app', async () => {
    app(PREFS);
    await draw();
    await act(async () => toggle('Close to the notification area').click());
    expect(saved).toEqual([{ ...PREFS, onClose: 'tray' }]);
    expect(toggle('Close to the notification area').getAttribute('aria-checked')).toBe('true');
  });

  it('holds the three switches back where the desktop has no tray', async () => {
    app({ ...PREFS, trayAvailable: false });
    await draw();
    expect(host.textContent).toContain('no notification area KnightLoader can use');
    for (const label of ['Start hidden', 'Close to the notification area', 'Minimise to the notification area']) {
      expect(toggle(label).closest('.pointer-events-none')).not.toBeNull();
    }
  });
});
