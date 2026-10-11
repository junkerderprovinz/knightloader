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

function draw(found: string, knownDomains: string[] = []) {
  const draft = { cfg: { instanceName: 'nas', knownDomains } as Settings, patch: () => {} } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <I18nProvider>
        <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
          <IdentityCard found={found} />
        </SettingsProvider>
      </I18nProvider>,
    ),
  );
}

const row = (label: string) => host.querySelector<HTMLElement>(`[data-glim-label="${label}"]`)!.closest<HTMLElement>('[data-set-row]')!;

describe('IdentityCard', () => {
  it('lists the address the instance found itself above the field for the ones that are added', () => {
    draw('http://192.168.20.89:8749', ['https://kl.example.org']);
    const addresses = row('Known addresses');
    const found = addresses.querySelector('[data-testid="found-address"]')!;
    expect(found.textContent).toContain('http://192.168.20.89:8749');
    expect(found.textContent).toContain('Found automatically');
    const field = addresses.querySelector('textarea')!;
    expect(field.value).toBe('https://kl.example.org');
    expect(found.compareDocumentPosition(field) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Shown, not offered for editing.
    expect(found.querySelector('input, textarea, button')).toBeNull();
  });

  it('shows only the field where the instance has no address on a network', () => {
    draw('');
    expect(host.querySelector('[data-testid="found-address"]')).toBeNull();
    expect(row('Known addresses').querySelector('textarea')).not.toBeNull();
  });

  it('parts its rows with a line and draws none above the first', () => {
    draw('');
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
