// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { announce } from './announce';

beforeEach(() => {
  vi.useFakeTimers();
  document.body.innerHTML = '';
});

afterEach(() => {
  vi.useRealTimers();
});

const regions = (politeness: string) => [...document.querySelectorAll(`[data-live-region][aria-live="${politeness}"]`)];

describe('announce', () => {
  it('puts a live region into a page that has none and says the words there', () => {
    announce('3 selected');
    vi.runAllTimers();
    const [region] = regions('polite');
    expect(region.getAttribute('role')).toBe('status');
    expect(region.textContent).toBe('3 selected');
  });

  it('says a failure through an alert of its own', () => {
    announce('Could not save', 'assertive');
    vi.runAllTimers();
    expect(regions('assertive')[0].getAttribute('role')).toBe('alert');
    expect(regions('polite')).toEqual([]);
  });

  it('uses the region already in the page', () => {
    document.body.innerHTML = '<div data-live-region aria-live="polite" role="status"></div>';
    announce('Saved');
    vi.runAllTimers();
    expect(regions('polite')).toHaveLength(1);
    expect(regions('polite')[0].textContent).toBe('Saved');
  });

  it('passes over a region behind an open window, which a screen reader cannot reach', () => {
    document.body.innerHTML = '<main inert><div data-live-region aria-live="polite" role="status"></div></main>';
    announce('Saved');
    vi.runAllTimers();
    const outside = regions('polite').filter((r) => !r.closest('[inert]'));
    expect(outside).toHaveLength(1);
    expect(outside[0].textContent).toBe('Saved');
  });

  it('empties the region first, so the same words said twice are read twice', () => {
    announce('Saved');
    vi.runAllTimers();
    announce('Saved');
    expect(regions('polite')[0].textContent).toBe('');
    vi.runAllTimers();
    expect(regions('polite')[0].textContent).toBe('Saved');
  });
});
