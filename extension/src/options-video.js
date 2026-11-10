// The options page's card for the download button on a video's player
// (video.js): the switch, whether it hides until the pointer is over the
// video, the place it was dragged to, and per site whether it shows there and
// what a click downloads. Switching on asks the browser for the sites it
// needs. Switching off keeps what the browser granted, so switching on again
// needs no question.

const videoEnabledEl = document.getElementById('videoEnabled');
const videoNoticeEl = document.getElementById('videoNotice');
const videoMoreEl = document.getElementById('videoMore');
const videoAutoHideEl = document.getElementById('videoAutoHide');
const videoSpotResetEl = document.getElementById('videoSpotReset');
const videoSitesEl = document.getElementById('videoSites');

const VIDEO_STORED = ['videoButtonAutoHide', 'videoButtonSpot', 'videoDefaults', 'videoQualities'];
const VIDEO_AUDIO_NAMES = { aac: 'AAC', alac: 'ALAC', flac: 'FLAC', mp3: 'MP3', opus: 'Opus', vorbis: 'Vorbis', wav: 'WAV' };

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
  videoMoreEl.hidden = !videoNow.on;
  videoAutoHideEl.setAttribute('aria-checked', String(videoNow.stored.videoButtonAutoHide === true));
  videoSpotResetEl.disabled = Object.keys(videoNow.stored.videoButtonSpot ?? {}).length === 0;
  // Every change redraws the sites, so the keyboard goes back to the control
  // it was on.
  const focusKey = document.activeElement?.dataset?.videoKey;
  videoSitesEl.replaceChildren(...VIDEO_SITES.map(videoSiteBlock));
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

/** One site: whether the button shows there, and while it does, what a click
 *  downloads. */
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
  block.append(row);
  if (on) block.append(...videoDefaultsRows(site));
  return block;
}

/** The parts a click downloads on a site, and the video's quality and the
 *  audio's format where those parts are on. */
function videoDefaultsRows(site) {
  const pick = videoDefaultsFor(videoNow.stored, site.id);
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

  const picks = document.createElement('div');
  picks.className = 'videoPicks';
  if (pick.parts.includes('video')) {
    const qualities = ['best', ...videoSiteQualities(videoNow.stored, site.id)];
    picks.append(videoSelect(`${site.id}-quality`, t('video.quality'), qualities, pick.quality, (q) => (q === 'best' ? t('video.qualityBest') : q), (quality) => save({ quality })));
  }
  if (pick.parts.includes('audio')) {
    const name = (f) => (f === 'best' ? t('video.qualityBest') : VIDEO_AUDIO_NAMES[f]);
    picks.append(videoSelect(`${site.id}-audio`, t('video.audioFormat'), VIDEO_AUDIO_FORMATS, pick.audio, name, (audio) => save({ audio })));
  }
  return picks.children.length ? [parts, picks] : [parts];
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
