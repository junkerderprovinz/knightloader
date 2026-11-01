import { takePageHue } from './appearance';

const TINTED = '.glim-btn-neutral, .glim-btn-subtle, .glim-btn-bare';
// A badge or button with a palette position of its own keeps that colour.
const OWN_HUE = '.glim-hue, .glim-tint-badge';

function reached(target: EventTarget | null, from: EventTarget | null): void {
  if (!(target instanceof Element)) return;
  const el = target.closest<HTMLElement>(TINTED);
  if (!el || el.matches(OWN_HUE)) return;
  // pointerover fires again for every child the pointer crosses inside the button.
  if (from instanceof Node && el.contains(from)) return;
  takePageHue(el);
}

/**
 * listenForPageHue gives every tinted button, glyph button and link badge its
 * place's colour under rainbow (GlimStone rule 40) as the pointer or the
 * keyboard reaches it. It listens on the document rather than on each control,
 * so windows and menus drawn outside the app's root are covered too.
 */
export function listenForPageHue(): void {
  document.addEventListener('pointerover', (e) => reached(e.target, e.relatedTarget));
  document.addEventListener('focusin', (e) => reached(e.target, null));
}
