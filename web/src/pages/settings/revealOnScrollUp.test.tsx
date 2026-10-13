// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { REST_MS, useRevealOnScrollUp } from './revealOnScrollUp';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let column: HTMLDivElement;
let summon: () => void;

/** The bar as SettingsSearch draws it: absent until the hook reveals it. */
function Bar({ page, pinned = false }: { page: string; pinned?: boolean }) {
  const { revealed, barRef, show } = useRevealOnScrollUp(page, pinned);
  summon = show;
  return revealed ? <div ref={barRef} data-bar /> : null;
}

const shown = () => column.querySelector('[data-bar]') !== null;

function wheel(deltaY: number) {
  act(() => {
    column.dispatchEvent(new WheelEvent('wheel', { deltaY }));
  });
}

function scrollTo(y: number) {
  act(() => {
    column.scrollTop = y;
    column.dispatchEvent(new Event('scroll'));
  });
}

function mount(pinned = false) {
  act(() => root.render(<Bar page="/settings/downloads" pinned={pinned} />));
}

beforeEach(() => {
  vi.useFakeTimers();
  // The column the hook looks up, with the bar's host inside it.
  column = document.createElement('div');
  column.setAttribute('data-settings-content', '');
  host = document.createElement('div');
  column.append(host);
  document.body.append(column);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  column.remove();
  vi.useRealTimers();
});

describe('the settings search bar', () => {
  it('stays away when somebody scrolls up in the middle of a page', () => {
    mount();
    scrollTo(400);
    vi.advanceTimersByTime(REST_MS * 2);
    wheel(-120);
    expect(shown()).toBe(false);
  });

  it('comes on a push upward once the page has rested at its top', () => {
    mount();
    vi.advanceTimersByTime(REST_MS);
    wheel(-120);
    expect(shown()).toBe(true);
  });

  it('does not take the wheel tick that carried the page to its top for a push past it', () => {
    mount();
    scrollTo(300);
    scrollTo(0);
    wheel(-120);
    expect(shown()).toBe(false);

    vi.advanceTimersByTime(REST_MS);
    wheel(-120);
    expect(shown()).toBe(true);
  });

  it('goes away on a downward scroll and on a change of page', () => {
    mount();
    vi.advanceTimersByTime(REST_MS);
    wheel(-120);
    wheel(120);
    expect(shown()).toBe(false);

    vi.advanceTimersByTime(REST_MS);
    wheel(-120);
    expect(shown()).toBe(true);
    act(() => root.render(<Bar page="/settings/network" />));
    expect(shown()).toBe(false);
  });

  it('ignores the settling of a fling', () => {
    mount();
    vi.advanceTimersByTime(REST_MS);
    wheel(-3);
    expect(shown()).toBe(false);
  });

  it('comes when summoned from the middle of a page and makes up for its own height', () => {
    mount();
    scrollTo(400);
    // jsdom lays nothing out, so the bar's height is given.
    const height = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(32);
    act(() => summon());
    expect(shown()).toBe(true);
    expect(column.scrollTop).toBe(432);
    // The scroll its own arrival causes is not somebody scrolling down.
    act(() => {
      column.dispatchEvent(new Event('scroll'));
    });
    expect(shown()).toBe(true);
    height.mockRestore();
  });

  it('stays while it is in use, whatever the wheel does', () => {
    mount();
    vi.advanceTimersByTime(REST_MS);
    wheel(-120);
    mount(true);
    wheel(120);
    expect(shown()).toBe(true);
  });
});
