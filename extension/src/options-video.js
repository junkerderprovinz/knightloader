// The options page's card for the download button on a video's player
// (video.js): the switch, whether it hides until the pointer is over the
// video, the place it was dragged to, the keyboard shortcut that sends as a
// click does, and the sites as tiles. A tile carries the site's switch and
// opens a window with what a click downloads there, offering only what the
// site has offered so far. Sites added here run on the access to all
// websites like all other sites, with defaults of their own.
// Switching on asks the browser for the sites it needs. All other sites run on
// the access to all websites, which is asked for on their own switch, so a
// "no" to it keeps the button on the named sites. Switching off keeps what the
// browser granted, so switching on again needs no question, and leaves every
// tile and window in view with its controls switched off.

const videoEnabledEl = document.getElementById('videoEnabled');
const videoNoticeEl = document.getElementById('videoNotice');
const videoMoreEl = document.getElementById('videoMore');
const videoAutoHideEl = document.getElementById('videoAutoHide');
const videoSpotResetEl = document.getElementById('videoSpotReset');
const videoSitesEl = document.getElementById('videoSites');
const videoShortcutRowEl = document.getElementById('videoShortcutRow');
const videoShortcutEl = document.getElementById('videoShortcut');
const videoShortcutActionEl = document.getElementById('videoShortcutAction');
const videoShortcutErrorEl = document.getElementById('videoShortcutError');
const videoSiteWindowEl = document.getElementById('videoSiteWindow');
const videoSiteHeadingEl = document.getElementById('videoSiteHeading');
const videoSiteTitleEl = document.getElementById('videoSiteTitle');
const videoSiteBodyEl = document.getElementById('videoSiteBody');
const videoSiteRemoveEl = document.getElementById('videoSiteRemove');
const videoSiteCancelEl = document.getElementById('videoSiteCancel');
const videoSiteCommitEl = document.getElementById('videoSiteCommit');
const videoSiteCloseEl = document.getElementById('videoSiteClose');
const videoAddWindowEl = document.getElementById('videoAddWindow');
const videoAddTitleEl = document.getElementById('videoAddTitle');
const videoAddHeadEl = document.getElementById('videoAddHead');
const videoAddLabelEl = document.getElementById('videoAddLabel');
const videoAddFieldEl = document.getElementById('videoAddField');
const videoAddPreviewEl = document.getElementById('videoAddPreview');
const videoAddErrorEl = document.getElementById('videoAddError');
const videoAddCancelEl = document.getElementById('videoAddCancel');
const videoAddCommitEl = document.getElementById('videoAddCommit');

const VIDEO_STORED = ['videoButtonAutoHide', 'videoButtonSpot', 'videoDefaults', 'videoSeen', 'videoCustomSites'];

/** Firefox lets the page change the shortcut. Chromium changes it only on its
 *  own shortcuts page, which the row opens instead. */
const VIDEO_SHORTCUT_EDITABLE = typeof chrome.commands?.update === 'function';
const VIDEO_SHORTCUT_PAGE = 'chrome://extensions/shortcuts';
const VIDEO_SHORTCUT_DEFAULT = chrome.runtime.getManifest().commands?.[VIDEO_COMMAND]?.suggested_key?.default ?? '';
const VIDEO_SHORTCUT_MAC = /Mac/.test(navigator.platform);
// What Firefox takes in a shortcut (commands.update), by name.
const VIDEO_SHORTCUT_MODIFIERS = ['Alt', 'Ctrl', 'Command', 'MacCtrl', 'Shift'];
const VIDEO_SHORTCUT_KEY = /^([A-Z0-9]|Comma|Period|Home|End|PageUp|PageDown|Space|Insert|Delete|Up|Down|Left|Right)$/;
const VIDEO_SHORTCUT_FKEY = /^F([1-9]|1[0-2])$/;
const VIDEO_SHORTCUT_MEDIA = { MediaTrackNext: 'MediaNextTrack', MediaPlayPause: 'MediaPlayPause', MediaTrackPrevious: 'MediaPrevTrack', MediaStop: 'MediaStop' };

/** The state last drawn. A click asks the browser before anything else, while
 *  it still counts as the user's, so it reads what to ask for from here. */
let videoNow = null;

/** Whether the browser refused the sites since this page opened, so the
 *  notice can say why the switch fell back. */
let videoRefused = false;

/** The site whose window is open, by id, and whether it asks about removing
 *  the site, or null while it is closed. */
let videoOpen = null;

/** The favicons that did not load, so a redraw goes straight to the
 *  letter. */
const videoMarkFailed = new Set();

