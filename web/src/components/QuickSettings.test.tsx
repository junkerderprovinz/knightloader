// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { QuickSettings } from './QuickSettings';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom lays nothing out and has no ResizeObserver, which the panel uses to
// place itself again when its rows change size.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

const torrent = { seedRatioTarget: 1, seedDurationSeconds: 7200, uploadLimitKiBs: 256, port: 6881, dhtEnabled: false, pexEnabled: true };

const settings = {
  speedLimit: 0,
  maxConcurrent: 3,
  maxPerHost: 2,
  chunks: 4,
  autoConfirm: false,
  idleAction: { action: 'none' },
  torrent,
};

let root: Root;
let host: HTMLDivElement;
let patches: Record<string, unknown>[];
let torrentsParked: boolean;

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  patches = [];
  torrentsParked = false;
  // Only the settings and the modules answer; the rows that need anything
  // else stay out, as they do when their read fails.
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/settings' && init?.method === 'PATCH') {
        const patch = JSON.parse(String(init.body)) as Record<string, unknown>;
        patches.push(patch);
        return reply({ ...settings, ...patch });
      }
      if (url === '/api/settings') return reply(settings);
      if (url === '/api/features') return reply({ modules: [{ id: 'torrents', parked: torrentsParked }], pages: [] });
      return new Response('not here', { status: 404 });
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

async function openPanel() {
  await act(async () =>
    root.render(
      <MemoryRouter>
        <QuickSettings />
      </MemoryRouter>,
    ),
  );
  await act(async () => host.querySelector<HTMLButtonElement>('button[aria-haspopup="dialog"]')!.click());
  // Let the reads behind the rows settle.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

const captions = () => [...host.querySelectorAll('[data-glim-label]')].map((el) => el.getAttribute('data-glim-label'));

function uploadInput() {
  const caption = [...host.querySelectorAll('[data-glim-label]')].find(
    (el) => el.getAttribute('data-glim-label') === 'Upload limit',
  );
  return caption?.closest('label')?.querySelector('input') ?? null;
}

function type(input: HTMLInputElement, text: string) {
  // React tracks the value it last rendered, so the native setter is what
  // makes the input event count as a change.
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
}

describe('the upload limit in the quick settings', () => {
  it('sits right below the download speed limit', async () => {
    await openPanel();
    expect(captions().slice(0, 2)).toEqual(['Speed limit (0 = ∞)', 'Upload limit']);
  });

  it('shows the stored KiB/s like every other speed', async () => {
    await openPanel();
    expect(uploadInput()!.getAttribute('aria-valuetext')).toBe('256 KiB/s');
  });

  it('saves the new limit in KiB/s with the rest of the torrent settings kept', async () => {
    await openPanel();
    type(uploadInput()!, '2m');
    // The panel waits out the settings page's autosave delay before it sends.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 700));
    });
    expect(patches).toEqual([{ torrent: { ...torrent, uploadLimitKiBs: 2048 } }]);
  });

  it('is left out while the built-in torrent client is switched off', async () => {
    torrentsParked = true;
    await openPanel();
    expect(captions()).toContain('Speed limit (0 = ∞)');
    expect(uploadInput()).toBeNull();
  });
});
