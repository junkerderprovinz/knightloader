// Tab inside the crypto window reaches everything the window makes focusable,
// the (i) on its heading included, and never leaves the window. Escape on that
// (i) closes its bubble first and the window only on the next press.
//
// The window keeps Tab inside itself by hand, as aria-modal promises, so its
// stops are a list options.js builds. Whatever that list leaves out cannot be
// reached from the keyboard at all, and the (i) is where the window's
// introduction lives: a list of buttons alone leaves it to the mouse.
//
// Both the bubble and the window listen for Escape on the document. The
// bubble's listener runs on the way down and keeps the key there; in the same
// phase as the window's, one press would hide the bubble and close the window,
// and the focus would go back to the page.
//
// The check runs the real key handler from options.js and the real (i) and
// bubble from tooltip.js against a stand-in of the window with the same
// nesting as options.html.
//
// Run by CI and by hand: `node extension/check-crypto-tab.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];
const fail = (why) => problems.push(why);

/** The source of one top-level function of a page script, keyword to closing brace. */
function functionSource(text, name) {
  const start = text.search(new RegExp(`^(async )?function ${name}\\(`, 'm'));
  if (start < 0) return null;
  let depth = 0;
  for (let i = text.indexOf('{', text.indexOf(')', start)); i < text.length; i++) {
    if (text[i] === '{') depth++;
    else if (text[i] === '}' && --depth === 0) return text.slice(start, i + 1);
  }
  return null;
}

