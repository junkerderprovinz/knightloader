/**
 * The 1337 easter egg's one fact, in one place (docs/easter-eggs.md).
 *
 * With the speed limit standing at exactly 1337 KiB/s the speed curve runs on
 * the storm curve, the hidden fourth motion level, for as long as the limit
 * stands. Two surfaces have to agree on when that is - the settings field that
 * shows the word beside the number, and the curve that changes - and a second
 * copy of `1337 * 1024` in either of them is the drift that makes an egg work
 * in one place and not the other.
 *
 * THE UNIT IS THE WHOLE OF IT. `settings.speedLimit` is BYTES per second
 * everywhere it travels (lib/api.ts, internal/app), while every field that
 * edits it draws KiB and multiplies on the way out - pages/settings/
 * DownloadsSettings.tsx divides by 1024 to show and multiplies by 1024 to
 * write, and components/QueueBar.tsx's own field picks a unit per value. So the
 * gesture a person performs is "type 1337 into a KiB field", and the value that
 * reaches here is 1369088. Comparing against a bare 1337 would be comparing
 * against 1337 B/s: a limit nobody would set on purpose, and an egg nobody
 * would ever find.
 *
 * NOTHING IS STORED AND NOTHING IS REMEMBERED. This is a question asked of the
 * current value, every render, which is what makes the way out the ordinary way
 * in: type a different number and it is off. There is no "found it" flag,
 * because a flag is how this app's other hidden level once put a permanent
 * fourth entry in a settings picker.
 */
const LEET_KIB = 1337;

/** Whether a speed limit in BYTES per second is the 1337 KiB/s the egg listens for. */
export function isLeet(speedLimitBytes: number): boolean {
  return speedLimitBytes === LEET_KIB * 1024;
}
