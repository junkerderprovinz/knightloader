/**
 * The popup's send button: it says what it is about to send, and what it sends
 * survives the popup closing.
 *
 * Both were wrong, and both were found in a store-review dry run on 2026-09-16
 * and 2026-09-17, walking the path a reviewer walks.
 *
 * The label. It said "Send this page" whatever was waiting. A right-clicked
 * link parked for a choice between two instances opened the popup with the page
 * title above that button, and pressing it sent the LINK. Three places have to
 * agree:
 *
 *   1. background.js tags every right-click payload with what was clicked.
 *   2. sendLabelKey() in shared.js turns a parked send into the right key.
 *   3. popup.js labels the button through sendLabelKey() and nowhere names
 *      'popup.send' itself, or the countdown's cancel path puts the old label
 *      back on a batch of links.
 *
 * The hand-over. popup.js sent its message and closed on the next line. When
 * the service worker was asleep, the normal state half a minute after anything
 * last happened, the worker had not started by the time the window was gone and
 * the send vanished: no badge, nothing at the instance. Measured on the same
 * press, same page, same instance: worker asleep, tasks 0 -> 0; worker awake,
 * 0 -> 1. So:
 *
 *   4. popup.js never calls window.close() in a send path except through
 *      handOver(), which awaits the message first.
 *   5. background.js answers the send message, so the await has something to
 *      wait for instead of depending on how a listener that says nothing is
 *      resolved.
 *
 * The keys themselves are in every language because check-locales.mjs says so.
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];
const fail = (why) => problems.push(why);

// 2. The mapping, run for real.
const ctx = vm.createContext({});
vm.runInContext(read('src', 'shared.js'), ctx);
const sendLabelKey = vm.runInContext('typeof sendLabelKey === "function" ? sendLabelKey : null', ctx);
if (!sendLabelKey) {
  fail('src/shared.js: no sendLabelKey() - nothing decides what the send button says');
} else {
  const cases = [
    [null, 'popup.send', 'the current tab (no parked send)'],
    [{ origin: '', payload: { kind: 'page', url: 'https://a.example/' } }, 'popup.send', 'a right-clicked page'],
    [{ origin: '', payload: { kind: 'link', url: 'https://a.example/f.zip' } }, 'popup.sendLink', 'a right-clicked link'],
    [{ origin: '', payload: { kind: 'image', url: 'https://a.example/i.png' } }, 'popup.sendImage', 'a right-clicked image'],
    [{ origin: '', payload: { kind: 'selection', text: 'https://a.example/f.zip' } }, 'popup.sendSelection', 'a right-clicked selection'],
    [{ origin: 'cnl', payload: { text: 'https://a.example/1\nhttps://a.example/2' } }, 'popup.sendLinks', "a caught Click'n'Load batch"],
  ];
  for (const [pending, want, what] of cases) {
    const got = sendLabelKey(pending);
    if (got !== want) fail(`sendLabelKey(${what}) gave ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  }
}

// 1. Every right-click payload says what it is.
const background = read('src', 'background.js');
const onClicked = background.slice(background.indexOf('chrome.contextMenus.onClicked.addListener'));
const handler = onClicked.slice(0, onClicked.indexOf('\n});') + 4);
for (const kind of ['link', 'image', 'selection', 'page']) {
  if (!new RegExp(`kind:\\s*'${kind}'`).test(handler)) {
    fail(`src/background.js: the context-menu handler builds no payload with kind: '${kind}'`);
  }
}

// 3. The popup never names the page label itself.
const popup = read('src', 'popup.js').replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
for (const m of popup.matchAll(/t\(\s*'popup\.send'\s*\)/g)) {
  fail(`src/popup.js:${popup.slice(0, m.index).split('\n').length} labels the send button with 'popup.send' directly - go through sendLabelKey(pending)`);
}
if (!/sendLabelKey\(\s*pending\s*\)/.test(popup)) {
  fail('src/popup.js: never calls sendLabelKey(pending)');
}

// 4. Every send is handed over before the window goes.
const handOverDef = popup.match(/async function handOver\s*\(\s*(\w+)\s*\)\s*\{([\s\S]*?)\n\}/);
if (!handOverDef) {
  fail('src/popup.js: no async function handOver(message) - nothing waits for the worker before the window closes');
} else {
  const body = handOverDef[2];
  const awaitAt = body.search(/await\s+chrome\.runtime\.sendMessage\(/);
  const closeAt = body.indexOf('window.close()');
  if (awaitAt < 0 || closeAt < 0 || closeAt < awaitAt) {
    fail('src/popup.js: handOver() has to await chrome.runtime.sendMessage() and only then call window.close()');
  }
}
const outside = handOverDef ? popup.replace(handOverDef[0], '') : popup;
for (const m of outside.matchAll(/chrome\.runtime\.sendMessage\(\s*\{\s*type:\s*'knightloader-send-to'/g)) {
  fail(`src/popup.js:${outside.slice(0, m.index).split('\n').length} sends 'knightloader-send-to' outside handOver() - the window can close before the worker has it`);
}
for (const m of outside.matchAll(/window\.close\(\)/g)) {
  fail(`src/popup.js:${outside.slice(0, m.index).split('\n').length} closes the window outside handOver()`);
}

// 5. The worker answers the send, so the popup's await resolves on a reply.
const listener = background.slice(background.indexOf('chrome.runtime.onMessage.addListener'));
const listenerBody = listener.slice(0, listener.indexOf('\n});') + 4);
if (!/\(\s*msg\s*,\s*_?sender\s*,\s*sendResponse\s*\)/.test(listenerBody) || !/knightloader-send-to[\s\S]*?sendResponse\(/.test(listenerBody)) {
  fail("src/background.js: the onMessage listener does not answer 'knightloader-send-to' with sendResponse()");
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: the send button names what is waiting, and every send reaches the worker before the popup closes');
