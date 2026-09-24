import type { TextStyle } from 'react-native';
import { NotoSans_400Regular } from '@expo-google-fonts/noto-sans/400Regular';
import { NotoSans_500Medium } from '@expo-google-fonts/noto-sans/500Medium';
import { NotoSans_600SemiBold } from '@expo-google-fonts/noto-sans/600SemiBold';
import { NotoSans_700Bold } from '@expo-google-fonts/noto-sans/700Bold';

/**
 * Noto Sans, GlimStone's house font, as one static cut per weight this app
 * sets.
 *
 * A font loaded at runtime is registered on Android under one name and one
 * style, so fontWeight cannot pick a cut out of a family: a weight of 700 or
 * more asks for a bold variant nobody registered and gets the system's bold
 * instead. Every weight is therefore a family of its own, and the Text in
 * components/Text.tsx trades the weight for the family.
 *
 * Imported per weight, since the package root requires all eighteen cuts and
 * the bundler would ship every one of them. The Latin file also carries
 * Cyrillic, Greek, Vietnamese and Devanagari. Arabic, Hebrew, Thai and CJK fall
 * back per character to the system's own font, which on stock Android is Noto
 * as well.
 */
const CUTS = [
  { weight: 400, family: 'NotoSans_400Regular', source: NotoSans_400Regular },
  { weight: 500, family: 'NotoSans_500Medium', source: NotoSans_500Medium },
  { weight: 600, family: 'NotoSans_600SemiBold', source: NotoSans_600SemiBold },
  { weight: 700, family: 'NotoSans_700Bold', source: NotoSans_700Bold },
];

/** The map expo-font's useFonts loads, family name to bundled file. */
export const HOUSE_FONTS: Record<string, number> = Object.fromEntries(CUTS.map((c) => [c.family, c.source]));

const NAMED_WEIGHTS: Record<string, number> = {
  thin: 100,
  ultralight: 200,
  light: 300,
  normal: 400,
  regular: 400,
  condensed: 400,
  medium: 500,
  semibold: 600,
  bold: 700,
  condensedBold: 700,
  heavy: 800,
  black: 900,
};

/** The family of the cut nearest to `weight`, with no weight read as 400. */
export function familyFor(weight: TextStyle['fontWeight']): string {
  const w = weight === undefined ? 400 : typeof weight === 'number' ? weight : (NAMED_WEIGHTS[weight] ?? Number(weight));
  let best = CUTS[0];
  for (const c of CUTS) if (Math.abs(c.weight - w) < Math.abs(best.weight - w)) best = c;
  return best.family;
}
