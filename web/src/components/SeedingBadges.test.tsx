// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { ToastProvider } from '../lib/toast';
import { SeedingBadges } from './ListToolbar';

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

const torrent = (id: string, over: Partial<Task> = {}): Task => ({
  id,
  url: `magnet:?xt=urn:btih:${id}`,
  name: id,
  package: id,
  resolver: 'torrent',
  size: 100,
  loaded: 100,
  speed: 0,
  status: 'done',
  createdAt: '2026-09-30T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

function badges(tasks: Task[]) {
  act(() =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <SeedingBadges tasks={tasks} base="/api" />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  const find = (label: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!;
  return { stop: find(en['task.stopSeedingAll']), start: find(en['task.startSeedingAll']) };
}

it('lets Stop be pressed while a torrent in the card seeds', () => {
  const { stop, start } = badges([torrent('a', { seeding: true })]);
  expect(stop.disabled).toBe(false);
  expect(start.disabled).toBe(true);
});

it('dims Stop in a card whose torrents have all stopped seeding, with a bubble to say why', () => {
  const { stop, start } = badges([torrent('a', { seedingOver: true })]);
  expect(stop.disabled).toBe(true);
  expect(start.disabled).toBe(false);
  // A disabled badge with a reason is wrapped in the box that carries its bubble.
  expect(stop.parentElement?.tagName).toBe('SPAN');
});
