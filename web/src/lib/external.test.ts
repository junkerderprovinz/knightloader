// @vitest-environment jsdom
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { followExternal, openExternal, popupsWork } from './external';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

function desktop(platform: string) {
  const runtime = {
    BrowserOpenURL: vi.fn(),
    Environment: vi.fn(async () => ({ buildType: 'production', platform, arch: 'amd64' })),
  };
  (window as unknown as { runtime?: typeof runtime }).runtime = runtime;
  return runtime;
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
  delete (window as unknown as { runtime?: unknown }).runtime;
  vi.restoreAllMocks();
});

describe('openExternal', () => {
  it('opens a tab without an opener in a browser', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    openExternal('https://github.com/junkerderprovinz/knightloader');
    expect(open).toHaveBeenCalledWith('https://github.com/junkerderprovinz/knightloader', '_blank', 'noopener,noreferrer');
  });

  it('hands the address to the system in the desktop build', () => {
    const runtime = desktop('linux');
    const open = vi.spyOn(window, 'open');
    openExternal('https://github.com/junkerderprovinz/knightloader');
    expect(runtime.BrowserOpenURL).toHaveBeenCalledWith('https://github.com/junkerderprovinz/knightloader');
    expect(open).not.toHaveBeenCalled();
  });
});

describe('followExternal', () => {
  it('leaves the anchor to the browser', async () => {
    expect(await clickAnchor('https://github.com/junkerderprovinz/knightloader')).toBe(true);
  });

  it('sends a mail anchor to the mail program in the desktop build', async () => {
    const runtime = desktop('darwin');
    const followed = await clickAnchor('mailto:hello@halleluja.design?subject=KnightLoader%20Feedback');
    expect(followed).toBe(false);
    expect(runtime.BrowserOpenURL).toHaveBeenCalledWith('mailto:hello@halleluja.design?subject=KnightLoader%20Feedback');
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
  ])('in the desktop build on %s is %s', async (platform, works) => {
    desktop(platform);
    expect(await popupsWork()).toBe(works);
  });
});
