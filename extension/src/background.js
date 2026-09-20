// The service worker: builds the context-menu entries and puts what the user
// picked into one of the instances in the group, through the relay (group.js,
// relay.js). Membership is the credential, so the API's same-origin guard is
// never worked around.
//
// Chrome runs this as a service worker and needs importScripts. Firefox ignores
// background.service_worker and runs background.scripts as an event page,
// where importScripts does not exist; the manifest lists the dependencies there
// instead.
if (typeof importScripts === 'function') {
  importScripts('shared.js', 'i18n.js', 'wordlist.js', 'phrase.js', 'relay.js', 'group.js', 'cnl.js');
}

const MENU_PAGE = 'knightloader-send-page';
const MENU_LINK = 'knightloader-send-link';
const MENU_IMAGE = 'knightloader-send-image';
const MENU_SELECTION = 'knightloader-send-selection';

/** menuTitles returns the current translation of every context-menu entry. */
function menuTitles() {
  return {
    [MENU_PAGE]: t('menu.page'),
    [MENU_LINK]: t('menu.link'),
    [MENU_IMAGE]: t('menu.image'),
    [MENU_SELECTION]: t('menu.selection'),
  };
}

// Every use of chrome.contextMenus is guarded. Firefox for Android has no such
// API, and an unguarded call at top level would stop the file before the
// message listener is registered. check-background.mjs loads it without one.
chrome.runtime.onInstalled.addListener(async (details) => {
  await loadLanguage();
  if (chrome.contextMenus) {
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
      // Separate from MENU_LINK: a linked image shows both entries, and the
      // link and the image source usually differ.
      contexts: ['image'],
    });
    chrome.contextMenus.create({
      id: MENU_SELECTION,
      title: titles[MENU_SELECTION],
      contexts: ['selection'],
    });
  }

  // Click'n'Load is on from a fresh install. An update never switches it back
  // on for someone who turned it off.
  if (details.reason === 'install') {
    await chrome.storage.local.set({ cnlEnabled: true });
  }
  await syncCnlScripts((await chrome.storage.local.get('cnlEnabled')).cnlEnabled !== false);

  // A fresh install opens the options page when there is no phrase yet, or
  // when Chromium has hidden the new button behind the puzzle piece, which it
  // does by default. An extension cannot pin itself, so the page explains where
  // it went.
  const hidden = await notOnToolbar();
  if (details.reason === 'install' && (!(await readPhrase()) || hidden)) {
    if (hidden) await chrome.storage.local.set({ showPinHint: true });
    chrome.runtime.openOptionsPage();
  }
});

/**
 * Whether the toolbar button is hidden behind the puzzle piece. False wherever
 * the browser cannot answer, since the hint only applies to Chromium.
 */
async function notOnToolbar() {
  try {
    if (!chrome.action?.getUserSettings) return false;
    const s = await chrome.action.getUserSettings();
    return s?.isOnToolbar === false;
  } catch {
    return false;
  }
}

// Menu titles are fixed at creation, so a language change from the options
// page updates the existing entries.
chrome.storage.onChanged.addListener(async (changes, area) => {
  if (area !== 'local' || !changes.language || !chrome.contextMenus) return;
  await loadLanguage();
  const titles = menuTitles();
  for (const [id, title] of Object.entries(titles)) {
    chrome.contextMenus.update(id, { title });
  }
});

// `kind` is for the popup's button label when the send is parked (sendLabelKey
// in shared.js); deliver() reads only url, text and title.
chrome.contextMenus?.onClicked.addListener(async (info, tab) => {
  const payload =
    info.menuItemId === MENU_LINK
      ? { kind: 'link', url: info.linkUrl, title: tab?.title }
      : info.menuItemId === MENU_IMAGE
        ? { kind: 'image', url: info.srcUrl, title: tab?.title }
        : info.menuItemId === MENU_SELECTION
          ? { kind: 'selection', text: info.selectionText, title: tab?.title }
          : { kind: 'page', url: info.pageUrl || tab?.url, title: tab?.title };
  await sendToInstance(payload);
});

/**
 * sendToInstance sends a payload to the group. With one instance it goes
 * straight through; with more, the popup opens preset to the default. The group
 * is read live from the relay, so only instances that are online are offered.
 */
