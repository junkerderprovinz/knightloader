// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { IconRetry } from '../lib/icons';
import { AccountTable, type AccountRow } from './AccountTable';
import { InfoBubble } from './ui';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

class OffScreen {
  observe() {}
  disconnect() {}
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', OffScreen);
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const row = (key: string, status: React.ReactNode): AccountRow => ({
  key,
  iconHost: `${key}.example`,
  label: key,
  enabled: true,
  status,
  onEdit: () => {},
});

/** The one call of node:fs used here; the web build carries no Node types. */
interface NodeFs {
  readFileSync(path: string, encoding: 'utf8'): string;
}

const draw = (rows: AccountRow[]) =>
  act(() =>
    root.render(
      <I18nProvider>
        <AccountTable label="Accounts" rows={rows} />
      </I18nProvider>,
    ),
  );

// The cards of the accounts page each draw this table, and a status with a
// bubble beside it is taller than a bare word. The row's own height is what
// keeps every card's rows level.
it('gives every row the same fixed height, whatever its status holds', () => {
  draw([
    row('plain', <span>Signed in</span>),
    row('bubble', <span className="inline-flex items-center whitespace-nowrap"><span>Not signed in</span><InfoBubble tip="Sign in first" /></span>),
  ]);
  const rows = [...host.querySelectorAll('tbody tr')];
  expect(rows).toHaveLength(2);
  const heights = rows.map((tr) => tr.className.split(/\s+/).find((c) => /^h-/.test(c)));
  expect(heights[0]).toBeDefined();
  expect(heights[1]).toBe(heights[0]);
  for (const td of host.querySelectorAll('tbody td')) expect(td.className).toMatch(/\bpy-0\b/);
});

it('offers each of a row’s options as its own button, the row’s own first and Remove last', () => {
  const done: string[] = [];
  draw([
    {
      ...row('torbox', <span>Signed in</span>),
      onToggle: (on) => done.push(on ? 'enable' : 'disable'),
      onEdit: () => done.push('edit'),
      onRemove: () => done.push('remove'),
      actions: [{ id: 'refresh', label: 'Refresh', icon: <IconRetry />, onSelect: () => done.push('refresh') }],
    },
  ]);
  const buttons = [...host.querySelectorAll<HTMLButtonElement>('tbody tr button')];
  expect(buttons.map((b) => b.getAttribute('aria-label'))).toEqual(['Refresh', 'Disable', 'Edit credential', 'Remove']);
  expect(host.querySelector('[aria-haspopup="menu"]')).toBeNull();

  for (const b of buttons) act(() => b.click());
  expect(done).toEqual(['refresh', 'disable', 'edit', 'remove']);
});

it('leaves out the options a row does not have and keeps a disabled one in its place', () => {
  draw([
    {
      ...row('site', <span>Signed in</span>),
      actions: [{ id: 'renew', label: 'Renew', icon: <IconRetry />, disabled: true, onSelect: () => {} }],
    },
  ]);
  const buttons = [...host.querySelectorAll<HTMLButtonElement>('tbody tr button')];
  expect(buttons.map((b) => b.getAttribute('aria-label'))).toEqual(['Renew', 'Edit credential']);
  expect(buttons[0].disabled).toBe(true);
});

it('shows the row’s buttons under the pointer, with the focus inside the row and always on a touch screen', () => {
  draw([{ ...row('torbox', <span>Signed in</span>), onRemove: () => {} }]);
  const tr = host.querySelector('tbody tr')!;
  expect(tr.classList).toContain('glim-row');
  for (const b of tr.querySelectorAll('button')) expect(b.closest('.glim-row-actions')).not.toBeNull();

  // Vitest hands a test every stylesheet empty, even as ?raw, so the rules are read off the disk.
  const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule('node:fs');
  const css = fs.readFileSync('src/index.css', 'utf8');
  expect(css).toMatch(/\.glim-row:is\(:hover, :focus-within\) \.glim-row-actions/);
  expect(css).toMatch(/@media \(hover: none\) \{\s*\.glim-row-actions \{\s*opacity: 1;/);
});
