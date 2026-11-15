// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
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

// The cards of the accounts page each draw this table, and a status with a
// bubble beside it is taller than a bare word. The row's own height is what
// keeps every card's rows level.
it('gives every row the same fixed height, whatever its status holds', () => {
  act(() =>
    root.render(
      <I18nProvider>
        <AccountTable
          label="Accounts"
          rows={[
            row('plain', <span>Signed in</span>),
            row('bubble', <span className="inline-flex items-center whitespace-nowrap"><span>Not signed in</span><InfoBubble tip="Sign in first" /></span>),
          ]}
        />
      </I18nProvider>,
    ),
  );
  const rows = [...host.querySelectorAll('tbody tr')];
  expect(rows).toHaveLength(2);
  const heights = rows.map((tr) => tr.className.split(/\s+/).find((c) => /^h-/.test(c)));
  expect(heights[0]).toBeDefined();
  expect(heights[1]).toBe(heights[0]);
  for (const td of host.querySelectorAll('tbody td')) expect(td.className).toMatch(/\bpy-0\b/);
});
