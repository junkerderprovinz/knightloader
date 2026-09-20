/**
 * Runs the background scripts against a stubbed `chrome`.
 *
 *   1. Without the context-menu API (Firefox for Android has none) the scripts
 *      still load, register their listeners and survive install and a
 *      language change. A top-level contextMenus call would stop the file
 *      before the message listener and lose every popup send.
 *   2. The jdcheck.js ruleset follows the Click'n'Load switch both ways, even
 *      when the scripting call fails, and an update or a browser start applies
 *      the stored state again, since an update resets a ruleset's state.
 *   3. Leaving the group also removes the random browser ID, so the relay does
 *      not see the same member in another group.
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

// 2b. An update and a browser start apply the stored state again, since Chrome
//     resets a static ruleset to its manifest default on every update.
for (const stored of [false, undefined]) {
  for (const path of ['update', 'startup']) {
    const { chrome, calls, store } = makeChrome({ withMenus: true });
    if (stored !== undefined) store.cnlEnabled = stored;
    const { error } = load(chrome);
    if (error) { fail(error); continue; }
    if (path === 'update') for (const f of chrome.runtime.onInstalled.listeners) await f({ reason: 'update' });
    else for (const f of chrome.runtime.onStartup.listeners) f();
    await new Promise((r) => setTimeout(r, 0)); // onStartup returns no promise
    const want = stored === false ? 'disableRulesetIds' : 'enableRulesetIds';
    const other = stored === false ? 'enableRulesetIds' : 'disableRulesetIds';
    const has = (k) => calls.dnr.some((o) => Array.isArray(o[k]) && o[k].includes('cnl'));
    if (!has(want) || has(other)) fail(`on ${path} with cnlEnabled ${stored === undefined ? 'not stored' : stored} the 'cnl' ruleset was not ${stored === false ? 'disabled' : 'enabled'}`);
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
console.log('ok: background survives without context menus, the jdcheck ruleset follows the switch and every update and start re-applies it, leaving forgets the browser id');
