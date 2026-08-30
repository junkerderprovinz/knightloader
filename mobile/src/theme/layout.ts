import { useWindowDimensions } from 'react-native';

/**
 * Whether this screen is wide enough to lay out in two columns.
 *
 * 900 dp, and the number is chosen rather than copied: every phone in
 * portrait is far below it, a phone in landscape is around 800-900 and
 * deliberately excluded (a landscape phone is short, so a second column costs
 * more height than it buys width), and a 10" tablet is 1024-1280 either way
 * up. Read from useWindowDimensions rather than measured once, so a rotation
 * or a split-screen resize changes the answer instead of freezing it at
 * whatever the app launched into.
 *
 * The first tablet pass only capped the content width and centred it (jdp,
 * 2026-08-30: "Ist unsere app auch für tablets optimiert?"), which stops a
 * card being 900 points wide but leaves half the screen empty. This is the
 * second half of that answer: on a real tablet the empty half becomes a second
 * column of cards.
 */
export const WIDE_AT = 900;

export function useWide(): boolean {
  const { width } = useWindowDimensions();
  return width >= WIDE_AT;
}

/** The content cap: 640 on a phone, wider once there are two columns to fit
 *  inside it. Not unbounded even then - a 2560-wide desktop-class screen with
 *  two 1200-point cards is the same "one card, absurdly wide" problem again,
 *  one column further out. */
export function contentMax(wide: boolean): number {
  return wide ? 980 : 640;
}
