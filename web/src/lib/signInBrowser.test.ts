import { describe, expect, it } from 'vitest';
import { keyMessage, pointerMessage, viewportFor, wheelMessage } from './signInBrowser';

const key = (over: Partial<KeyboardEvent>) =>
  ({
    type: 'keydown',
    key: 'a',
    code: 'KeyA',
    keyCode: 65,
    ctrlKey: false,
    altKey: false,
    metaKey: false,
    shiftKey: false,
    isComposing: false,
    ...over,
  }) as KeyboardEvent;

describe('keys for the page', () => {
  it('sends a letter, an umlaut and the keys that move through a form', () => {
    expect(keyMessage(key({}), false)).toEqual({ type: 'keydown', key: 'a', code: 'KeyA', keyCode: 65, mods: 0 });
    expect(keyMessage(key({ key: 'ä', code: 'Quote', keyCode: 222 }), false)?.key).toBe('ä');
    for (const k of ['Tab', 'Enter', 'Backspace', 'ArrowLeft', 'Delete']) {
      expect(keyMessage(key({ key: k, code: k }), false)?.key).toBe(k);
    }
    expect(keyMessage(key({ type: 'keyup' }), false)?.type).toBe('keyup');
  });

  it('carries the modifiers as DevTools counts them', () => {
    expect(keyMessage(key({ key: 'A', shiftKey: true }), false)?.mods).toBe(8);
    expect(keyMessage(key({ ctrlKey: true, altKey: true, key: '@' }), false)?.mods).toBe(3);
  });

  it('turns Command into Control on a Mac, since the page runs on Linux', () => {
    expect(keyMessage(key({ metaKey: true }), true)?.mods).toBe(2);
    expect(keyMessage(key({ metaKey: true }), false)?.mods).toBe(4);
  });

  it('leaves to the browser what arrives as text or a paste, and Escape', () => {
    for (const e of [
      key({ isComposing: true }),
      key({ key: 'Process', keyCode: 229 }),
      key({ key: 'Unidentified', keyCode: 229 }),
      key({ key: 'Dead', code: 'BracketLeft' }),
      key({ key: 'v', ctrlKey: true }),
      key({ key: 'v', metaKey: true }),
      key({ key: 'Escape', code: 'Escape', keyCode: 27 }),
    ]) {
      expect(keyMessage(e, true)).toBeNull();
    }
  });
});

describe('the pointer on the page', () => {
  const rect = { left: 100, top: 50, width: 400, height: 300 } as DOMRect;
  const vp = { width: 800, height: 600, scale: 1 };

  it('maps a click on the shown frame onto the page it shows', () => {
    const e = { clientX: 300, clientY: 200, button: 0, buttons: 1, detail: 2, ctrlKey: false, altKey: false, metaKey: false, shiftKey: true } as PointerEvent;
    expect(pointerMessage('mousedown', e, rect, vp)).toEqual({
      type: 'mousedown', x: 400, y: 300, button: 0, buttons: 1, clicks: 2, mods: 8,
    });
  });

  it('scrolls by pixels whatever unit the wheel counts in', () => {
    const lines = { clientX: 100, clientY: 50, deltaX: 0, deltaY: 3, deltaMode: 1 } as WheelEvent;
    expect(wheelMessage(lines, rect, vp)).toMatchObject({ type: 'wheel', x: 0, y: 0, dx: 0, dy: 120 });
    const pages = { clientX: 100, clientY: 50, deltaX: 0, deltaY: 1, deltaMode: 2 } as WheelEvent;
    expect(wheelMessage(pages, rect, vp).dy).toBe(600);
  });
});

describe('the size of the page', () => {
  it('fits the panel within what the server opens', () => {
    expect(viewportFor(900.6, 640.2, 2)).toEqual({ width: 900, height: 640, scale: 2 });
    expect(viewportFor(200, 100, 0.5)).toEqual({ width: 320, height: 320, scale: 1 });
    expect(viewportFor(5000, 5000, 4)).toEqual({ width: 1600, height: 1200, scale: 3 });
  });
});
