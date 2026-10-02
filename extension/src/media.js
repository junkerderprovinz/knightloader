// Finds the video and audio a page loads, for the popup's list (popup-media.js).
// It watches responses rather than the page, so it sees a stream whatever
// player asked for it, and keeps what it found per tab in session storage,
// since this worker sleeps between events.
//
// Loaded by background.js. webRequest comes with an optional permission, so
// it is wired only once the browser has granted it.

let mediaWired = false;

function wireMedia() {
  if (mediaWired || !chrome.webRequest) return;
  mediaWired = true;
  chrome.webRequest.onResponseStarted.addListener(
    onMediaResponse,
    { urls: ['<all_urls>'], types: ['main_frame', 'media', 'xmlhttprequest', 'other'] },
    ['responseHeaders'],
  );
}

/**
 * One response, of which almost none are media, so everything before the
 * storage write is synchronous. A page loaded in a tab starts its list afresh.
 */
function onMediaResponse(details) {
  if (details.tabId < 0) return;
  if (details.type === 'main_frame') {
    void updateMedia(details.tabId, () => ({ page: details.url, items: [] }));
    return;
  }
  const contentType = (details.responseHeaders ?? []).find((h) => h.name.toLowerCase() === 'content-type')?.value;
  const kind = mediaKind(details.url, contentType, details.type);
  if (!kind) return;
  const length = Number((details.responseHeaders ?? []).find((h) => h.name.toLowerCase() === 'content-length')?.value);
  const entry = { url: details.url, kind, size: kind === 'video' || kind === 'audio' ? length || 0 : 0 };
  void updateMedia(details.tabId, (list) => withMedia(list, entry));
}

/** Writes to one tab's list one change after another, so two responses
 *  arriving together do not overwrite each other. */
let mediaQueue = Promise.resolve();
function updateMedia(tabId, change) {
  mediaQueue = mediaQueue
    .catch(() => {})
    .then(async () => {
      if (!(await mediaState()).on) return;
      const key = mediaKey(tabId);
      const stored = (await chrome.storage.session.get(key))[key];
      const next = change(stored);
      if (next !== stored) await chrome.storage.session.set({ [key]: next });
    });
  return mediaQueue;
}

/**
 * Sends one stream the popup picked, with the page it played on. The page is
 * also the Referer, which a stream's server often checks, and the cookies are
 * the ones for the stream's own address. A private window's cookies stay
 * where they are.
 */
async function sendMedia(msg) {
  const headers = handedHeaders({
    cookie: msg.incognito ? '' : await cookiesFor(msg.url, msg.cookieStoreId),
    referrer: msg.page,
    userAgent: navigator.userAgent,
  });
  const inst = await handLinkOver({ url: msg.url, source: msg.page, title: msg.title, headers, target: msg.target }).catch(
    () => null,
  );
  if (inst) flashBadge('✓', '#24a148', 'send.delivered');
  else flashBadge('!', '#da1e28', 'send.refused');
}

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg?.type !== 'knightloader-send-media') return;
  // Only the popup picks a stream; a content script runs inside any site.
  if (!String(sender?.url ?? '').startsWith(chrome.runtime.getURL('')) || !/^https?:\/\//i.test(msg.url ?? '')) return;
  void sendMedia(msg);
  sendResponse({ accepted: true });
});

// A closed tab's list goes with it.
chrome.tabs?.onRemoved.addListener((tabId) => void chrome.storage.session.remove(mediaKey(tabId)));

chrome.runtime.onInstalled.addListener(() => wireMedia());
chrome.runtime.onStartup?.addListener(() => wireMedia());
chrome.permissions.onAdded.addListener(() => wireMedia());
chrome.storage.onChanged.addListener((changes, area) => {
  // Switched off, the lists go too.
  if (area !== 'local' || !changes.mediaEnabled || changes.mediaEnabled.newValue) return;
  void chrome.storage.session.get(null).then((all) => {
    const keys = Object.keys(all).filter((k) => k.startsWith('media:'));
    if (keys.length) return chrome.storage.session.remove(keys);
  });
});
wireMedia();
