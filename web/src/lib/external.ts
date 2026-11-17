// Addresses that lead out of the app. The desktop build's webview has no
// browser behind it: on macOS and Linux window.open and target="_blank" do
// nothing, and on Windows they open a bare webview window. There the Wails
// runtime hands the address to the system browser or mail program instead.

import type { MouseEvent } from 'react';

import { desktopOS, isDesktop, openURL } from './desktop';

export function openExternal(url: string): void {
  if (isDesktop()) void openURL(url);
  else window.open(url, '_blank', 'noopener,noreferrer');
}

/**
 * openTab opens url in a new tab and hands the tab back, so the page can show
 * something else there later. The tab starts empty and follows a link of its
 * own, so the site gets neither this page as its referrer nor a handle on it,
 * as noopener and noreferrer would keep them. The desktop build hands the
 * address to the system browser and gets no tab back.
 */
export function openTab(url: string): Window | null {
  if (isDesktop()) {
    void openURL(url);
    return null;
  }
  const tab = window.open('', '_blank');
  if (!tab) return null;
  tab.opener = null;
  const go = tab.document.createElement('a');
  go.href = url;
  go.rel = 'noreferrer';
  tab.document.body.append(go);
  go.click();
  return tab;
}

/**
 * The onClick of an anchor that leads out of the app. In a browser the anchor
 * does its own work, so middle-click and copy-link keep working.
 */
export function followExternal(e: MouseEvent<HTMLAnchorElement>): void {
  if (!isDesktop()) return;
  e.preventDefault();
  openExternal(e.currentTarget.href);
}

/**
 * Whether window.open gives the page a popup that can talk back to its opener,
 * which the PayPal wallet's login needs. A browser and the Windows webview do;
 * the WebKit views the desktop build uses on macOS and Linux open nothing.
 */
export async function popupsWork(): Promise<boolean> {
  if (!isDesktop()) return true;
  return (await desktopOS()) === 'windows';
}
