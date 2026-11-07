// Where the system accent comes from (GlimStone's rule 44, browserAccent.ts
// there). The desktop app asks its shell, which reads the desktop's accent. A
// browser reads CSS AccentColor off a probe element, with the browser's
// limits:
//
//   Firefox and Safari resolve AccentColor to the system's colour. Chromium
//   resolves it to a fixed colour of its own whatever the system uses, so
//   Chrome, Edge, Opera and every other Chromium browser count as no source.
//   Where the system has no accent of its own, as on Linux without a theme
//   that names one or on iOS, the browser answers with its default.
//   Firefox with resistFingerprinting answers with a fixed colour too, and
//   nothing on the page can tell.
//   No event fires when the system's accent changes, so the colour is read
//   again when the window comes back to the front, which is when somebody
//   returns from the system's settings.

import { setSystemAccent } from './appearance';
import { desktopAccent, isDesktop } from './desktop';

/** browserAccentWorks says whether AccentColor carries the system's colour in this browser. */
export function browserAccentWorks(): boolean {
  if (typeof CSS === 'undefined' || !CSS.supports('color', 'AccentColor')) return false;
  // No word boundary, so a headless Chrome's `HeadlessChrome/` counts as well.
  return !/Chrom(e|ium)\//.test(navigator.userAgent);
}

/** browserAccent is the system's accent as the browser reports it, or undefined where it is no source. */
export function browserAccent(): string | undefined {
  if (!browserAccentWorks()) return undefined;
  const probe = document.createElement('span');
  probe.style.color = 'AccentColor';
  probe.style.display = 'none';
  document.documentElement.append(probe);
  const colour = getComputedStyle(probe).color;
  probe.remove();
  return cssColourToHex(colour);
}

/** cssColourToHex turns a computed rgb() or rgba() colour into #RRGGBB, and anything else into undefined. */
export function cssColourToHex(colour: string): string | undefined {
  const m = colour.match(/^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)/);
  if (!m) return undefined;
  return '#' + m.slice(1, 4).map((v) => Number(v).toString(16).padStart(2, '0')).join('').toUpperCase();
}

/**
 * watchSystemAccent feeds the system's colour to appearance.ts now and each
 * time the window comes back to the front, and returns the function that
 * stops. A desktop call that fails keeps the colour it had.
 */
export function watchSystemAccent(): () => void {
  const read = isDesktop()
    ? () => void desktopAccent().then((hex) => setSystemAccent(hex || undefined), () => {})
    : () => setSystemAccent(browserAccent());
  read();
  const again = () => {
    if (document.visibilityState !== 'hidden') read();
  };
  window.addEventListener('focus', again);
  document.addEventListener('visibilitychange', again);
  return () => {
    window.removeEventListener('focus', again);
    document.removeEventListener('visibilitychange', again);
  };
}
