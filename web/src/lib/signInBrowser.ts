// What the sign-in browser's window sends up its socket (internal/signinbrowser's
// Input): the user's keys, pointer and wheel over the page, in the page's own
// CSS pixels, and the size the panel gives it.

/** The page's size in CSS pixels and the pixel ratio of the screen it is shown on. */
export interface Viewport {
  width: number;
  height: number;
  scale: number;
}

export interface PageInput {
  type: string;
  x?: number;
  y?: number;
  button?: number;
  buttons?: number;
  clicks?: number;
  dx?: number;
  dy?: number;
  key?: string;
  code?: string;
  keyCode?: number;
  mods?: number;
  text?: string;
  width?: number;
  height?: number;
  scale?: number;
}

const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), hi);

/** viewportFor is the page size the server opens for a panel, which keeps a sign-in page within reason. */
export function viewportFor(width: number, height: number, ratio: number): Viewport {
  return {
    width: clamp(Math.floor(width), 320, 1600),
    height: clamp(Math.floor(height), 320, 1200),
    scale: clamp(ratio, 1, 3),
  };
}

type Mods = Pick<KeyboardEvent, 'altKey' | 'ctrlKey' | 'metaKey' | 'shiftKey'>;

/** mods are the held modifiers as DevTools counts them. Command on a Mac is Control on the Linux page. */
function mods(e: Mods, mac = false): number {
  let m = 0;
  if (e.altKey) m |= 1;
  if (e.ctrlKey || (mac && e.metaKey)) m |= 2;
  if (e.metaKey && !mac) m |= 4;
  if (e.shiftKey) m |= 8;
  return m;
}

/**
 * keyMessage is a key for the page, or null for one the window leaves to the
 * browser: text an input method is composing arrives whole through the text
 * field, a paste through its paste event, and Escape takes the focus out of
 * the page.
 */
export function keyMessage(e: KeyboardEvent, mac: boolean): PageInput | null {
  if (e.isComposing || e.keyCode === 229 || ['Process', 'Unidentified', 'Dead', 'Escape'].includes(e.key)) return null;
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v') return null;
  return { type: e.type, key: e.key, code: e.code, keyCode: e.keyCode, mods: mods(e, mac) };
}

/** at maps a point on the shown frame onto the page it shows. */
function at(e: { clientX: number; clientY: number }, rect: DOMRect, vp: Viewport) {
  return {
    x: Math.round(((e.clientX - rect.left) * vp.width) / rect.width),
    y: Math.round(((e.clientY - rect.top) * vp.height) / rect.height),
  };
}

/** pointerMessage is a press, release or move over the frame. clicks counts a double click as 2. */
export function pointerMessage(
  type: 'mousedown' | 'mouseup' | 'mousemove',
  e: MouseEvent,
  rect: DOMRect,
  vp: Viewport,
  clicks = e.detail,
): PageInput {
  return { type, ...at(e, rect, vp), button: e.button, buttons: e.buttons, clicks, mods: mods(e) };
}

// A wheel that counts in lines or pages, as Firefox's does, is turned into
// the pixels Chromium scrolls by.
const LINE = 40;

export function wheelMessage(
  e: Pick<WheelEvent, 'clientX' | 'clientY' | 'deltaX' | 'deltaY' | 'deltaMode'>,
  rect: DOMRect,
  vp: Viewport,
): PageInput {
  const unit = e.deltaMode === 1 ? LINE : e.deltaMode === 2 ? vp.height : 1;
  return { type: 'wheel', ...at(e, rect, vp), dx: e.deltaX * unit, dy: e.deltaY * unit };
}
