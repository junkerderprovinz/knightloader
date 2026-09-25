// Addresses that lead out of the app. The desktop build's webview has no
// browser behind it: on macOS and Linux window.open and target="_blank" do
// nothing, and on Windows they open a bare webview window. There the Wails
// runtime hands the address to the system browser or mail program instead.

import type { MouseEvent } from 'react';

interface WailsRuntime {
  BrowserOpenURL(url: string): void;
  Environment(): Promise<{ platform: string }>;
}

function wails(): WailsRuntime | undefined {
  return (window as unknown as { runtime?: WailsRuntime }).runtime;
}

export function openExternal(url: string): void {
  const runtime = wails();
  if (runtime) runtime.BrowserOpenURL(url);
  else window.open(url, '_blank', 'noopener,noreferrer');
}

/**
 * The onClick of an anchor that leads out of the app. In a browser the anchor
 * does its own work, so middle-click and copy-link keep working.
 */
export function followExternal(e: MouseEvent<HTMLAnchorElement>): void {
  if (!wails()) return;
  e.preventDefault();
  openExternal(e.currentTarget.href);
}

/**
 * Whether window.open gives the page a popup that can talk back to its opener,
 * which the PayPal wallet's login needs. A browser and the Windows webview do;
 * the WebKit views the desktop build uses on macOS and Linux open nothing.
 */
export async function popupsWork(): Promise<boolean> {
  const runtime = wails();
  if (!runtime) return true;
  const { platform } = await runtime.Environment();
  return platform === 'windows';
}
