// What only the desktop app has: its two OS-native actions, reveal-in-folder
// and open-natively, the live stream over Wails events, the news of a
// downloaded update, and the tray's words.
//
// The shared interface does not load the Wails runtime. The page calls the
// runtime's endpoint the way @wailsio/runtime does, by the bound method's
// name, and takes Wails events through the dispatcher Wails calls.

import { useEffect } from 'react';

import type { WatchOutcome } from './clipboardWatch';
import type { TranslationKey } from './i18n';

/** Whether this page runs in the desktop app's window, which Wails serves from an origin of its own. */
export function isDesktop(): boolean {
  return window.location.protocol === 'wails:' || window.location.hostname === 'wails.localhost';
}

// The runtime's objects and their methods; objectNames in @wailsio/runtime.
const CALL = 0;
const SYSTEM = 8;
const SYSTEM_ENVIRONMENT = 1;
const BROWSER = 9;
const BROWSER_OPEN_URL = 0;

async function runtimeCall(object: number, method: number, args?: unknown): Promise<unknown> {
  const res = await fetch('/wails/runtime', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(args === undefined ? { object, method } : { object, method, args }),
  });
  const json = res.headers.get('Content-Type')?.includes('application/json');
  if (!res.ok) {
    // A bound method's error arrives as {message}, with the Go side's reason.
    const message = json ? ((await res.json()) as { message?: string }).message : await res.text();
    throw new Error(message || res.statusText);
  }
  return json ? res.json() : res.text();
}

// Tells this page's calls and streams from those of the page before a reload;
// desktop/stream.go drops the streams it sees a new page for.
const pageId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
let callCount = 0;

/** call runs a method desktop/main.go binds, such as main.DesktopFiles.OpenNatively. */
function call(method: string, ...args: unknown[]): Promise<unknown> {
  return runtimeCall(CALL, 0, { 'call-id': `${pageId}.${++callCount}`, methodName: method, args });
}

/** The window this page runs in: the tray window at /tray, or the main one. */
function windowName(): string {
  return window.location.pathname === '/tray' ? 'tray' : 'main';
}

/** showMain brings the main window forward, from the tray window. */
export async function showMain(): Promise<void> {
  await call('main.Tray.ShowMain');
}

/** revealInFolder and openNatively reject with the Go side's reason, which
 *  the caller shows. */
export async function revealInFolder(taskId: string): Promise<void> {
  if (!isDesktop()) throw new Error('reveal-in-folder is only available in the desktop app');
  await call('main.DesktopFiles.RevealInFolder', taskId);
}

export async function openNatively(taskId: string): Promise<void> {
  if (!isDesktop()) throw new Error('open natively is only available in the desktop app');
  await call('main.DesktopFiles.OpenNatively', taskId);
}

/** openURL hands an address to the system browser or mail program. */
export async function openURL(url: string): Promise<void> {
  await runtimeCall(BROWSER, BROWSER_OPEN_URL, { url });
}

/** desktopOS is the operating system the desktop app runs on, as Go names it. */
export async function desktopOS(): Promise<string> {
  const env = (await runtimeCall(SYSTEM, SYSTEM_ENVIRONMENT)) as { OS?: string };
  return env.OS ?? '';
}

type Channel = { postMessage(message: string): void };

type WailsHost = {
  _wails?: { dispatchWailsEvent?: (ev: { name: string; data: unknown }) => void };
  chrome?: { webview?: Channel };
  webkit?: { messageHandlers?: { external?: Channel } };
};

const listeners = new Map<string, Set<(data: unknown) => void>>();

let announced = false;

/**
 * Tells Wails the page can take events. Wails holds every event for a window
 * until it hears this, which its own runtime would send. It goes straight to
 * the webview's message channel, WebView2's chrome.webview or WebKit's
 * handler, which is there from the start. Wails' copy in `_wails.invoke`
 * arrives only after the page has loaded, and unbound from its channel.
 */
