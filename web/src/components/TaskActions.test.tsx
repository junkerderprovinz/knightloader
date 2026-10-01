// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { TaskActions } from './TaskList';

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

const task = (over: Partial<Task> = {}): Task => ({
  id: 'a',
  url: 'https://files.example/a',
  name: 'a.zip',
  package: '',
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'collected',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

function answer(response: () => Response) {
  vi.stubGlobal('fetch', vi.fn(async () => response()));
}

async function press(t: Task, label: string) {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <TaskActions task={t} base="/api/instances/cellar" current />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  const badge = host.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!;
  await act(async () => badge.click());
  return document.body.textContent;
}

describe('a row badge', () => {
  it('says why Start started nothing', async () => {
    answer(() => Response.json({ started: 0, skipped: 0, disabled: 1, released: false, blocked: false }));
    expect(await press(task({ enabled: false }), 'Start')).toContain('1 links are disabled and were not started.');
  });

  it('says a schedule is holding the queue', async () => {
    answer(() => Response.json({ started: 1, skipped: 0, released: false, blocked: true }));
    expect(await press(task(), 'Start')).toContain('A schedule is holding the queue.');
  });

  it('says why a pause, a resume or a removal was refused', async () => {
    for (const [status, label] of [
      ['running', 'Pause'],
      ['paused', 'Resume'],
      ['done', 'Remove'],
    ] as const) {
      answer(() => new Response('the instance cellar is not connected', { status: 502 }));
      expect(await press(task({ status }), label)).toContain('That did not work: the instance cellar is not connected');
      act(() => root.unmount());
      root = createRoot(host);
    }
  });
});
