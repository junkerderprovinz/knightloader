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
/** A payload the service worker parked for this window: a Click'n'Load batch,
 *  or a right-clicked link that needed a choice. Null on an ordinary click on
 *  the toolbar button. */
let pending = null;

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

  // A send that is already waiting takes precedence over the current tab. This
  // is how a Click'n'Load button or a right-click reaches a choice now: the
  // service worker parks the payload and opens THIS window, rather than
  // creating a second window with its own title bar and taskbar entry (jdp,
  // 2026-08-29: "es soll sich das popupfenster der erweiterung öffnen").
  //
  // Read-once, exactly as picker.html does it: a stale entry from a popup
  // somebody closed without choosing must never resurface and send the wrong
  // links on the next toolbar click.
  const { pendingSend } = await chrome.storage.session.get('pendingSend');
  await chrome.storage.session.remove('pendingSend');
  pending = pendingSend ?? null;

  if (pending) {
    // The roster came WITH the payload - the service worker had just listed
    // the group to decide whether a choice was needed at all, and asking again
    // here would be a second chance to get a different answer.
    group = pending.siblings ?? [];
    targetEl.textContent = pending.payload?.title || pending.payload?.url || pending.payload?.text || t('picker.untitled');
    if (group.length === 0) {
      statusEl.textContent = t('popup.noneOnline');
      return;
    }
    await renderTargets(pending.defaultName);
    void loadStatus();
    return;
  }

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

  // The queue readings come AFTER the cards are on screen, never before them
  // (jdp asked for the controls here too, 2026-08-29: "auch ins popup"). Three
  // reads per instance is six answers to wait on with two instances, and this
  // window has one job that must not queue behind them. So the cards appear
  // with their names, their badge and their controls immediately, and the
  // status line fills itself in a moment later.
  //
  // Not awaited on purpose: a failure here leaves a popup that can still send,
  // which is the whole point of splitting it out.
  void loadStatus();
})();

/**
 * loadStatus fetches what each instance is doing and redraws once.
 *
 * `status` stays undefined until this lands, and the card reads that as "the
 * caller did not ask" rather than "asked and got nothing" - so the first paint
 * carries no empty line and no wrong "offline".
 */
async function loadStatus() {
  let rows;
  try {
    rows = await groupStatus();
  } catch {
    return;
  }
  const byId = new Map(rows.map((r) => [r.instanceId, r.status]));
  // Only for instances that are still in the list this popup drew. A roster
  // that changed underneath is not worth a second surprise redraw in a window
  // somebody is already reading.
  group = group.map((g) => (byId.has(g.instanceId) ? { ...g, status: byId.get(g.instanceId) } : g));
  await renderTargets();
}

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
async function renderTargets(preferredFromPending) {
  paintHues();
  const preferred = defaultOf(group, preferredFromPending ?? (await readDefaultTarget()));
  if (!chosen) chosen = preferred;
  instanceRow.hidden = false;
  instanceList.innerHTML = '';
  group.forEach((inst, i) => {
    instanceList.appendChild(
      instanceCard(inst, {
        index: i,
        isDefault: inst.instanceId === preferred,
        isChosen: inst.instanceId === chosen,
        status: inst.status,
        onPick: (picked) => {
          chosen = picked.instanceId;
          void renderTargets();
        },
        onSetDefault: async (picked) => {
          await writeDefaultTarget(picked.instanceId);
          await renderTargets();
        },
        onQueue: async (picked, halted) => {
          const ok = await setQueueHalted(picked.instanceId, halted).catch(() => false);
          statusEl.textContent = ok ? '' : t('options.followFailed');
          if (ok) await loadStatus();
        },
        onOpen: (picked, url) => {
          if (url) void chrome.tabs.create({ url });
        },
      }),
    );
  });
}

sendBtn.addEventListener('click', async () => {
  // Either a payload the service worker parked here, or the tab this window
  // opened over. Never both, and never neither.
  const payload = pending ? pending.payload : activeTab?.url ? { url: activeTab.url, title: activeTab.title } : null;
  if (!payload || !chosen) return;
  sendBtn.disabled = true;
  // No "sending…" line (jdp: "der Text 'Wird gesendet' kann weg"). This window
  // closes on the next line, so the sentence would flash for a frame and then
  // be gone — and the toolbar badge is what actually reports the outcome.
  chrome.runtime.sendMessage({ type: 'knightloader-send-to', target: chosen, payload });
  // Closed straight away rather than waiting for the answer: the send happens
  // in the service worker and outlives this window, and the toolbar badge is
  // what reports it either way (background.js's flashBadge). Waiting here would
  // hold a popup open on a spinner for a result it is not the right place to
  // show.
  window.close();
});
