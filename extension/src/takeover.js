// Takes downloads over from the browser (the rules are in capture.js). The
// browser's download is held while the instance is asked, and only cancelled
// once the instance has the link, so a hand-over that fails leaves the
// download running where it was.
//
// Loaded by background.js. Every API used here comes with an optional
// permission, so each use checks that it exists.

/** A click with a modifier held, reported by bypass.js. The download it starts
 *  arrives a moment later, so the click counts for a few seconds. */
let lastModifiedClick = { at: 0, alt: false, shift: false, ctrl: false };
const BYPASS_WINDOW_MS = 4000;

let takeoverWired = false;
const CHROMIUM_NAMING_EVENT = 'onDeterminingFilename';

/**
 * Listens for downloads once the browser has granted the permission, which
 * can happen while this worker runs. Chromium names a file before writing it,
 * and that is the moment a download can still be held without a trace on
 * disk; Firefox has no such event and reports a download as it starts, so
 * there it is paused instead.
 */
function wireTakeover() {
  if (takeoverWired || !chrome.downloads) return;
  takeoverWired = true;
  // Looked up by a name held in a variable: the package is linted for
  // Firefox, which rightly reports that it has no such event.
  const naming = chrome.downloads[CHROMIUM_NAMING_EVENT];
  if (naming) {
    naming.addListener((item, suggest) => {
      // An answer of true keeps the download waiting until suggest() is
      // called, whatever the hand-over comes to.
      void takeOver(item, false).finally(() => {
        try {
          suggest();
        } catch {
          // Cancelled already, so there is nothing left to name.
        }
      });
      return true;
    });
    return;
  }
  chrome.downloads.onCreated?.addListener((item) => void takeOver(item, true));
}

/**
 * Hands one download over if the rules take it, and reports whether it went.
 * `pause` holds a download that is already being written, as Firefox's is.
 */
async function takeOver(item, pause) {
  if (!(await takeoverState()).on) return false;
  const rules = await readTakeoverRules();
  if (!takeoverVerdict(item, rules, Date.now(), bypassUntil(rules)).take) return false;

  let paused = false;
  if (pause) {
    paused = await chrome.downloads
      .pause(item.id)
      .then(() => true)
      .catch(() => false);
  }
  const headers = handedHeaders({
    cookie: await cookiesFor(item.url, item.cookieStoreId),
    referrer: item.referrer,
    userAgent: navigator.userAgent,
  });
  const inst = await handLinkOver({ url: item.url, source: item.referrer, headers }).catch(() => null);
  if (!inst) {
    if (paused) await chrome.downloads.resume(item.id).catch(() => {});
    flashBadge('!', '#da1e28', 'takeover.kept');
    return false;
  }
  await chrome.downloads.cancel(item.id).catch(() => {});
  await chrome.downloads.erase({ id: item.id }).catch(() => {});
  noteHandedOver(downloadName(item), inst);
  return true;
}

/** Until when the bypass key keeps a download in the browser. */
function bypassUntil(rules) {
  return lastModifiedClick[rules.bypassKey] ? lastModifiedClick.at + BYPASS_WINDOW_MS : 0;
}

/** The Cookie header for url, or '' without the permission or the access. */
async function cookiesFor(url, storeId) {
  if (!chrome.cookies) return '';
  try {
    return cookieHeader(await chrome.cookies.getAll(storeId ? { url, storeId } : { url }));
  } catch {
    return '';
  }
}

/**
 * Puts one link with the browser's headers into an instance: `target` when the
 * popup picked one, else the default. Resolves with the instance it went to,
 * or null when it did not arrive, which includes a link the instance already
 * had. A download from one of the group's own instances is never handed back.
 */