/** A selector test for the few forms the page uses; anything else is an error, never a silent miss. */
function matcher(selector) {
  if (/^[a-z][a-z0-9]*$/.test(selector)) return (el) => el.tagName === selector.toUpperCase();
  const cls = selector.match(/^\.([\w-]+)$/);
  if (cls) return (el) => el.className.split(/\s+/).includes(cls[1]);
  const attr = selector.match(/^\[([\w-]+)(?:="([^"]*)")?\]$/);
  if (attr) return (el) => el.getAttribute(attr[1]) !== null && (attr[2] === undefined || el.getAttribute(attr[1]) === attr[2]);
  throw new Error(`the stand-in DOM does not know the selector "${selector}"`);
}
const matchesAny = (selector) => {
  const tests = selector.split(',').map((s) => matcher(s.trim()));
  return (el) => tests.some((t) => t(el));
};

class Element {
  constructor(tag, doc) {
    this.tagName = tag.toUpperCase();
    this.doc = doc;
    this.parent = null;
    this.children = [];
    this.attrs = {};
    this.className = '';
    this.style = { setProperty() {} };
    this.offsetWidth = 120;
    this.offsetHeight = 40;
    this.isConnected = true;
  }
  get id() {
    return this.attrs.id ?? '';
  }
  set id(v) {
    this.attrs.id = String(v);
  }
  get classList() {
    const names = () => this.className.split(/\s+/).filter(Boolean);
    return {
      contains: (c) => names().includes(c),
      toggle: (c, on) => {
        const rest = names().filter((n) => n !== c);
        this.className = (on ? [...rest, c] : rest).join(' ');
      },
    };
  }
  get tabIndex() {
    if ('tabindex' in this.attrs) return Number(this.attrs.tabindex);
    return this.tagName === 'BUTTON' ? 0 : -1;
  }
  set tabIndex(v) {
    this.attrs.tabindex = String(v);
  }
  setAttribute(k, v) {
    this.attrs[k] = String(v);
  }
  getAttribute(k) {
    return k in this.attrs ? this.attrs[k] : null;
  }
  removeAttribute(k) {
    delete this.attrs[k];
  }
  appendChild(child) {
    child.parent = this;
    this.children.push(child);
    return child;
  }
  append(...children) {
    for (const c of children) this.appendChild(c);
  }
  contains(node) {
    for (let n = node; n; n = n.parent) if (n === this) return true;
    return false;
  }
  closest(selector) {
    const test = matchesAny(selector);
    for (let n = this; n; n = n.parent) if (test(n)) return n;
    return null;
  }
  querySelectorAll(selector) {
    const test = matchesAny(selector);
    const out = [];
    const walk = (el) => {
      for (const c of el.children) {
        if (test(c)) out.push(c);
        walk(c);
      }
    };
    walk(this);
    return out;
  }
  querySelector(selector) {
    return this.querySelectorAll(selector)[0] ?? null;
  }
  getBoundingClientRect() {
    return { left: 100, top: 100, right: 115, bottom: 115, width: 15, height: 15 };
  }
  focus() {
    this.doc.activeElement = this;
  }
}

/** Document listeners by phase, run in the order the browser runs them on the document itself. */
const listeners = [];
function dispatch(type, init) {
  let stopped = false;
  const event = { type, ...init, stopPropagation: () => (stopped = true), preventDefault() {} };
  for (const capture of [true, false]) {
    for (const l of listeners.filter((l) => l.type === type && l.capture === capture)) l.fn(event);
    if (stopped) return;
  }
}

const doc = {
  activeElement: null,
  body: null,
  documentElement: { clientWidth: 1024, clientHeight: 768 },
  createElement: (tag) => new Element(tag, doc),
  getElementById: (id) => doc.body.querySelector(`[id="${id}"]`),
  addEventListener: (type, fn, capture = false) => listeners.push({ type, fn, capture: capture === true }),
};
const el = (tag, id, ...children) => {
  const e = doc.createElement(tag);
  if (id) e.setAttribute('id', id);
  e.append(...children);
  return e;
};

const copy = el('button', 'cryptoCopy');
const close = el('button', 'cryptoClose');
const cryptoEl = el(
  'div',
  'cryptoDonate',
  el(
    'div',
    null,
    el('h2', 'cryptoHeading', el('span', 'cryptoTitle')),
    el(
      'div',
      null,
      el(
        'div',
        null,
        el('div', 'cryptoQr'),
        el('p', 'cryptoAddress'),
        el('div', 'cryptoChains', el('button'), el('button')),
        el('p', 'cryptoNote'),
        copy,
      ),
      el('div', 'cryptoCoins', el('button'), el('button'), el('button')),
    ),
    el('div', null, close),
  ),
);
doc.body = el('body', null, cryptoEl);

let closed = 0;
const ctx = vm.createContext({
  document: doc,
  window: { innerWidth: 1024, innerHeight: 768, addEventListener() {} },
  Element,
  Node: Element,
  MutationObserver: class {
    observe() {}
  },
  cryptoEl,
  closeCrypto: () => closed++,
});
vm.runInContext(read('src', 'tooltip.js'), ctx, { filename: 'tooltip.js' });
ctx.window.glimSetInfo('cryptoHeading', 'Pick a coin and a network, then scan the code or copy the address.');
const icon = doc.getElementById('cryptoHeading').querySelector('.glim-info-icon');
if (!icon) fail('src/tooltip.js: glimSetInfo put no (i) on the heading, so there is nothing to reach');

const source = functionSource(read('src', 'options.js'), 'onCryptoKey');
if (!source) {
  fail('src/options.js: no onCryptoKey() - nothing keeps Tab inside the crypto window');
} else if (icon) {
  const onCryptoKey = vm.runInContext(`(${source})`, ctx);
  const focusable = [];
  const walk = (e) => {
    for (const c of e.children) {
      if (c.tabIndex >= 0) focusable.push(c);
      walk(c);
    }
  };
  walk(cryptoEl);

  for (const shiftKey of [false, true]) {
    const key = shiftKey ? 'Shift+Tab' : 'Tab';
    copy.focus();
    const seen = new Set();
    for (let i = 0; i < focusable.length; i++) {
      let kept = false;
      onCryptoKey({ key: 'Tab', shiftKey, preventDefault: () => (kept = true) });
      if (!kept || !focusable.includes(doc.activeElement)) {
        fail(`${key} leaves the crypto window, which aria-modal promises it does not`);
        break;
      }
      seen.add(doc.activeElement);
    }
    const missed = focusable.filter((e) => !seen.has(e));
    if (missed.includes(icon)) {
      fail(`${key} never reaches the (i) on the crypto window's heading, so its introduction cannot be read from the keyboard`);
    } else if (missed.length) {
      fail(`${key} skips ${missed.length} of the crypto window's ${focusable.length} focusable elements`);
    }
  }

  // The page wires the bubble at load and the window its key handler when it
  // opens, which is the order the two listeners are added in here.
  ctx.window.wireTooltips();
  doc.addEventListener('keydown', onCryptoKey);
  const bubble = () => doc.getElementById('glim-bubble');
  dispatch('keydown', { key: 'Tab' });
  icon.focus();
  dispatch('focusin', { target: icon });
  if (!bubble() || bubble().style.display !== 'block') {
    fail('src/tooltip.js: focusing the (i) from the keyboard opened no bubble, so there is nothing for Escape to close');
  } else {
    dispatch('keydown', { key: 'Escape' });
    if (bubble().style.display !== 'none') fail('Escape on the (i) left its bubble open');
    if (closed > 0) {
      fail('one Escape on the (i) closed its bubble and the whole crypto window with it');
    } else {
      dispatch('keydown', { key: 'Escape' });
      if (closed !== 1) fail('a second Escape, with the bubble gone, did not close the crypto window');
    }
  }
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log(
  'ok: Tab and Shift+Tab stay inside the crypto window and reach every stop in it, the (i) on its heading included, ' +
    'and Escape on the (i) closes its bubble before the window',
);
