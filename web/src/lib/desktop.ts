// The frontend's only door onto package 20's two OS-native actions
// (reveal-in-folder, open-natively) and the only place that asks "am I
// running inside the desktop app at all".
//
// Wails binds Go methods onto window.go.<package>.<Type>.<Method> at runtime
// - there is nothing to import, because the desktop build injects the object
// itself into the page before any script of ours runs. The container/browser
// build never does that: desktop/files.go's DesktopFiles type is only ever
// bound from desktop/main.go, which is a separate Go module the server never
// imports. So window.go.main.DesktopFiles is simply undefined in a browser,
// and isDesktop() below is a plain, synchronous check for that - not a
// server round trip, and not a build-time flag that could disagree with what
// is actually running.
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

/**
 * revealInFolder and openNatively reject with the server's own reason
 * (SafeTaskFile's refusal, or the OS call's own error) rather than failing
 * silently - a caller shows that message, it does not swallow it.
 */
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