async function handLinkOver({ url, source, title, headers, target }) {
  return withGroup(async ({ siblings, call }) => {
    if (siblings.length === 0) return null;
    if (await fromOwnInstance(url, siblings, call)) return null;
    const to = siblings.some((s) => s.instanceId === target) ? target : defaultOf(siblings, await readDefaultTarget());
    const res = await call(
      to,
      'POST',
      '/api/links',
      JSON.stringify({
        links: url,
        package: title || '',
        origin: 'cnl',
        source: /^https?:\/\//i.test(source ?? '') ? source : '',
        headers,
      }),
    );
    if (res.status < 200 || res.status >= 300) return null;
    let created;
    try {
      created = JSON.parse(res.body);
    } catch {
      return null;
    }
    if (!Array.isArray(created) || created.length === 0) return null;
    return siblings.find((s) => s.instanceId === to) ?? null;
  });
}

/** Whether url is on an address one of the instances reports for itself. */
async function fromOwnInstance(url, siblings, call) {
  const host = hostOf(url);
  const answers = await Promise.all(siblings.map((s) => call(s.instanceId, 'GET', '/api/remote-access').catch(() => null)));
  for (const res of answers) {
    if (!res || res.status < 200 || res.status >= 300) continue;
    let remote;
    try {
      remote = JSON.parse(res.body);
    } catch {
      continue;
    }
    const addresses = Array.isArray(remote?.addresses) ? remote.addresses : [];
    if (addresses.some((a) => a?.url && hostOf(a.url) === host)) return true;
  }
  return false;
}

/** A short note naming the file and where it went. Without the permission
 *  the toolbar badge says it instead. */
function noteHandedOver(file, inst) {
  if (!chrome.notifications) {
    flashBadge('✓', '#24a148', 'send.delivered');
    return;
  }
  const id = `takeover-${Date.now()}`;
  chrome.notifications
    .create(id, {
      type: 'basic',
      iconUrl: chrome.runtime.getURL('icons/icon128.png'),
      title: 'KnightLoader',
      message: t('takeover.sent', { file: file || t('picker.untitled'), instance: instanceLabel(inst) }),
    })
    .catch(() => {});
  setTimeout(() => chrome.notifications.clear(id).catch(() => {}), 6000);
}

/** The content script that reports a click with a modifier held. It runs
 *  only while taking over is on. */
const BYPASS_SCRIPT = {
  id: 'takeover-bypass',
  matches: ['<all_urls>'],
  js: ['bypass.js'],
  runAt: 'document_start',
  allFrames: true,
  persistAcrossSessions: true,
};

async function syncBypassScript(on) {
  try {
    const have = (await chrome.scripting.getRegisteredContentScripts()).some((s) => s.id === BYPASS_SCRIPT.id);
    if (on && !have) await chrome.scripting.registerContentScripts([BYPASS_SCRIPT]);
    if (!on && have) await chrome.scripting.unregisterContentScripts({ ids: [BYPASS_SCRIPT.id] });
  } catch (e) {
    console.warn('[KnightLoader] bypass script not switched:', e);
  }
}

/** Applies the switch and the permissions together, one call after another,
 *  as applyCnl does. */
let takeoverQueue = Promise.resolve();
function applyTakeover() {
  takeoverQueue = takeoverQueue
    .catch(() => {})
    .then(async () => {
      wireTakeover();
      await syncBypassScript((await takeoverState()).on);
    });
  return takeoverQueue;
}

chrome.runtime.onMessage.addListener((msg, sender) => {
  // Only from a content script, which is where bypass.js runs.
  if (msg?.type !== 'knightloader-bypass' || !sender?.tab) return;
  lastModifiedClick = { at: Date.now(), alt: !!msg.alt, shift: !!msg.shift, ctrl: !!msg.ctrl };
});

chrome.runtime.onInstalled.addListener(() => void applyTakeover());
chrome.runtime.onStartup?.addListener(() => void applyTakeover());
chrome.permissions.onAdded.addListener(() => void applyTakeover());
chrome.permissions.onRemoved.addListener(() => void applyTakeover());
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && changes.takeoverEnabled) void applyTakeover();
});
wireTakeover();
