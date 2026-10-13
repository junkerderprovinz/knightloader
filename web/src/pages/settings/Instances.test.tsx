// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Settings } from '../../lib/api';
import { SettingsProvider, type SettingsDraft } from './context';
import { InstancesTab } from './Instances';

// The page itself asks the server for its instances; the tile only draws it.
vi.mock('../Instances', () => ({ Instances: () => <div data-testid="whole-page" /> }));

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

function draw(hidden: boolean) {
  const draft = { cfg: { hideInstancesFromSidebar: hidden } as Settings, patch: () => {} } as unknown as SettingsDraft;
  const federation = { id: 'federation', verdict: 'shipped', page: 'instances', enabled: true, switch: 'setting', parked: false };
  const features = { features: { modules: [federation], pages: [] }, toggle: async () => {} };
  act(() =>
    root.render(
      <SettingsProvider draft={draft} features={features as never}>
        <MemoryRouter>
          <InstancesTab />
        </MemoryRouter>
      </SettingsProvider>,
    ),
  );
}

describe('the settings tile of the Instances page', () => {
  it.each([false, true])('draws the module switch over the whole page, in the sidebar or not (hidden: %s)', (hidden) => {
    draw(hidden);
    expect(host.querySelector('[data-testid="whole-page"]')).not.toBeNull();
    expect(host.querySelector('[role="switch"]')).not.toBeNull();
    // The sidebar switch is on the Appearance page.
    expect(host.textContent).not.toContain('sidebar');
  });
});
