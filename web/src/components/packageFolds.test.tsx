// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { ListProfile } from './columns';
import { useCollapsedPackages } from './TaskList';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let folds: ReturnType<typeof useCollapsedPackages>;

function Probe({ profile }: { profile: ListProfile }) {
  folds = useCollapsedPackages(profile);
  return null;
}

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })),
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

it('folds a package that arrives on Downloads and keeps one somebody opened open', async () => {
  await act(async () => root.render(<Probe profile="downloads" />));
  expect(folds.collapsed.has('Interview im Burghof')).toBe(true);
  await act(async () => folds.expand(['Interview im Burghof']));
  expect(folds.collapsed.has('Interview im Burghof')).toBe(false);
  expect(folds.collapsed.has('Burgchronik')).toBe(true);
  await act(async () => folds.toggle('Interview im Burghof'));
  expect(folds.collapsed.has('Interview im Burghof')).toBe(true);
});

it('opens a package in the collector until somebody folds it', async () => {
  await act(async () => root.render(<Probe profile="collector" />));
  expect(folds.collapsed.has('Interview im Burghof')).toBe(false);
  await act(async () => folds.collapse(['Interview im Burghof']));
  expect(folds.collapsed.has('Interview im Burghof')).toBe(true);
});
