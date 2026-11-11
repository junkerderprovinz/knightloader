// The service worker's half of the download button on a video's player
// (video.js): it registers the button on the sites it runs on, brings it into
// tabs that were open before, and sends the video a button asks for: straight
// to the one instance online, or through the popup's choice where there are
// more.
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
 * showing one of the sites get it too, and switching the button on in the
 * options shows it on the video playing in another tab. A frame that has it
 * from an earlier round is left alone, since loading it twice would declare
 * its constants twice.
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

/** The extension's mark as SVG markup, read once, which the button draws
 *  inline since the page may not load the extension's files. */
let videoMarkSvg = null;

/** Everything the button says, in the extension's language, since the page
 *  it sits on has no catalogue, and the mark it shows. */
async function videoLabels() {
  videoMarkSvg ??= await fetch(chrome.runtime.getURL('logo.svg')).then((r) => r.text());
  return {
    send: t('video.send'),
    sendTip: t('video.sendTip'),
    more: t('video.more'),
    best: t('video.qualityBest'),
    language: t('video.language'),
    dubbed: t('video.dubbed'),
    autoCaptions: t('video.autoCaptions'),
    subFormat: t('video.subFormat'),
    parts: Object.fromEntries(VIDEO_PARTS.map((p) => [p, t(VIDEO_PART_KEYS[p])])),
    checking: t('video.checking'),
    unknown: t('video.unknown'),
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
    mark: videoMarkSvg,
    dir: LANGUAGES.find((l) => l.code === currentLanguage())?.rtl ? 'rtl' : 'ltr',
    lang: currentLanguage(),
  };
}

/** Each part's name, spelled out so check-locales.mjs sees every key read. */
const VIDEO_PART_KEYS = {
  video: 'video.partVideo',
  audio: 'video.partAudio',
  subtitle: 'video.partSubtitle',
  thumbnail: 'video.partThumbnail',
  description: 'video.partDescription',
};

/**
 * What became of a send, from the instance's answer to POST /api/links with
 * report set: sent where it created a row it did not hold back, held
 * where it held back every one, already where the video was in the list
 * before. An instance older than report answers with the bare list, empty
 * for a video it already had.
 */
function videoResult(answer) {
  const tasks = Array.isArray(answer) ? answer : answer?.tasks;
  const listed = Array.isArray(answer) ? [] : answer?.listed;
  if (!Array.isArray(tasks) || !Array.isArray(listed)) return { result: 'refused' };
  if (tasks.some((task) => task && !task.skipped)) return { result: 'sent' };
  if (tasks.length) return { result: 'held' };
  return { result: listed.length || Array.isArray(answer) ? 'already' : 'refused' };
}

/**
 * What a button may send: the address is worked out here again from the
 * page's own, on the site the message came from, so a button can only ever
 * send the video it is showing, and the pick is cleaned of anything the
 * instance would refuse. Null for a send the button may not make.
 */
async function videoSend(frameUrl, msg) {
  const page = videoPage(msg.href);
  if (!page || page.site !== videoSiteOf(frameUrl)?.id) return null;
  if (!(await videoButtonState()).running.includes(page.site)) return null;
  const link = msg.playlist && page.playlist ? page.playlist : page.url;
  return { page, link, choice: videoChoice(videoPick(msg.pick)) };
}

/** Adds the video to one instance, to download at once whatever the instance
 *  does with links added otherwise. */
async function postVideo(call, target, send) {
  const res = await call(
    target,
    'POST',
    '/api/links',
    JSON.stringify({ links: send.link, package: '', origin: 'cnl', source: send.page.url, choice: send.choice, start: true, report: true }),
  );
  if (res.status < 200 || res.status >= 300) return { result: 'refused', reason: t(refusalKey(res)) };
  try {
    return videoResult(JSON.parse(res.body));
  } catch {
    return { result: 'refused' };
  }
}

function videoFailure(e) {
  if (e?.code === 'no-phrase') return { result: 'unpaired' };
  const key = { 'relay-unreachable': 'send.relayFailed', 'no-answer': 'options.followFailed' }[e?.code];
  return key ? { result: 'failed', reason: t(key) } : { result: 'failed' };
}

/**
 * Sends the video of the page a button sits on and answers what became of
 * it, for the button to show. With more than one instance online the popup
 * asks where to, as it does for a right-clicked link, and the answer is
 * `asking`; the button hears the rest from answerVideoButton. `sender` is the
 * frame that asked, where that answer goes.
 */
