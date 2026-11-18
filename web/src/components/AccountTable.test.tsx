// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { IconRetry } from '../lib/icons';
import { peekUIState, writeUIState } from '../lib/uistate';
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
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200 })));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  for (const table of ['debrid', 'hoster', 'usenet']) writeUIState(`accounts.columns.${table}`, undefined);
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

const draw = (rows: AccountRow[], tableId = 'debrid') =>
  act(() =>
    root.render(
      <I18nProvider>
        <AccountTable tableId={tableId} label="Accounts" rows={rows} />
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

it('shows a name cut short to fit its column whole in a tooltip, and a name that fits without one', () => {
  vi.useFakeTimers();
  try {
    draw([row('long-hoster-name', <span>Signed in</span>), row('short', <span>Signed in</span>)]);
    const names = [...host.querySelectorAll<HTMLElement>('tbody tr td:first-child .truncate')];
    Object.defineProperty(names[0], 'scrollWidth', { value: 240 });
    Object.defineProperty(names[0], 'clientWidth', { value: 80 });
    for (const name of names) {
      act(() => name.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
      act(() => vi.advanceTimersByTime(1000));
    }
    const tips = [...document.querySelectorAll('[role="tooltip"]')].map((t) => t.textContent);
    expect(tips).toEqual(['long-hoster-name']);
  } finally {
    vi.useRealTimers();
  }
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

/** Each column's share of the table, in percent, as its header carries it; the name takes what is left. */
function widths(): Record<string, number> {
  const [, status, plan, expiry, traffic] = [...host.querySelectorAll<HTMLElement>('thead th')].map(
    (th) => parseFloat(th.style.getPropertyValue('--kl-share')) * 100,
  );
  return { status, plan, expiry, traffic, name: 100 - status - plan - expiry - traffic };
}

function expectSameWidths(a: Record<string, number>, b: Record<string, number>) {
  for (const col of Object.keys(b)) expect(a[col]).toBeCloseTo(b[col], 6);
}

/** The resize handles in the header, from the name and status edge on. */
const handles = () => [...host.querySelectorAll<HTMLElement>('thead [role="separator"]')];

function drawWide(tableId = 'debrid') {
  draw([row('torbox', <span>Signed in</span>)], tableId);
  Object.defineProperty(host.querySelector('table'), 'offsetWidth', { configurable: true, value: 1000 });
}

function dragBy(handle: HTMLElement, dx: number) {
  const at = (type: string, clientX: number) =>
    act(() => handle.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, clientX })));
  at('pointerdown', 500);
  at('pointermove', 500 + dx / 2);
  at('pointermove', 500 + dx);
  at('pointerup', 500 + dx);
}

describe('column widths', () => {
  beforeEach(() => {
    HTMLElement.prototype.setPointerCapture = () => {};
  });

  it('gives the status more room than the name and spreads the rest over plan, expiry and traffic', () => {
    drawWide();
    const w = widths();
    expect(w.status).toBeGreaterThanOrEqual(w.name);
    expect(w.status).toBeGreaterThanOrEqual(24);
    expect(w.name).toBeLessThanOrEqual(30);
    expect(w.plan).toBeGreaterThanOrEqual(10);
    expect(w.expiry).toBeGreaterThanOrEqual(14);
    expect(w.traffic).toBeGreaterThanOrEqual(18);
  });

  it('puts a handle on every column edge but the row buttons', () => {
    drawWide();
    expect(handles()).toHaveLength(4);
    expect(host.querySelector('thead th.kl-col-actions [role="separator"]')).toBeNull();
  });

  it('moves room between the two columns beside the edge that is dragged', () => {
    drawWide();
    const before = widths();
    dragBy(handles()[1], -50);
    const after = widths();
    expect(after.plan).toBeCloseTo(before.plan + 5, 1);
    expect(after.status).toBeCloseTo(before.status - 5, 1);
    expect(after.name).toBeCloseTo(before.name, 1);
    expect(after.expiry).toBeCloseTo(before.expiry, 1);
    expect(after.traffic).toBeCloseTo(before.traffic, 1);
  });

  it('stops a column at its minimum, so the table never grows past its card', () => {
    drawWide();
    const before = widths();
    dragBy(handles()[0], 5000);
    const after = widths();
    expect(after.status).toBeGreaterThan(5);
    expect(after.status).toBeLessThan(before.status);
    expect(after.name).toBeCloseTo(before.name + before.status - after.status, 1);
    expect(after.status + after.plan + after.expiry + after.traffic + after.name).toBeCloseTo(100, 1);

    dragBy(handles()[0], -5000);
    expect(widths().name).toBeGreaterThan(5);
  });

  it('gives a column its own width back on a double-click on its handle', () => {
    drawWide();
    const before = widths();
    dragBy(handles()[2], -80);
    expect(widths().expiry).not.toBeCloseTo(before.expiry, 1);
    act(() => handles()[2].dispatchEvent(new MouseEvent('dblclick', { bubbles: true })));
    expectSameWidths(widths(), before);
  });

  it('remembers the widths of each table on its own', () => {
    drawWide('hoster');
    const before = widths();
    dragBy(handles()[0], -100);
    const dragged = widths();
    expect(peekUIState('accounts.columns.hoster', null)).not.toBeNull();
    expect(peekUIState('accounts.columns.usenet', null)).toBeNull();

    act(() => root.unmount());
    root = createRoot(host);
    drawWide('usenet');
    expectSameWidths(widths(), before);

    act(() => root.unmount());
    root = createRoot(host);
    drawWide('hoster');
    expectSameWidths(widths(), dragged);
  });

  it('widens a column from the keyboard', () => {
    drawWide();
    const before = widths();
    const handle = handles()[3];
    expect(handle.tabIndex).toBe(0);
    act(() => handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true })));
    expect(widths().traffic).toBeGreaterThan(before.traffic);
    expect(widths().expiry).toBeLessThan(before.expiry);
  });

  // A column a narrow card leaves out hands its room to the columns still
  // shown, and the row buttons' column on a touch screen comes off first.
  it('sizes each header from the shares of the columns shown, less the row buttons', () => {
    const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule('node:fs');
    const css = fs.readFileSync('src/index.css', 'utf8');
    expect(css).toMatch(/width: calc\(var\(--kl-share\) \/ var\(--kl-sum\) \* \(100cqw - var\(--kl-buttons\)\)\);/);
    const blocks = css
      .split('@container')
      .slice(1)
      .map((piece) => {
        let depth = 0;
        for (let i = 0; i < piece.length; i++) {
          if (piece[i] === '{') depth++;
          else if (piece[i] === '}' && --depth === 0) return piece.slice(0, i);
        }
        return piece;
      });
    const hiding = blocks.filter((b) => /\.kl-accounts [^{]*kl-col-(status|plan|expiry|traffic)[^{]*\{\s*display/.test(b));
    expect(hiding).toHaveLength(5);
    for (const rule of hiding) expect(rule).toMatch(/--kl-sum:/);
  });
});

it('lays the row buttons on the ground the row shows, the hover colour or the focus grey', () => {
  const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule('node:fs');
  const css = fs.readFileSync('src/index.css', 'utf8');
  expect(css).toMatch(/\.kl-accounts \.glim-row:focus-within:not\(:hover\) \{\s*--kl-row-ground: var\(--carbon-surface3\);/);
  expect(css).toMatch(/linear-gradient\(to left, var\(--kl-row-ground\)/);
  expect(css).toMatch(/linear-gradient\(to right, var\(--kl-row-ground\)/);
});
