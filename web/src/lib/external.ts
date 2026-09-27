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
 * openMail hands a mailto: address to the mail program. In a browser the page
 * navigates to it rather than opening it in a new tab, which would leave an
 * empty tab behind once the mail program has it.
 */
export function openMail(url: string): void {
  if (isDesktop()) void openURL(url);
  else window.location.href = url;
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
