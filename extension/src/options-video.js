// The options page's card for the download button on a video's player
// (video.js): the switch, whether it hides until the pointer is over the
// video, the place it was dragged to, and per site whether it shows there and
// what a click downloads, offering only what the site has offered so far.
// Switching on asks the browser for the sites it needs. Switching off keeps
// what the browser granted, so switching on again needs no question, and
// leaves every row in view with its controls switched off.

const videoEnabledEl = document.getElementById('videoEnabled');
const videoNoticeEl = document.getElementById('videoNotice');
const videoMoreEl = document.getElementById('videoMore');
const videoAutoHideEl = document.getElementById('videoAutoHide');
const videoSpotResetEl = document.getElementById('videoSpotReset');
const videoSitesEl = document.getElementById('videoSites');

const VIDEO_STORED = ['videoButtonAutoHide', 'videoButtonSpot', 'videoDefaults', 'videoSeen'];
const VIDEO_AUDIO_NAMES = { aac: 'AAC', opus: 'Opus', vorbis: 'Vorbis', mp3: 'MP3', flac: 'FLAC', alac: 'ALAC' };

/** The state last drawn. A click asks the browser before anything else, while
 *  it still counts as the user's, so it reads what to ask for from here. */
let videoNow = null;

/** Whether the browser refused the sites since this page opened, so the
 *  notice can say why the switch fell back. */
let videoRefused = false;

function videoText() {
  document.getElementById('videoHeading').textContent = t('options.videoHeading');
  rowLabel('videoLabel', t('options.videoToggle'), t('options.videoInfo'), videoEnabledEl);
  rowLabel('videoAutoHideLabel', t('options.videoAutoHide'), t('options.videoAutoHideInfo'), videoAutoHideEl);
  rowLabel('videoSpotLabel', t('options.videoSpot'), t('options.videoSpotInfo'));
  videoSpotResetEl.replaceChildren(glimGlyph('IconRetry', 14), document.createTextNode(t('options.videoSpotReset')));
  rowLabel('videoSitesLabel', t('options.videoSites'), t('options.videoSitesInfo'));
}

/** The switch shows whether the button runs anywhere, never only whether it
 *  is wanted. Wanted without the access brings up the notice. */
async function renderVideo() {
  videoNow = { ...(await videoButtonState()), stored: await chrome.storage.local.get(VIDEO_STORED) };
  videoEnabledEl.setAttribute('aria-checked', String(videoNow.on));
  const waiting = videoNow.wanted && !videoNow.on && videoNow.shown.length > 0;
  videoNoticeEl.hidden = !videoRefused && !waiting;
  document.getElementById('videoNoticeTitle').textContent = t(videoRefused ? 'options.videoRefusedTitle' : 'options.videoAccessTitle');
  document.getElementById('videoNoticeReason').textContent = t(videoRefused ? 'options.videoRefused' : 'options.videoAccess');
  videoAutoHideEl.setAttribute('aria-checked', String(videoNow.stored.videoButtonAutoHide === true));
  // Every change redraws the sites, so the keyboard goes back to the control
  // it was on.
  const focusKey = document.activeElement?.dataset?.videoKey;
  videoSitesEl.replaceChildren(...VIDEO_SITES.map(videoSiteBlock));
  // Off, the rows stay in view, so what the button will do can be read and
  // found, and each control refuses on its own.
  for (const el of videoMoreEl.querySelectorAll('button, select')) el.disabled = !videoNow.on;
  if (videoNow.on) videoSpotResetEl.disabled = Object.keys(videoNow.stored.videoButtonSpot ?? {}).length === 0;
  if (focusKey) videoSitesEl.querySelector(`[data-video-key="${focusKey}"]`)?.focus();
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

/** One site: whether the button shows there, and what a click downloads. */
function videoSiteBlock(site) {
  const block = document.createElement('div');
  block.className = 'videoSite glim-well';
  const row = document.createElement('div');
  row.className = 'glim-row';
  const label = document.createElement('span');
  label.className = 'glim-row-label';
  label.textContent = site.name;
  const on = videoNow.running.includes(site.id);
  const toggle = videoSwitch(on, site.name);
  toggle.dataset.videoKey = `${site.id}-shown`;
  toggle.addEventListener('click', async () => {
    const hidden = VIDEO_SITES.map((s) => s.id).filter((id) => !videoNow.shown.includes(id));
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
  row.append(label, toggle);
  block.append(row, ...videoDefaultsRows(site));
  return block;
}

/** The parts a click downloads on a site, and where those parts are on, the
 *  video's cap, the audio's codec and the subtitles' languages and format,
 *  each from what the site has offered so far. */
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
  const picks = document.createElement('div');
  picks.className = 'videoPicks';
  if (pick.parts.includes('video')) {
    const qualities = ['best', ...offered(seen.qualities, pick.quality)];
    const formats = ['best', ...offered(seen.videoFormats, pick.videoFormat)];
    picks.append(
      videoSelect(`${site.id}-quality`, t('video.quality'), qualities, pick.quality, best((q) => q), (quality) => save({ quality })),
      videoSelect(`${site.id}-format`, t('video.videoFormat'), formats, pick.videoFormat, best((f) => f.replace(' ', ' · ')), (videoFormat) => save({ videoFormat })),
    );
  }
  if (pick.parts.includes('audio')) {
    const rates = ['best', ...offered(seen.audioRates, pick.audioRate)];
    const codecs = ['best', ...offered(seen.audio, pick.audio)];
    picks.append(
      videoSelect(`${site.id}-rate`, t('video.audioQuality'), rates, pick.audioRate, best((r) => r), (audioRate) => save({ audioRate })),
      videoSelect(`${site.id}-audio`, t('video.audioFormat'), codecs, pick.audio, best((c) => VIDEO_AUDIO_NAMES[c]), (audio) => save({ audio })),
    );
  }
  if (pick.parts.includes('subtitle')) {
    const formats = ['best', ...offered(seen.subFormats, pick.subFormat)];
    picks.append(videoSelect(`${site.id}-subformat`, t('options.videoSubFormat'), formats, pick.subFormat, best((f) => f), (subFormat) => save({ subFormat })));
  }
  const rows = [parts];
  if (picks.children.length) rows.push(picks);
  if (pick.parts.includes('subtitle')) rows.push(videoSubLangs(site, pick, seen, save));
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
    const code = document.createElement('span');
    code.textContent = lang;
    b.append(code);
    b.addEventListener('click', () => save({ subLangs: on ? pick.subLangs.filter((l) => l !== lang) : [...pick.subLangs, lang] }));
    chips.append(b);
  }
  box.append(chips);
  return box;
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
  // Hidden on every site, switching it on shows it everywhere again.
  const sites = videoNow.shown.length ? videoNow.shown : VIDEO_SITES.map((s) => s.id);
  const missing = sites.filter((id) => !videoNow.granted.includes(id));
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
