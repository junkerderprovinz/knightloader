// The widest range the server believes as a trusted proxy, per family. It
// mirrors maxProxyRangeV4 and maxProxyRangeV6 in internal/settings/settings_access.go.
const WIDEST_V4 = 28;
const WIDEST_V6 = 124;

/**
 * unusedProxyRanges lists the saved entries the sign-in throttle passes over
 * because the range is wider than a proxy needs, such as one an earlier build
 * took. The server keeps them in the settings so the owner sees them here.
 */
export function unusedProxyRanges(entries: string[] | null | undefined): string[] {
  return (entries ?? [])
    .map((e) => e.trim())
    .filter((e) => {
      const [addr, bits, extra] = e.split('/');
      if (bits === undefined || extra !== undefined || !/^\d{1,3}$/.test(bits)) return false;
      return Number(bits) < (addr.includes(':') ? WIDEST_V6 : WIDEST_V4);
    });
}
