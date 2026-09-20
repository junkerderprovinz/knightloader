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
