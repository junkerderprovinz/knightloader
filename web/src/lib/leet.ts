/**
 * The 1337 easter egg (docs/easter-eggs.md): with the speed limit at exactly
 * 1337 KiB/s, the speed curve runs on the hidden storm motion level. The
 * settings field and the curve both ask this function, so they agree.
 *
 * settings.speedLimit is bytes per second while the egg is counted in KiB/s,
 * so the value to match is 1337 * 1024. Nothing is stored: any other number
 * turns the egg off.
 */
const LEET_KIB = 1337;

/** Whether a speed limit in bytes per second is the 1337 KiB/s the egg listens for. */
export function isLeet(speedLimitBytes: number): boolean {
  return speedLimitBytes === LEET_KIB * 1024;
}
