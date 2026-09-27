// @vitest-environment jsdom
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { desktopOS, isDesktop, openURL } from './desktop';
import { followExternal, openExternal, popupsWork } from './external';

// How the page reaches the desktop app is desktop.test.ts's business; here
// only whether it asks.
vi.mock('./desktop', () => ({
  isDesktop: vi.fn(() => false),
  openURL: vi.fn(async () => {}),
  desktopOS: vi.fn(async () => ''),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

function desktop(os: string) {
  vi.mocked(isDesktop).mockReturnValue(true);
  vi.mocked(desktopOS).mockResolvedValue(os);
}

/** Clicks an anchor wired the way the pages wire theirs, and says whether the browser would still follow it. */
async function clickAnchor(href: string): Promise<boolean> {
  await act(async () => root.render(createElement('a', { href, target: '_blank', onClick: followExternal }, 'link')));
  const click = new MouseEvent('click', { bubbles: true, cancelable: true });
  host.querySelector('a')!.dispatchEvent(click);
  return !click.defaultPrevented;
}

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.mocked(isDesktop).mockReturnValue(false);
  vi.restoreAllMocks();
  vi.mocked(openURL).mockClear();
});

describe('openExternal', () => {
  it('opens a tab without an opener in a browser', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    openExternal('https://github.com/junkerderprovinz/knightloader');
    expect(open).toHaveBeenCalledWith('https://github.com/junkerderprovinz/knightloader', '_blank', 'noopener,noreferrer');
    expect(openURL).not.toHaveBeenCalled();
  });

  it('hands the address to the system in the desktop build', () => {
    desktop('linux');
    const open = vi.spyOn(window, 'open');
    openExternal('https://github.com/junkerderprovinz/knightloader');
    expect(openURL).toHaveBeenCalledWith('https://github.com/junkerderprovinz/knightloader');
    expect(open).not.toHaveBeenCalled();
  });
});

describe('followExternal', () => {
  it('leaves the anchor to the browser', async () => {
    expect(await clickAnchor('https://github.com/junkerderprovinz/knightloader')).toBe(true);
  });

  it('sends a mail anchor to the mail program in the desktop build', async () => {
    desktop('darwin');
    const followed = await clickAnchor('mailto:hello@halleluja.design?subject=KnightLoader%20Feedback');
    expect(followed).toBe(false);
    expect(openURL).toHaveBeenCalledWith('mailto:hello@halleluja.design?subject=KnightLoader%20Feedback');
  });
});

describe('popupsWork', () => {
  it('is true in a browser', async () => {
    expect(await popupsWork()).toBe(true);
  });

  it.each([
    ['windows', true],
    ['darwin', false],
    ['linux', false],
  ])('in the desktop build on %s is %s', async (os, works) => {
    desktop(os);
    expect(await popupsWork()).toBe(works);
  });
});
