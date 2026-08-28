// The service worker: builds the four context-menu entries and does the one
// thing every entrance in this extension needs — open a small window at this
// instance's own /quickadd, same-origin, so the normal session cookie carries
// it and nothing here ever has to hold a copy of the instance's password.
// See internal/cnl/cnl.go's own withCORS comment for why that door is closed
// on the server side rather than worked around here with a stripped Origin
// header: this extension talks to KnightLoader exactly the way a person with
// a browser tab open to it would, never around that boundary.
// Chrome runs this file as a service worker, where importScripts is how a
// worker pulls in its dependencies. Firefox ignores background.service_worker
// entirely (web-ext lint says so out loud: BACKGROUND_SERVICE_WORKER_IGNORED)
// and runs background.scripts as an EVENT PAGE instead - a document-like
// context, where importScripts does not exist at all.
//
// Unguarded, that is a ReferenceError on the first line that runs, which kills
// the background script before a single context menu is built: the extension
// installs, shows up in the list, and does nothing whatsoever in Firefox. The
// manifest lists shared.js and i18n.js in background.scripts for exactly this
// reason, so Firefox has already loaded them by the time this line is reached
// and there is nothing left to import.
if (typeof importScripts === 'function') {
  importScripts('shared.js', 'i18n.js', 'cnl.js');
}

const MENU_PAGE = 'knightloader-send-page';
const MENU_LINK = 'knightloader-send-link';
const MENU_IMAGE = 'knightloader-send-image';
const MENU_SELECTION = 'knightloader-send-selection';

/** menuTitles() reads the fresh translation for every context-menu entry — called on install and whenever the language changes. */
function menuTitles() {
  return {
    [MENU_PAGE]: t('menu.page'),
    [MENU_LINK]: t('menu.link'),
    [MENU_IMAGE]: t('menu.image'),
    [MENU_SELECTION]: t('menu.selection'),
  };
}

chrome.runtime.onInstalled.addListener(async (details) => {
  await loadLanguage();
  const titles = menuTitles();
  chrome.contextMenus.create({
    id: MENU_PAGE,
    title: titles[MENU_PAGE],
    contexts: ['page'],
  });
  chrome.contextMenus.create({
    id: MENU_LINK,
    title: titles[MENU_LINK],
    contexts: ['link'],
  });
  chrome.contextMenus.create({
    id: MENU_IMAGE,
    title: titles[MENU_IMAGE],
    // A separate entry from MENU_LINK: Chrome shows both 'link' and 'image'
    // together when an image is itself wrapped in an <a>, and the two
    // usually point at different URLs (a thumbnail's link vs. its full-size
    // src) — collapsing them into one entry would leave no way to choose.
    contexts: ['image'],
  });
  chrome.contextMenus.create({
    id: MENU_SELECTION,
    title: titles[MENU_SELECTION],
    contexts: ['selection'],
  });

  // First install only: a reinstall or an update must never overwrite an
  // instance list somebody already configured, and an unconfigured install
  // has nothing to clobber, so this is a no-op the second time either way —
  // the guard is about not undoing a later Options edit on an update, not
  // about idempotency. readInstances() itself folds an old single-URL config
  // (from before multi-instance support) into a one-entry list, so that path
  // is covered here too, not just on the next popup/options open.
  if (details.reason === 'install') {
    const { instances } = await readInstances();
    if (instances.length === 0) {
      const baked = await readInstanceUrl();
      if (baked) await writeInstances([{ name: 'Default', url: baked }], 'Default');
      else chrome.runtime.openOptionsPage();
    }
  }
});

// The context-menu titles are set once at creation time — Chrome has no
// "re-read this on every open" hook — so a language change made in Options
// (this service worker may be asleep at that moment) needs its own nudge:
// wake up on the storage write and push updated titles onto the existing
// menu items via chrome.contextMenus.update rather than recreating them.
chrome.storage.onChanged.addListener(async (changes, area) => {
  if (area !== 'local' || !changes.language) return;
  await loadLanguage();
  const titles = menuTitles();
  for (const [id, title] of Object.entries(titles)) {
    chrome.contextMenus.update(id, { title });
  }
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  const payload =
    info.menuItemId === MENU_LINK
      ? { url: info.linkUrl, title: tab?.title }
      : info.menuItemId === MENU_IMAGE
        ? { url: info.srcUrl, title: tab?.title }
        : info.menuItemId === MENU_SELECTION
          ? { text: info.selectionText, title: tab?.title }
          : { url: info.pageUrl || tab?.url, title: tab?.title };
  await sendToInstance(payload);
});

