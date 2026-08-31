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
const cancelBtn = document.getElementById('cancelCountdown');
const tabsEl = document.getElementById('tabs');
const paneSendEl = document.getElementById('paneSend');
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
  targetEl.hidden = false;

  // A send that is already waiting takes precedence over the current tab. This
  // is how a Click'n'Load button or a right-click reaches a choice now: the
  // service worker parks the payload and opens THIS window, rather than
  // creating a second window with its own title bar and taskbar entry (jdp,
  // 2026-08-29: "es soll sich das popupfenster der erweiterung öffnen").
  //
  // Read-once: a stale entry from a popup somebody closed without choosing
  // must never resurface and send the wrong links on the next toolbar click.
  const { pendingSend } = await chrome.storage.session.get('pendingSend');
  await chrome.storage.session.remove('pendingSend');
  pending = pendingSend ?? null;

  if (pending) {
    // The badge said "something is waiting" if the popup could not be opened
    // by itself. This window IS that popup, so the mark has done its job.
    chrome.action?.setBadgeText?.({ text: '' });
    chrome.action?.setTitle?.({ title: '' });
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
    // Only a caught Click'n'Load batch counts itself down. A right-clicked
    // link was a deliberate act aimed at one thing, and finishing it for
    // somebody after five seconds would be the surprise, not the service.
    showCollector();
    if (pending.origin === 'cnl') await startCountdown();
    return;
  }

  // No target line for an ordinary toolbar click (jdp, 2026-08-29: "im popup
  // steht KnightLoader wieder zweimal. das zweite (untere) entfernen"). On the
  // extension's own pages it repeated the heading word for word, and even
  // elsewhere it only restated what the button underneath already says. Where
  // it DOES carry something nothing else does - a parked Click'n'Load batch,
  // a right-clicked link - it stays, above.
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  activeTab = tab;
  targetEl.hidden = true;

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
  showCollector();
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
          // Taking hold of the window stops the clock: somebody choosing a
          // different instance is the one case the countdown exists to leave
          // room for, and finishing on a timer behind them would undo it.
          cancelCountdown();
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

/**
 * The countdown a caught Click'n'Load batch runs before it sends itself.
 *
 * This is what JDownloader's own extension does, and what this one did not
 * (jdp, 2026-08-30: "countdown zeigt es nicht an und man muss manuell auf den
 * senden button klicken"). A container button is a decision somebody already
 * made on the site; the popup exists to say WHERE it is going and to give a
 * moment to change that, not to ask for the same decision a second time.
 *
 * Cancelled by touching anything: picking a different instance, or pressing
 * the button, which sends immediately. Cancelled and not merely paused - once
 * somebody has taken hold of this window, finishing on a timer behind them is
 * exactly the surprise the timer is supposed to avoid.
 */
let countdownTimer = null;

function cancelCountdown() {
  if (countdownTimer === null) return;
  clearInterval(countdownTimer);
  countdownTimer = null;
  sendBtn.textContent = t('popup.send');
  // The cancel goes with the clock it stops. A button that stops something not
  // happening is a button that has to be explained.
  cancelBtn.hidden = true;
}

