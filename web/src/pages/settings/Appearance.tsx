// The Aussehen tab: corners, navigation labels, motion, the colour set and the
// light/dark switch.
//
// Three lines, and deliberately so. All five cards read the same draft, the
// same accent-slot memory, the same palette and the same save error as the
// General tab's own cards, so the split jdp asked for (2026-09-07: "alle
// theming sachen schieben wir in einen neuen aussehen tab. sonst wir der
// allgemein tab zu unübersichtlich") is a split by SECTION inside one
// component, not a move of markup into a second one. See Look.tsx's own
// LookSection type.
import { Look } from './Look';

export function Appearance() {
  return <Look section="appearance" />;
}