function videoText() {
  document.getElementById('videoHeading').textContent = t('options.videoHeading');
  rowLabel('videoLabel', t('options.videoToggle'), t('options.videoInfo'), videoEnabledEl);
  rowLabel('videoAutoHideLabel', t('options.videoAutoHide'), t('options.videoAutoHideInfo'), videoAutoHideEl);
  rowLabel('videoSpotLabel', t('options.videoSpot'), t('options.videoSpotInfo'));
  videoSpotResetEl.replaceChildren(glimGlyph('IconRetry', 14), document.createTextNode(t('options.videoSpotReset')));
  document.getElementById('videoSitesHeading').textContent = t('options.videoSites');
  glimSetInfo('videoSitesHeading', t('options.videoSitesInfo'));
  rowLabel('videoShortcutLabel', t('options.videoShortcut'), t('options.videoShortcutInfo'), videoShortcutEl);
  if (VIDEO_SHORTCUT_EDITABLE) {
    videoShortcutActionEl.replaceChildren(glimGlyph('IconRetry', 14), document.createTextNode(t('options.videoSpotReset')));
  } else {
    videoShortcutActionEl.replaceChildren(glimGlyph('IconExternalLink', 14), document.createTextNode(t('options.videoShortcutChange')));
    videoShortcutActionEl.setAttribute('data-tip', VIDEO_SHORTCUT_PAGE);
    videoShortcutEl.tabIndex = -1;
  }
  videoSiteRemoveEl.replaceChildren(glimGlyph('IconTrash', 14), document.createTextNode(t('options.videoRemoveSite')));
  videoSiteCancelEl.replaceChildren(glimGlyph('IconClose', 14), document.createTextNode(t('common.cancel')));
  videoSiteCommitEl.replaceChildren(glimGlyph('IconTrash', 14), document.createTextNode(t('options.videoRemoveCommit')));
  videoSiteCloseEl.replaceChildren(glimGlyph('IconClose', 14), document.createTextNode(t('common.close')));
  videoAddTitleEl.textContent = t('options.videoAddSite');
  videoAddLabelEl.textContent = t('options.videoAddAddress');
  glimSetInfo('videoAddHead', t('options.videoAddAddressInfo'));
  videoAddCancelEl.replaceChildren(glimGlyph('IconClose', 14), document.createTextNode(t('common.cancel')));
  videoAddCommitEl.replaceChildren(glimGlyph('IconAdd', 14), document.createTextNode(t('options.videoAddCommit')));
  if (videoNow) drawVideoShortcut();
}

/** The shortcut the browser has for the button, '' where it has none. */
async function videoShortcut() {
  if (!chrome.commands) return '';
  return (await chrome.commands.getAll()).find((c) => c.name === VIDEO_COMMAND)?.shortcut ?? '';
}

/** The field shows the shortcut, and is empty for the new one while it has
 *  the keyboard in Firefox. */
function drawVideoShortcut() {
  const recording = VIDEO_SHORTCUT_EDITABLE && document.activeElement === videoShortcutEl;
  videoShortcutEl.value = recording ? '' : videoNow.shortcut;
  videoShortcutEl.placeholder = t(recording ? 'options.videoShortcutPress' : 'options.videoShortcutNone');
}

/** The shortcut a key press makes, as commands.update spells it, or '' while
 *  only modifiers are down. Letters and digits come from the character typed,
 *  which Firefox matches, unless Option or Shift turned it into another. */
function videoShortcutOf(e) {
  if (['Alt', 'AltGraph', 'Control', 'Meta', 'OS', 'Shift'].includes(e.key)) return '';
  const modifiers = [];
  if (e.ctrlKey) modifiers.push(VIDEO_SHORTCUT_MAC ? 'MacCtrl' : 'Ctrl');
  if (e.metaKey) modifiers.push(VIDEO_SHORTCUT_MAC ? 'Command' : 'Meta');
  if (e.altKey) modifiers.push('Alt');
  if (e.shiftKey) modifiers.push('Shift');
  const key = /^[a-z0-9]$/i.test(e.key)
    ? e.key.toUpperCase()
    : /^(Key[A-Z]|Digit[0-9])$/.test(e.code)
      ? e.code.slice(-1)
      : (VIDEO_SHORTCUT_MEDIA[e.code] ?? e.code.replace(/^Arrow/, ''));
  return [...modifiers, key].join('+');
}

/** Whether Firefox takes a shortcut: a media key alone, or one or two
 *  different modifiers with a key, where Shift alone or no modifier at all
 *  goes only with an F key. */
