// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Settings } from '../../lib/api';
import { applyAccent, followsSystemAccent, setFollowsSystemAccent, setSystemAccent } from '../../lib/appearance';
import { I18nProvider } from '../../lib/i18n';
import { ToastProvider } from '../../lib/toast';
import { SettingsProvider, type SettingsDraft } from './context';
import { Look } from './Look';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { headers: { 'Content-Type': 'application/json' } })));
  setSystemAccent(undefined);
  setFollowsSystemAccent(false);
  document.documentElement.removeAttribute('data-oled');
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

function renderLook() {
  const cfg = { shape: 'soft', accent: '#1D99F3', rainbow: false } as Settings;
  const patch = vi.fn();
  const draft = { cfg, saved: cfg, patch, patchNow: vi.fn(async () => {}) } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: vi.fn() }}>
              <Look section="appearance" />
            </SettingsProvider>
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  return patch;
}

const button = (name: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="${name}"]`);
const accent = () => document.documentElement.style.getPropertyValue('--accent');

it('offers the wand only where the device has a colour', () => {
  renderLook();
  expect(button('System colour')).toBeNull();
  act(() => setSystemAccent('#E0383E'));
  expect(button('System colour')).not.toBeNull();
});

it('wears the device colour here without touching the instance accent', () => {
  setSystemAccent('#E0383E');
  const patch = renderLook();
  act(() => button('System colour')!.click());
  expect(followsSystemAccent()).toBe(true);
  expect(accent()).toBe('#E0383E');
  expect(button('System colour')!.getAttribute('aria-pressed')).toBe('true');
  expect(patch).not.toHaveBeenCalledWith(expect.objectContaining({ accent: expect.anything() }));
});

it('goes back to an instance accent when a circle is chosen after the wand', () => {
  setSystemAccent('#E0383E');
  const patch = renderLook();
  act(() => button('System colour')!.click());
  act(() => button('Blue')!.click());
  expect(followsSystemAccent()).toBe(false);
  expect(patch).toHaveBeenCalledWith({ accent: '#1D99F3' });
  act(() => applyAccent('#1D99F3'));
  expect(accent()).toBe('#1D99F3');
});

it('keeps OLED in this browser and out of the instance settings', () => {
  const patch = renderLook();
  const toggle = [...host.querySelectorAll<HTMLButtonElement>('[role="switch"]')].find(
    (s) => s.getAttribute('aria-label') === 'OLED',
  )!;
  act(() => toggle.click());
  expect(document.documentElement.hasAttribute('data-oled')).toBe(true);
  expect(localStorage.getItem('kl-oled')).toBe('1');
  expect(patch).not.toHaveBeenCalledWith(expect.objectContaining({ oled: expect.anything() }));
});
