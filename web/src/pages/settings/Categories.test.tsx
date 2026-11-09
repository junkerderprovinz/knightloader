// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Category } from '../../lib/api';
import { CategoriesCard } from './Categories';
import { SettingsProvider, type SettingsDraft } from './context';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// The menus come from the server, which a test has none of; the card draws
// its rows without them.
vi.mock('../../lib/api', async (actual) => ({
  ...(await actual<typeof import('../../lib/api')>()),
  fetchOptions: () => new Promise(() => {}),
  fetchMediaHooks: () => Promise.resolve([]),
  priorityChoices: () => Promise.resolve([]),
}));

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

async function mount(categories: Category[], extractTo = '') {
  const patch = vi.fn();
  const draft = {
    cfg: { categories, extractTo },
    patch,
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <MemoryRouter>
        <SettingsProvider draft={draft} features={features}>
          <CategoriesCard hue={0} />
        </SettingsProvider>
      </MemoryRouter>,
    ),
  );
  await act(async () => host.querySelector<HTMLButtonElement>('button[aria-haspopup="dialog"]')!.click());
  return patch;
}

const extractionField = () =>
  document.querySelector('[data-glim-label="Extraction folder"]')!.closest('label')!.querySelector('input')!;

describe('a category', () => {
  it('shows its own extraction folder beside the download folder', async () => {
    await mount([{ id: 'serien', name: 'Serien', extractTo: '/media/serien/<jd:packagename>' }]);
    expect(extractionField().value).toBe('/media/serien/<jd:packagename>');
  });

  it('shows the global extraction folder while it has none of its own', async () => {
    await mount([{ id: 'serien', name: 'Serien' }], '/unpacked');
    expect(extractionField().value).toBe('');
    expect(extractionField().placeholder).toBe('/unpacked');
  });

  it('saves the folder typed into it', async () => {
    const patch = await mount([{ id: 'serien', name: 'Serien' }]);
    const input = extractionField();
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '/media/serien');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(patch).toHaveBeenLastCalledWith({
      categories: [{ id: 'serien', name: 'Serien', extractTo: '/media/serien' }],
    });
  });
});