function videoShortcutValid(shortcut) {
  if (Object.values(VIDEO_SHORTCUT_MEDIA).includes(shortcut)) return true;
  const modifiers = shortcut.split('+');
  const key = modifiers.pop();
  const fkey = VIDEO_SHORTCUT_FKEY.test(key);
  if (!fkey && !VIDEO_SHORTCUT_KEY.test(key)) return false;
  if (modifiers.length > 2 || new Set(modifiers).size < modifiers.length || modifiers.some((m) => !VIDEO_SHORTCUT_MODIFIERS.includes(m))) return false;
  return fkey || modifiers.some((m) => m !== 'Shift');
}

function refuseVideoShortcut() {
  videoShortcutErrorEl.textContent = t('options.videoShortcutInvalid');
  videoShortcutErrorEl.hidden = false;
  shake(videoShortcutEl);
}

/** The switch shows whether the button runs anywhere, never only whether it
 *  is wanted. Wanted without the access brings up the notice. */
async function renderVideo() {
  videoNow = { ...(await videoButtonState()), stored: await chrome.storage.local.get(VIDEO_STORED), shortcut: await videoShortcut() };
  videoEnabledEl.setAttribute('aria-checked', String(videoNow.on));
  const waiting = videoNow.wanted && !videoNow.on && videoNow.shown.length > 0;
  videoNoticeEl.hidden = !videoRefused && !waiting;
  document.getElementById('videoNoticeTitle').textContent = t(videoRefused ? 'options.videoRefusedTitle' : 'options.videoAccessTitle');
  document.getElementById('videoNoticeReason').textContent = t(videoRefused ? 'options.videoRefused' : 'options.videoAccess');
  videoAutoHideEl.setAttribute('aria-checked', String(videoNow.stored.videoButtonAutoHide === true));
  // Firefox for Android has no keyboard shortcuts.
  videoShortcutRowEl.hidden = !chrome.commands;
  drawVideoShortcut();
  // Every change redraws the sites and the open window, so the keyboard goes
  // back to the control it was on.
  const focusKey = document.activeElement?.dataset?.videoKey;
  videoSitesEl.replaceChildren(...videoSitesNow().map(videoTile), videoTile(videoSiteById(VIDEO_OTHER.id)), videoAddTile());
  // Off, the tiles stay in view and still open, so what the button will do
  // can be read and found, and each control refuses on its own.
  for (const el of [...videoMoreEl.querySelectorAll('button, select, input'), ...videoSitesEl.querySelectorAll('button, select, input')]) el.disabled = !videoNow.on && !el.dataset.videoOpen;
  if (videoNow.on) {
    videoSpotResetEl.disabled = Object.keys(videoNow.stored.videoButtonSpot ?? {}).length === 0;
    if (VIDEO_SHORTCUT_EDITABLE) videoShortcutActionEl.disabled = videoNow.shortcut === VIDEO_SHORTCUT_DEFAULT;
  }
  if (videoOpen) drawVideoWindow();
  if (focusKey) videoControl(videoOpen ? videoSiteWindowEl : videoSitesEl, focusKey)?.focus();
}

/** The control a redraw gave the key, inside `root`. */
function videoControl(root, key) {
  return [...root.querySelectorAll('button, select, input')].find((el) => el.dataset.videoKey === key);
}

/** The named sites, then those added here in the order they came. Each has
 *  the host its favicon comes from. */
function videoSitesNow() {
  const named = VIDEO_SITES.map((s) => ({ ...s, host: new URL(s.origins[0].slice(0, -1)).hostname }));
  return [...named, ...videoCustomList(videoNow.stored).map((host) => ({ id: host, name: host, host, custom: true }))];
}

function videoCustomList(stored) {
  return stored.videoCustomSites ?? [];
}

function videoSiteById(id) {
  if (id === VIDEO_OTHER.id) return { ...VIDEO_OTHER, name: t('options.videoOtherSites'), glyph: 'IconBrowser' };
  return videoSitesNow().find((s) => s.id === id);
}

/**
 * The host a site is added under for what was typed: "example.com" for
 * "https://www.example.com/watch?v=1" or "example.com", '' for anything
 * that is no web address. The button finds the site by it on the host and
 * every host under it.
 */
function videoCustomHost(text) {
  const typed = text.trim();
  if (!typed) return '';
  let u;
  try {
    u = new URL(/^[a-z][a-z0-9+.-]*:/i.test(typed) ? typed : `https://${typed}`);
  } catch {
    return '';
  }
  const host = u.hostname.replace(/^www\./, '');
  return /^https?:$/.test(u.protocol) && /^[^.]+(\.[^.]+)+$/.test(host) ? host : '';
}

/** The tile a host already has: a named site's, whose domains it is on, or
 *  one added before. */
