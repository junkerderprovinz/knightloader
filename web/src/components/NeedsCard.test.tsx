// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import type { Need } from '../lib/needs';
import type { NeedsFeed } from '../lib/useNeeds';
import { NeedsCard } from './NeedsCard';

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
  vi.unstubAllGlobals();
});

const URL = 'https://github.com/junkerderprovinz/knightloader/releases/tag/v2.8.0';

const release = (flatpak: boolean): Need => ({
  kind: 'update',
  key: 'update:v2.8.0',
  covers: ['update:v2.8.0'],
  tone: 'warn',
  version: 'v2.8.0',
  url: URL,
  flatpak,
});

async function draw(need: Need) {
  const markDone = vi.fn();
  const feed: NeedsFeed = { needs: [need], done: new Set(), markDone, restore: vi.fn() };
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route path="/" element={<NeedsCard feed={feed} />} />
            <Route path="/settings/help" element={<p>Info page</p>} />
          </Routes>
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
  return markDone;
}

const button = (text: string) => [...host.querySelectorAll('button')].find((b) => b.textContent?.includes(text));

describe('a newer release on the Needs you card', () => {
  it('names the version and says how a container gets it', async () => {
    await draw(release(false));
    const row = host.querySelector('[data-need="update"]')!;
    expect(row.textContent).toContain('KnightLoader v2.8.0 is out');
    expect(row.textContent).toContain(en['overview.needs.updateContainerHint']);
  });

  it('says how a Flatpak gets it', async () => {
    await draw(release(true));
    expect(host.querySelector('[data-need="update"]')!.textContent).toContain(en['overview.needs.updateFlatpakHint']);
  });

  it('opens the release notes and can be set aside', async () => {
    const open = vi.fn();
    vi.stubGlobal('open', open);
    const markDone = await draw(release(false));
    await act(async () => button('Release notes')!.click());
    expect(open).toHaveBeenCalledWith(URL, '_blank', 'noopener,noreferrer');
    await act(async () => button('Done')!.click());
    expect(markDone).toHaveBeenCalledWith(expect.objectContaining({ key: 'update:v2.8.0' }));
  });

  it('leads to the Info page, where the switch is', async () => {
    await draw(release(false));
    await act(async () => button('KnightLoader v2.8.0 is out')!.click());
    expect(host.textContent).toContain('Info page');
  });
});
