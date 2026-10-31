// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import type { CleanupClass } from '../lib/api';
import type { MenuGroup } from './ContextMenu';
import { ListMore, type CleanupState } from './ListToolbar';

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

const own: MenuGroup[] = [{ id: 'failed', items: [{ id: 'retryFailed', label: 'Reset failed', onSelect: () => {} }] }];
const cleanup = (classes: CleanupClass[]) => ({ classes, preview: vi.fn() }) as unknown as CleanupState;

async function open(props: Partial<Parameters<typeof ListMore>[0]> = {}) {
  const onSelect = vi.fn();
  await act(async () =>
    root.render(
      <I18nProvider>
        <ListMore allChosen={false} shown={3} onSelect={onSelect} own={own} cleanup={cleanup(['finished'])} {...props} />
      </I18nProvider>,
    ),
  );
  const badge = host.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]')!;
  await act(async () => badge.click());
  const entries = [...document.querySelectorAll('[role="menuitem"]')].map((e) => e.textContent ?? '');
  return { badge, entries, onSelect };
}

it('is one More badge that opens the verbs over the whole list', async () => {
  const { badge, entries } = await open();
  expect(badge.getAttribute('aria-label')).toBe('More');
  expect(entries[0]).toContain('Select all');
  expect(entries.some((e) => e.includes('Reset failed'))).toBe(true);
  expect(entries.some((e) => e.includes('Clean up'))).toBe(true);
});

it('selects every row on show, and none once they all are', async () => {
  const first = await open();
  await act(async () => document.querySelector<HTMLElement>('[role="menuitem"]')!.click());
  expect(first.onSelect).toHaveBeenCalledWith(true);

  const second = await open({ allChosen: true });
  expect(second.entries[0]).toContain('Clear selection');
});

it('leaves the clean-up out where it does not run', async () => {
  const { entries } = await open({ cleanup: null });
  expect(entries.some((e) => e.includes('Clean up'))).toBe(false);
});
