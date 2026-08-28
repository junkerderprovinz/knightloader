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

const targetEl = document.getElementById('target');
const instanceRow = document.getElementById('instanceRow');
const instanceLabelEl = document.getElementById('instanceLabel');
const instanceList = document.getElementById('instanceList');
const sendBtn = document.getElementById('send');
const statusEl = document.getElementById('status');
const openOptionsBtn = document.getElementById('openOptions');
openOptionsBtn.addEventListener('click', () => chrome.runtime.openOptionsPage());

let activeTab = null;
let group = [];
let chosen = null;

/**
 * This window's own equal-member set: the header block (mark, name and the
 * gear badge inside it) and the send button. The instance cards carry their
 * own run, which is why they are not in this list.
 *
 * Called on every render AND once at startup, because a popup that never
 * reaches a group still has a header and a button, and they should wear the
 * mode too (jdp, 2026-08-29: "der Regenbogenmodus funktioniert erweiterungs-weit
 * nicht überall").
 */
function paintHues() {
  setHues([document.querySelector('.header'), sendBtn]);
}

(async () => {
  // Before anything is drawn: the look goes on <html> first, so no page is
  // ever painted in one look and repainted in another.
  await applyAppearance();
  paintHues();
  await loadLanguage();
  wireTooltips();
  openOptionsBtn.setAttribute('aria-label', t('common.settings'));
  openOptionsBtn.setAttribute('data-tip', t('common.settings'));
  instanceLabelEl.textContent = t('popup.sendToLabel');
  sendBtn.textContent = t('popup.send');
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
  await renderTargets();
})();

/**
 * The group as cards, the same object the options page and the send-to window
 * draw (jdp, 2026-08-28: "Im erweiterungsfenster sollen die Instanzen auch als
 * cards erscheinen. Gleich wie im instanzentab.").
 *
 * A dropdown stood here before. It could not show the mark, could not carry a
 * badge, and could not be right-clicked — three things this window now does,
 * and none of them worth a bespoke control when the card already exists.
 *
 * Shown even for a single instance: the card is also what says WHERE this is
 * about to go, and a popup that hides that until there is a choice to make is a
 * popup that tells you least when you know least.
 */
async function renderTargets() {
  paintHues();
  const preferred = defaultOf(group, await readDefaultTarget());
  if (!chosen) chosen = preferred;
  instanceRow.hidden = false;
  instanceList.innerHTML = '';
  group.forEach((inst, i) => {
    instanceList.appendChild(
      instanceCard(inst, {
        index: i,
        isDefault: inst.instanceId === preferred,
        isChosen: inst.instanceId === chosen,
        onPick: (picked) => {
          chosen = picked.instanceId;
          void renderTargets();
        },
        onSetDefault: async (picked) => {
          await writeDefaultTarget(picked.instanceId);
          await renderTargets();
        },
      }),
    );
  });
}

sendBtn.addEventListener('click', async () => {
  if (!activeTab?.url || !chosen) return;
  sendBtn.disabled = true;
  // No "sending…" line (jdp: "der Text 'Wird gesendet' kann weg"). This window
  // closes on the next line, so the sentence would flash for a frame and then
  // be gone — and the toolbar badge is what actually reports the outcome.
  chrome.runtime.sendMessage({
    type: 'knightloader-send-to',
    target: chosen,
    payload: { url: activeTab.url, title: activeTab.title },
  });
  // Closed straight away rather than waiting for the answer: the send happens
  // in the service worker and outlives this window, and the toolbar badge is
  // what reports it either way (background.js's flashBadge). Waiting here would
  // hold a popup open on a spinner for a result it is not the right place to
  // show.
  window.close();
});
