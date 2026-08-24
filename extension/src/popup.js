// The toolbar button's popup: send the tab that was active when it opened,
// to whichever instance is selected (only shown as a choice once there is
// more than one), or point at Options when none is configured yet.
//
// The square gear badge top-right (jdp: "im fenster oben rechts ein
// quadratischer badge mit zahnrad um die einstellungen zu öffnen") opens
// that same Options page rather than a second settings surface — it
// already holds every setting (instances, default instance, language), so
// this is just a more discoverable door to it than the old full-width
// "Instance settings" button used to be.

const targetEl = document.getElementById('target');
const instanceRow = document.getElementById('instanceRow');
const instanceLabel = document.getElementById('instanceLabel');
const instanceSelect = document.getElementById('instance');
const sendBtn = document.getElementById('send');
const statusEl = document.getElementById('status');
const openOptionsBtn = document.getElementById('openOptions');
openOptionsBtn.addEventListener('click', () => chrome.runtime.openOptionsPage());

let activeTab = null;

(async () => {
  await loadLanguage();
  openOptionsBtn.setAttribute('aria-label', t('common.settings'));
  openOptionsBtn.title = t('common.settings');
  instanceLabel.textContent = t('popup.sendToLabel');
  sendBtn.textContent = t('popup.send');
  targetEl.textContent = t('popup.loading');

  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  activeTab = tab;
  targetEl.textContent = tab?.title || tab?.url || t('popup.noPage');

  const { instances, defaultName } = await readInstances();
  if (instances.length === 0) {
    sendBtn.disabled = true;
    statusEl.textContent = t('popup.noInstance');
    chrome.runtime.openOptionsPage();
    return;
  }
  if (instances.length > 1) {
    instanceRow.hidden = false;
    for (const inst of instances) {
      const opt = document.createElement('option');
      opt.value = inst.url;
      opt.textContent = inst.name;
      if (inst.name === defaultName) opt.selected = true;
      instanceSelect.appendChild(opt);
    }
  }
})();

sendBtn.addEventListener('click', async () => {
  if (!activeTab?.url) return;
  sendBtn.disabled = true;
  statusEl.textContent = t('popup.opening');
  const { instances, defaultName } = await readInstances();
  const origin =
    instances.length > 1 ? instanceSelect.value : (instances.find((i) => i.name === defaultName) ?? instances[0])?.url;
  const url = quickAddUrl(origin, { url: activeTab.url, title: activeTab.title });
  await chrome.windows.create({ url, type: 'popup', width: 420, height: 560 });
  window.close();
});
