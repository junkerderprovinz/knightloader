// The toolbar button's popup: send the tab that was active when it opened,
// or point at Options when no instance is configured yet.

const targetEl = document.getElementById('target');
const sendBtn = document.getElementById('send');
const statusEl = document.getElementById('status');
document.getElementById('openOptions').addEventListener('click', () => chrome.runtime.openOptionsPage());

let activeTab = null;

(async () => {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  activeTab = tab;
  targetEl.textContent = tab?.title || tab?.url || 'No page found in this window.';

  const origin = await readInstanceUrl();
  if (!origin) {
    sendBtn.disabled = true;
    statusEl.textContent = 'No instance configured yet — opening settings.';
    chrome.runtime.openOptionsPage();
  }
})();

sendBtn.addEventListener('click', async () => {
  if (!activeTab?.url) return;
  sendBtn.disabled = true;
  statusEl.textContent = 'Opening KnightLoader…';
  const origin = await readInstanceUrl();
  const url = quickAddUrl(origin, { url: activeTab.url, title: activeTab.title });
  await chrome.windows.create({ url, type: 'popup', width: 420, height: 560 });
  window.close();
});
