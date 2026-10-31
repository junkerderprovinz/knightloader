// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import type { MenuGroup } from './ContextMenu';
import { useFileMenu } from './FileActions';

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

const task = (id: string, more: Partial<Task> = {}) =>
  ({
    id,
    url: `https://rapidgator.example/file/${id}`,
    name: `${id}.rar`,
    resolver: 'direct',
    status: 'done',
    package: 'Film',
    ...more,
  }) as Task;

async function entries(chosen: Task[], files = true): Promise<string[]> {
  let groups: MenuGroup[] = [];
  function Probe() {
    groups = useFileMenu({ chosen, base: '/api', local: true, files });
    return null;
  }
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <Probe />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  return groups.flatMap((g) => g.items.map((i) => i.id));
}

it('offers the folder and the native app in the desktop app only', async () => {
  expect(await entries([task('a')])).not.toContain('openFolder');
  expect(await entries([task('a')])).not.toContain('openNatively');

  vi.stubGlobal('location', { ...window.location, protocol: 'wails:', hostname: 'wails.localhost' });
  const desktop = await entries([task('a')]);
  expect(desktop).toContain('openFolder');
  expect(desktop).toContain('openNatively');
});

it('opens the folder of a whole package and copies all its links', async () => {
  vi.stubGlobal('location', { ...window.location, protocol: 'wails:', hostname: 'wails.localhost' });
  const items = await entries([task('a'), task('b')]);
  expect(items).toEqual(expect.arrayContaining(['openFolder', 'copyLinks']));
  // One file to open and one page to visit only make sense for one link.
  expect(items).not.toContain('open');
  expect(items).not.toContain('openInBrowser');
});

it('leaves the files out in the collector and a magnet out of the browser', async () => {
  const collector = await entries([task('a', { status: 'collected' })], false);
  expect(collector).not.toContain('open');
  expect(collector).toContain('openInBrowser');
  expect(await entries([task('m', { url: 'magnet:?xt=urn:btih:abc' })], false)).not.toContain('openInBrowser');
});
