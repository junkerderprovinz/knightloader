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
 *   3. The phrase stays out of storage.local, which content scripts can read:
 *      it is written to the extension's IndexedDB, one found in storage.local
 *      moves there, and leaving the group deletes it. Every relay session
 *      joins under an id of its own, so two at once do not knock each other
 *      off, while the group knows the browser by one member id until it
 *      leaves. Only an extension page can pick a send's target.
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

/**
 * fakeIndexedDB holds object stores in memory and answers the part of the API
 * group.js uses: open with an upgrade, one store per transaction, get, put and
 * delete. Transactions run one after another, as two over the same store do in
 * a browser, and one completes once its last request, including any made from
 * an onsuccess, has run.
 */
function fakeIndexedDB() {
  const stores = new Map();
  const later = (f) => setTimeout(f, 0);
  let previous = Promise.resolve();
  const indexedDB = {
    open() {
      const req = {};
      later(() => {
        const db = {
          createObjectStore: (name) => stores.set(name, new Map()),
          transaction: (name) => {
            const data = stores.get(name);
            const tx = {};
            const start = previous;
            let finish;
            previous = new Promise((r) => (finish = r));
            let pending = 0;
            const run = (fn) => {
              const r = {};
              pending++;
              start.then(() =>
                later(() => {
                  r.result = fn();
                  r.onsuccess?.();
                  if (--pending === 0) {
                    later(() => {
                      finish();
                      tx.oncomplete?.();
                    });
                  }
                }),
              );
              return r;
            };
            tx.objectStore = () => ({
              get: (k) => run(() => data.get(k)),
              put: (v, k) => run(() => data.set(k, v)),
              delete: (k) => run(() => data.delete(k)),
            });
            return tx;
          },
          close() {},
        };
        req.result = db;
        if (stores.size === 0) req.onupgradeneeded?.();
        req.onsuccess?.();
      });
      return req;
    },
  };
  return { indexedDB, stores };
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

function load(chrome, idb = fakeIndexedDB()) {
  const ctx = vm.createContext({
    chrome, indexedDB: idb.indexedDB, console: { log() {}, warn() {}, error() {} }, setTimeout, clearTimeout,
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

// 3. The phrase is kept where content scripts cannot read it.
{
  const PHRASE = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
  const { chrome, calls, store } = makeChrome({ withMenus: true });
  const idb = fakeIndexedDB();
  const { ctx, error } = load(chrome, idb);
  if (error) fail(error);
  else {
    const run = (name) => vm.runInContext(name, ctx);
    await run('writePhrase')(` ${PHRASE.toUpperCase()} `);
    if ('phrase' in store) fail('writePhrase() puts the phrase into storage.local, which every content script can read');
    if ((await run('readPhrase')()) !== PHRASE) fail('readPhrase() does not return the phrase writePhrase() stored');

    await run('forgetGroup')();
    if ((await run('readPhrase')()) !== '') fail('forgetGroup() leaves the phrase readable');
    if (!calls.removed.includes('defaultInstance')) fail("forgetGroup() does not remove 'defaultInstance'");

    store.phrase = PHRASE;
    if ((await run('readPhrase')()) !== PHRASE) fail('readPhrase() loses a phrase found in storage.local');
    if ('phrase' in store) fail('readPhrase() leaves a phrase found in storage.local where content scripts can read it');
    if (idb.stores.get('secrets')?.get('phrase') !== PHRASE) fail('readPhrase() does not move a phrase found in storage.local into IndexedDB');

    const ids = [run('sessionInstanceId')(), run('sessionInstanceId')()];
    if (!ids.every((id) => /^[0-9a-f]{40}$/.test(id))) fail(`a relay session joins under ${JSON.stringify(ids)}, want 40 hex characters`);
    if (ids[0] === ids[1]) fail('two relay sessions join under the same id, so the relay drops the first one');

    // The group knows the browser by one member id, or every session leaves a
    // card of its own and removing the browser never reaches it.
    const members = await Promise.all([run('readMemberId')(), run('readMemberId')()]);
    if (!/^[0-9a-f]{40}$/.test(members[0])) fail(`the browser's member id is ${JSON.stringify(members[0])}, want 40 hex characters`);
    if (members[0] !== members[1]) fail('two sessions starting together get different member ids');
    if ((await run('readMemberId')()) !== members[0]) fail('the member id changes from one session to the next');
    if ('member' in store) fail('the member id is kept in storage.local, which every content script can read');
    await run('forgetGroup')();
    if ((await run('readMemberId')()) === members[0]) fail('a browser that left the group comes back under its old member id');

    await run('writePhrase')(PHRASE);
    const opened = [];
    ctx.record = (o) => opened.push(o);
    vm.runInContext('relaySession = (opts, work) => { record(opts); return work({ siblings: [], call: async () => null }); }', ctx);
    await run('groupInstances')();
    await run('groupInstances')();
    if (opened.length !== 2 || !opened[0].memberId || opened[0].memberId !== opened[1].memberId) {
      fail(`two sessions announce the member ids ${JSON.stringify(opened.map((o) => o.memberId))}, want one lasting id`);
    } else if (opened[0].selfId === opened[0].memberId) {
      fail('a session routes under the member id, so two at once knock each other off the relay');
    }

    let answered = false;
    const message = { type: 'knightloader-send-to', target: 'a'.repeat(40), payload: { url: 'https://files.example/a' } };
    for (const f of chrome.runtime.onMessage.listeners) f(message, { url: 'https://evil.example/' }, () => { answered = true; });
    if (answered) fail('a content script on a website can pick the target of a send');
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
console.log('ok: background survives without context menus, the jdcheck ruleset follows the scripts and every update and start re-applies it, the phrase stays out of storage content scripts read, every session joins under its own id and one lasting member id, Click\'n\'Load follows the site access');
