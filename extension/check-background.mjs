/**
 * Runs the background scripts against a stubbed `chrome`.
 *
 *   1. Without the context-menu API (Firefox for Android has none) the scripts
 *      still load, register their listeners and survive install and a
 *      language change. A top-level contextMenus call would stop the file
 *      before the message listener and lose every popup send.
 *   2. The jdcheck.js ruleset follows the Click'n'Load scripts both ways and
 *      goes off when the scripting call fails, so a site never shows a button
 *      nothing catches. An update or a browser start applies the stored state
 *      again, since an update resets a ruleset's state.
 *   3. Leaving the group also removes the random browser ID, so the relay does
 *      not see the same member in another group.
 *   4. Click'n'Load runs only with the optional site access: wanted without it
 *      stays off, granting or withdrawing it switches the scripts, the grant
 *      and the stored switch arriving together register nothing twice, and an
 *      update that finds the access missing opens the page that asks for it.
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

function makeChrome({ withMenus, scriptingThrows = false, granted = true }) {
  const calls = { dnr: [], registered: [], unregistered: [], removed: [], openedOptions: 0, menuCreates: 0 };
  const store = {};
  const access = { granted };
  // Registrations are kept, and a second one under a taken id throws as the
  // browser does.
  const live = new Map();
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
    permissions: {
      onAdded: event(),
      onRemoved: event(),
      contains: async (p) => access.granted && p?.origins?.includes('<all_urls>'),
    },
    action: {
      getUserSettings: async () => ({ isOnToolbar: true }),
      setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {}, openPopup: async () => {},
    },
    scripting: {
      getRegisteredContentScripts: async () => {
        if (scriptingThrows) throw new Error('no host permission');
        // Answered a tick later with what was there when asked, so two
        // overlapping syncs both read before either writes.
        const now = [...live.values()];
        await new Promise((r) => setTimeout(r, 1));
        return now;
      },
      registerContentScripts: async (s) => {
        for (const x of s) if (live.has(x.id)) throw new Error(`Duplicate script ID '${x.id}'`);
        for (const x of s) live.set(x.id, x);
        calls.registered.push(...s.map((x) => x.id));
      },
      unregisterContentScripts: async ({ ids }) => { for (const id of ids) live.delete(id); calls.unregistered.push(...ids); },
    },
    declarativeNetRequest: {
      updateEnabledRulesets: async (o) => { calls.dnr.push(o); },
      getEnabledRulesets: async () => ['cnl'],
    },
  };
  if (withMenus) {
    chrome.contextMenus = { create: () => { calls.menuCreates++; }, update: () => {}, onClicked: event() };
  }
  return { chrome, calls, store, access, live };
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

/** Lets listeners that return no promise finish their work. */
const settle = () => new Promise((r) => setTimeout(r, 30));
const lastRuleset = (calls) => {
  const o = calls.dnr.at(-1);
  if (!o) return 'untouched';
  return o.enableRulesetIds?.includes('cnl') ? 'on' : o.disableRulesetIds?.includes('cnl') ? 'off' : 'untouched';
};

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

// 2. The jdcheck.js ruleset follows the scripts.
for (const scriptingThrows of [false, true]) {
  for (const on of [false, true]) {
    const { chrome, calls } = makeChrome({ withMenus: true, scriptingThrows });
    const { ctx, error } = load(chrome);
    if (error) { fail(error); continue; }
    await vm.runInContext('syncCnlScripts', ctx)(on);
    const want = on && !scriptingThrows ? 'on' : 'off';
    if (lastRuleset(calls) !== want) {
      fail(`syncCnlScripts(${on})${scriptingThrows ? ' with the scripting API failing' : ''} left the 'cnl' ruleset ${lastRuleset(calls)}, want ${want}`);
    }
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
    await settle();
    const want = stored === false ? 'off' : 'on';
    if (lastRuleset(calls) !== want) fail(`on ${path} with cnlEnabled ${stored === undefined ? 'not stored' : stored} the 'cnl' ruleset was left ${lastRuleset(calls)}, want ${want}`);
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

// 4. The site access decides.
{
  // A fresh install wants the feature but has no access yet.
  const { chrome, calls, live, store } = makeChrome({ withMenus: true, granted: false });
  const { error } = load(chrome);
  if (error) fail(error);
  else {
    for (const f of chrome.runtime.onInstalled.listeners) await f({ reason: 'install' });
    if (store.cnlEnabled !== true) fail('a fresh install does not store Click\'n\'Load as wanted');
    if (live.size || lastRuleset(calls) !== 'off') fail('a fresh install without the site access registered scripts or left the ruleset on');
  }
}
{
  // Granted and withdrawn through the browser.
  const { chrome, calls, live, access } = makeChrome({ withMenus: true, granted: false });
  const { error } = load(chrome);
  if (error) fail(error);
  else {
    access.granted = true;
    for (const f of chrome.permissions.onAdded.listeners) f({ origins: ['<all_urls>'] });
    await settle();
    if (live.size !== 2 || lastRuleset(calls) !== 'on') fail(`granting the access left ${live.size} script(s) and the ruleset ${lastRuleset(calls)}, want 2 and on`);
    access.granted = false;
    for (const f of chrome.permissions.onRemoved.listeners) f({ origins: ['<all_urls>'] });
    await settle();
    if (live.size !== 0 || lastRuleset(calls) !== 'off') fail(`withdrawing the access left ${live.size} script(s) and the ruleset ${lastRuleset(calls)}, want 0 and off`);
  }
}
{
  // The options page grants and stores the switch in one go, which reaches the
  // worker as two events at once.
  const { chrome, calls, live, access, store } = makeChrome({ withMenus: true, granted: false });
  store.cnlEnabled = false;
  const { error } = load(chrome);
  if (error) fail(error);
  else {
    access.granted = true;
    store.cnlEnabled = true;
    for (const f of chrome.permissions.onAdded.listeners) f({ origins: ['<all_urls>'] });
    for (const f of chrome.storage.onChanged.listeners) f({ cnlEnabled: { newValue: true } }, 'local');
    await settle();
    if (live.size !== 2 || lastRuleset(calls) !== 'on') {
      fail(`a grant and the stored switch arriving together left ${live.size} script(s) and the ruleset ${lastRuleset(calls)}; overlapping syncs register the same id twice`);
    }
  }
}
for (const granted of [true, false]) {
  const { chrome, calls, store } = makeChrome({ withMenus: true, granted });
  store.cnlEnabled = true;
  store.phrase = 'twelve words';
  const { error } = load(chrome);
  if (error) { fail(error); continue; }
  for (const f of chrome.runtime.onInstalled.listeners) await f({ reason: 'update' });
  const opened = calls.openedOptions > 0;
  if (opened === granted) {
    fail(granted ? 'an update with the access in place opens the options page' : 'an update that finds the access gone does not open the options page that asks for it');
  }
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: background survives without context menus, the jdcheck ruleset follows the scripts and every update and start re-applies it, leaving forgets the browser id, Click\'n\'Load follows the site access');
