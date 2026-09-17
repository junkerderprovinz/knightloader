/**
 * The background scripts, run for real against a stubbed `chrome`.
 *
 * Three things a store review found on 2026-09-17, each of which no syntax or
 * locale check could see:
 *
 *   1. A browser without the context-menu API (Firefox for Android has none)
 *      must still get a working background. background.js called
 *      chrome.contextMenus at top level, the TypeError stopped the file there,
 *      and the message listener below it was never registered: every popup
 *      send was then lost without a word. Here the scripts load with no
 *      contextMenus at all, and the listeners, the install path and a language
 *      change all have to survive.
 *   2. Switching Click'n'Load off has to switch off the jdcheck.js redirect
 *      too. Only the content scripts were unregistered, so the static
 *      declarativeNetRequest ruleset kept answering sites' probes with code
 *      from this extension. The ruleset has to follow the switch both ways,
 *      even when the scripting call fails, and the state has to be written
 *      again on every sync because a ruleset's enabled state does not survive
 *      an extension update.
 *   3. Leaving the group removes the random browser ID as well, so a browser
 *      that joins another group is not the same member to the relay.
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const manifest = JSON.parse(readFileSync(join(here, 'src', 'manifest.json'), 'utf8'));
const problems = [];
const fail = (why) => problems.push(why);

function event() {
  const listeners = [];
  return { addListener: (f) => listeners.push(f), listeners };
}

function makeChrome({ withMenus, scriptingThrows = false }) {
  const calls = { dnr: [], registered: [], unregistered: [], removed: [], openedOptions: 0, menuCreates: 0 };
  const store = {};
  const chrome = {
    runtime: {
      onInstalled: event(),
      onMessage: event(),
      onStartup: event(),
      openOptionsPage: () => { calls.openedOptions++; },
      getManifest: () => manifest,
      getURL: (p) => `chrome-extension://test/${p}`,
    },
    storage: {
      onChanged: event(),
      local: {
        get: async (keys) => {
          if (keys == null) return { ...store };
          const list = Array.isArray(keys) ? keys : typeof keys === 'string' ? [keys] : Object.keys(keys);
          return Object.fromEntries(list.filter((k) => k in store).map((k) => [k, store[k]]));
        },
        set: async (o) => { Object.assign(store, o); },
        remove: async (k) => { const list = Array.isArray(k) ? k : [k]; calls.removed.push(...list); for (const x of list) delete store[x]; },
      },
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
    },
    action: {
      getUserSettings: async () => ({ isOnToolbar: true }),
      setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {}, openPopup: async () => {},
    },
    scripting: {
      getRegisteredContentScripts: async () => { if (scriptingThrows) throw new Error('no host permission'); return []; },
      registerContentScripts: async (s) => { calls.registered.push(...s.map((x) => x.id)); },
      unregisterContentScripts: async ({ ids }) => { calls.unregistered.push(...ids); },
    },
    declarativeNetRequest: {
      updateEnabledRulesets: async (o) => { calls.dnr.push(o); },
      getEnabledRulesets: async () => ['cnl'],
    },
  };
  if (withMenus) {
    chrome.contextMenus = { create: () => { calls.menuCreates++; }, update: () => {}, onClicked: event() };
  }
  return { chrome, calls, store };
}

function load(chrome) {
  const ctx = vm.createContext({
    chrome, console: { log() {}, warn() {}, error() {} }, setTimeout, clearTimeout,
    crypto: globalThis.crypto, TextEncoder, TextDecoder, URL, navigator: { language: 'en-US', languages: ['en-US'] },
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'), atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    WebSocket: class {},
  });
  // The order Firefox loads them in (manifest background.scripts). Chrome's
  // importScripts in background.js is skipped here because the context has none.
  for (const f of manifest.background.scripts) {
    try {
      vm.runInContext(readFileSync(join(here, 'src', f), 'utf8'), ctx, { filename: f });
    } catch (e) {
      return { ctx, error: `${f}: ${e.message}` };
    }
  }
  return { ctx, error: null };
}

// 1. No context-menu API.
{
  const { chrome, calls } = makeChrome({ withMenus: false });
  const { error } = load(chrome);
  if (error) fail(`without chrome.contextMenus the background scripts stop loading: ${error}`);
  if (chrome.runtime.onMessage.listeners.length === 0) fail('without chrome.contextMenus no runtime.onMessage listener is registered, so every popup send is lost');
  if (chrome.runtime.onStartup.listeners.length === 0) fail('without chrome.contextMenus no runtime.onStartup listener is registered');
  for (const f of chrome.runtime.onInstalled.listeners) {
    try { await f({ reason: 'install' }); } catch (e) { fail(`without chrome.contextMenus the install handler throws: ${e.message}`); }
  }
  if (calls.registered.length === 0 && !calls.dnr.length) fail('without chrome.contextMenus the install handler never reaches the Click\'n\'Load sync');
  if (calls.openedOptions === 0) fail('without chrome.contextMenus the install handler never opens the options page');
  for (const f of chrome.storage.onChanged.listeners) {
    try { await f({ language: { newValue: 'de' } }, 'local'); } catch (e) { fail(`without chrome.contextMenus a language change throws: ${e.message}`); }
  }
}

// With the API present the menus are still built.
{
  const { chrome, calls } = makeChrome({ withMenus: true });
  const { error } = load(chrome);
  if (error) fail(`the background scripts do not load: ${error}`);
  for (const f of chrome.runtime.onInstalled.listeners) await f({ reason: 'install' });
  if (calls.menuCreates !== 4) fail(`with chrome.contextMenus present the install handler created ${calls.menuCreates} menu entries, want 4`);
  if (chrome.contextMenus.onClicked.listeners.length !== 1) fail('with chrome.contextMenus present no onClicked listener is registered');
}

// 2. The jdcheck.js ruleset follows the switch.
for (const scriptingThrows of [false, true]) {
  for (const on of [false, true]) {
    const { chrome, calls } = makeChrome({ withMenus: true, scriptingThrows });
    const { ctx, error } = load(chrome);
    if (error) { fail(error); continue; }
    await vm.runInContext('syncCnlScripts', ctx)(on);
    const want = on ? 'enableRulesetIds' : 'disableRulesetIds';
    const hit = calls.dnr.some((o) => Array.isArray(o[want]) && o[want].includes('cnl'));
    if (!hit) fail(`syncCnlScripts(${on})${scriptingThrows ? ' with the scripting API failing' : ''} did not ${on ? 'enable' : 'disable'} the 'cnl' ruleset`);
  }
}

// 3. Leaving forgets the browser ID.
{
  const { chrome, calls } = makeChrome({ withMenus: true });
  const { ctx, error } = load(chrome);
  if (error) fail(error);
  else {
    await vm.runInContext('forgetGroup', ctx)();
    for (const key of ['phrase', 'defaultInstance', 'selfId']) {
      if (!calls.removed.includes(key)) fail(`forgetGroup() does not remove '${key}'`);
    }
  }
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: background survives without context menus, the jdcheck ruleset follows the switch, leaving forgets the browser id');