async function sendVideo(frameUrl, msg, sender) {
  const send = await videoSend(frameUrl, msg);
  if (!send) return { result: 'refused' };
  try {
    return await withGroup(async ({ siblings, call }) => {
      if (siblings.length > 1) return askVideoTarget(siblings, send, msg, sender);
      const target = defaultOf(siblings, await readDefaultTarget());
      if (!target) return { result: 'offline' };
      return postVideo(call, target, send);
    });
  } catch (e) {
    return videoFailure(e);
  }
}

/**
 * Parks the video for the popup and opens it. The message goes along as the
 * button sent it, since it is checked again on its way out. Only a top
 * frame's tab title names the video; an embedded player's tab is the page
 * around it.
 */
async function askVideoTarget(siblings, send, msg, sender) {
  await loadLanguage();
  const pick = videoPick(msg.pick);
  const playlist = send.link === send.page.playlist;
  const parts = new Intl.ListFormat(currentLanguage(), { style: 'short', type: 'unit' }).format(pick.parts.map((p) => t(VIDEO_PART_KEYS[p])));
  const payload = {
    kind: 'video',
    href: msg.href,
    pick,
    playlist,
    frameUrl: sender.url,
    tabId: sender.tab.id,
    frameId: sender.frameId,
    title: (sender.frameId === 0 && sender.tab.title) || videoName(send.page.url),
    detail: playlist ? `${t('video.playlist')} · ${parts}` : parts,
  };
  await park({ payload, defaultName: await readDefaultTarget(), siblings, origin: '' });
  await showPopupOrMark(sender.tab.url);
  return { result: 'asking' };
}

/**
 * Sends a parked video to the instance picked in the popup and tells the
 * button. The frame is marked before anything is awaited, so the popup
 * closing right after its hand-over does not call the button off.
 */
async function deliverVideo(target, v) {
  videoPicked.add(videoFrameKey(v));
  let answer;
  try {
    const send = await videoSend(v.frameUrl, v);
    answer = send ? await withGroup(({ call }) => postVideo(call, target, send)) : { result: 'refused' };
  } catch (e) {
    answer = videoFailure(e);
  }
  answerVideoButton(v, answer);
}

/** Tells the button that asked what became of its video; null calls it off,
 *  and the button goes back to rest. */
function answerVideoButton(v, answer) {
  chrome.tabs.sendMessage(v.tabId, { type: 'knightloader-video-result', answer }, { frameId: v.frameId }).catch(() => {});
}

const videoFrameKey = (v) => `${v.tabId}/${v.frameId}`;

/** The frames whose video the popup sent, which its closing must not call
 *  off. */
const videoPicked = new Set();

// An open popup holding a video keeps a port to the worker. It closing
// without a pick is the only word of that, and the button waits until then.
chrome.runtime.onConnect.addListener((port) => {
  if (port.name !== 'knightloader-video-pick' || !String(port.sender?.url ?? '').startsWith(chrome.runtime.getURL(''))) return;
  let asked = null;
  port.onMessage.addListener((v) => {
    if (v?.kind === 'video') asked = v;
  });
  port.onDisconnect.addListener(() => {
    if (!asked) return;
    if (!videoPicked.delete(videoFrameKey(asked))) answerVideoButton(asked, null);
  });
});

/**
 * What the menu offers for the video of the page a button sits on: what the
 * default instance's yt-dlp says it has (videoOffer), or nothing known where
 * it cannot say. Checked like sendVideo, so a button only asks about the
 * video it is showing. What it offered is kept for the site, for the options
 * page to offer as defaults.
 */
async function videoFormats(frameUrl, msg) {
  const page = videoPage(msg.href);
  if (!page || page.site !== videoSiteOf(frameUrl)?.id) return videoOffer(null);
  let answer = null;
  try {
    answer = await withGroup(async ({ siblings, call }) => {
      const target = defaultOf(siblings, await readDefaultTarget());
      return target ? readJSON(call, target, `/api/ytdlp/link?url=${encodeURIComponent(page.url)}`) : null;
    });
  } catch {
    return videoOffer(null);
  }
  const offer = videoOffer(answer);
  if (offer.known) {
    const { videoSeen } = await chrome.storage.local.get('videoSeen');
    await chrome.storage.local.set({ videoSeen: { ...videoSeen, [page.site]: videoSeenWith(videoSeen?.[page.site], offer) } });
  }
  return offer;
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
    void loadLanguage().then(videoLabels).then(sendResponse);
    return true;
  }
  if (msg?.type === 'knightloader-video-send') {
    void sendVideo(sender.url, msg, sender).then(sendResponse);
    return true;
  }
  if (msg?.type === 'knightloader-video-formats') {
    void videoFormats(sender.url, msg).then(sendResponse);
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
