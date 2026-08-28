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

  for (const inst of pendingSend.siblings ?? []) {
    const label = document.createElement('label');
    label.className = 'choice';
    const radio = document.createElement('input');
    radio.type = 'radio';
    radio.name = 'instance';
    // Keyed by the relay instance id: a group whose members were renamed keeps
    // pointing at the same machine.
    radio.value = inst.instanceId;
    radio.checked = inst.instanceId === pendingSend.defaultName;
    const info = document.createElement('span');
    const name = document.createElement('div');
    name.className = 'name';
    name.textContent = instanceLabel(inst);
    const where = document.createElement('div');
    where.className = 'url';
    // Every instance here is reached the same way, so the second line says what
    // it IS rather than repeating an address that no longer exists in this
    // model. A container and a desktop build are worth telling apart.
    where.textContent = deploymentLabel(inst.deployment);
    info.append(name, where);
    label.append(radio, info);
    choicesEl.appendChild(label);
  }
  // A remembered default that is no longer in the group must not leave the
  // dialog with nothing selected and a Send button that quietly does nothing.
  if (!choicesEl.querySelector('input[name="instance"]:checked')) {
    const first = choicesEl.querySelector('input[name="instance"]');
    if (first) first.checked = true;
    else sendBtn.disabled = true;
  }
})();

sendBtn.addEventListener('click', async () => {
  const chosen = choicesEl.querySelector('input[name="instance"]:checked');
  if (!chosen || !payload) return;
  // Remembered, so the next send of several defaults to the same place. The
  // picker is the only surface that knows a deliberate choice was just made.
  await writeDefaultTarget(chosen.value);
  chrome.runtime.sendMessage({ type: 'knightloader-send-to', target: chosen.value, payload });
  window.close();
});

cancelBtn.addEventListener('click', () => window.close());
