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

const targetLabelEl = document.getElementById('targetLabel');
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
  // Before anything is drawn: the look goes on <html> first, so no page is
  // ever painted in one look and repainted in another.
  await applyAppearance();
  await loadLanguage();
  openOptionsBtn.setAttribute('aria-label', t('common.settings'));
  openOptionsBtn.title = t('common.settings');
  instanceLabel.textContent = t('popup.sendToLabel');
  sendBtn.textContent = t('popup.send');
  targetLabelEl.textContent = t('popup.targetLabel');
  targetEl.textContent = t('popup.loading');

  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  activeTab = tab;
  targetEl.textContent = tab?.title || tab?.url || t('popup.noPage');

  const { instances, defaultName } = await readInstances();
  if (instances.length === 0) {
    // The popup used to open Options here and close itself, so a click on the
    // toolbar icon never showed the popup at all - it hijacked the click and
    // took you somewhere you had not asked to go (jdp: "wenn ich auf das
    // erweiterungsicon im browser klicke öffnet es sofort die einstellungen.
    // es soll aber nur das popupfenster öffnen"). The popup stays open and
    // offers the way there instead: the same destination, reached on purpose.
    sendBtn.textContent = t('popup.addInstance');
    sendBtn.disabled = false;
    sendBtn.onclick = () => chrome.runtime.openOptionsPage();
    statusEl.textContent = t('popup.noInstance');
    return;
  }
  if (instances.length > 1) {
    instanceRow.hidden = false;
    for (const inst of instances) {
      const opt = document.createElement('option');
      // Keyed by name, not URL: an entry reached through a forwarder has no
      // URL of its own (issue #27, see entryTarget in shared.js).
      opt.value = inst.name;
      opt.textContent = entryLabel(inst);
      opt.disabled = !entryTarget(inst);
      // Never preselect an option that cannot be sent to - same reasoning as
      // picker.js: a selection the Send button cannot act on is worse than no
      // selection, because the button still looks ready.
      if (!opt.disabled && inst.name === defaultName) opt.selected = true;
      instanceSelect.appendChild(opt);
    }
  }
})();

sendBtn.addEventListener('click', async () => {
  if (!activeTab?.url) return;
  sendBtn.disabled = true;
  statusEl.textContent = t('popup.opening');
  const { instances, defaultName } = await readInstances();
  const picked =
    instances.length > 1
      ? instances.find((i) => i.name === instanceSelect.value)
      : (instances.find((i) => i.name === defaultName) ?? instances[0]);
  const target = entryTarget(picked ?? {});
  if (!target) {
    // Said out loud rather than opening a window on nothing - this is the
    // exact failure issue #27 was filed about.
    statusEl.textContent = t('popup.unreachable');
    sendBtn.disabled = false;
    return;
  }
  const url = quickAddUrl(target.origin, { url: activeTab.url, title: activeTab.title, to: target.to });
  await chrome.windows.create({ url, type: 'popup', width: 420, height: 560 });
  window.close();
});
