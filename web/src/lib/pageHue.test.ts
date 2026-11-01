// @vitest-environment jsdom
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { applyRainbow } from './appearance';
import { listenForPageHue } from './pageHue';

const tint = (el: Element) => (el as HTMLElement).style.getPropertyValue('--btn-tint');
const over = (el: Element, from: Element | null = null) =>
  el.dispatchEvent(new MouseEvent('pointerover', { bubbles: true, relatedTarget: from }));

describe('listenForPageHue', () => {
  beforeAll(() => {
    listenForPageHue();
    applyRainbow({ on: true });
  });
  afterAll(() => applyRainbow({ on: false }));

  beforeEach(() => {
    document.body.innerHTML = `
      <button id="a" class="glim-btn-subtle"><span id="glyph" class="glim-btn-glyph"></span></button>
      <a id="link" href="#" class="glim-btn-subtle"></a>
      <button id="hued" class="glim-btn-subtle glim-tint-badge"></button>
      <button id="bare" class="glim-btn-bare"></button>`;
  });

  const $ = (id: string) => document.getElementById(id)!;

  it('tints the button the pointer reaches through its glyph', () => {
    over($('glyph'));
    expect(tint($('a'))).toContain('--rb-0');
  });

  it('counts a link badge among the buttons', () => {
    over($('link'));
    expect(tint($('link'))).toContain('--rb-1');
  });

  it('tints a glyph button reached by the keyboard', () => {
    $('bare').dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    expect(tint($('bare'))).toContain('--rb-3');
  });

  it('leaves a button with a colour of its own alone', () => {
    over($('hued'));
    expect(tint($('hued'))).toBe('');
  });

  it('does not count again while the pointer moves within the button', () => {
    over($('a'));
    ($('a') as HTMLElement).style.removeProperty('--btn-tint');
    over($('glyph'), $('a'));
    expect(tint($('a'))).toBe('');
  });
});