/**
 * sendToInstance is also called from popup.js (imported there via <script>,
 * not importScripts — see popup.html), so the toolbar button's "send this
 * page" action and every context-menu entry go through the identical choice.
 *
 * One configured instance sends straight through, same as before multi-
 * instance support existed — jdp: "es soll einfach immer zuverlässig
 * funktionieren ohne das man manuell was machen muss", and a picker nobody
 * needs is exactly the manual step that breaks that. More than one instance
 * opens the picker (jdp, 2026-08-23: "wenn man auf einen click n load
 * button klcikt soll die erweiterung aufploppen wie die von JD") so there is
 * always a real choice, defaulted to whichever instance is marked default.
 */
async function sendToInstance(payload) {
  const { instances, defaultName } = await readInstances();
  if (instances.length === 0) {
    chrome.runtime.openOptionsPage();
    return;
  }
  if (instances.length === 1) {
    const only = entryTarget(instances[0]);
    // An entry with no address and no forwarder is one nothing can be sent
    // to. Options is where that is fixed, and it is where the reason is
    // shown - better than a window that opens on nothing.
    if (!only) {
      chrome.runtime.openOptionsPage();
      return;
    }
    openQuickAdd(only.origin, { ...payload, to: only.to });
    return;
  }
  await chrome.storage.session.set({ pendingSend: { payload, defaultName } });
  chrome.windows.create({ url: chrome.runtime.getURL('picker.html'), type: 'popup', width: 400, height: 480 });
}

/**
 * openQuickAdd is exported for picker.js to call once a target is chosen —
 * that page runs as a normal extension page (a <script> tag, like popup.js),
 * not a service worker, so it reaches this through chrome.runtime.sendMessage
 * rather than a direct call.
 */
function openQuickAdd(origin, payload) {
  const url = quickAddUrl(origin, payload);
  // A small popup window, not a background tab: the confirmation
  // (web/src/pages/QuickAdd.tsx) is the only thing anybody needs to see, and
  // a background tab is a confirmation nobody looks at until they go hunting
  // for it.
  chrome.windows.create({ url, type: 'popup', width: 420, height: 560 });
}

chrome.runtime.onMessage.addListener((msg) => {
  if (msg?.type === 'knightloader-send-to' && msg.origin && msg.payload) {
    openQuickAdd(msg.origin, msg.payload);
  }
  if (msg?.type === 'knightloader-cnl') {
    void handleCnl(msg);
  }
  // The options page cannot register content scripts itself in a way that
  // survives it being closed, so it asks here once the permission is granted.
  if (msg?.type === 'knightloader-cnl-scripts') {
    void syncCnlScripts(msg.on === true);
  }
});

/**
 * One Click'n'Load submission, caught in the page by cnl-main.js and relayed
 * here by cnl-relay.js.
 *
 * The decoded links go through sendToInstance() — the same function the
 * toolbar button and every context-menu entry use — so a CnL button behaves
 * exactly like every other send: straight through when one instance is
 * configured, and the picker when there are several. That is what jdp asked
 * for twice, on 2026-08-23 ("wenn man auf einen click n load button klickt
 * soll die erweiterung aufploppen wie die von JD") and again on 2026-08-28
 * ("das CnL immer an die erweiterung gehen und die verteilt es dann"), and it
 * costs nothing to honour because the machinery was already there.
 *
 * Off unless switched on: interception changes what a page's own button does,
 * and that is not a thing to start doing to somebody without asking. See
 * options.js.
 */