function videoTileFor(host) {
  const page = `https://${host}/`;
  if (videoSiteIdOf(page) !== VIDEO_OTHER.id) {
    const domain = (s) => s.origins.map((o) => new URL(o.slice(0, -1)).hostname.split('.').slice(-2).join('.'));
    return VIDEO_SITES.find((s) => domain(s).some((d) => host === d || host.endsWith(`.${d}`)));
  }
  return videoSitesNow().find((s) => s.id === host);
}

/** A site's mark: its favicon, fetched from the site as any page's is, its
 *  first letter where none loads, or the glyph standing for all other sites
 *  and for adding one. */
function videoMark(site) {
  const mark = document.createElement('span');
  mark.className = 'videoMark';
  mark.setAttribute('aria-hidden', 'true');
  if (!site.host) {
    mark.append(glimGlyph(site.glyph, 16));
    return mark;
  }
  const letter = () => mark.replaceChildren(document.createTextNode(site.host.replace(/^www\./, '').charAt(0)));
  // A favicon at the root is the one every browser asks for, so most sites
  // serve one there, and some only under www.
  const sources = [`https://${site.host}/favicon.ico`, ...(site.host.startsWith('www.') ? [] : [`https://www.${site.host}/favicon.ico`])].filter((src) => !videoMarkFailed.has(src));
  if (sources.length === 0) {
    letter();
    return mark;
  }
  const img = document.createElement('img');
  img.alt = '';
  img.width = 20;
  img.height = 20;
  img.referrerPolicy = 'no-referrer';
  img.addEventListener('error', () => {
    videoMarkFailed.add(sources.shift());
    if (sources.length) img.src = sources[0];
    else letter();
  });
  img.src = sources[0];
  mark.append(img);
  return mark;
}

/** What a click downloads on a site, in one line: the parts, and for a video
 *  its quality and container, for audio alone its format. */
function videoSummary(pick) {
  const words = [new Intl.ListFormat(currentLanguage(), { style: 'short', type: 'unit' }).format(pick.parts.map((p) => t(VIDEO_PART_LABELS[p])))];
  if (pick.parts.includes('video')) {
    words.push(pick.quality === 'best' ? t('video.qualityBest') : pick.quality);
    if (pick.videoFormat !== 'best') words.push(videoFormatName(pick.videoFormat.split(' ')[0]));
  } else if (pick.parts.includes('audio') && pick.audio !== 'best') {
    words.push(videoFormatName(pick.audio));
  }
  return words.join(' · ');
}

function videoTileButton(key, mark, name, summary) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'videoTileOpen';
  b.dataset.videoKey = key;
  const text = document.createElement('span');
  text.className = 'videoTileText';
  const title = document.createElement('span');
  title.className = 'videoTileName';
  title.textContent = name;
  text.append(title);
  if (summary) {
    const line = document.createElement('span');
    line.className = 'videoTileSum';
    line.textContent = summary;
    text.append(line);
  }
  b.append(mark, text);
  return b;
}

function videoTile(site) {
  const tile = document.createElement('div');
  tile.className = 'videoTile';
  const open = videoTileButton(`${site.id}-open`, videoMark(site), site.name, videoSummary(videoDefaultsFor(videoNow.stored, site.id)));
  open.dataset.videoOpen = site.id;
  open.addEventListener('click', () => openVideoSite(site.id));
  tile.append(open);
  const on = videoNow.running.includes(site.id);
  const toggle = videoSwitch(on, site.name);
  toggle.dataset.videoKey = `${site.id}-shown`;
  toggle.addEventListener('click', async () => {
    const hidden = [...VIDEO_ALL_SITES.map((s) => s.id), ...videoCustomList(videoNow.stored)].filter((id) => !videoNow.shown.includes(id));
    if (on) {
      await chrome.storage.local.set({ videoButtonHidden: [...hidden, site.id] });
      return;
    }
    const granted = videoNow.granted.includes(site.id) || (await chrome.permissions.request(videoAccess([site.id])).catch(() => false));
    if (!granted) {
      shake(toggle);
      return;
    }
    await chrome.storage.local.set({ videoButtonHidden: hidden.filter((id) => id !== site.id) });
  });
  tile.append(toggle);
  return tile;
}

function videoAddTile() {
  const tile = document.createElement('div');
  tile.className = 'videoTile';
  const add = videoTileButton('add-open', videoMark({ glyph: 'IconAdd' }), t('options.videoAddSite'), '');
  add.addEventListener('click', openVideoAdd);
  tile.append(add);
  return tile;
}

/** Fills the open window: a site's controls, or the question whether to
 *  remove it, with the buttons that go with each. */