async function sendToInstance(payload, origin) {
  let siblings;
  try {
    siblings = await groupInstances();
  } catch (e) {
    // Without a phrase the popup explains and offers the options page, rather
    // than a download button opening a settings tab.
    if (e?.code === 'no-phrase') {
      await showPopupOrMark();
      return;
    }
    notifyCnl('send.relayFailed');
    return;
  }
  if (siblings.length === 0) {
    notifyCnl('send.noneOnline');
    return;
  }
  // A Click'n'Load batch always goes through the popup, even with one
  // instance, because the page acted rather than the user and the countdown
  // and its cancel are there to catch it.
  if (siblings.length === 1 && origin !== 'cnl') {
    await deliver(siblings[0].instanceId, payload);
    return;
  }
  await chrome.storage.session.set({
    // The popup counts down only for a Click'n'Load batch.
    pendingSend: { payload, defaultName: await readDefaultTarget(), siblings, origin: origin ?? '' },
  });

  // Always the extension's own popup, never a window of its own. If the popup
  // cannot be opened the send stays parked and the badge says so; opening the
  // popup by hand shows it.
  await showPopupOrMark();
}

/**
 * Opens the extension's popup, or marks the toolbar icon when the browser will
 * not open it.
 */
async function showPopupOrMark() {
  try {
    await chrome.action.openPopup();
  } catch {
    flashBadge('…', '#f1c21b', 'send.waiting', true);
  }
}

/**
 * deliver puts one payload into one instance through the relay. `text` carries
 * a whole batch; the server pulls every URL out of it, so a Click'n'Load batch
 * and a single link take the same path. `origin: 'cnl'` tells the collector it
 * came from the browser rather than the paste box.
 */
async function deliver(target, payload) {
  const links = [payload.url, payload.text].filter(Boolean).join('\n\n');
  if (!links) return;
  try {
    const res = await withGroup(({ call }) =>
      call(
        target,
        'POST',
        '/api/links',
        JSON.stringify({
          links,
          package: payload.title || '',
          origin: 'cnl',
        }),
      ),
    );
    if (res.status >= 200 && res.status < 300) {
      flashBadge('✓', '#24a148', 'send.delivered');
    } else {
      notifyCnl('send.refused');
    }
  } catch {
    notifyCnl('send.relayFailed');
  }
}

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg?.type === 'knightloader-send-to' && msg.target && msg.payload) {
    void deliver(msg.target, msg.payload);
    // Answered before the delivery: the popup only waits for the hand-over
    // (handOver in popup.js), and the badge reports the rest.
    sendResponse({ accepted: true });
  }
  if (msg?.type === 'knightloader-cnl') {
    void handleCnl(msg);
  }
  // The options page cannot register content scripts that outlive it, so it
  // asks here once the permission is granted.
  if (msg?.type === 'knightloader-cnl-scripts') {
    void syncCnlScripts(msg.on === true);
  }
});

/**
 * One Click'n'Load submission, caught by cnl-main.js and relayed by
 * cnl-relay.js. The decoded links go through sendToInstance like every other
 * send.
 */
async function handleCnl(msg) {
  const { cnlEnabled } = await chrome.storage.local.get('cnlEnabled');
  // Absent means on; install writes the flag, and a lost value must not turn
  // the main feature off.
  if (cnlEnabled === false) return;

  const f = msg.fields || {};
  let links = [];
  try {
    if (f.crypted && f.jk) {
      links = await cnlDecrypt(f.jk, f.crypted);
    } else if (f.urls) {
      // /flash/add posts an unencrypted list.
      links = splitCnlLinks(f.urls);
    } else if (f.crypted) {
      // addcrypted v1 is encrypted to JDownloader's own RSA key, which only
      // JDownloader holds (docs/clicknload.md).
      notifyCnl('cnl.containerUnsupported');
      return;
    }
  } catch (e) {
    notifyCnl('cnl.decodeFailed');
    return;
  }
  if (links.length === 0) return;

  // The site's `package` or `source` field names the batch; the page title
  // beats the first link's file name as a fallback.
  const title = f.package || f.source || msg.pageTitle || '';
  await sendToInstance({ text: links.join('\n'), title }, 'cnl');
}

/**
 * The two content scripts that catch a Click'n'Load submission: cnl-main.js in
 * the page's main world and cnl-relay.js in the isolated world, which only work
 * as a pair.
 *
 * matchOriginAsFallback reaches about:blank, data: and blob: documents, which
 * no URL pattern matches. Sites such as filecrypt open a blank window and write
 * their Click'n'Load form into it.
 */
