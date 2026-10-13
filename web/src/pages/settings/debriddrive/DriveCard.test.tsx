// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { SettingsProvider, type SettingsDraft } from '../context';
import { DriveCard } from './DriveCard';

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

function render(cfg: object, patch = vi.fn()) {
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  const draft = { cfg, saved: cfg, patch } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <SettingsProvider draft={draft} features={features}>
        <DriveCard hue={4} />
      </SettingsProvider>,
    ),
  );
  return patch;
}

describe('DriveCard', () => {
  it('hands rclone this instance’s address and asks for a token', () => {
    render({ debridDrive: { enabled: true, refreshMinutes: 5 } });
    const snippet = host.querySelector('pre')!.textContent!;
    expect(snippet).toContain(`url = ${location.origin}/dav/`);
    expect(snippet).toContain('type = webdav');
    expect(snippet).toContain('bearer_token = your-read-token');
  });

  it('keeps the refresh interval between a minute and a day', () => {
    const patch = render({ debridDrive: { enabled: true, refreshMinutes: 5 } });
    const field = host.querySelector<HTMLInputElement>('input[type="number"]')!;
    expect(field.value).toBe('5');
    act(() => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      set.call(field, '100000');
      field.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const last = patch.mock.calls[patch.mock.calls.length - 1]?.[0] as { debridDrive: { refreshMinutes: number } } | undefined;
    expect(last?.debridDrive.refreshMinutes).toBe(24 * 60);
  });

  it('shows the default interval to a server that sends no drive settings', () => {
    render({});
    expect(host.querySelector<HTMLInputElement>('input[type="number"]')!.value).toBe('5');
  });
});