function drawVideoWindow() {
  const site = videoSiteById(videoOpen.id);
  if (!site) {
    closeVideoSite();
    return;
  }
  const removing = videoOpen.removing;
  videoSiteTitleEl.textContent = removing ? t('options.videoRemoveSite') : site.name;
  videoSiteHeadingEl.querySelector('.glim-info-icon')?.remove();
  if (removing) {
    const question = document.createElement('p');
    question.className = 'glim-modal-message';
    question.textContent = t('options.videoRemoveQuestion', { site: site.name });
    videoSiteBodyEl.replaceChildren(question);
  } else {
    videoSiteBodyEl.replaceChildren(...videoDefaultsRows(site));
  }
  videoSiteRemoveEl.hidden = !site.custom || removing;
  videoSiteCancelEl.hidden = !removing;
  videoSiteCommitEl.hidden = !removing;
  videoSiteCloseEl.hidden = removing;
  for (const el of [...videoSiteBodyEl.querySelectorAll('button, select, input'), videoSiteRemoveEl]) el.disabled = !videoNow.on;
}

/** The controls Tab moves between while a window is open: what it shows and
 *  can be pressed, the (i) on its heading included. */
function videoWindowStops(win) {
  if (win === videoAddWindowEl) return [videoAddHeadEl.querySelector('.glim-info-icon'), videoAddFieldEl, videoAddCancelEl, videoAddCommitEl].filter(Boolean);
  const body = videoSiteBodyEl.querySelectorAll('button, select, input');
  const foot = [videoSiteRemoveEl, videoSiteCancelEl, videoSiteCommitEl, videoSiteCloseEl];
  return [videoSiteHeadingEl.querySelector('.glim-info-icon'), ...body, ...foot].filter((el) => el && !el.hidden && !el.disabled);
}

/** Escape leaves, and Tab stays inside the window, as aria-modal promises. */
function onVideoWindowKey(event) {
  const win = videoAddWindowEl.hidden ? videoSiteWindowEl : videoAddWindowEl;
  if (event.key === 'Escape') {
    event.preventDefault();
    if (win === videoAddWindowEl) closeVideoAdd();
    else if (videoOpen?.removing) cancelVideoRemove();
    else closeVideoSite();
    return;
  }
  if (event.key !== 'Tab') return;
  const stops = videoWindowStops(win);
  const at = stops.indexOf(document.activeElement);
  event.preventDefault();
  // -1 means focus is on the page behind; Tab pulls it back in.
  const next = event.shiftKey ? (at <= 0 ? stops.length - 1 : at - 1) : at === stops.length - 1 ? 0 : at + 1;
  stops[next]?.focus();
}

function openVideoSite(id) {
  videoOpen = { id, removing: false };
  drawVideoWindow();
  videoSiteWindowEl.hidden = false;
  document.addEventListener('keydown', onVideoWindowKey);
  // The first control the site has, or the way out where it has none yet.
  videoWindowStops(videoSiteWindowEl).find((el) => el !== videoSiteHeadingEl.querySelector('.glim-info-icon'))?.focus();
}

/** Focus goes back to the tile, or to Add site where the tile is gone. */
function closeVideoSite() {
  const id = videoOpen?.id;
  videoOpen = null;
  videoSiteWindowEl.hidden = true;
  document.removeEventListener('keydown', onVideoWindowKey);
  (videoControl(videoSitesEl, `${id}-open`) ?? videoControl(videoSitesEl, 'add-open'))?.focus();
}

function askVideoRemove() {
  videoOpen.removing = true;
  drawVideoWindow();
  // Focus starts on the answer that changes nothing.
  videoSiteCancelEl.focus();
}

function cancelVideoRemove() {
  videoOpen.removing = false;
  drawVideoWindow();
  videoSiteRemoveEl.focus();
}

/** A site goes with its switch, its defaults and what it offered, so the
 *  button downloads there as on all other sites again. */
async function removeVideoSite() {
  const host = videoOpen.id;
  const { videoDefaults = {}, videoSeen = {} } = videoNow.stored;
  const { videoButtonHidden = [] } = await chrome.storage.local.get('videoButtonHidden');
  const without = (o) => Object.fromEntries(Object.entries(o).filter(([k]) => k !== host));
  await chrome.storage.local.set({
    videoButtonHidden: videoButtonHidden.filter((id) => id !== host),
    videoCustomSites: videoCustomList(videoNow.stored).filter((h) => h !== host),
    videoDefaults: without(videoDefaults),
    videoSeen: without(videoSeen),
  });
  await renderVideo();
  closeVideoSite();
}

function openVideoAdd() {
  videoAddFieldEl.value = '';
  drawVideoAdd();
  videoAddErrorEl.hidden = true;
  videoAddWindowEl.hidden = false;
  document.addEventListener('keydown', onVideoWindowKey);
  videoAddFieldEl.focus();
}

