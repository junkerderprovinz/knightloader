// The service worker: builds the four context-menu entries and does the one
// thing every entrance in this extension needs — open a small window at this
// instance's own /quickadd, same-origin, so the normal session cookie carries
// it and nothing here ever has to hold a copy of the instance's password.
// See internal/cnl/cnl.go's own withCORS comment for why that door is closed
// on the server side rather than worked around here with a stripped Origin
// header: this extension talks to KnightLoader exactly the way a person with
// a browser tab open to it would, never around that boundary.
importScripts('shared.js');

const MENU_PAGE = 'knightloader-send-page';
const MENU_LINK = 'knightloader-send-link';
const MENU_IMAGE = 'knightloader-send-image';
const MENU_SELECTION = 'knightloader-send-selection';

chrome.runtime.onInstalled.addListener(async (details) => {
  chrome.contextMenus.create({
    id: MENU_PAGE,
    title: 'Send page to KnightLoader',
    contexts: ['page'],
  });
  chrome.contextMenus.create({
    id: MENU_LINK,
    title: 'Send link to KnightLoader',
    contexts: ['link'],
  });
  chrome.contextMenus.create({
    id: MENU_IMAGE,
    title: 'Send image to KnightLoader',
    // A separate entry from MENU_LINK: Chrome shows both 'link' and 'image'
    // together when an image is itself wrapped in an <a>, and the two
    // usually point at different URLs (a thumbnail's link vs. its full-size
    // src) — collapsing them into one entry would leave no way to choose.
    contexts: ['image'],
  });
  chrome.contextMenus.create({
    id: MENU_SELECTION,
    title: 'Send selection to KnightLoader',
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
    openQuickAdd(instances[0].url, payload);
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
});
