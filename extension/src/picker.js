// Opened by background.js's sendToInstance() only when more than one
// instance is configured — the JD-style "the extension pops up and asks
// where" moment (jdp, 2026-08-23) for context-menu sends, which otherwise
// bypass popup.html entirely and had no place to offer that choice.

const headingEl = document.getElementById('heading');
const targetEl = document.getElementById('target');
const choicesEl = document.getElementById('choices');
const sendBtn = document.getElementById('send');
const cancelBtn = document.getElementById('cancel');

let payload = null;

(async () => {
  await loadLanguage();
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

  const { instances } = await readInstances();
  for (const inst of instances) {
    const target = entryTarget(inst);
    const label = document.createElement('label');
    label.className = 'choice';
    const radio = document.createElement('input');
    radio.type = 'radio';
    radio.name = 'instance';
    // The NAME is the value now, not the URL: an entry reached through a
    // forwarder has no URL of its own to identify it by (issue #27).
    radio.value = inst.name;
    // Still listed when it cannot be sent to, just not selectable: leaving it
    // out entirely turns "this peer cannot be reached" into "this peer does
    // not exist", and only one of those is true.
    //
    // disabled BEFORE checked, and never checked while disabled: a disabled
    // radio still matches :checked, so setting it first would leave the
    // fallback below thinking a selection exists and hand the user a Send
    // button that silently does nothing.
    radio.disabled = !target;
    radio.checked = !radio.disabled && inst.name === pendingSend.defaultName;
    const info = document.createElement('span');
    const name = document.createElement('div');
    name.className = 'name';
    name.textContent = entryLabel(inst);
    const url = document.createElement('div');
    url.className = 'url';
    url.textContent = inst.url || (inst.via ? t('picker.viaPeer', { via: inst.via }) : t('picker.unreachable'));
    info.append(name, url);
    label.append(radio, info);
    choicesEl.appendChild(label);
  }
  // A default that cannot be sent to must not leave the dialog with nothing
  // selected and a Send button that quietly does nothing.
  if (!choicesEl.querySelector('input[name="instance"]:checked')) {
    const first = choicesEl.querySelector('input[name="instance"]:not(:disabled)');
    if (first) first.checked = true;
    else sendBtn.disabled = true;
  }
})();

sendBtn.addEventListener('click', async () => {
  const chosen = choicesEl.querySelector('input[name="instance"]:checked');
  if (!chosen || !payload) return;
  const { instances } = await readInstances();
  const target = entryTarget(instances.find((i) => i.name === chosen.value) ?? {});
  if (!target) return;
  chrome.runtime.sendMessage({
    type: 'knightloader-send-to',
    origin: target.origin,
    payload: { ...payload, to: target.to },
  });
  window.close();
});

cancelBtn.addEventListener('click', () => window.close());
