// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { COLUMN_BY_ID, type CellContext } from './columns';

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

const ctx: CellContext = {
  t: (key, vars) => Object.entries(vars ?? {}).reduce<string>((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]),
  base: '/api',
  profile: 'downloads',
};

function nameCell(task: Task): string {
  const name = COLUMN_BY_ID.get('name');
  act(() => root.render(<I18nProvider>{name?.render(task, ctx)}</I18nProvider>));
  return host.textContent ?? '';
}

// The queue's last filter check sets a reject code and no error code, and the
// row has to say which rule stopped the link rather than call it unknown.
it('words a link the link filter rejected by its rule', () => {
  const text = nameCell({
    id: 'r1',
    url: 'https://host.example/sample.mkv',
    name: 'sample.mkv',
    package: '',
    resolver: 'direct',
    size: 0,
    loaded: 0,
    speed: 0,
    status: 'error',
    createdAt: '2026-09-24T12:00:00Z',
    priority: 0,
    position: 0,
    enabled: true,
    error: 'rejected by link filter rule "no samples"',
    rejectCode: 'filterRule',
    rejectParams: { rule: 'no samples' },
  });
  expect(text).toContain('rejected by link filter rule "no samples"');
  expect(text).not.toContain(en['failure.unknown.line']);
});
