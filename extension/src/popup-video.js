// The popup's switch for the download button on a video's player, the stored
// switch the options page shows too (video.js), so the button is one click
// away. Off takes it out of every open tab at once. On needs the browser's
// access to the sites; where none is granted yet the options page asks for
// it, since a permission prompt closes this window and the request with it.

const videoRowEl = document.getElementById('videoRow');
const videoSwitchEl = document.getElementById('videoEnabled');
let videoNow = null;

async function renderVideoSwitch() {
  videoNow = await videoButtonState();
  document.getElementById('videoLabel').textContent = t('options.videoToggle');
  videoSwitchEl.setAttribute('aria-label', t('options.videoToggle'));
  videoSwitchEl.setAttribute('aria-checked', String(videoNow.on));
  videoRowEl.hidden = false;
}

videoSwitchEl.addEventListener('click', async () => {
  if (!videoNow) return;
  if (videoNow.on) {
    await chrome.storage.local.set({ videoButton: false });
    await renderVideoSwitch();
    return;
  }
  // Hidden on every site, switching it on shows it everywhere again.
  const sites = videoNow.shown.length ? videoNow.shown : VIDEO_SITES.map((s) => s.id);
  await chrome.storage.local.set({ videoButton: true, ...(videoNow.shown.length ? {} : { videoButtonHidden: [] }) });
  if (!sites.some((id) => videoNow.granted.includes(id))) {
    await chrome.tabs.create({ url: chrome.runtime.getURL('options.html#video') });
    window.close();
    return;
  }
  await renderVideoSwitch();
});

(async () => {
  await loadLanguage();
  await renderVideoSwitch();
})();