const CNL_SCRIPTS = [
  {
    id: 'cnl-main',
    matches: ['<all_urls>'],
    js: ['cnl-main.js'],
    runAt: 'document_start',
    allFrames: true,
    matchOriginAsFallback: true,
    world: 'MAIN',
    persistAcrossSessions: true,
  },
  {
    id: 'cnl-relay',
    matches: ['<all_urls>'],
    js: ['cnl-relay.js'],
    runAt: 'document_start',
    allFrames: true,
    matchOriginAsFallback: true,
    persistAcrossSessions: true,
  },
];

/**
 * Whether a registration the browser holds matches the current definition.
 * Persisted registrations outlive the code that made them, so matching ids is
 * not enough. Only the fields that decide behaviour are compared.
 */
function cnlScriptMatches(have, want) {
  return (
    (have.matches || []).join('|') === want.matches.join('|') &&
    (have.js || []).join('|') === want.js.join('|') &&
    have.runAt === want.runAt &&
    !!have.allFrames === !!want.allFrames &&
    !!have.matchOriginAsFallback === !!want.matchOriginAsFallback &&
    (have.world || 'ISOLATED') === (want.world || 'ISOLATED')
  );
}

/**
 * Registers, updates or removes the interception scripts to match the flag.
 * It reconciles rather than only filling gaps, because a persisted
 * registration keeps its old definition after an update until something
 * rewrites it.
 */
async function syncCnlScripts(on) {
  // The jdcheck.js redirect (cnl-rules.json) follows the same switch. It sits
  // in its own try so a scripting failure cannot skip it, and it is written on
  // every sync because an update resets a static ruleset's state.
  try {
    await chrome.declarativeNetRequest.updateEnabledRulesets(on ? { enableRulesetIds: ['cnl'] } : { disableRulesetIds: ['cnl'] });
  } catch (e) {
    console.warn('[KnightLoader] Click’n’Load redirect rule not switched:', e);
  }
  try {
    const have = await chrome.scripting.getRegisteredContentScripts();
    const mine = have.filter((s) => s.id.startsWith('cnl-'));
    if (!on) {
      if (mine.length) await chrome.scripting.unregisterContentScripts({ ids: mine.map((s) => s.id) });
      return;
    }
    const stale = mine.filter((s) => {
      const want = CNL_SCRIPTS.find((w) => w.id === s.id);
      // An id this version no longer defines is stale too.
      return !want || !cnlScriptMatches(s, want);
    });
    if (stale.length) await chrome.scripting.unregisterContentScripts({ ids: stale.map((s) => s.id) });
    const kept = new Set(mine.filter((s) => !stale.includes(s)).map((s) => s.id));
    const missing = CNL_SCRIPTS.filter((s) => !kept.has(s.id));
    if (missing.length) await chrome.scripting.registerContentScripts(missing);
  } catch (e) {
    // Without the host permission this throws and the feature stays off
    // rather than half on. Logged so a failed registration is visible.
    console.warn('[KnightLoader] Click’n’Load scripts not registered:', e);
  }
}

// Reapplied on every start, since a permission revoked in the browser's
// settings sends no event. `!== false` because nothing stored means on, as on
// install and in the options page.
chrome.runtime.onStartup?.addListener(() => {
  void chrome.storage.local.get('cnlEnabled').then(({ cnlEnabled }) => syncCnlScripts(cnlEnabled !== false));
});

/** Marks the toolbar icon when a send ends with nothing arriving. */
function notifyCnl(key) {
  flashBadge('!', '#da1e28', key);
}

/**
 * flashBadge is the extension's only feedback channel: a tick when a send
 * arrived, an exclamation mark when it did not. A badge needs no
 * `notifications` permission, and the tooltip carries the sentence.
 */
function flashBadge(mark, colour, key, sticky) {
  try {
    chrome.action.setBadgeText({ text: mark });
    chrome.action.setBadgeBackgroundColor({ color: colour });
    chrome.action.setTitle({ title: `KnightLoader: ${t(key)}` });
    // A sticky mark stands for a parked send and stays until the popup takes
    // it.
    if (sticky) return;
    // An empty title restores the manifest's default_title.
    setTimeout(() => {
      chrome.action.setBadgeText({ text: '' });
      chrome.action.setTitle({ title: '' });
    }, 6000);
  } catch {
    // The action API can be missing during startup; the send itself is done.
  }
}
