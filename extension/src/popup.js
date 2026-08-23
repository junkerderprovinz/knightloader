// The toolbar button's popup: send the tab that was active when it opened,
// to whichever instance is selected (only shown as a choice once there is
// more than one), or point at Options when none is configured yet.

const targetEl = document.getElementById('target');
const instanceRow = document.getElementById('instanceRow');
const instanceSelect = document.getElementById('instance');
const sendBtn = document.getElementById('send');
const statusEl = document.getElementById('status');
document.getElementById('openOptions').addEventListener('click', () => chrome.runtime.openOptionsPage());

let activeTab = null;

(async () => {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  activeTab = tab;
  targetEl.textContent = tab?.title || tab?.url || 'No page found in this window.';

  const { instances, defaultName } = await readInstances();
  if (instances.length === 0) {
    sendBtn.disabled = true;
    statusEl.textContent = 'No instance configured yet — opening settings.';
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
  statusEl.textContent = 'Opening KnightLoader…';
  const { instances, defaultName } = await readInstances();
  const origin =
    instances.length > 1 ? instanceSelect.value : (instances.find((i) => i.name === defaultName) ?? instances[0])?.url;
  const url = quickAddUrl(origin, { url: activeTab.url, title: activeTab.title });
  await chrome.windows.create({ url, type: 'popup', width: 420, height: 560 });
  window.close();
});