async function startCountdown() {
  const seconds = await readCnlCountdown();
  // Zero is "ask me", the setting's own off position. Also skipped when there
  // is nothing chosen to send to, which would make the timer a countdown to a
  // no-op.
  if (seconds <= 0 || !chosen) return;
  let left = seconds;
  const paint = () => {
    sendBtn.textContent = t('popup.sendIn', { n: String(left) });
  };
  paint();
  // Visible only while the clock runs (jdp, 2026-08-31: "Dort fehlt auch ein
  // Abbrechen button wenn ein links reingeladen wird und der countdown läuft").
  // Until now the only way to stop it was to pick a different instance, which
  // is a side effect of another action rather than a way to say no.
  cancelBtn.textContent = t('popup.cancel');
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
 * The two halves of this window, one visible at a time (jdp, 2026-08-31:
 * "Können wir im popupfenster zwei tabs machen wie inder app? Instanzen und
 * Linksammler?").
 *
 * It was one column: instances, send, and then a collector far enough down that
 * a window opened for one of them buried the other. The same well selector the
 * options page and the app both use, so the three surfaces agree on what a
 * chooser looks like.
 *
 * The strip appears only once there IS a group. With nothing to send to, two
 * labels over an empty page are a choice between two kinds of nothing.
 */
let pane = 'send';

function renderTabs() {
  if (group.length === 0) {
    tabsEl.hidden = true;
    return;
  }
  tabsEl.hidden = false;
  tabsEl.innerHTML = '';
  for (const [value, label] of [
    ['send', t('popup.tabInstances')],
    ['collector', t('popup.tabCollector')],
  ]) {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = label;
    b.setAttribute('aria-pressed', String(value === pane));
    b.addEventListener('click', () => {
      // Switching away from a running clock stops it, for the same reason
      // picking a different instance does: taking hold of the window is the one
      // case the countdown exists to leave room for.
      cancelCountdown();
      pane = value;
      showPane();
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

// --- The collector -----------------------------------------------------
//
// Everything above sends ONE thing that something else chose: the current tab,
// a right-clicked link, a caught container. This is the other direction (jdp,
// 2026-08-30: "unter den instanzencards soll eine linksammler cards sein mit
// dropzone für links und button um dateien hinzuzufügen") - paste, drop, or
// pick files, and it goes to whichever instance the cards above have selected.
//
// Files are read for their TEXT, not uploaded. A browser extension cannot hand
// a file to an instance it reaches through an encrypted relay frame, and it
// does not need to: what people drop here are .txt/.dlc/.crawljob lists, and
// what the instance wants is the links inside them. A binary dropped by
// mistake yields no http(s)/ftp lines and is reported as such rather than
// posted as noise.
const collectorEl = document.getElementById('collector');
const collectorLabelEl = document.getElementById('collectorLabel');
const dropEl = document.getElementById('drop');
const linksEl = document.getElementById('links');
const addLinksBtn = document.getElementById('addLinks');
const pickFilesBtn = document.getElementById('pickFiles');
const filesEl = document.getElementById('files');

/** Every http(s)/ftp URL in a blob of text, in order, without duplicates. The
 *  same shape splitCnlLinks uses, so a list pasted here and a list caught from
 *  a container are read the same way. */
function linksIn(text) {
  const seen = new Set();
  for (const raw of String(text).split(/[\r\n\s]+/)) {
    const s = raw.trim();
    if (/^(https?|ftp):\/\//i.test(s)) seen.add(s);
  }
  return [...seen];
}

// Fills the collector's own labels. Whether it is ON SCREEN is showPane's
// decision now, not this function's: two places setting `hidden` on the same
// element is how one of them silently wins.
function showCollector() {
  collectorLabelEl.textContent = t('popup.collectorLabel');
  linksEl.placeholder = t('popup.collectorPlaceholder');
  addLinksBtn.textContent = t('popup.collectorAdd');
  pickFilesBtn.textContent = t('popup.collectorFiles');
  showPane();
}

// dragover must be prevented or the drop never fires - the browser's default
// for a dragged link is to navigate to it, which would take the popup with it.
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
  // A dragged link arrives as text; a dragged file arrives as a file. Both are
  // read, because both are things people drag onto a box like this.
  const dropped = e.dataTransfer?.getData('text') ?? '';
  if (dropped) parts.push(dropped);
  for (const file of e.dataTransfer?.files ?? []) parts.push(await file.text().catch(() => ''));
  appendToBox(parts.join('\n'));
});

pickFilesBtn.addEventListener('click', () => {
  cancelCountdown();
  filesEl.click();
});

filesEl.addEventListener('change', async () => {
  const parts = [];
  for (const file of filesEl.files ?? []) parts.push(await file.text().catch(() => ''));
  // Cleared so picking the SAME file twice in a row fires 'change' again; a
  // file input holds its value otherwise and the second pick does nothing.
  filesEl.value = '';
  appendToBox(parts.join('\n'));
});

/** Adds what was dropped or picked to whatever is already in the box, rather
 *  than replacing it: two drops in a row are two batches, not a correction. */
function appendToBox(text) {
  const found = linksIn(text);
  if (found.length === 0) {
    statusEl.textContent = t('popup.collectorNoLinks');
    return;
  }
  statusEl.textContent = '';
  const have = linksEl.value.trim();
  linksEl.value = (have ? have + '\n' : '') + found.join('\n');
}

addLinksBtn.addEventListener('click', () => {
  cancelCountdown();
  const found = linksIn(linksEl.value);
  if (found.length === 0) {
    statusEl.textContent = t('popup.collectorNoLinks');
    return;
  }
  if (!chosen) return;
  // The same message every other send in this window uses, so a batch pasted
  // here takes the identical path through the service worker - including the
  // badge that reports whether it arrived.
  chrome.runtime.sendMessage({
    type: 'knightloader-send-to',
    target: chosen,
    payload: { text: found.join('\n'), title: t('popup.collectorPackage') },
  });
  window.close();
});