function announce(host: WailsHost) {
  if (announced) return;
  announced = true;
  post(host, 'wails:runtime:ready');
}

function post(host: WailsHost, message: string) {
  (host.chrome?.webview ?? host.webkit?.messageHandlers?.external)?.postMessage(message);
}

/** A resize edge by the name Wails takes it in `wails:resize:<edge>`, or '' inside the window. */
export type ResizeEdge = '' | 'n-resize' | 's-resize' | 'e-resize' | 'w-resize' | 'ne-resize' | 'nw-resize' | 'se-resize' | 'sw-resize';

/** How close to a side the pointer grabs it, and how far a corner reaches along the sides. */
const EDGE = 5;
const CORNER = 15;

/** edgeAt names the side or corner of a width by height window that the point x,y grabs. */
export function edgeAt(x: number, y: number, width: number, height: number): ResizeEdge {
  const left = x < EDGE;
  const right = width - x <= EDGE;
  const top = y < EDGE;
  const bottom = height - y <= EDGE;
  const nearLeft = x < CORNER;
  const nearRight = width - x <= CORNER;
  const nearTop = y < CORNER;
  const nearBottom = height - y <= CORNER;
  if ((bottom && nearRight) || (right && nearBottom)) return 'se-resize';
  if ((bottom && nearLeft) || (left && nearBottom)) return 'sw-resize';
  if ((top && nearLeft) || (left && nearTop)) return 'nw-resize';
  if ((top && nearRight) || (right && nearTop)) return 'ne-resize';
  if (left) return 'w-resize';
  if (right) return 'e-resize';
  if (top) return 'n-resize';
  if (bottom) return 's-resize';
  return '';
}

const CURSOR: Record<Exclude<ResizeEdge, ''>, string> = {
  'n-resize': 'ns-resize',
  's-resize': 'ns-resize',
  'e-resize': 'ew-resize',
  'w-resize': 'ew-resize',
  'ne-resize': 'nesw-resize',
  'sw-resize': 'nesw-resize',
  'nw-resize': 'nwse-resize',
  'se-resize': 'nwse-resize',
};

/**
 * useEdgeResize lets a frameless window be resized from its edges. A frameless
 * window has no frame for Windows or GTK to grab, so the page shows the resize
 * cursor at an edge and, once the button goes down there and the pointer
 * moves, hands the resize to Wails, as Wails' own runtime would. macOS resizes
 * such a window by itself.
 */
export function useEdgeResize(): void {
  useEffect(() => {
    if (!isDesktop()) return;
    const host = window as unknown as WailsHost & { _wails?: { environment?: { OS?: string } } };
    let edge: ResizeEdge = '';
    let armed = false;
    const move = (e: MouseEvent) => {
      if (armed && edge) {
        armed = false;
        post(host, `wails:resize:${edge}`);
        return;
      }
      if (host._wails?.environment?.OS === 'darwin') return;
      edge = edgeAt(e.clientX, e.clientY, window.innerWidth, window.innerHeight);
      document.body.style.cursor = edge ? CURSOR[edge] : '';
    };
    const down = (e: MouseEvent) => {
      armed = e.button === 0 && edge !== '';
      if (armed) e.preventDefault();
    };
    const up = () => {
      armed = false;
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mousedown', down, true);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mousedown', down, true);
      window.removeEventListener('mouseup', up);
    };
  }, []);
}

/** listen calls back with every Wails event of one name until the returned function is called. */
function listen(name: string, onEvent: (data: unknown) => void): () => void {
  const host = window as unknown as WailsHost;
  host._wails = host._wails ?? {};
  host._wails.dispatchWailsEvent = (ev) => {
    for (const listener of listeners.get(ev.name) ?? []) listener(ev.data);
  };
  announce(host);
  const named = listeners.get(name) ?? new Set();
  listeners.set(name, named);
  named.add(onEvent);
  return () => {
    named.delete(onEvent);
  };
}