function closeVideoAdd() {
  videoAddWindowEl.hidden = true;
  document.removeEventListener('keydown', onVideoWindowKey);
  videoControl(videoSitesEl, 'add-open')?.focus();
}

/** The site as its tile will show it, while what is typed is an address. */
function drawVideoAdd() {
  const host = videoCustomHost(videoAddFieldEl.value);
  videoAddPreviewEl.hidden = !host;
  if (host) videoAddPreviewEl.replaceChildren(videoMark({ host }), document.createTextNode(host));
}

function refuseVideoAdd(text) {
  videoAddErrorEl.textContent = text;
  videoAddErrorEl.hidden = false;
  shake(videoAddFieldEl);
}

/** The new site gets its tile and opens its window, since what it should
 *  download is the next thing to set. */
async function addVideoSite() {
  const host = videoCustomHost(videoAddFieldEl.value);
  if (!host) {
    refuseVideoAdd(t('options.videoAddInvalid'));
    return;
  }
  const known = videoTileFor(host);
  if (known) {
    refuseVideoAdd(t('options.videoAddKnown', { site: known.name }));
    return;
  }
  videoAddWindowEl.hidden = true;
  document.removeEventListener('keydown', onVideoWindowKey);
  await chrome.storage.local.set({ videoCustomSites: [...videoCustomList(videoNow.stored), host] });
  await renderVideo();
  openVideoSite(host);
}

function videoSwitch(on, name) {
  const toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.className = 'glim-switch';
  toggle.setAttribute('role', 'switch');
  toggle.setAttribute('aria-checked', String(on));
  toggle.setAttribute('aria-label', name);
  const track = document.createElement('span');
  track.className = 'glim-switch-track';
  const knob = document.createElement('span');
  knob.className = 'glim-switch-knob';
  track.append(knob);
  toggle.append(track);
  return toggle;
}

/** The parts a click downloads on a site, and where those parts are on, the
 *  video's cap, the audio's codec, the subtitles' languages and format and
 *  the thumbnail's format, each from what the site has offered so far. */
function videoDefaultsRows(site) {
  const pick = videoDefaultsFor(videoNow.stored, site.id);
  const seen = videoSiteSeen(videoNow.stored, site.id);
  // A default stored before the site stopped offering it stays readable.
  const offered = (list, current) => (current === 'best' || list.includes(current) ? list : [...list, current]);
  const save = (change) => {
    const all = videoNow.stored.videoDefaults ?? {};
    void chrome.storage.local.set({ videoDefaults: { ...all, [site.id]: { ...pick, ...change } } });
  };

  const parts = document.createElement('div');
  parts.className = 'glim-seg videoParts';
  parts.setAttribute('role', 'group');
  parts.setAttribute('aria-label', `${site.name}: ${t('options.videoParts')}`);
  for (const part of VIDEO_PARTS) {
    const on = pick.parts.includes(part);
    const b = document.createElement('button');
    b.type = 'button';
    b.setAttribute('aria-pressed', String(on));
    b.dataset.videoKey = `${site.id}-${part}`;
    const words = document.createElement('span');
    words.textContent = t(VIDEO_PART_LABELS[part]);
    b.append(words);
    b.addEventListener('click', () => {
      // The last part stays, since a click that downloads nothing is no
      // download.
      if (on && pick.parts.length === 1) {
        shake(b);
        return;
      }
      save({ parts: VIDEO_PARTS.filter((p) => (p === part ? !on : pick.parts.includes(p))) });
    });
    parts.append(b);
  }

  const best = (name) => (v) => (v === 'best' ? t('video.qualityBest') : name(v));
  const rows = [parts];
  // One row per part, so a part's quality and format sit side by side.
  const row = (...selects) => {
    const picks = document.createElement('div');
    picks.className = 'videoPicks';
    picks.append(...selects);
    rows.push(picks);
  };
  if (pick.parts.includes('video')) {
    const qualities = ['best', ...offered(seen.qualities, pick.quality)];
    // The conversions the instance makes after the download, by the same
    // words the button's menu uses.
    const formats = ['best', ...new Set([...offered(seen.videoFormats, pick.videoFormat), ...VIDEO_CONVERSIONS])];
    const formatName = (f) => {
      const [container, codec] = f.split(' ').map(videoFormatName);
      return VIDEO_CONVERSIONS.includes(f) ? `${codec} (${container}, ${t('video.converted')})` : `${container} · ${codec}`;
    };
    row(
      videoSelect(`${site.id}-quality`, t('video.quality'), qualities, pick.quality, best((q) => q), (quality) => save({ quality })),
      videoSelect(`${site.id}-format`, t('video.videoFormat'), formats, pick.videoFormat, best(formatName), (videoFormat) => save({ videoFormat })),
    );
  }
  if (pick.parts.includes('audio')) {
    const rates = ['best', ...offered(seen.audioRates, pick.audioRate)];
    // As in the button's menu, a format the site has not served is one the
    // instance converts to, which an option says on its one line.
    const codecs = ['best', ...[...new Set([...offered(seen.audio, pick.audio), ...VIDEO_AUDIO_MENU])].sort(audioCodecOrder)];
    const codecName = (c) => (seen.audio.includes(c) ? videoFormatName(c) : `${videoFormatName(c)} (${t('video.converted')})`);
    row(
      videoSelect(`${site.id}-rate`, t('video.audioQuality'), rates, pick.audioRate, best((r) => r), (audioRate) => save({ audioRate })),
      videoSelect(`${site.id}-audio`, t('video.audioFormat'), codecs, pick.audio, best(codecName), (audio) => save({ audio })),
    );
  }
  if (pick.parts.includes('subtitle')) {
    row(videoSelect(`${site.id}-subformat`, t('options.videoSubFormat'), VIDEO_SUB_FORMATS, pick.subFormat, videoFormatName, (subFormat) => save({ subFormat })));
    rows.push(videoSubLangs(site, pick, seen, save));
  }
  if (pick.parts.includes('thumbnail') && seen.thumbFormats.length) {
    const thumbFormat = videoThumbFormat(pick.thumbFormat, seen.thumbFormats);
    // Formats seen before the site's own were remembered carry no mark until
    // the next video shows which one that is.
    const thumbName = (f) => (seen.thumbServed.length === 0 || seen.thumbServed.includes(f) ? videoFormatName(f) : `${videoFormatName(f)} (${t('video.converted')})`);
    row(videoSelect(`${site.id}-thumbformat`, t('options.videoThumbFormat'), seen.thumbFormats, thumbFormat, thumbName, (f) => save({ thumbFormat: f })));
  }
  return rows;
}

