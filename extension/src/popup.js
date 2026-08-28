// The toolbar button's popup: send the tab that was active when it opened, to
// whichever instance in your group is selected (only shown as a choice once
// there is more than one), or point at Options when no phrase has been entered
// yet.
//
// The square gear badge top-right (jdp: "im fenster oben rechts ein
// quadratischer badge mit zahnrad um die einstellungen zu öffnen") opens that
// same Options page rather than a second settings surface — it already holds
// every setting (the phrase, the default instance, the language), so this is
// just a more discoverable door to it than the old full-width "Instance
// settings" button used to be.
//
// The instance list is now read LIVE from the relay rather than from storage,
// which is why this page has a loading state it did not have before: an
// instance that is switched off is not offered, and one that came online a
// minute ago is, without anybody telling this browser anything.

const targetLabelEl = document.getElementById('targetLabel');
const targetEl = document.getElementById('target');
const instanceRow = document.getElementById('instanceRow');
const instanceLabelEl = document.getElementById('instanceLabel');
const instanceSelect = document.getElementById('instance');
const sendBtn = document.getElementById('send');
const statusEl = document.getElementById('status');
const openOptionsBtn = document.getElementById('openOptions');
openOptionsBtn.addEventListener('click', () => chrome.runtime.openOptionsPage());

let activeTab = null;
let group = [];

(async () => {
  // Before anything is drawn: the look goes on <html> first, so no page is
  // ever painted in one look and repainted in another.
  await applyAppearance();
  await loadLanguage();
  wireTooltips();
  openOptionsBtn.setAttribute('aria-label', t('common.settings'));
  openOptionsBtn.setAttribute('data-tip', t('common.settings'));
  instanceLabelEl.textContent = t('popup.sendToLabel');
  sendBtn.textContent = t('popup.send');
  targetLabelEl.textContent = t('popup.targetLabel');
  targetEl.textContent = t('popup.loading');

  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  activeTab = tab;
  targetEl.textContent = tab?.title || tab?.url || t('popup.noPage');

  if (!(await readPhrase())) {
    // The popup used to open Options here and close itself, so a click on the
    // toolbar icon never showed the popup at all — it hijacked the click and
    // took you somewhere you had not asked to go (jdp: "wenn ich auf das
    // erweiterungsicon im browser klicke öffnet es sofort die einstellungen.
    // es soll aber nur das popupfenster öffnen"). The popup stays open and
    // offers the way there instead: the same destination, reached on purpose.
    sendBtn.textContent = t('popup.addInstance');
    sendBtn.onclick = () => chrome.runtime.openOptionsPage();
    statusEl.textContent = t('popup.noInstance');
    return;
  }

  sendBtn.disabled = true;
  statusEl.textContent = t('popup.findingGroup');
  try {
    group = await groupInstances();
  } catch {
    statusEl.textContent = t('popup.relayFailed');
    return;
  }
  if (group.length === 0) {
    statusEl.textContent = t('popup.noneOnline');
    return;
  }
  statusEl.textContent = '';
  sendBtn.disabled = false;
  if (group.length > 1) {
    instanceRow.hidden = false;
    const preferred = await readDefaultTarget();
    for (const inst of group) {
      const opt = document.createElement('option');
      // Keyed by the relay instance id, not the name: a group whose members
      // were renamed keeps pointing at the same machine.
      opt.value = inst.instanceId;
      opt.textContent = instanceLabel(inst);
      if (inst.instanceId === preferred) opt.selected = true;
      instanceSelect.appendChild(opt);
    }
  }
})();

sendBtn.addEventListener('click', async () => {
  if (!activeTab?.url || group.length === 0) return;
  sendBtn.disabled = true;
  statusEl.textContent = t('popup.sending');
  const target = group.length > 1 ? instanceSelect.value : group[0].instanceId;
  chrome.runtime.sendMessage({
    type: 'knightloader-send-to',
    target,
    payload: { url: activeTab.url, title: activeTab.title },
  });
  // Closed straight away rather than waiting for the answer: the send happens
  // in the service worker and outlives this window, and the toolbar badge is
  // what reports it either way (background.js's flashBadge). Waiting here would
  // hold a popup open on a spinner for a result it is not the right place to
  // show.
  window.close();
});
