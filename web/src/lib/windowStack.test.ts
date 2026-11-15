// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest';

import { openWindow } from './windowStack';

afterEach(() => {
  document.body.replaceChildren();
});

function page() {
  document.body.innerHTML = `
    <div id="app">
      <nav id="nav"></nav>
      <main id="main"><div id="backdrop"><button id="inside"></button></div></main>
    </div>
    <div id="region" data-live-region aria-live="polite"></div>
    <div id="chooser"></div>`;
  const $ = (id: string) => document.getElementById(id)!;
  return $;
}

it('makes everything beside the open window inert and gives it back on close', () => {
  const $ = page();
  const close = openWindow($('backdrop'), () => {});
  expect($('nav').hasAttribute('inert')).toBe(true);
  expect($('chooser').hasAttribute('inert')).toBe(true);
  expect($('backdrop').hasAttribute('inert')).toBe(false);
  expect($('inside').closest('[inert]')).toBeNull();
  close();
  expect(document.querySelector('[inert]')).toBeNull();
});

it('leaves the live regions reachable', () => {
  const $ = page();
  const close = openWindow($('backdrop'), () => {});
  expect($('region').hasAttribute('inert')).toBe(false);
  close();
});

it('makes the window under a later one inert until the later one closes', () => {
  const $ = page();
  const closeLower = openWindow($('backdrop'), () => {});
  const closeUpper = openWindow($('chooser'), () => {});
  expect($('app').hasAttribute('inert')).toBe(true);
  expect($('chooser').hasAttribute('inert')).toBe(false);
  closeUpper();
  expect($('app').hasAttribute('inert')).toBe(false);
  expect($('chooser').hasAttribute('inert')).toBe(true);
  closeLower();
});

it('keeps an element the app made inert itself', () => {
  const $ = page();
  $('nav').setAttribute('inert', '');
  const close = openWindow($('backdrop'), () => {});
  close();
  expect($('nav').hasAttribute('inert')).toBe(true);
});