/** The subtitle languages a click asks for on a site, in the order they were
 *  ticked, from those the site has offered so far. */
function videoSubLangs(site, pick, seen, save) {
  const box = document.createElement('div');
  box.className = 'videoLangs';
  const words = document.createElement('span');
  words.textContent = t('options.videoSubLangs');
  box.append(words);
  const langs = [...seen.subLangs, ...pick.subLangs.filter((l) => !seen.subLangs.includes(l))];
  if (langs.length === 0) {
    const none = document.createElement('span');
    none.className = 'videoNone';
    none.textContent = t('options.videoNoneYet');
    box.append(none);
    return box;
  }
  const chips = document.createElement('div');
  chips.className = 'glim-seg videoParts';
  chips.setAttribute('role', 'group');
  chips.setAttribute('aria-label', `${site.name}: ${t('options.videoSubLangs')}`);
  for (const lang of langs) {
    const on = pick.subLangs.includes(lang);
    const b = document.createElement('button');
    b.type = 'button';
    b.setAttribute('aria-pressed', String(on));
    b.dataset.videoKey = `${site.id}-sub-${lang}`;
    const name = document.createElement('span');
    name.textContent = videoLanguageName(lang);
    b.append(name);
    b.addEventListener('click', () => save({ subLangs: on ? pick.subLangs.filter((l) => l !== lang) : [...pick.subLangs, lang] }));
    chips.append(b);
  }
  box.append(chips);
  return box;
}

/** A language's name in the page's language, "Deutsch" for "de". */
function videoLanguageName(code) {
  const name = new Intl.DisplayNames(currentLanguage(), { type: 'language' }).of(code);
  return name.charAt(0).toLocaleUpperCase(currentLanguage()) + name.slice(1);
}

/** Each part's name, spelled out so check-locales.mjs sees every key read. */
const VIDEO_PART_LABELS = {
  video: 'video.partVideo',
  audio: 'video.partAudio',
  subtitle: 'video.partSubtitle',
  thumbnail: 'video.partThumbnail',
  description: 'video.partDescription',
};

function videoSelect(key, text, values, current, name, onPick) {
  const label = document.createElement('label');
  const words = document.createElement('span');
  words.textContent = text;
  const select = document.createElement('select');
  select.dataset.videoKey = key;
  for (const v of values) {
    const option = document.createElement('option');
    option.value = v;
    option.textContent = name(v);
    option.selected = v === current;
    select.append(option);
  }
  select.addEventListener('change', () => onPick(select.value));
  label.append(words, select);
  return label;
}

