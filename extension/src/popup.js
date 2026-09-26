// The toolbar button's popup: sends the active tab to the chosen instance of
// the group, or points at the options page when no phrase is set. The gear
// badge at the end of the header opens the same options page.
//
// The instance list comes live from the relay, hence the loading state: only
// instances that are online are offered.

const targetEl = document.getElementById('target');
const instanceRow = document.getElementById('instanceRow');
const instanceLabelEl = document.getElementById('instanceLabel');
const instanceList = document.getElementById('instanceList');
const sendBtn = document.getElementById('send');
const cancelBtn = document.getElementById('cancelCountdown');
const tabsEl = document.getElementById('tabs');
const paneSendEl = document.getElementById('paneSend');

/**
 * Every button carries a filled glyph beside its label, 14px like the label's
 * text (GlimStone "Icon glyphs" and "The sidebar"), built with createElementNS
 * because Mozilla's linter, a release gate here, fails an innerHTML assignment
 * from a variable. `label()` sets text and glyph together.
 */
const NS = 'http://www.w3.org/2000/svg';
function glyph(d, size = 14, box = '0 0 16 16') {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', box);
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS(NS, 'path');
  path.setAttribute('fill', 'currentColor');
  path.setAttribute('d', d);
  svg.appendChild(path);
  return svg;
}
// The cross is the plus rotated, so the two match.
const G_SEND = 'M1.3 7.1 14.2 1.4c.6-.3 1.2.3.9.9L9.4 15.2c-.3.6-1.1.5-1.3-.1l-1.4-4.3-4.3-1.4c-.6-.2-.7-1-.1-1.3z';
const G_PLUS = 'M7 2h2v5h5v2H9v5H7V9H2V7h5V2z';
const G_CROSS =
  'M4.2 2.8 8 6.6l3.8-3.8 1.4 1.4L9.4 8l3.8 3.8-1.4 1.4L8 9.4l-3.8 3.8-1.4-1.4L6.6 8 2.8 4.2z';
const G_FILE = 'M4 1h5l4 4v9.2A.8.8 0 0 1 12.2 15H4a.8.8 0 0 1-.8-.8V1.8A.8.8 0 0 1 4 1zm5 1.4V5h2.6L9 2.4z';
const G_ADD_INSTANCE = 'M8 1a7 7 0 1 0 0 14A7 7 0 0 0 8 1zm1 6h3v2H9v3H7V9H4V7h3V4h2v3z';
// GlimStone's IconFleet for the other instances, Streamline's
// interface-essential/hierarchy-2.svg (CC BY 4.0), in a box with the margin
// the glyphs above have.
const G_FLEET =
  'M6.5 0.5C5.67157 0.5 5 1.17157 5 2v1c0 0.74325 0.54057 1.36024 1.25 1.47926V6.25H3c-0.9665 0 -1.75 0.7835 -1.75 1.75v1.52074C0.540572 9.63976 0 10.2568 0 11v1c0 0.8284 0.671573 1.5 1.5 1.5h1c0.82843 0 1.5 -0.6716 1.5 -1.5v-1c0 -0.7432 -0.54057 -1.36024 -1.25 -1.47926V8c0 -0.13807 0.11193 -0.25 0.25 -0.25h3.25v1.77074C5.54057 9.63976 5 10.2568 5 11v1c0 0.8284 0.67157 1.5 1.5 1.5h1c0.82843 0 1.5 -0.6716 1.5 -1.5v-1c0 -0.7432 -0.54057 -1.36024 -1.25 -1.47926V7.75H11c0.1381 0 0.25 0.11193 0.25 0.25v1.52074C10.5406 9.63976 10 10.2568 10 11v1c0 0.8284 0.6716 1.5 1.5 1.5h1c0.8284 0 1.5 -0.6716 1.5 -1.5v-1c0 -0.7432 -0.5406 -1.36024 -1.25 -1.47926V8c0 -0.9665 -0.7835 -1.75 -1.75 -1.75H7.75V4.47926C8.45943 4.36024 9 3.74325 9 3V2C9 1.17157 8.32843 0.5 7.5 0.5h-1Z';
const G_FLEET_BOX = '-1 -1 16 16';

function label(btn, text, d) {
  btn.replaceChildren(glyph(d), document.createTextNode(text));
}
const statusEl = document.getElementById('status');
const openOptionsBtn = document.getElementById('openOptions');
openOptionsBtn.addEventListener('click', () => chrome.runtime.openOptionsPage());

