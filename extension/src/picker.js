// Opened by background.js's sendToInstance() only when more than one
// instance is configured — the JD-style "the extension pops up and asks
// where" moment (jdp, 2026-08-23) for context-menu sends, which otherwise
// bypass popup.html entirely and had no place to offer that choice.

const targetEl = document.getElementById('target');
const choicesEl = document.getElementById('choices');
const sendBtn = document.getElementById('send');
const cancelBtn = document.getElementById('cancel');

let payload = null;

(async () => {
  const { pendingSend } = await chrome.storage.session.get('pendingSend');
  // Read-once: a stale entry from a window the user closed without choosing
  // must never resurface and send the wrong link on the next click.
  await chrome.storage.session.remove('pendingSend');
  if (!pendingSend) {
    targetEl.textContent = 'Nothing to send — this window can be closed.';
    sendBtn.disabled = true;
    return;
  }
  payload = pendingSend.payload;
  targetEl.textContent = payload.title || payload.url || payload.text || 'Untitled';

  const { instances } = await readInstances();
  for (const inst of instances) {
    const label = document.createElement('label');
    label.className = 'choice';
    const radio = document.createElement('input');
    radio.type = 'radio';
    radio.name = 'instance';
    radio.value = inst.url;
    radio.checked = inst.name === pendingSend.defaultName;
    const info = document.createElement('span');
    const name = document.createElement('div');
    name.className = 'name';
    name.textContent = inst.name;
    const url = document.createElement('div');
    url.className = 'url';
    url.textContent = inst.url;
    info.append(name, url);
    label.append(radio, info);
    choicesEl.appendChild(label);
  }
})();

sendBtn.addEventListener('click', () => {
  const chosen = choicesEl.querySelector('input[name="instance"]:checked');
  if (!chosen || !payload) return;
  chrome.runtime.sendMessage({ type: 'knightloader-send-to', origin: chosen.value, payload });
  window.close();
});

cancelBtn.addEventListener('click', () => window.close());
