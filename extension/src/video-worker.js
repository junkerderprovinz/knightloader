// The service worker's half of the download button on a video's player
// (video.js): it registers the button on the sites it runs on, brings it into
// tabs that were open before, and sends the video a button asks for to the
// default instance, the one a send goes to without asking, since the button is
// one click.
//
// Loaded by background.js.

const VIDEO_SCRIPT = {
  id: 'video-button',
  // appearance.js gives the button the look the extension's own pages have.
  js: ['appearance.js', 'video.js', 'video-button.js'],
  runAt: 'document_idle',
  // Embedded players are frames of their own site inside another page.
  allFrames: true,
  persistAcrossSessions: true,
};

/**
 * Registers the button where it runs and nowhere else. A tab keeps a script
 * it already has when the button goes off; the script watches the switch and
 * takes the button away itself (video-button.js).
 */
async function syncVideoScript() {
  const { running } = await videoButtonState();
  const matches = videoAccess(running).origins;
  try {
    const have = (await chrome.scripting.getRegisteredContentScripts()).find((s) => s.id === VIDEO_SCRIPT.id);
    if (have && (have.matches ?? []).join('|') === matches.join('|') && (have.js ?? []).join('|') === VIDEO_SCRIPT.js.join('|')) return;
    if (have) await chrome.scripting.unregisterContentScripts({ ids: [VIDEO_SCRIPT.id] });
    if (matches.length === 0) return;
    await chrome.scripting.registerContentScripts([{ ...VIDEO_SCRIPT, matches }]);
    await injectVideoButton(matches);
  } catch (e) {
    console.warn('[KnightLoader] video button not registered:', e);
  }
}

/**
 * A registered script only reaches pages loaded after it, so the tabs already
 * showing one of the sites get it too, and switching the button on from the
 * popup shows it on the video playing behind. A frame that has it from an
 * earlier round is left alone, since loading it twice would declare its
 * constants twice.
 */
async function injectVideoButton(matches) {
  if (!chrome.tabs?.query) return;
  const tabs = await chrome.tabs.query({ url: matches }).catch(() => []);
  for (const tab of tabs) {
    try {
      const probes = await chrome.scripting.executeScript({
        target: { tabId: tab.id, allFrames: true },
        func: () => typeof videoPage === 'function',
      });
      const frameIds = probes.filter((p) => p.result === false).map((p) => p.frameId);
      if (frameIds.length) await chrome.scripting.executeScript({ target: { tabId: tab.id, frameIds }, files: VIDEO_SCRIPT.js });
    } catch {
      // A tab that is loading or was closed meanwhile gets the registered
      // script when it shows a page.
    }
  }
}

let videoQueue = Promise.resolve();
function applyVideoButton() {
  videoQueue = videoQueue.catch(() => {}).then(syncVideoScript);
  return videoQueue;
}

/** Everything the button says, in the extension's language, since the page
 *  it sits on has no catalogue. */
function videoLabels() {
  return {
    send: t('video.send'),
    sendTip: t('video.sendTip'),
    more: t('video.more'),
    best: t('video.best'),
    audio: t('video.audio'),
    playlist: t('video.playlist'),
    hide: t('video.hide'),
    sending: t('video.sending'),
    sent: t('video.sent'),
    already: t('video.already'),
    held: t('video.held'),
    refused: t('video.refused'),
    offline: t('video.offline'),
    unpaired: t('video.unpaired'),
    failed: t('video.failed'),
    dir: LANGUAGES.find((l) => l.code === currentLanguage())?.rtl ? 'rtl' : 'ltr',
    lang: currentLanguage(),
  };
}

/**
 * Sends the video of the page a button sits on. The address is worked out
 * here again from the page's own, on the site the message came from, so a
 * button can only ever send the video it is showing. Answers what became of
 * it, for the button to show.
 */
async function sendVideo(frameUrl, msg) {
  const page = videoPage(msg.href);
  if (!page || page.site !== videoSiteOf(frameUrl)?.id) return { result: 'refused' };
  if (!(await videoButtonState()).running.includes(page.site)) return { result: 'refused' };
  const link = msg.playlist && page.playlist ? page.playlist : page.url;
  const format = VIDEO_FORMATS.includes(msg.format) ? msg.format : '';
  try {
    return await withGroup(async ({ siblings, call }) => {
      const target = defaultOf(siblings, await readDefaultTarget());
      if (!target) return { result: 'offline' };
      const res = await call(
        target,
        'POST',
        '/api/links',
        JSON.stringify({ links: link, package: '', origin: 'cnl', source: page.url, ...(format ? { format } : {}) }),
      );
      if (res.status < 200 || res.status >= 300) return { result: 'refused', reason: t(refusalKey(res)) };
      let created;
      try {
        created = JSON.parse(res.body);
      } catch {
        return { result: 'refused' };
      }
      if (!Array.isArray(created) || created.length === 0) return { result: 'already' };
      // A skipped task is one the instance held back, as already downloaded
      // or by its filter.
      return { result: created.some((task) => task && !task.skipped) ? 'sent' : 'held' };
    });
  } catch (e) {
    return { result: e?.code === 'no-phrase' ? 'unpaired' : 'failed' };
  }
}

/** Hides the button on the site a page belongs to, from the button's menu. */
async function hideVideoSite(frameUrl) {
  const site = videoSiteOf(frameUrl);
  const { videoButtonHidden } = await chrome.storage.local.get('videoButtonHidden');
  const hidden = Array.isArray(videoButtonHidden) ? videoButtonHidden : [];
  if (!hidden.includes(site.id)) await chrome.storage.local.set({ videoButtonHidden: [...hidden, site.id] });
}

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  // Only from a content script on one of the sites, which is where the button
  // runs; the page itself cannot reach this listener.
  if (!sender?.tab || !videoSiteOf(sender.url ?? '')) return;
  if (msg?.type === 'knightloader-video-labels') {
    void loadLanguage().then(() => sendResponse(videoLabels()));
    return true;
  }
  if (msg?.type === 'knightloader-video-send') {
    void sendVideo(sender.url, msg).then(sendResponse);
    return true;
  }
  if (msg?.type === 'knightloader-video-hide') void hideVideoSite(sender.url);
  if (msg?.type === 'knightloader-video-setup') chrome.runtime.openOptionsPage();
});

chrome.runtime.onInstalled.addListener(() => void applyVideoButton());
chrome.runtime.onStartup?.addListener(() => void applyVideoButton());
chrome.permissions.onAdded.addListener(() => void applyVideoButton());
chrome.permissions.onRemoved.addListener(() => void applyVideoButton());
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && (changes.videoButton || changes.videoButtonHidden)) void applyVideoButton();
});