let group = [];
let chosen = null;
/** A payload the service worker parked for this window: a Click'n'Load batch,
 *  or a right-clicked link that needed a choice. Null on an ordinary click on
 *  the toolbar button. */
let pending = null;

/**
 * Gives the header and the send button their rainbow positions; the instance
 * cards have their own. Also called at startup, for a popup that never reaches
 * a group.
 */
function paintHues() {
  setHues([document.querySelector('.header'), sendBtn]);
}

(async () => {
  const look = await applyAppearance();
  paintHues();
  // Disco walks here too, or the popup would sit still beside a settings page
  // that moves. The walk writes the root, so the cards drawn later follow.
  applyDisco(look.disco);
  await loadLanguage();
  wireTooltips();
  openOptionsBtn.setAttribute('aria-label', t('common.settings'));
  openOptionsBtn.setAttribute('data-tip', t('common.settings'));
  instanceLabelEl.textContent = t('popup.sendToLabel');
  label(sendBtn, t(sendLabelKey(pending)), G_SEND);
  targetEl.textContent = t('popup.loading');
  targetEl.hidden = false;

  // A send parked by the service worker (a Click'n'Load batch or a right-click
  // that needs a choice) takes precedence over the current tab. It is read
  // once, so a popup closed without choosing cannot resend it later.
  const { pendingSend } = await chrome.storage.session.get('pendingSend');
  await chrome.storage.session.remove('pendingSend');
  pending = pendingSend ?? null;

  if (pending) {
    // Clear the "something is waiting" badge now that the popup is open.
    chrome.action?.setBadgeText?.({ text: '' });
    chrome.action?.setTitle?.({ title: '' });
    // The roster the service worker decided with comes along, so the choice is
    // offered over the same list.
    group = pending.siblings ?? [];
    targetEl.textContent = pending.payload?.title || pending.payload?.url || pending.payload?.text || t('picker.untitled');
    label(sendBtn, t(sendLabelKey(pending)), G_SEND);
    if (group.length === 0) {
      statusEl.textContent = t('popup.noneOnline');
      return;
    }
    await renderTargets(pending.defaultName);
    void loadStatus();
    // Only a Click'n'Load batch counts down; a right-clicked link waits for
    // the user.
    showCollector();
    if (pending.origin === 'cnl') await startCountdown();
    return;
  }

  // For an ordinary click the target line would only repeat the button.
  targetEl.hidden = true;

  if (!(await readPhrase())) {
    // The popup stays open and offers the way to the options page rather
    // than jumping there.
    label(sendBtn, t('popup.addInstance'), G_ADD_INSTANCE);
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

  // The queue readings arrive after the cards are drawn and are not awaited,
  // so sending never waits for them and still works if they fail.
  void loadStatus();
  watchStatus();
  showCollector();
})();

/**
 * Keeps the status line live while the popup is open. A timer here rather than
 * an alarm in the service worker needs no "alarms" permission and stops with
 * the window. A tick is skipped while the previous fetch is still out, so a
 * slow relay cannot pile up requests.
 */
function watchStatus() {
  let busy = false;
  const iv = setInterval(async () => {
    if (busy || document.hidden) return;
    busy = true;
    try {
      await loadStatus();
    } finally {
      busy = false;
    }
  }, 2000);
  addEventListener('pagehide', () => clearInterval(iv), { once: true });
}

/**
 * loadStatus fetches what each instance is doing and redraws once. Until then
 * `status` stays undefined, so the first paint shows no false "offline".
 */
async function loadStatus() {
  let rows;
  try {
    rows = await groupStatus();
  } catch {
    return;
  }
  const byId = new Map(rows.map((r) => [r.instanceId, r.status]));
  // Only for instances already drawn, so a changed roster does not reshuffle
  // the list under the reader.
  group = group.map((g) => (byId.has(g.instanceId) ? { ...g, status: byId.get(g.instanceId) } : g));
  await renderTargets();
}

/**
 * Draws the group as the same cards the options page uses. A single instance
 * is shown too, since the card says where the send goes.
 */
async function renderTargets(preferredFromPending) {
  paintHues();
  const preferred = defaultOf(group, preferredFromPending ?? (await readDefaultTarget()));
  if (!chosen) chosen = preferred;
  instanceRow.hidden = false;
  instanceList.innerHTML = '';
  const cards = group.map((inst, i) => [
    inst.instanceId,
    instanceCard(inst, {
      index: i,
      isDefault: inst.instanceId === preferred,
      isChosen: inst.instanceId === chosen,
      status: inst.status,
      onPick: (picked) => {
        // Choosing another instance is what the countdown leaves room for,
        // so it stops the clock.
        cancelCountdown();
        chosen = picked.instanceId;
        void renderTargets();
      },
      onSetDefault: async (picked) => {
        await writeDefaultTarget(picked.instanceId);
        await renderTargets();
      },
      onQueue: async (picked, halted, el) => {
        const ok = await setQueueHalted(picked.instanceId, halted).catch(() => false);
        statusEl.textContent = ok ? '' : t('options.followFailed');
        // Shakes the pressed control, as the options page does.
        if (!ok) shake(el);
        if (ok) await loadStatus();
      },
      onOpen: (picked, url) => {
        if (url) void chrome.tabs.create({ url });
      },
    }),
  ]);
  staggerRows(arrivals, cards);
  // What an instance is doing arrives after its card, and fades in once. A
  // running line pulses instead, since one element runs one animation.
  cards.forEach(([id, card], i) => {
    if (group[i].status === undefined || statusShown.has(id)) return;
    statusShown.add(id);
    for (const el of card.querySelectorAll('.glim-instance-top .glim-status, .glim-instance-stats:not(.glim-live)')) {
      el.classList.add('glim-content-fade');
    }
  });
  instanceList.append(...cards.map(([, card]) => card));
}

/** When each card first came in, kept across the redraws every two seconds. */
const arrivals = new Map();
/** The instances whose status has been shown once, so it fades in only then. */
const statusShown = new Set();

/**
 * The countdown before a caught Click'n'Load batch sends itself, as in
 * JDownloader's own extension. The user already decided on the site; the popup
 * only shows where the links go and leaves a moment to change that. Any
 * interaction cancels it for good.
 */
let countdownTimer = null;

function cancelCountdown() {
  if (countdownTimer === null) return;
  clearInterval(countdownTimer);
  countdownTimer = null;
  label(sendBtn, t(sendLabelKey(pending)), G_SEND);
  cancelBtn.hidden = true;
}

async function startCountdown() {
  const seconds = await readCnlCountdown();
  // Zero means ask; without a chosen instance there is nothing to send.
  if (seconds <= 0 || !chosen) return;
  let left = seconds;
  const paint = () => {
    label(sendBtn, t('popup.sendIn', { n: String(left) }), G_SEND);
  };
  paint();
  // The cancel button shows only while the clock runs.
  label(cancelBtn, t('popup.cancel'), G_CROSS);
  cancelBtn.hidden = false;
  countdownTimer = setInterval(() => {
    left -= 1;
    if (left > 0) {
      paint();
      return;
    }
    cancelCountdown();
    sendBtn.click();
  }, 1000);
}

cancelBtn.addEventListener('click', () => cancelCountdown());

/**
 * The popup's two tabs, instances and the link collector, in the same well
 * selector the options page and the app use. The strip shows only once there
 * is a group.
 */
let pane = 'send';

function renderTabs() {
  if (group.length === 0) {
    tabsEl.hidden = true;
    return;
  }
  tabsEl.hidden = false;
  tabsEl.innerHTML = '';
  for (const [value, label, d, box] of [
    ['send', t('popup.tabInstances'), G_FLEET, G_FLEET_BOX],
    ['collector', t('popup.tabCollector')],
  ]) {
    const b = document.createElement('button');
    b.type = 'button';
    if (d) b.replaceChildren(glyph(d, 14, box), document.createTextNode(label));
    else b.textContent = label;
    b.setAttribute('aria-pressed', String(value === pane));
    b.addEventListener('click', () => {
      cancelCountdown();
      if (value === pane) return;
      const incoming = value === 'send' ? paneSendEl : collectorEl;
      // The pane slides in from the side its tab lies on, the collector's
      // being the later one.
      incoming.style.setProperty('--tab-dir', value === 'collector' ? '1' : '-1');
      pane = value;
      showPane();
      replay(incoming, 'glim-tab-slide');
    });
    tabsEl.appendChild(b);
  }
}

function showPane() {
  paneSendEl.hidden = pane !== 'send';
  collectorEl.hidden = pane !== 'collector' || group.length === 0;
  renderTabs();
}

sendBtn.addEventListener('click', async () => {
  cancelCountdown();
  // Without a phrase this button only opens the options page, so stop before
  // the tab is read.
  if (!chosen) return;
  // The tab is read on the press, not when the popup opens, as the privacy
  // policy says.
  const payload = pending ? pending.payload : await currentTabPayload();
  if (!payload) return;
  sendBtn.disabled = true;
  // No "sending" line: the window closes at once and the toolbar badge
  // reports the outcome.
  await handOver({ type: 'knightloader-send-to', target: chosen, payload });
});

/** The page this popup was opened over, as a send payload, or null when the
 *  browser gives no address for it (an internal page, or site access withheld). */
async function currentTabPayload() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab?.url ? { url: tab.url, title: tab.title } : null;
}