async function handleCnl(msg) {
  const { cnlEnabled } = await chrome.storage.local.get('cnlEnabled');
  if (cnlEnabled !== true) return;

  const f = msg.fields || {};
  let links = [];
  try {
    if (f.crypted && f.jk) {
      links = await cnlDecrypt(f.jk, f.crypted);
    } else if (f.urls) {
      // The plain variant, /flash/add: older or simpler sites post an
      // unencrypted list.
      links = splitCnlLinks(f.urls);
    } else if (f.crypted) {
      // addcrypted v1: encrypted against JDownloader's own RSA key, which
      // nobody else holds and KnightLoader deliberately never will (see
      // docs/clicknload.md). Nothing to decode, and pretending otherwise
      // would drop the links silently.
      notifyCnl('cnl.containerUnsupported');
      return;
    }
  } catch (e) {
    notifyCnl('cnl.decodeFailed');
    return;
  }
  if (links.length === 0) return;

  // The site's own `source`/`package` field names the batch when it sends one;
  // the page title is the fallback, and it is a better package name than the
  // first link's filename, which is what the collector would fall back to.
  const title = f.package || f.source || msg.pageTitle || '';
  await sendToInstance({ text: links.join('\n'), title });
}

/**
 * The two content scripts that catch a Click'n'Load submission, registered at
 * runtime instead of declared in the manifest.
 *
 * This is the whole reason the extension can offer Click'n'Load without asking
 * every installer for access to every website. Static content_scripts matching
 * <all_urls> produce that permission warning at INSTALL time, for everybody,
 * whether or not they ever want the feature. Registered from here they exist
 * only once somebody switches it on and grants the permission themselves — and
 * unregistering takes it away again.
 *
 * MAIN world for the interceptor, isolated for the relay: see cnl-main.js. The
 * pair has to be registered together or neither half does anything.
 */
const CNL_SCRIPTS = [
  {
    id: 'cnl-main',
    matches: ['<all_urls>'],
    js: ['cnl-main.js'],
    runAt: 'document_start',
    allFrames: true,
    world: 'MAIN',
    persistAcrossSessions: true,
  },
  {
    id: 'cnl-relay',
    matches: ['<all_urls>'],
    js: ['cnl-relay.js'],
    runAt: 'document_start',
    allFrames: true,
    persistAcrossSessions: true,
  },
];

/** Registers or removes the interception scripts to match the stored flag.
 *  Idempotent: registering an id that already exists throws, so the existing
 *  set is read first. */
async function syncCnlScripts(on) {
  try {
    const have = await chrome.scripting.getRegisteredContentScripts();
    const ids = have.filter((s) => s.id.startsWith('cnl-')).map((s) => s.id);
    if (on) {
      const missing = CNL_SCRIPTS.filter((s) => !ids.includes(s.id));
      if (missing.length) await chrome.scripting.registerContentScripts(missing);
    } else if (ids.length) {
      await chrome.scripting.unregisterContentScripts({ ids });
    }
  } catch (e) {
    // Without the host permission this throws, which is the correct outcome:
    // the feature stays off rather than half-on. options.js asks for the
    // permission before it ever gets here.
  }
}

// On every startup, not only when a submission arrives. persistAcrossSessions
// already survives a restart, but a permission revoked from the browser's own
// settings page does not tell this extension about it - reasserting is what
// keeps the switch and the reality in step.
chrome.runtime.onStartup?.addListener(() => {
  void chrome.storage.local.get('cnlEnabled').then(({ cnlEnabled }) => syncCnlScripts(cnlEnabled === true));
});

/**
 * A mark on the toolbar icon for the two cases that end with nothing arriving,
 * so a click that looked like it worked does not just vanish.
 *
 * The badge and not a notification: `notifications` would be a fourth
 * permission at install time, asked for on every install so that two rare
 * failures can announce themselves. The badge costs nothing, the tooltip
 * carries the actual sentence, and both clear themselves.
 */
function notifyCnl(key) {
  try {
    chrome.action.setBadgeText({ text: '!' });
    chrome.action.setBadgeBackgroundColor({ color: '#da1e28' });
    chrome.action.setTitle({ title: `KnightLoader — ${t(key)}` });
    // An empty title is not a blank tooltip: it puts the manifest's own
    // default_title back, which is the one string that must not be duplicated
    // into the locale catalogues to be restored.
    setTimeout(() => {
      chrome.action.setBadgeText({ text: '' });
      chrome.action.setTitle({ title: '' });
    }, 8000);
  } catch {
    // An action API that is not there yet during startup. The submission is
    // already lost; losing the badge with it changes nothing.
  }
}
