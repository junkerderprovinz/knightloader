// @vitest-environment jsdom
import { beforeEach, expect, it, vi } from 'vitest';

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
});

it('shades the lists in a browser that never chose', async () => {
  const { listShaded } = await import('./listShade');
  expect(listShaded()).toBe(true);
});

it('keeps the choice in this browser across a reload', async () => {
  const first = await import('./listShade');
  first.setListShade(false);
  expect(first.listShaded()).toBe(false);

  vi.resetModules();
  const second = await import('./listShade');
  expect(second.listShaded()).toBe(false);
  second.setListShade(true);

  vi.resetModules();
  const third = await import('./listShade');
  expect(third.listShaded()).toBe(true);
});

it('tells every list when the choice changes', async () => {
  const { setListShade, subscribeListShade } = await import('./listShade');
  const heard = vi.fn();
  const stop = subscribeListShade(heard);
  setListShade(false);
  stop();
  setListShade(true);
  expect(heard).toHaveBeenCalledTimes(1);
});
