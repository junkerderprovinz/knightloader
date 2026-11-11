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
    downloading: t('video.downloading'),
    done: t('video.done'),
    doneTip: t('video.doneTip'),
    loadFailed: t('video.loadFailed'),
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
 * Sends the video of the page a button sits on, to download at once whatever
 * the instance does with links added otherwise. The address is worked out
 * here again from the page's own, on the site the message came from, so a
 * button can only ever send the video it is showing, and the pick is cleaned
 * of anything the instance would refuse. Answers what became of it, for the
 * button to show, and for a send that started rows where they are, for the
 * button to follow.
 */
async function sendVideo(frameUrl, msg) {
  const page = videoPage(msg.href);
  if (!page || page.site !== videoSiteOf(frameUrl)?.id) return { result: 'refused' };
  if (!(await videoButtonState()).running.includes(page.site)) return { result: 'refused' };
  const link = msg.playlist && page.playlist ? page.playlist : page.url;
  const choice = videoChoice(videoPick(msg.pick));
  try {
    return await withGroup(async ({ siblings, call }) => {
      const target = defaultOf(siblings, await readDefaultTarget());
      if (!target) return { result: 'offline' };
      const res = await call(
        target,
        'POST',
        '/api/links',
        JSON.stringify({ links: link, package: '', origin: 'cnl', source: page.url, choice, start: true, report: true }),
      );
      if (res.status < 200 || res.status >= 300) return { result: 'refused', reason: t(refusalKey(res)) };
      let answer;
      try {
        answer = JSON.parse(res.body);
      } catch {
        return { result: 'refused' };
      }
      const result = videoResult(answer);
      const ids = videoWatchIds(answer);
      return result.result === 'sent' && ids.length ? { ...result, watch: { instance: target, ids } } : result;
    });
  } catch (e) {
    if (e?.code === 'no-phrase') return { result: 'unpaired' };
    const key = { 'relay-unreachable': 'send.relayFailed', 'no-answer': 'options.followFailed' }[e?.code];
    return key ? { result: 'failed', reason: t(key) } : { result: 'failed' };
  }
}

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

/**
 * Where the rows a button follows stand on the instance it sent them to
 * (videoProgress), or null where that cannot be said this time, for the
 * button to ask again. Only task ids are taken, from an instance of the
 * group, so a button can do no more than read the state of rows by id.
 */
async function videoRowsProgress(msg) {
  const ids = Array.isArray(msg.ids) ? msg.ids : [];
  if (typeof msg.instance !== 'string' || ids.length === 0 || ids.length > VIDEO_WATCH_MAX || !ids.every((id) => videoIs(VIDEO_TASK_ID, id))) return null;
  try {
    return await withGroup(async ({ siblings, call }) => {
      if (!siblings.some((s) => s.instanceId === msg.instance)) return null;
      const tasks = await readJSON(call, msg.instance, `/api/tasks?ids=${ids.join(',')}`);
      return Array.isArray(tasks) ? videoProgress(tasks, ids) : null;
    });
  } catch {
    return null;
  }
}

/** Opens the downloads in the web interface of the instance a button sent to,
 *  at the address the popup's cards open (bestWebUrl). */
async function openVideoInstance(msg) {
  if (typeof msg.instance !== 'string') return;
  let url = '';
  try {
    url = await withGroup(async ({ siblings, call }) =>
      siblings.some((s) => s.instanceId === msg.instance) ? bestWebUrl(await readJSON(call, msg.instance, '/api/remote-access')) : '',
    );
  } catch {
    return;
  }
  if (url) await chrome.tabs.create({ url: `${url.replace(/\/+$/, '')}/downloads` });
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
    void sendVideo(sender.url, msg).then(sendResponse);
    return true;
  }
  if (msg?.type === 'knightloader-video-formats') {
    void videoFormats(sender.url, msg).then(sendResponse);
    return true;
  }
  if (msg?.type === 'knightloader-video-progress') {
    void videoRowsProgress(msg).then(sendResponse);
    return true;
  }
  if (msg?.type === 'knightloader-video-open') void openVideoInstance(msg);
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
