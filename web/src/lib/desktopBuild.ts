// Which desktop bundle suits the computer this page is open on. desktop.yml
// builds Windows and Linux for x64 and for ARM64, and one universal bundle for
// macOS, which leaves nothing to choose there.

export type Arch = 'x64' | 'arm64';
export type DesktopOS = 'windows' | 'linux';
export type DesktopSlug = `${DesktopOS}-${'amd64' | 'arm64'}` | 'macos-universal';

/** How the reader knows each architecture, which is not how Go names x64. */
export const ARCH_LABEL: Record<Arch, string> = { x64: 'x64', arm64: 'ARM64' };

export function desktopSlug(os: DesktopOS, arch: Arch): DesktopSlug {
  return `${os}-${arch === 'x64' ? 'amd64' : 'arm64'}`;
}

interface HighEntropyHints {
  getHighEntropyValues(hints: string[]): Promise<{ architecture?: string; bitness?: string }>;
}

/**
 * visitorArch is this computer's architecture where the browser tells, and null
 * where it does not. Chromium tells through User-Agent Client Hints, which a
 * page gets only from a secure origin, so a server reached by plain http on the
 * LAN learns nothing that way. Firefox on Linux names aarch64 in its user agent.
 */
export async function visitorArch(nav: Navigator = navigator): Promise<Arch | null> {
  const hints = (nav as Navigator & { userAgentData?: HighEntropyHints }).userAgentData;
  const values = await hints?.getHighEntropyValues(['architecture', 'bitness']).catch(() => undefined);
  if (values?.bitness === '64') {
    if (values.architecture === 'arm') return 'arm64';
    if (values.architecture === 'x86') return 'x64';
  }
  return /\b(aarch64|arm64)\b/i.test(nav.userAgent) ? 'arm64' : null;
}