/**
 * Hands one send to the service worker, then closes the window.
 *
 * The await covers the hand-over, not the delivery, which the toolbar badge
 * reports. Closing right after sendMessage lost sends whenever the worker was
 * asleep and had not started yet. The worker answers at once, so this never
 * waits for the relay.
 */
async function handOver(message) {
  try {
    await chrome.runtime.sendMessage(message);
  } catch {
    // The worker could not be reached; closing still beats a frozen popup.
  }
  window.close();
}

// The link collector: paste, drop or pick files, and the links go to the chosen
// instance. Files are read for their text rather than uploaded, since the relay
// carries links and the lists people drop (.txt, .dlc, .crawljob) are text. A
// binary yields no links and is reported as such.
const collectorEl = document.getElementById('collector');
const collectorLabelEl = document.getElementById('collectorLabel');
const dropEl = document.getElementById('drop');
const linksEl = document.getElementById('links');
const addLinksBtn = document.getElementById('addLinks');
const pickFilesBtn = document.getElementById('pickFiles');
const filesEl = document.getElementById('files');

/** Every http(s)/ftp URL in a text, in order and without duplicates, split the
 *  way splitCnlLinks splits. */
function linksIn(text) {
  const seen = new Set();
  for (const raw of String(text).split(/[\r\n\s]+/)) {
    const s = raw.trim();
    if (/^(https?|ftp):\/\//i.test(s)) seen.add(s);
  }
  return [...seen];
}

// Fills the collector's labels; showPane alone decides whether it is visible.
function showCollector() {
  collectorLabelEl.textContent = t('popup.collectorLabel');
  linksEl.placeholder = t('popup.collectorPlaceholder');
  label(addLinksBtn, t('popup.collectorAdd'), G_PLUS);
  label(pickFilesBtn, t('popup.collectorFiles'), G_FILE);
  showPane();
}

// Without preventing dragover the drop never fires, and the browser would
// navigate to a dragged link.
for (const ev of ['dragenter', 'dragover']) {
  dropEl.addEventListener(ev, (e) => {
    e.preventDefault();
    dropEl.classList.add('over');
  });
}
for (const ev of ['dragleave', 'drop']) {
  dropEl.addEventListener(ev, () => dropEl.classList.remove('over'));
}

dropEl.addEventListener('drop', async (e) => {
  e.preventDefault();
  cancelCountdown();
  const parts = [];
  // A dragged link arrives as text, a dragged file as a file.
  const dropped = e.dataTransfer?.getData('text') ?? '';
  if (dropped) parts.push(dropped);
  for (const file of e.dataTransfer?.files ?? []) parts.push(await file.text().catch(() => ''));
  appendToBox(parts.join('\n'), dropEl);
});

pickFilesBtn.addEventListener('click', () => {
  cancelCountdown();
  filesEl.click();
});

filesEl.addEventListener('change', async () => {
  const parts = [];
  for (const file of filesEl.files ?? []) parts.push(await file.text().catch(() => ''));
  // Cleared so picking the same file again fires 'change'.
  filesEl.value = '';
  appendToBox(parts.join('\n'), pickFilesBtn);
});

/**
 * Appends dropped or picked links to the box; two drops are two batches. A
 * drop or a pick without a link shakes `from`, where it came in.
 */
function appendToBox(text, from) {
  const found = linksIn(text);
  if (found.length === 0) {
    statusEl.textContent = t('popup.collectorNoLinks');
    shake(from);
    return;
  }
  statusEl.textContent = '';
  const have = linksEl.value.trim();
  linksEl.value = (have ? have + '\n' : '') + found.join('\n');
}

addLinksBtn.addEventListener('click', async () => {
  cancelCountdown();
  const found = linksIn(linksEl.value);
  if (found.length === 0) {
    statusEl.textContent = t('popup.collectorNoLinks');
    shake(addLinksBtn);
    return;
  }
  if (!chosen) return;
  // Same message as every other send, so the badge reports this one too.
  addLinksBtn.disabled = true;
  await handOver({
    type: 'knightloader-send-to',
    target: chosen,
    payload: { text: found.join('\n'), title: t('popup.collectorPackage') },
  });
});
