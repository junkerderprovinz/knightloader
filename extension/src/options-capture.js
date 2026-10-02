// The options page's two cards for capture.js: taking downloads over from the
// browser, and finding the media a page plays. Each switch asks for its
// permissions when turned on and hands them back when turned off; the rules
// under the takeover switch go with it.

const takeoverEnabledEl = document.getElementById('takeoverEnabled');
const takeoverRefusedEl = document.getElementById('takeoverRefused');
const takeoverRulesEl = document.getElementById('takeoverRules');
const takeoverTypesEl = document.getElementById('takeoverTypes');
const takeoverMinSizeEl = document.getElementById('takeoverMinSize');
const takeoverSkipEl = document.getElementById('takeoverSkip');
const takeoverBypassEl = document.getElementById('takeoverBypass');
const mediaEnabledEl = document.getElementById('mediaEnabled');
const mediaRefusedEl = document.getElementById('mediaRefused');

/** Whether the browser refused a feature's permissions since this page
 *  opened, so its notice can say why the switch fell back. */
const captureRefused = { takeover: false, media: false };

function captureText() {
  document.getElementById('takeoverHeading').textContent = t('options.takeoverHeading');
  glimSetInfo('takeoverHeading', t('options.takeoverInfo'));
  document.getElementById('takeoverToggle').textContent = t('options.takeoverToggle');
  document.getElementById('takeoverRefusedTitle').textContent = t('options.takeoverRefusedTitle');
  document.getElementById('takeoverRefusedReason').textContent = t('options.takeoverRefused');
  document.getElementById('takeoverTypesLabel').textContent = t('options.takeoverTypes');
  takeoverTypesEl.placeholder = t('options.takeoverTypesPlaceholder');
  document.getElementById('takeoverMinSizeLabel').textContent = t('options.takeoverMinSize');
  document.getElementById('takeoverSkipLabel').textContent = t('options.takeoverSkip');
  document.getElementById('takeoverBypassLabel').textContent = t('options.takeoverBypass');

  takeoverMinSizeEl.replaceChildren(
    ...TAKEOVER_MIN_SIZES.map((mb) => new Option(mb === 0 ? t('options.takeoverAnySize') : `${mb} ${t('options.megabytes')}`, String(mb))),
  );
  const keyNames = { alt: t('options.keyAlt'), shift: t('options.keyShift'), ctrl: t('options.keyCtrl') };
  takeoverBypassEl.replaceChildren(...BYPASS_KEYS.map((k) => new Option(keyNames[k], k)));

  document.getElementById('mediaHeading').textContent = t('options.mediaHeading');
  glimSetInfo('mediaHeading', t('options.mediaInfo'));
  document.getElementById('mediaToggle').textContent = t('options.mediaToggle');
  document.getElementById('mediaRefusedTitle').textContent = t('options.mediaRefusedTitle');
  document.getElementById('mediaRefusedReason').textContent = t('options.mediaRefused');
}

/** The switches show whether a feature runs, never only whether it is wanted,
 *  as Click'n'Load's does. */
async function renderCapture() {
  const [takeover, media, rules] = await Promise.all([takeoverState(), mediaState(), readTakeoverRules()]);
  takeoverEnabledEl.setAttribute('aria-checked', String(takeover.on));
  takeoverRefusedEl.hidden = takeover.on || !captureRefused.takeover;
  takeoverRulesEl.hidden = !takeover.on;
  // A field being typed in keeps what is in it.
  if (document.activeElement !== takeoverTypesEl) takeoverTypesEl.value = rules.types;
  if (document.activeElement !== takeoverSkipEl) takeoverSkipEl.value = rules.skip;
  takeoverMinSizeEl.value = String(rules.minSizeMb);
  takeoverBypassEl.value = rules.bypassKey;

  mediaEnabledEl.setAttribute('aria-checked', String(media.on));
  mediaRefusedEl.hidden = media.on || !captureRefused.media;
}

/**
 * One switch: on asks for the permissions, which must come before any other
 * await while the click still counts as the user's; off hands back what no
 * other feature uses.
 */
function wireCaptureSwitch(el, key, access, refusedKey, refusedEl) {
  el.addEventListener('click', async () => {
    if (el.getAttribute('aria-checked') !== 'true') {
      const granted = await requestFeature(key, access);
      captureRefused[refusedKey] = !granted;
      await renderCapture();
      if (!granted) {
        shake(el);
        refusedEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      }
      return;
    }
    el.setAttribute('aria-checked', 'false');
    captureRefused[refusedKey] = false;
    await releaseFeature(key);
    await renderCapture();
  });
}

wireCaptureSwitch(takeoverEnabledEl, 'takeoverEnabled', TAKEOVER_ACCESS, 'takeover', takeoverRefusedEl);
wireCaptureSwitch(mediaEnabledEl, 'mediaEnabled', MEDIA_ACCESS, 'media', mediaRefusedEl);

// The lists are written back as they were read, tidied, so what is stored is
// what the field shows.
takeoverTypesEl.addEventListener('change', async () => {
  takeoverTypesEl.value = parseList(takeoverTypesEl.value).join(', ');
  await writeTakeoverRules({ types: takeoverTypesEl.value });
});
takeoverSkipEl.addEventListener('change', async () => {
  takeoverSkipEl.value = parseList(takeoverSkipEl.value).join(', ');
  await writeTakeoverRules({ skip: takeoverSkipEl.value });
});
takeoverMinSizeEl.addEventListener('change', () => void writeTakeoverRules({ minSizeMb: Number(takeoverMinSizeEl.value) }));
takeoverBypassEl.addEventListener('change', () => void writeTakeoverRules({ bypassKey: takeoverBypassEl.value }));

chrome.storage.onChanged.addListener(async (changes, area) => {
  if (area !== 'local' || !changes.language) return;
  await loadLanguage();
  captureText();
  await renderCapture();
});
// Click'n'Load's switch and the browser's own settings change the shared
// access.
chrome.permissions.onAdded.addListener(() => void renderCapture());
chrome.permissions.onRemoved.addListener(() => void renderCapture());

(async () => {
  await loadLanguage();
  captureText();
  await renderCapture();
})();
