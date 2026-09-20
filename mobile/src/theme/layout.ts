import { useWindowDimensions } from 'react-native';

/**
 * Whether this screen is wide enough to lay out in two columns.
 *
 * 900 dp: every phone in portrait is far below it, a phone in landscape is
 * around 800 to 900 and excluded, since a landscape phone is short and a second
 * column costs more height than it buys width, and a 10" tablet is 1024 to 1280
 * either way up. Read from useWindowDimensions rather than measured once, so a
 * rotation or a split-screen resize changes the answer instead of freezing it
 * at whatever the app launched into.
 *
 * Capping the content width alone stops a card being 900 points wide and leaves
 * half a tablet screen empty; this is what turns that half into a second column.
 */
export const WIDE_AT = 900;

export function useWide(): boolean {
  const { width } = useWindowDimensions();
  return width >= WIDE_AT;
}

/** The content cap: 640 on a phone, wider once there are two columns to fit
 *  inside it, and bounded even then. Two 1200-point cards on a 2560-wide screen
 *  are the same absurdly wide card, one column further out. */
export function contentMax(wide: boolean): number {
  return wide ? 980 : 640;
}
