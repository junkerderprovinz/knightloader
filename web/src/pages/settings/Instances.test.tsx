// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Settings } from '../../lib/api';
import { SettingsProvider, type SettingsDraft } from './context';
import { InstancesTab } from './Instances';
import { clearJump, requestJump } from './jump';

// The page itself asks the server for its instances; the tile only decides
// whether to draw it.
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
  clearJump();
});

function draw(hidden: boolean) {
  const draft = { cfg: { hideInstancesFromSidebar: hidden } as Settings, patch: () => {} } as unknown as SettingsDraft;
  // The module switch finds no module and draws nothing.
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  act(() =>
    root.render(
      <SettingsProvider draft={draft} features={features}>
        <MemoryRouter initialEntries={['/settings/instances']}>
          <Routes>
            <Route path="/settings/instances" element={<InstancesTab />} />
            <Route path="/instances" element={<div data-testid="rail-page" />} />
          </Routes>
        </MemoryRouter>
      </SettingsProvider>,
    ),
  );
}

const shows = (id: string) => host.querySelector(`[data-testid="${id}"]`) !== null;
const open = () => [...host.querySelectorAll('button')].find((b) => b.textContent === 'Open');

describe('the settings tile of a page that can stand in the sidebar', () => {
  it('keeps only its switches and a way to the page while the page is in the sidebar', () => {
    draw(false);
    expect(shows('whole-page')).toBe(false);
    expect(host.querySelector('[role="switch"]')).not.toBeNull();
    expect(open()).toBeDefined();
  });

  it('leads to the page from its Open row', () => {
    draw(false);
    act(() => open()!.click());
    expect(shows('rail-page')).toBe(true);
  });

  it('draws the whole page once the page is taken out of the sidebar', () => {
    draw(true);
    expect(shows('whole-page')).toBe(true);
    expect(open()).toBeUndefined();
  });

  it('brings the whole page along for a search result that leads to one of its rows', () => {
    requestJump({ page: 'instances', title: 'instances.foundTitle' });
    draw(false);
    expect(shows('whole-page')).toBe(true);
    // The search spends the request once it has landed, and the page stays.
    act(() => clearJump());
    expect(shows('whole-page')).toBe(true);
  });
});
