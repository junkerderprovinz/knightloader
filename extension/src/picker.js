// Opened by background.js's sendToInstance() only when more than one instance
// in the group is online — the JD-style "the extension pops up and asks where"
// moment (jdp, 2026-08-23) for context-menu and Click'n'Load sends, which
// otherwise bypass popup.html entirely and had no place to offer that choice.
//
// The roster arrives WITH the payload rather than being fetched again here.
// background.js has just listed the group to decide whether this window was
// needed at all, and a second relay connection to ask the same question would
// be a second chance to get a different answer — an instance appearing in the
// list that was not there when the decision to show this window was made.

const headingEl = document.getElementById('heading');
const targetEl = document.getElementById('target');
const choicesEl = document.getElementById('choices');
const sendBtn = document.getElementById('send');
const cancelBtn = document.getElementById('cancel');

let payload = null;
let siblings = [];
let preferred = null;
let chosen = null;

(async () => {
  // Before anything is drawn: the look goes on <html> first, so no page is
  // ever painted in one look and repainted in another.
  await applyAppearance();
  await loadLanguage();
  wireTooltips();
  headingEl.textContent = t('picker.title');
  targetEl.textContent = t('picker.loading');
  sendBtn.textContent = t('picker.send');
  cancelBtn.textContent = t('picker.cancel');

  const { pendingSend } = await chrome.storage.session.get('pendingSend');
  // Read-once: a stale entry from a window the user closed without choosing
  // must never resurface and send the wrong link on the next click.
  await chrome.storage.session.remove('pendingSend');
  if (!pendingSend) {
    targetEl.textContent = t('picker.nothing');
    sendBtn.disabled = true;
    return;
  }
  payload = pendingSend.payload;
  targetEl.textContent = payload.title || payload.url || payload.text || t('picker.untitled');

  siblings = pendingSend.siblings ?? [];
  preferred = defaultOf(siblings, pendingSend.defaultName);
  // A remembered default that is no longer in the group must not leave the
  // dialog with nothing selected and a Send button that quietly does nothing.
  chosen = preferred;
  if (!chosen) sendBtn.disabled = true;
  render();
})();

/** The same card the popup and the options page draw — see shared.js. */
function render() {
  choicesEl.innerHTML = '';
  siblings.forEach((inst, i) => {
    choicesEl.appendChild(
      instanceCard(inst, {
        index: i,
        isDefault: inst.instanceId === preferred,
        isChosen: inst.instanceId === chosen,
        onPick: (picked) => {
          chosen = picked.instanceId;
          render();
        },
        onSetDefault: async (picked) => {
          await writeDefaultTarget(picked.instanceId);
          preferred = picked.instanceId;
          render();
        },
      }),
    );
  });
}

sendBtn.addEventListener('click', async () => {
  if (!chosen || !payload) return;
  chrome.runtime.sendMessage({ type: 'knightloader-send-to', target: chosen, payload });
  window.close();
});

cancelBtn.addEventListener('click', () => window.close());
