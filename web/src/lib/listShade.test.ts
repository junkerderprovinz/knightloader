// @vitest-environment jsdom
import { beforeEach, expect, it, vi } from 'vitest';

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
});

it('shades the lists moderately in a browser that never chose', async () => {
  const { listShade } = await import('./listShade');
  expect(listShade()).toBe('moderate');
});

it('keeps the level in this browser across a reload', async () => {
  const first = await import('./listShade');
  first.setListShade('strong');
  expect(first.listShade()).toBe('strong');

  vi.resetModules();
  const second = await import('./listShade');
  expect(second.listShade()).toBe('strong');
  second.setListShade('off');

  vi.resetModules();
  const third = await import('./listShade');
  expect(third.listShade()).toBe('off');
  third.setListShade('moderate');

  vi.resetModules();
  const fourth = await import('./listShade');
  expect(fourth.listShade()).toBe('moderate');
});

it('keeps the lists off where an earlier switch turned them off', async () => {
  localStorage.setItem('kl-list-shade', 'off');
  const { listShade } = await import('./listShade');
  expect(listShade()).toBe('off');
});

it('tells every list when the level changes', async () => {
  const { setListShade, subscribeListShade } = await import('./listShade');
  const heard = vi.fn();
  const stop = subscribeListShade(heard);
  setListShade('strong');
  stop();
  setListShade('moderate');
  expect(heard).toHaveBeenCalledTimes(1);
});
