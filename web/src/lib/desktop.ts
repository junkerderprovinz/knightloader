// The desktop app's two OS-native actions, reveal-in-folder and
// open-natively, and the check whether the page runs inside the desktop app.
//
// Wails injects the Go bindings as window.go.<package>.<Type>.<Method> before
// the page's scripts run. Only desktop/main.go binds DesktopFiles, so in a
// browser the object is undefined and isDesktop() is a plain check for it.
interface DesktopFilesBinding {
  RevealInFolder(taskId: string): Promise<void>;
  OpenNatively(taskId: string): Promise<void>;
}

function binding(): DesktopFilesBinding | null {
  const w = window as unknown as { go?: { main?: { DesktopFiles?: DesktopFilesBinding } } };
  return w.go?.main?.DesktopFiles ?? null;
}

/** isDesktop is whether the two OS-native actions can work at all here. */
export function isDesktop(): boolean {
  return binding() !== null;
}

/** revealInFolder and openNatively reject with the Go side's reason, which
 *  the caller shows. */
export async function revealInFolder(taskId: string): Promise<void> {
  const b = binding();
  if (!b) throw new Error('reveal-in-folder is only available in the desktop app');
  await b.RevealInFolder(taskId);
}

export async function openNatively(taskId: string): Promise<void> {
  const b = binding();
  if (!b) throw new Error('open natively is only available in the desktop app');
  await b.OpenNatively(taskId);
}

interface WailsRuntime {
  EventsOn(name: string, callback: (...data: unknown[]) => void): () => void;
}

interface HubBridgeBinding {
  Open(page: string, id: string): Promise<void>;
  Send(id: string, frame: string): Promise<void>;
  Close(id: string): Promise<void>;
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

// Tells this page's streams from those of the page before a reload, which
// never closed its own; desktop/stream.go drops them when it sees a new page.
const pageId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
let streamCount = 0;

/**
 * openDesktopStream opens the live stream over Wails events, because the
 * window's asset handler cannot carry a WebSocket. It returns null outside the
 * desktop app.
 */
export function openDesktopStream(h: StreamHandlers): LiveStream | null {
  const w = window as unknown as { go?: { main?: { HubBridge?: HubBridgeBinding } }; runtime?: WailsRuntime };
  const bridge = w.go?.main?.HubBridge;
  const runtime = w.runtime;
  if (!bridge || !runtime) return null;

  const id = `${pageId}.${++streamCount}`;
  let closed = false;
  const offMessage = runtime.EventsOn(`hub:${id}`, (raw) => h.onMessage(String(raw)));
  const offClosed = runtime.EventsOn(`hub:${id}:closed`, () => end());
  const end = () => {
    if (closed) return;
    closed = true;
    offMessage();
    offClosed();
    h.onClose();
  };
  // Every call waits for the one before, because Wails runs bound methods
  // concurrently and a socket keeps its frames in order.
  let calls: Promise<unknown> = bridge.Open(pageId, id).then(
    () => {
      if (!closed) h.onOpen();
    },
    () => end(),
  );
  const call = (f: () => Promise<void>) => {
    calls = calls.then(f).catch(() => undefined);
  };
  return {
    send(frame) {
      if (!closed) call(() => bridge.Send(id, frame));
    },
    close() {
      if (closed) return;
      closed = true;
      offMessage();
      offClosed();
      call(() => bridge.Close(id));
    },
  };
}

/**
 * onUpdateReady calls back with the version the desktop app has downloaded for
 * its next start. The desktop shell sends it as a Wails event of its own, not
 * through the hub. Outside the desktop app there is no runtime and it does
 * nothing.
 */
export function onUpdateReady(callback: (version: string) => void): () => void {
  const runtime = (window as unknown as { runtime?: WailsRuntime }).runtime;
  return runtime?.EventsOn('updateReady', (version) => callback(String(version))) ?? (() => {});
}
