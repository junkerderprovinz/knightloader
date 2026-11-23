// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { I18nProvider } from '../../lib/i18n';
import type { Settings } from '../../lib/api';
import { SettingsProvider, type SettingsDraft } from './context';
import { IdentityCard } from './Pairing';

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

function draw(knownDomains: string[] = []) {
  const draft = { cfg: { instanceName: 'nas', knownDomains } as Settings, patch: () => {} } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <I18nProvider>
        <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
          <IdentityCard />
        </SettingsProvider>
      </I18nProvider>,
    ),
  );
}

const row = (label: string) => host.querySelector<HTMLElement>(`[data-glim-label="${label}"]`)!.closest<HTMLElement>('[data-set-row]')!;

describe('IdentityCard', () => {
  it('offers the domains the owner adds and shows no address of its own', () => {
    draw(['https://kl.example.org']);
    const addresses = row('Known addresses');
    expect(addresses.querySelector('textarea')!.value).toBe('https://kl.example.org');
    expect(host.querySelector('[data-testid="found-address"]')).toBeNull();
    expect(host.textContent).not.toMatch(/https?:\/\/\d/);
    expect(host.textContent).not.toContain('Found automatically');
  });

  it('parts its rows with a line and draws none above the first', () => {
    draw();
    const line = (el: HTMLElement) => el.className.split(/\s+/).includes('border-t');
    const between = (el: HTMLElement) => el.className.includes('[[data-set-row]+&]:border-t');
    const rows = [...host.querySelectorAll<HTMLElement>('[data-set-row]')];
    expect(rows).toHaveLength(2);
    for (const r of rows) {
      expect(line(r)).toBe(false);
      expect(between(r)).toBe(true);
    }
    // The heading stands before the first row, which is why "first child" would not find it.
    expect(rows[0].previousElementSibling?.hasAttribute('data-set-row')).toBe(false);
    expect(rows[1].previousElementSibling).toBe(rows[0]);
  });
});