videoEnabledEl.addEventListener('click', async () => {
  if (!videoNow) return;
  if (videoNow.on) {
    videoRefused = false;
    await chrome.storage.local.set({ videoButton: false });
    return;
  }
  // Hidden on every site, switching it on shows it everywhere again. The
  // access to all websites is asked for here only when no named site is left.
  const sites = videoNow.shown.length ? videoNow.shown : VIDEO_ALL_SITES.map((s) => s.id);
  const named = sites.filter((id) => id !== VIDEO_OTHER.id);
  const missing = (named.length ? named : sites).filter((id) => !videoNow.granted.includes(id));
  const granted = missing.length === 0 || (await chrome.permissions.request(videoAccess(missing)).catch(() => false));
  videoRefused = !granted;
  await chrome.storage.local.set({ videoButton: granted, ...(videoNow.shown.length ? {} : { videoButtonHidden: [] }) });
  if (!granted) {
    await renderVideo();
    shake(videoEnabledEl);
    videoNoticeEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }
});

videoAutoHideEl.addEventListener('click', () => {
  void chrome.storage.local.set({ videoButtonAutoHide: videoAutoHideEl.getAttribute('aria-checked') !== 'true' });
});

videoSpotResetEl.addEventListener('click', () => {
  void chrome.storage.local.remove('videoButtonSpot');
});

videoShortcutEl.addEventListener('focus', () => {
  if (videoNow) drawVideoShortcut();
});
videoShortcutEl.addEventListener('blur', () => {
  videoShortcutErrorEl.hidden = true;
  if (videoNow) drawVideoShortcut();
});
// Tab still moves on, and Escape leaves the shortcut as it was.
videoShortcutEl.addEventListener('keydown', async (e) => {
  if (!VIDEO_SHORTCUT_EDITABLE || e.key === 'Tab') return;
  e.preventDefault();
  if (e.key === 'Escape') {
    videoShortcutEl.blur();
    return;
  }
  const shortcut = videoShortcutOf(e);
  if (!shortcut) return;
  if (!videoShortcutValid(shortcut)) {
    refuseVideoShortcut();
    return;
  }
  try {
    await chrome.commands.update({ name: VIDEO_COMMAND, shortcut });
  } catch {
    refuseVideoShortcut();
    return;
  }
  videoShortcutErrorEl.hidden = true;
  videoShortcutEl.blur();
  await renderVideo();
});

videoShortcutActionEl.addEventListener('click', async () => {
  if (!VIDEO_SHORTCUT_EDITABLE) {
    void chrome.tabs.create({ url: VIDEO_SHORTCUT_PAGE });
    return;
  }
  await chrome.commands.reset(VIDEO_COMMAND);
  videoShortcutErrorEl.hidden = true;
  await renderVideo();
});

videoSiteCloseEl.addEventListener('click', closeVideoSite);
videoSiteRemoveEl.addEventListener('click', askVideoRemove);
videoSiteCancelEl.addEventListener('click', cancelVideoRemove);
videoSiteCommitEl.addEventListener('click', () => void removeVideoSite());
// Only a press on the backdrop itself leaves, and while the window asks
// about removing a site it goes back to the site.
videoSiteWindowEl.addEventListener('click', (event) => {
  if (event.target !== videoSiteWindowEl) return;
  if (videoOpen?.removing) cancelVideoRemove();
  else closeVideoSite();
});

videoAddFieldEl.addEventListener('input', () => {
  videoAddErrorEl.hidden = true;
  drawVideoAdd();
});
videoAddFieldEl.addEventListener('keydown', (event) => {
  if (event.key !== 'Enter') return;
  event.preventDefault();
  void addVideoSite();
});
videoAddCommitEl.addEventListener('click', () => void addVideoSite());
videoAddCancelEl.addEventListener('click', closeVideoAdd);
videoAddWindowEl.addEventListener('click', (event) => {
  if (event.target === videoAddWindowEl) closeVideoAdd();
});

// The shortcut also changes on the browser's own page, in another tab.
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) void renderVideo();
});

// The button's own menu and its drags change the same storage.
chrome.storage.onChanged.addListener(async (changes, area) => {
  if (area !== 'local') return;
  if (changes.language) {
    await loadLanguage();
    videoText();
  }
  if (changes.language || changes.videoButton || changes.videoButtonHidden || VIDEO_STORED.some((k) => changes[k])) await renderVideo();
});
chrome.permissions.onAdded.addListener(() => void renderVideo());
chrome.permissions.onRemoved.addListener(() => void renderVideo());

(async () => {
  await loadLanguage();
  videoText();
  await renderVideo();
})();