/** StreamHandlers are a WebSocket's callbacks for one live stream. */
export interface StreamHandlers {
  onOpen(): void;
  onMessage(raw: string): void;
  /** Called when the stream ends without close(). */
  onClose(): void;
}

/** LiveStream is one open live stream, however it travels. */
export interface LiveStream {
  send(frame: string): void;
  close(): void;
}

let streamCount = 0;

/**
 * openDesktopStream opens the live stream over Wails events, because the
 * window's asset handler cannot carry a WebSocket. It returns null outside the
 * desktop app.
 */
export function openDesktopStream(h: StreamHandlers): LiveStream | null {
  if (!isDesktop()) return null;

  const id = `${pageId}.${++streamCount}`;
  let closed = false;
  const offMessage = listen(`hub:${id}`, (raw) => h.onMessage(String(raw)));
  const offClosed = listen(`hub:${id}:closed`, () => end());
  const end = () => {
    if (closed) return;
    closed = true;
    offMessage();
    offClosed();
    h.onClose();
  };
  // Every call waits for the one before, because Wails runs bound methods
  // concurrently and a socket keeps its frames in order.
  let calls: Promise<unknown> = call('main.HubBridge.Open', windowName(), pageId, id).then(
    () => {
      if (!closed) h.onOpen();
    },
    () => end(),
  );
  const queue = (f: () => Promise<unknown>) => {
    calls = calls.then(f).catch(() => undefined);
  };
  return {
    send(frame) {
      if (!closed) queue(() => call('main.HubBridge.Send', id, frame));
    },
    close() {
      if (closed) return;
      closed = true;
      offMessage();
      offClosed();
      queue(() => call('main.HubBridge.Close', id));
    },
  };
}

/**
 * onUpdateReady calls back with the version the desktop app has downloaded for
 * its next start; updateReadyEvent in desktop/updates.go. Outside the desktop
 * app nothing sends it, and nothing is set up.
 */
export function onUpdateReady(callback: (version: string) => void): () => void {
  if (!isDesktop()) return () => {};
  return listen('updateReady', (version) => callback(String(version)));
}

/**
 * onClipboardOutcome calls back with what the desktop app's clipboard watch
 * did with a copied text, as clipOutcome in desktop/clipwatch.go sends it.
 * Outside the desktop app nothing sends it, and nothing is set up.
 */
export function onClipboardOutcome(callback: (outcome: WatchOutcome) => void): () => void {
  if (!isDesktop()) return () => {};
  return listen('clipboardWatch', (outcome) => callback(outcome as WatchOutcome));
}

/** The tray menu's labels; TrayWords in desktop/config.go. */
const TRAY_WORDS = {
  show: 'tray.show',
  hide: 'tray.hide',
  startHidden: 'tray.startHidden',
  closeToTray: 'tray.closeToTray',
  minimiseToTray: 'tray.minimiseToTray',
  captcha: 'tray.captcha',
  raiseOff: 'tray.raiseOff',
  raiseFront: 'tray.raiseFront',
  raiseFocus: 'tray.raiseFocus',
  stopQueue: 'queue.stop',
  startQueue: 'queue.start',
  quit: 'tray.quit',
} as const satisfies Record<string, TranslationKey>;

export type TrayWords = Record<keyof typeof TRAY_WORDS, string>;

export function trayWords(t: (key: TranslationKey) => string): TrayWords {
  return Object.fromEntries(Object.entries(TRAY_WORDS).map(([field, key]) => [field, t(key)])) as TrayWords;
}

/** Hands the tray its words whenever the language changes, so the menu speaks it too. */
export function useTrayWords(t: (key: TranslationKey) => string): void {
  useEffect(() => {
    if (!isDesktop()) return;
    // A failure leaves the menu in its last language, which is no reason to
    // bother anybody.
    void call('main.Tray.SetWords', trayWords(t)).catch(() => {});
  }, [t]);
}
