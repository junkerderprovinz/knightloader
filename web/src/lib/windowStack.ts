// The windows that are open, by backdrop, each with its way out. A window
// opened from a window, such as the folder chooser from a download's options,
// leaves both open, and one press of Escape closes only the top one. One
// listener decides for all of them: with one each, the window below would
// already be on top when its own listener ran, and the same press would close
// it too. It takes no DOM of its own beyond the elements it is handed, so
// web/check-window-stack.mjs can run it on stand-ins.

const open = new Map<Element, () => void>();

// Node.DOCUMENT_POSITION_FOLLOWING, spelled out for the same stand-ins.
const FOLLOWING = 4;

/**
 * topWindow is the window painted over the others: the last backdrop in the
 * document, since the backdrops are fixed layers of the same kind. Opening
 * order would not do, because the folder chooser is portalled to <body> and
 * paints over a captcha window that arrives later inside the app.
 */
function topWindow(): Element | undefined {
  let top: Element | undefined;
  for (const el of open.keys()) {
    if (!top || top.compareDocumentPosition(el) & FOLLOWING) top = el;
  }
  return top;
}

function closeTop(e: KeyboardEvent) {
  if (e.key !== 'Escape') return;
  const top = topWindow();
  if (top) open.get(top)?.();
}

/** openWindow puts a window on the stack until the returned function takes it off. */
export function openWindow(backdrop: Element, close: () => void): () => void {
  if (open.size === 0) document.addEventListener('keydown', closeTop);
  open.set(backdrop, close);
  return () => {
    open.delete(backdrop);
    if (open.size === 0) document.removeEventListener('keydown', closeTop);
  };
}

/**
 * isOnTop reports whether el is inside the window painted over every other
 * one. A window that opens by itself, such as a captcha, asks before it takes
 * the focus, which would otherwise leave the window above without it.
 */
export function isOnTop(el: Element): boolean {
  return topWindow()?.contains(el) ?? false;
}
