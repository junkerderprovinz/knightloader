// The options page's row for the download button on a video's player
// (video.js): the switch, the same one the popup has, and under it one switch
// per site. Switching on asks the browser for the sites it needs. Switching
// off keeps what the browser granted, so the popup can bring the button back
// with one click and no question.

const videoEnabledEl = document.getElementById('videoEnabled');
const videoNoticeEl = document.getElementById('videoNotice');
const videoSitesEl = document.getElementById('videoSites');

/** The state last drawn. A click asks the browser before anything else, while
 *  it still counts as the user's, so it reads what to ask for from here. */
let videoNow = null;

/** Whether the browser refused the sites since this page opened, so the
 *  notice can say why the switch fell back. */
let videoRefused = false;

function videoText() {
  rowLabel('videoLabel', t('options.videoToggle'), t('options.videoInfo'), videoEnabledEl);
}

/** The switch shows whether the button runs anywhere, never only whether it
 *  is wanted. Wanted without the access brings up the notice. */
async function renderVideo() {
  videoNow = await videoButtonState();
  videoEnabledEl.setAttribute('aria-checked', String(videoNow.on));
  const waiting = videoNow.wanted && !videoNow.on && videoNow.shown.length > 0;
  videoNoticeEl.hidden = !videoRefused && !waiting;
  document.getElementById('videoNoticeTitle').textContent = t(videoRefused ? 'options.videoRefusedTitle' : 'options.videoAccessTitle');
  document.getElementById('videoNoticeReason').textContent = t(videoRefused ? 'options.videoRefused' : 'options.videoAccess');
  videoSitesEl.hidden = !videoNow.on;
  videoSitesEl.replaceChildren(...VIDEO_SITES.map(videoSiteRow));
}

function videoSiteRow(site) {
  const row = document.createElement('div');
  row.className = 'glim-row';
  const label = document.createElement('span');
  label.className = 'glim-row-label';
  label.textContent = site.name;
  const on = videoNow.running.includes(site.id);
  const toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.className = 'glim-switch';
  toggle.setAttribute('role', 'switch');
  toggle.setAttribute('aria-checked', String(on));
  toggle.setAttribute('aria-label', site.name);
  const track = document.createElement('span');
  track.className = 'glim-switch-track';
  const knob = document.createElement('span');
  knob.className = 'glim-switch-knob';
  track.append(knob);
  toggle.append(track);
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
  return row;
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

// The popup and the button's own menu change the same switches.
chrome.storage.onChanged.addListener(async (changes, area) => {
  if (area !== 'local') return;
  if (changes.language) {
    await loadLanguage();
    videoText();
  }
  if (changes.language || changes.videoButton || changes.videoButtonHidden) await renderVideo();
});
chrome.permissions.onAdded.addListener(() => void renderVideo());
chrome.permissions.onRemoved.addListener(() => void renderVideo());

(async () => {
  await loadLanguage();
  videoText();
  await renderVideo();
})();
