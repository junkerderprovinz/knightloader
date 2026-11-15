// Words for a screen reader that have no element of their own, such as
// "3 selected" or a toast. A live region announces a change of its content,
// not its arrival, so the regions have to be in the page before the first
// message: ToastProvider renders both for good, and announce() adds one to
// <body> where none is there yet.

/** "assertive" interrupts the screen reader and belongs to failures alone. */
export type Politeness = 'polite' | 'assertive';

// The region is emptied first and filled a moment later, so the same words
// said twice are read twice.
const SETTLE_MS = 100;

/** announce has the screen reader say `text`. */
export function announce(text: string, politeness: Politeness = 'polite'): void {
  // A region behind an open window is inert and out of a screen reader's reach.
  let region = [...document.querySelectorAll<HTMLElement>(`[data-live-region][aria-live="${politeness}"]`)].find(
    (r) => !r.closest('[inert]'),
  );
  if (!region) {
    region = document.createElement('div');
    region.className = 'sr-only';
    region.setAttribute('data-live-region', '');
    region.setAttribute('role', politeness === 'assertive' ? 'alert' : 'status');
    region.setAttribute('aria-live', politeness);
    region.setAttribute('aria-atomic', 'true');
    document.body.append(region);
  }
  const target = region;
  target.textContent = '';
  setTimeout(() => {
    target.textContent = text;
  }, SETTLE_MS);
}
