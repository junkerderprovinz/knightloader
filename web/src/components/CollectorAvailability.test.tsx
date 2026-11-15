// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Availability, Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { COLUMN_BY_ID, type CellContext, type ListProfile } from './columns';

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

const ctxFor = (profile: ListProfile): CellContext => ({
  t: (key, vars) => Object.entries(vars ?? {}).reduce<string>((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]),
  base: '/api',
  profile,
});

const link = (online: Availability, extra: Partial<Task> = {}): Task => ({
  id: `l-${online}`,
  url: 'https://rapidgator.net/file/abc/clip.mkv',
  name: 'clip.mkv',
  package: 'Clips',
  resolver: 'rapidgator',
  size: 0,
  loaded: 0,
  speed: 0,
  status: 'collected',
  createdAt: '2026-11-01T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  online,
  ...extra,
});

const gone = (extra: Partial<Task> = {}) => link('offline', { reason: 'gone', error: 'offline (rapidgator)', ...extra });

function render(node: React.ReactNode) {
  act(() => root.render(<I18nProvider>{node}</I18nProvider>));
}

/** The words the availability dot says on hover and to a screen reader. */
const dotSays = () => host.querySelector('[role="img"]')?.getAttribute('aria-label') ?? null;

it('calls a link the source reports as gone Offline beside its name, on one line, in the collector', () => {
  render(COLUMN_BY_ID.get('name')?.render(gone(), ctxFor('collector')));
  expect(host.textContent).toBe(`clip.mkv${en['task.offline']}`);
  expect(host.textContent).not.toContain(en['failure.gone.line']);
});

it('keeps the sentence under a gone download in the download list, under the same Offline tag', () => {
  render(COLUMN_BY_ID.get('name')?.render(gone({ status: 'error' }), ctxFor('downloads')));
  expect(host.textContent).toContain(en['task.offline']);
  expect(host.textContent).toContain(en['failure.gone.line']);
});

it('names the collector dot by its verdict alone', () => {
  const status = COLUMN_BY_ID.get('status');
  render(status?.render(gone(), ctxFor('collector')));
  expect(dotSays()).toBe('Offline');
  render(status?.render(link('online'), ctxFor('collector')));
  expect(dotSays()).toBe('Online');
});

it('names a package dot by what its links found, partly offline when they disagree', () => {
  const status = COLUMN_BY_ID.get('status');
  render(status?.aggregate?.([link('online'), gone()], ctxFor('collector')));
  expect(dotSays()).toBe('Partly offline');
  render(status?.aggregate?.([gone(), gone({ id: 'l2' })], ctxFor('collector')));
  expect(dotSays()).toBe('Offline');
  render(status?.aggregate?.([link('online'), link('online', { id: 'l2' })], ctxFor('collector')));
  expect(dotSays()).toBe('Online');
});
