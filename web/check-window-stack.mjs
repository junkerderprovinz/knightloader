// Escape closes the top window only, and a window that opens by itself leaves
// the focus with the window above it.
//
// Every window registers its backdrop with src/lib/windowStack.ts. The top one
// is the last backdrop in the document, not the last one opened: the folder
// chooser is portalled to <body> and paints over a captcha window that arrives
// later inside the app. The captcha is also the one window nobody asked for:
// an answer box that took the focus on arrival would take it out of the folder
// chooser or the command palette somebody is typing in.
//
// The stack runs here on stand-in elements that know only their place in the
// document and what they contain. CaptchaModal.tsx is read as text for the
// other half: the answer box asks isOnTop before it takes the focus, and does
// not carry autoFocus, which React honours without asking anybody.
//
// Run: `node web/check-window-stack.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const listeners = new Set();
globalThis.document = {
  addEventListener: (type, fn) => type === 'keydown' && listeners.add(fn),
  removeEventListener: (type, fn) => type === 'keydown' && listeners.delete(fn),
};

const { openWindow, isOnTop } = await import('./src/lib/windowStack.ts');

const problems = [];
const check = (what, ok) => ok || problems.push(what);

/** An element at `at` in document order, holding `children`. */
function el(at, ...children) {
  return {
    at,
    children,
    compareDocumentPosition: (other) => (other.at > at ? 4 : 2),
    contains(node) {
      return node === this || children.some((c) => c.contains(node));
    },
  };
}

const press = (key) => {
  for (const fn of [...listeners]) fn({ key });
};

// The captcha window sits in the app, before <body>'s portalled chooser.
const answer = el(31);
const captcha = el(30, answer);
const chooser = el(90, el(91));
const closed = [];

const offChooser = openWindow(chooser, () => closed.push('chooser'));
const offCaptcha = openWindow(captcha, () => closed.push('captcha'));

check('the captcha that arrived under the folder chooser counts as the top window', !isOnTop(answer));
press('Escape');
check(`one Escape closed ${closed.join(' and ') || 'nothing'}, want the folder chooser alone`, closed.join() === 'chooser');

offChooser();
check('with the folder chooser gone, the captcha window does not count as the top one', isOnTop(answer));
press('Escape');
check(`the second Escape closed ${closed.slice(1).join(' and ') || 'nothing'}, want the captcha window`, closed.join() === 'chooser,captcha');

offCaptcha();
check('the Escape listener stays on the document with no window open', listeners.size === 0);
check('an element outside every window counts as on top with none open', !isOnTop(el(5)));

const modal = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'src/components/CaptchaModal.tsx'), 'utf8');
check('CaptchaModal.tsx gives the answer box autoFocus, which takes the focus from any window above', !/\bautoFocus\b/.test(modal));
check('CaptchaModal.tsx focuses without asking isOnTop whether its window is the top one', /isOnTop\(/.test(modal));

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: Escape closes the top window alone, and the captcha takes the focus only as the top window');
