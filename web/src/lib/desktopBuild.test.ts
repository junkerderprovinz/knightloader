import { describe, expect, it } from 'vitest';

import { desktopSlug, visitorArch } from './desktopBuild';

const WINDOWS_CHROME =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36';
const LINUX_ARM_FIREFOX = 'Mozilla/5.0 (X11; Linux aarch64; rv:142.0) Gecko/20100101 Firefox/142.0';

function browser(userAgent: string, hints?: { architecture?: string; bitness?: string } | Error): Navigator {
  const userAgentData =
    hints === undefined
      ? undefined
      : { getHighEntropyValues: async () => (hints instanceof Error ? Promise.reject(hints) : hints) };
  return { userAgent, userAgentData } as unknown as Navigator;
}

describe('visitorArch', () => {
  it('reads ARM64 and x64 from the client hints', async () => {
    expect(await visitorArch(browser(WINDOWS_CHROME, { architecture: 'arm', bitness: '64' }))).toBe('arm64');
    expect(await visitorArch(browser(WINDOWS_CHROME, { architecture: 'x86', bitness: '64' }))).toBe('x64');
  });

  it('does not offer a 64-bit build to a 32-bit machine', async () => {
    expect(await visitorArch(browser(WINDOWS_CHROME, { architecture: 'arm', bitness: '32' }))).toBeNull();
  });

  it('falls back to the user agent without client hints or when they are refused', async () => {
    expect(await visitorArch(browser(LINUX_ARM_FIREFOX))).toBe('arm64');
    expect(await visitorArch(browser(LINUX_ARM_FIREFOX, new Error('NotAllowedError')))).toBe('arm64');
  });

  it('leaves the choice open when nothing tells', async () => {
    expect(await visitorArch(browser(WINDOWS_CHROME))).toBeNull();
  });
});

describe('desktopSlug', () => {
  it('names the zips the way desktop.yml does', () => {
    expect(desktopSlug('windows', 'x64')).toBe('windows-amd64');
    expect(desktopSlug('windows', 'arm64')).toBe('windows-arm64');
    expect(desktopSlug('linux', 'x64')).toBe('linux-amd64');
    expect(desktopSlug('linux', 'arm64')).toBe('linux-arm64');
  });
});
