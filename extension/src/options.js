// The options page: the group this browser belongs to (group.js), the
// language, the look, Click'n'Load, and a report for when something is wrong.
//
// As GlimStone asks, every explanation is an info bubble on a heading or label,
// and every switch is a toggle rather than a checkbox.

const list = document.getElementById('list');
const status = document.getElementById('status');
const groupHeadingEl = document.getElementById('groupHeading');
const joinForm = document.getElementById('joinForm');
const phraseInput = document.getElementById('phrase');
const joinBtn = document.getElementById('join');
const leaveBtn = document.getElementById('leave');
const refreshBtn = document.getElementById('refreshGroup');
const languageHeadingEl = document.getElementById('languageHeading');
const languageBox = document.getElementById('languageBox');
const cnlHeadingEl = document.getElementById('cnlHeading');
const cnlToggleEl = document.getElementById('cnlToggle');
const cnlEnabledEl = document.getElementById('cnlEnabled');
const cnlCountdownEl = document.getElementById('cnlCountdown');
const cnlCountdownRow = document.getElementById('cnlCountdownRow');
const cnlCountdownLabelEl = document.getElementById('cnlCountdownLabel');
const cnlCountdownUnitEl = document.getElementById('cnlCountdownUnit');
const cnlCountdownUpEl = document.getElementById('cnlCountdownUp');
const cnlCountdownDownEl = document.getElementById('cnlCountdownDown');
const appearanceHeadingEl = document.getElementById('appearanceHeading');
const themeHeadingEl = document.getElementById('themeHeading');
const shapeHeadingEl = document.getElementById('shapeHeading');
const coloursHeadingEl = document.getElementById('coloursHeading');
const rainbowOnEl = document.getElementById('rainbowOn');
const rainbowReactiveEl = document.getElementById('rainbowReactive');
const rainbowRotateEl = document.getElementById('rainbowRotate');
const rainbowRow = document.getElementById('rainbowRow');
const rainbowReactiveRow = document.getElementById('rainbowReactiveRow');
const rainbowRotateRow = document.getElementById('rainbowRotateRow');
const paletteRow = document.getElementById('paletteRow');
const paletteSwatches = document.getElementById('paletteSwatches');
const followInstanceEl = document.getElementById('followInstance');
const followInstanceRow = document.getElementById('followInstanceRow');
const followUnavailableEl = document.getElementById('followUnavailable');
const followUnavailableTitleEl = document.getElementById('followUnavailableTitle');
const followUnavailableReasonEl = document.getElementById('followUnavailableReason');
const accentLabelEl = document.getElementById('accentLabel');
// The labels of the rows that get dimmed.
const rainbowLabelEl = document.getElementById('rainbowLabel');
const rainbowReactiveLabelEl = document.getElementById('rainbowReactiveLabel');
const rainbowRotateLabelEl = document.getElementById('rainbowRotateLabel');
const paletteLabelEl = document.getElementById('paletteLabel');
const leaveConfirmEl = document.getElementById('leaveConfirm');
const leaveConfirmTitleEl = document.getElementById('leaveConfirmTitle');
const leaveConfirmMessageEl = document.getElementById('leaveConfirmMessage');
const leaveConfirmCancelEl = document.getElementById('leaveConfirmCancel');
const leaveConfirmCommitEl = document.getElementById('leaveConfirmCommit');
const problemsHeadingEl = document.getElementById('problemsHeading');
const aboutHeadingEl = document.getElementById('aboutHeading');
const phraseEye = document.getElementById('phraseEye');
const phrasePaste = document.getElementById('phrasePaste');

/**
 * Builds a filled glyph with createElementNS, because Mozilla's linter, a
 * release gate here, fails an innerHTML assignment from a variable
 * (UNSAFE_VAR_ASSIGNMENT).
 *
 * Brand logos override fill="currentColor" from the stylesheet
 * (`.glim-brand-btn svg path` in glimstone.css), since an author rule beats a
 * presentation attribute.
 */
const NS = 'http://www.w3.org/2000/svg';
function glyph(d, size) {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 16 16');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS(NS, 'path');
  path.setAttribute('fill', 'currentColor');
  path.setAttribute('d', d);
  svg.appendChild(path);
  return svg;
}
/**
 * The web UI's IconInstances (web/src/lib/icons.tsx), copied shape for shape so
 * "instances" looks the same everywhere. Used for the empty group list.
 */
function instancesGlyph(size) {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 20 20');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('fill', 'currentColor');
  svg.setAttribute('aria-hidden', 'true');
  for (const [x, y, o] of [[2.5, 3, '.55'], [2.5, 12, '.55']]) {
    const r = document.createElementNS(NS, 'rect');
    r.setAttribute('x', String(x));
    r.setAttribute('y', String(y));
    r.setAttribute('width', '15');
    r.setAttribute('height', '5');
    r.setAttribute('rx', '1.5');
    r.setAttribute('opacity', o);
    svg.appendChild(r);
  }
  for (const cy of ['5.5', '14.5']) {
    const c = document.createElementNS(NS, 'circle');
    c.setAttribute('cx', '5.5');
    c.setAttribute('cy', cy);
    c.setAttribute('r', '1');
    svg.appendChild(c);
  }
  return svg;
}

const D_RETRY = 'M8 3V1L5 3.5 8 6V4a3.5 3.5 0 1 1-3.5 3.5H3A5 5 0 1 0 8 3z';
// The same cross popup.js uses for its cancel.
const D_CROSS =
  'M4.2 2.8 8 6.6l3.8-3.8 1.4 1.4L9.4 8l3.8 3.8-1.4 1.4L8 9.4l-3.8 3.8-1.4-1.4L6.6 8 2.8 4.2z';
// A cup with a handle and a saucer, for the thank-you in the About card.
const D_COFFEE =
  'M2.5 3h8.2v5.2a3.6 3.6 0 0 1-3.6 3.6H6.1A3.6 3.6 0 0 1 2.5 8.2V3z' +
  'M11.6 4.1h1.2a2.1 2.1 0 0 1 0 4.2h-1.2V6.9h1.2a.8.8 0 0 0 0-1.6h-1.2V4.1z' +
  'M1.6 13h10.8v1.5H1.6z';
// GitHub's Octicon "mark-github", which GitHub publishes for this use.
const D_GITHUB =
  'M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49' +
  '-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82' +
  '.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15' +
  '-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27' +
  '1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95' +
  '.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.012 8.012 0 0 0 16 8c0-4.42-3.58-8-8-8z';
// Two offset sheets as separate subpaths, so the overlap reads as depth at
// 14px.
const D_COPY =
  'M4 1.5h6.5a1 1 0 0 1 1 1V4H6a1.5 1.5 0 0 0-1.5 1.5V11H3a1 1 0 0 1-1-1V2.5a1 1 0 0 1 1-1z' +
  'M6.5 5.5h7a1 1 0 0 1 1 1v7a1 1 0 0 1-1 1h-7a1 1 0 0 1-1-1v-7a1 1 0 0 1 1-1z';
// An envelope whose flap is cut out, so the ground shows through.
const D_MAIL =
  'M1.5 3.5h13v9h-13v-9zm1.6 1.4L8 8.4l4.9-3.5H3.1z';
// A filled bin in one path.
const D_TRASH =
  'M6.5 1h3a1 1 0 0 1 1 1v1H13v1.5H3V3h2.5V2a1 1 0 0 1 1-1zm.5 2h2v-.5H7V3z' +
  'M4 5.5h8l-.6 8.1a1.4 1.4 0 0 1-1.4 1.4H6a1.4 1.4 0 0 1-1.4-1.4L4 5.5z';
// A solid clipboard with its clip, the same shape the app's paste button uses.
const D_PASTE =
  'M4.1 2.8h7.8a1.6 1.6 0 0 1 1.6 1.6v8.8a1.6 1.6 0 0 1-1.6 1.6H4.1a1.6 1.6 0 0 1-1.6-1.6V4.4a1.6 1.6 0 0 1 1.6-1.6z' +
  'M5.8 1.2h4.4a.8.8 0 0 1 .8.8v1a.8.8 0 0 1-.8.8H5.8a.8.8 0 0 1-.8-.8V2a.8.8 0 0 1 .8-.8z';
const D_EYE =
  'M8 3C4.4 3 1.5 6.1.7 7.6a.8.8 0 0 0 0 .8C1.5 9.9 4.4 13 8 13s6.5-3.1 7.3-4.6a.8.8 0 0 0 0-.8' +
  'C14.5 6.1 11.6 3 8 3zm0 8a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm0-1.6a1.4 1.4 0 1 0 0-2.8 1.4 1.4 0 0 0 0 2.8z';
const D_EYE_OFF =
  'M2.3 1.3 1.2 2.4l2 2C1.9 5.4.9 6.7.7 7.6a.8.8 0 0 0 0 .8C1.5 9.9 4.4 13 8 13c1.2 0 2.3-.4 3.3-.9' +
  'l2.3 2.3 1.1-1.1L2.3 1.3zM8 11a3 3 0 0 1-2.6-4.5l1.2 1.2A1.4 1.4 0 0 0 8 9.4l1.2 1.2A3 3 0 0 1 8 11z' +
  'm7.3-2.6C14.7 9.4 13 11 11 12l-1.4-1.4A3 3 0 0 0 5.4 6.4L4 5a7.6 7.6 0 0 1 4-2c3.6 0 6.5 3.1 7.3 4.6a.8.8 0 0 1 0 .8z';

/**
 * The page's status line, which clears itself after four seconds like a
 * GlimStone toast, so an old failure does not look current. `hold` keeps a
 * progress message such as "connecting" until its outcome replaces it.
 */
let sayTimer = 0;

function say(text, ok, hold) {
  status.textContent = text;
  status.className = ok ? 'ok' : '';
  clearTimeout(sayTimer);
  if (text && !hold) {
    sayTimer = setTimeout(() => {
      status.textContent = '';
      status.className = '';
    }, 4000);
  }
}

/**
 * labelWords puts a row label's words in a span of their own, so dimming can
 * land on the words while the "(i)" beside them stays readable (GlimStone
 * 1.9.0). applyStaticText() calls it on every language change, so it reuses
 * the span.
 */
function labelWords(id, text) {
  const host = document.getElementById(id);
  if (!host) return;
  let span = host.querySelector('.glim-label-words');
  if (!span) {
    // Drop the markup's English placeholder text, but keep the "(i)" element.
    for (const node of [...host.childNodes]) {
      if (node.nodeType === Node.TEXT_NODE) node.remove();
    }
    span = document.createElement('span');
    span.className = 'glim-label-words';
    host.prepend(span);
  }
  span.textContent = text;
}

/**
 * setDimmed marks a setting whose value is overridden elsewhere but still in
 * force. It dims only; it never switches the control off (GlimStone 1.16.0). A
 * control with nothing behind it is removed instead, and one that refuses uses
 * setRefused(), since pointer-events: none would still let the keyboard press
 * it.
 *
 * Callers pass the label's words and the controls, not the row, so the "(i)"
 * keeps full strength. The empty string hands the value back to the
 * stylesheet.
 */
function setDimmed(parts, off) {
  for (const el of parts) {
    if (!el) continue;
    el.style.opacity = off ? '.5' : '';
  }
}

/**
 * setRefused switches controls off through their own `disabled`, which screen
 * readers and the keyboard respect; the stylesheet's `:disabled` rules do the
 * greying. `groups` may be buttons or containers of buttons.
 */
function setRefused(groups, off) {
  for (const el of groups) {
    if (!el) continue;
    const buttons = el.tagName === 'BUTTON' ? [el] : [...el.querySelectorAll('button')];
    for (const b of buttons) b.disabled = off;
  }
}

/** The words inside a row label, or null before applyStaticText() has run. */
function labelWordsOf(labelEl) {
  return labelEl?.querySelector('.glim-label-words') ?? null;
}

/**
 * applyStaticText fills every fixed label in the current language, on load and
 * on every language change.
 */
function applyStaticText() {
  groupHeadingEl.textContent = t('options.groupHeading');
  glimSetInfo('groupHeading', t('options.groupInfo'));
  phraseInput.placeholder = t('options.phrasePlaceholder');
  renderPhraseEye();
  renderPhrasePaste();
  joinBtn.textContent = t('options.join');
  // The badge has no text; its name is in aria-label and the tooltip.
  leaveBtn.textContent = '';
  leaveBtn.setAttribute('aria-label', t('options.leave'));
  leaveBtn.setAttribute('data-tip', t('options.leave'));
  // A glyph alone in a square fills half of it (GlimStone 1.8.0).
  leaveBtn.replaceChildren(glyph(D_TRASH, 16));
  refreshBtn.textContent = t('options.refresh');

  // Both buttons are neutral, since a destructive control is not coloured
  // (GlimStone 1.12.0), and the committing one names its action.
  leaveConfirmTitleEl.textContent = t('options.leaveConfirmTitle');
  leaveConfirmCancelEl.replaceChildren(glyph(D_CROSS, 14), document.createTextNode(t('common.cancel')));
  leaveConfirmCommitEl.replaceChildren(glyph(D_TRASH, 14), document.createTextNode(t('options.leave')));

  languageHeadingEl.textContent = t('options.languageHeading');
  glimSetInfo('languageHeading', t('options.languageSub'));

  cnlToggleEl.textContent = t('options.cnlToggle');
  glimSetInfo('cnlHeading', t('options.cnlSub'));

  // Look and Colours have no heading bubble because their rows explain
  // themselves.
  appearanceHeadingEl.textContent = t('options.appearanceHeading');
  themeHeadingEl.textContent = t('options.themeHeading');
  glimSetInfo('themeHeading', t('options.themeHint'));
  shapeHeadingEl.textContent = t('options.shapeHeading');
  glimSetInfo('shapeHeading', t('options.shapeHint'));
  coloursHeadingEl.textContent = t('options.coloursHeading');

  // Each label gets its own bubble. The captions sit outside their switches,
  // since a focusable icon inside a <button> is invalid and a click on it would
  // flip the switch.
  const label = (id, text, tip) => {
    labelWords(id, text);
    glimSetInfo(id, tip);
  };
  label('accentLabel', t('options.accentLabel'), t('options.accentHint'));
  label('rainbowLabel', t('options.rainbow'), t('options.rainbowHint'));
  label('rainbowReactiveLabel', t('options.rainbowReactive'), t('options.rainbowReactiveHint'));
  label('rainbowRotateLabel', t('options.rainbowRotate'), t('options.rainbowRotateHint'));
  label('paletteLabel', t('options.paletteLabel'), t('options.paletteHint'));
  label('followInstanceLabel', t('options.followInstance'), t('options.followInstanceHint'));

  // The notice standing in for the follow switch without a group (GlimStone
  // 1.15.0) carries the switch's own label and names the card that fixes it.
  followUnavailableTitleEl.textContent = t('options.followInstance');
  followUnavailableReasonEl.textContent = t('options.followNoGroup', { card: t('options.groupHeading') });

  const pinHeading = document.getElementById('pinHeading');
  const pinBody = document.getElementById('pinBody');
  const pinDismiss = document.getElementById('pinDismiss');
  if (pinHeading) pinHeading.textContent = t('options.pinHeading');
  if (pinBody) pinBody.textContent = t('options.pinBody');
  if (pinDismiss) pinDismiss.textContent = t('options.pinDismiss');

  problemsHeadingEl.textContent = t('options.problemsHeading');
  aboutHeadingEl.textContent = t('options.aboutHeading');
  renderAbout();
  glimSetInfo('problemsHeading', t('options.problemsSub'));
  // renderReport() labels the report's button and link.
}

/**
 * The card explaining that the extension sits behind the puzzle piece. It
 * shows when background.js saw an unpinned button at install and the button is
 * still unpinned.
 */
async function renderPinHint() {
  const card = document.getElementById('pinHint');
  if (!card) return;
  const { showPinHint } = await chrome.storage.local.get('showPinHint');
  if (!showPinHint) return;
  let hidden = false;
  try {
    const s = await chrome.action?.getUserSettings?.();
    hidden = s?.isOnToolbar === false;
  } catch {
    hidden = false;
  }
  if (!hidden) {
    await chrome.storage.local.remove('showPinHint');
    return;
  }
  card.hidden = false;
  document.getElementById('pinDismiss')?.addEventListener('click', async () => {
    await chrome.storage.local.remove('showPinHint');
    card.hidden = true;
  });
}

/**
 * The phrase is masked because it is a credential for every instance in the
 * group, and options pages get opened with others looking on. The eye shows
 * what was pasted. The glyph and its name are redrawn with every toggle and
 * language change.
 */
function renderPhraseEye() {
  const shown = phraseInput.type === 'text';
  // Half of .glim-eye's 28px box.
  phraseEye.replaceChildren(glyph(shown ? D_EYE_OFF : D_EYE, 14));
  const name = shown ? t('options.phraseHide') : t('options.phraseShow');
  phraseEye.setAttribute('aria-label', name);
  phraseEye.setAttribute('data-tip', name);
  // With the keyboard the bubble is already open with the old text.
  glimRefreshTip(phraseEye);
}

phraseEye.addEventListener('click', () => {
  phraseInput.type = phraseInput.type === 'password' ? 'text' : 'password';
  renderPhraseEye();
});

/** Draws the paste button's glyph and name together, like the eye. */
function renderPhrasePaste() {
  phrasePaste.replaceChildren(glyph(D_PASTE, 14));
  const name = t('options.phrasePaste');
  phrasePaste.setAttribute('aria-label', name);
  phrasePaste.setAttribute('data-tip', name);
  glimRefreshTip(phrasePaste);
}

/**
 * Pastes the phrase and normalises it; validation happens on join.
 *
 * clipboardRead is an optional permission, because adding a required one would
 * disable the extension for existing users until they approve it. The request
 * has to be the first await, while the click still counts as a user gesture,
 * and it prompts only once.
 *
 * Case and whitespace are fixed here so a phrase copied from a chat shows up
 * the way it will be stored.
 */
phrasePaste.addEventListener('click', async () => {
  try {
    await chrome.permissions.request({ permissions: ['clipboardRead'] });
  } catch {
    // Firefox rejects without a gesture and Chrome throws for a permission not
    // listed as optional; the read is tried anyway.
  }
  // The request's answer says nothing reliable, so the read itself decides.
  let text = '';
  try {
    text = await navigator.clipboard.readText();
  } catch {
    // Reported whatever the permission said, for example after a dismissed
    // paste prompt in Firefox or with no text on the clipboard.
    say(t('options.phrasePasteBlocked'), false);
    shake(phrasePaste);
    return;
  }
  const words = String(text).trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
  if (!words) return;
  phraseInput.value = words;
  // The field stays masked; the eye is beside it.
  phraseInput.focus();
});

/**
 * phraseProblemText turns a PhraseError into a translated sentence, naming the
 * word and its position where that applies.
 */
function phraseProblemText(err) {
  const p = err?.problem;
  if (!p) return t('options.phraseBad');
  if (p.reason === 'word_count') return t('options.phraseWordCount', { count: p.count });
  if (p.reason === 'unknown_word') return t('options.phraseUnknownWord', { word: p.word, position: p.position });
  return t('options.phraseChecksum');
}

/**
 * renderGroup shows who is in the group right now, asked of the relay, so only
 * instances that are online appear.
 */
async function renderGroup() {
  const phrase = await readPhrase();
  phraseInput.value = phrase;
  const joined = phrase !== '';
  leaveBtn.hidden = !joined;
  refreshBtn.hidden = !joined;
  joinBtn.textContent = joined ? t('options.reconnect') : t('options.join');
  list.innerHTML = '';
  if (!joined) return;

  const loading = document.createElement('div');
  loading.className = 'empty';
  loading.textContent = t('options.groupLoading');
  list.appendChild(loading);

  let siblings;
  try {
    // The roster plus what each instance is doing; the popup only needs the
    // roster.
    siblings = await groupStatus();
  } catch {
    loading.textContent = t('options.groupUnreachable');
    return;
  }
  list.innerHTML = '';
  if (siblings.length === 0) {
    // The shared empty state, without a card of its own inside the Group card
    // and without an action button, since connect and refresh sit right above.
    const empty = document.createElement('div');
    empty.className = 'emptyState';
    empty.appendChild(instancesGlyph(28));
    const title = document.createElement('span');
    title.textContent = t('options.groupEmpty');
    empty.appendChild(title);
    list.appendChild(empty);
    return;
  }

  // The same card the popup draws (shared.js). The default is changed by
  // right-clicking another card.
  const preferred = defaultOf(siblings, await readDefaultTarget());
  siblings.forEach((inst, i) => {
    list.appendChild(
      instanceCard(inst, {
        index: i,
        isDefault: inst.instanceId === preferred,
        status: inst.status,
        onSetDefault: async (picked) => {
          await writeDefaultTarget(picked.instanceId);
          await renderGroup();
          // The report names the default instance.
          void renderReport();
        },
        onQueue: async (picked, halted, el) => {
          const ok = await setQueueHalted(picked.instanceId, halted).catch(() => false);
          if (!ok) {
            // The same sentence as for the follow switch: the instance did not
            // answer.
            say(t('options.followFailed'), false);
            shake(el);
            return;
          }
          // Read back what the instance actually did.
          await renderGroup();
        },
        onOpen: (picked, url) => {
          if (url) void chrome.tabs.create({ url });
        },
      }),
    );
  });
}

joinForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  joinBtn.disabled = true;
  say(t('options.joining'), true, true);
  try {
    await writePhrase(phraseInput.value);
  } catch (err) {
    joinBtn.disabled = false;
    // The reason goes into the button's tooltip rather than a sentence that
    // never clears (GlimStone "Failure feedback").
    joinBtn.setAttribute('data-tip', phraseProblemText(err));
    say('', false);
    shake(joinBtn);
    return;
  }
  // A phrase that decodes may still reach nobody, so the relay is asked before
  // this counts as a success.
  try {
    const siblings = await groupInstances();
    // The cards show the members; only an empty group needs a sentence.
    say(siblings.length ? '' : t('options.joinedEmpty'), true);
    joinBtn.removeAttribute('data-tip');
  } catch {
    joinBtn.setAttribute('data-tip', t('options.groupUnreachable'));
    say('', false);
    shake(joinBtn);
  }
  joinBtn.disabled = false;
  await renderGroup();
  // Joining brings back the follow switch, and the report names the group.
  await renderAppearance();
  void renderReport();
});

/**
 * The confirmation before leaving the group. GlimStone 1.12.0 keeps the bin
 * neutral because an irreversible action asks first, stating what is at stake.
 *
 * The count comes from the list on screen rather than a relay round trip, so
 * the window opens at once. Escape and a press on the backdrop cancel; there is
 * no corner X.
 */
function openLeaveConfirm() {
  leaveConfirmMessageEl.textContent = t('options.leaveConfirmBody', {
    count: String(list.querySelectorAll('.glim-instance').length),
  });
  leaveConfirmEl.hidden = false;
  // Focus starts on the answer that changes nothing.
  leaveConfirmCancelEl.focus();
  document.addEventListener('keydown', onLeaveConfirmKey);
}

/** `back` receives focus afterwards; after leaving, the bin is hidden, so it
 *  cannot always be the bin. */
function closeLeaveConfirm(back) {
  leaveConfirmEl.hidden = true;
  document.removeEventListener('keydown', onLeaveConfirmKey);
  back?.focus();
}

/**
 * Escape closes the window, and Tab cycles between its two buttons, as
 * aria-modal promises.
 */
function onLeaveConfirmKey(event) {
  if (event.key === 'Escape') {
    event.preventDefault();
    closeLeaveConfirm(leaveBtn);
    return;
  }
  if (event.key !== 'Tab') return;
  const stops = [leaveConfirmCancelEl, leaveConfirmCommitEl];
  const at = stops.indexOf(document.activeElement);
  event.preventDefault();
  // -1 means focus is on the page behind; Tab pulls it back in.
  const next = event.shiftKey ? (at <= 0 ? stops.length - 1 : at - 1) : at === stops.length - 1 ? 0 : at + 1;
  stops[next].focus();
}

leaveBtn.addEventListener('click', () => openLeaveConfirm());
leaveConfirmCancelEl.addEventListener('click', () => closeLeaveConfirm(leaveBtn));
// Only a press on the backdrop itself cancels.
leaveConfirmEl.addEventListener('click', (event) => {
  if (event.target === leaveConfirmEl) closeLeaveConfirm(leaveBtn);
});

leaveConfirmCommitEl.addEventListener('click', async () => {
  // Focus goes to Connect, since renderGroup() hides the bin.
  closeLeaveConfirm(joinBtn);
  await forgetGroup();
  // Leaving stops following and restores the local look. Without a group the
  // follow switch is removed, so nothing could turn it off later.
  const { followInstance } = await chrome.storage.local.get('followInstance');
  if (followInstance === true) {
    await restoreLocalLook();
    await writeAppearance({ followInstance: false });
    const back = await readAppearance();
    applyShape(back.shape);
    applyAccent(back.accent);
    applyRainbow(back.rainbow);
  }
  phraseInput.value = '';
  phraseInput.type = 'password';
  renderPhraseEye();
  say(t('options.left'), true);
  await renderGroup();
  // The notice replaces the follow switch.
  await renderAppearance();
  void renderReport();
});

refreshBtn.addEventListener('click', async () => {
  refreshBtn.disabled = true;
  await renderGroup();
  refreshBtn.disabled = false;
});

/**
 * The language picker: a listbox with flags and no "Automatic" entry. Until the
 * user chooses, the browser's language is resolved and selected, so the picker
 * always shows the language in use.
 */
async function renderLanguagePicker() {
  listbox(
    languageBox,
    LANGUAGES.map((l) => ({ value: l.code, label: l.label, flag: l.flag })),
    currentLanguage(),
    async (code) => {
      await setLanguage(code);
      await loadLanguage();
      applyStaticText();
      await renderLanguagePicker();
      // renderCnl writes the countdown's texts, which applyStaticText does not.
      await renderCnl();
      await renderAppearance();
      await renderGroup();
      void renderReport();
    },
  );
}

/**
 * Click'n'Load is on by default, as the main reason to install the extension.
 * That is why <all_urls> is in the manifest: a default-on feature has to work
 * on a fresh install, and the install dialog names the access, as with
 * JDownloader's own extension. Switching it off unregisters the content
 * scripts, so sites reach 127.0.0.1:9666 as before.
 */
async function renderCnl() {
  const stored = await chrome.storage.local.get('cnlEnabled');
  // Absent means on, as in background.js.
  const on = stored.cnlEnabled !== false;
  cnlEnabledEl.setAttribute('aria-checked', String(on));

  // The countdown goes with its switch (GlimStone 1.10.0 and 1.16.0): with
  // Click'n'Load off no batch is ever parked, so nothing reads the number.
  cnlCountdownRow.hidden = !on;

  cnlCountdownLabelEl.textContent = t('options.cnlCountdown');
  cnlCountdownUnitEl.textContent = t('options.seconds');
  cnlCountdownEl.value = String(await readCnlCountdown());
  cnlCountdownUpEl.setAttribute('aria-label', t('options.cnlCountdownUp'));
  cnlCountdownUpEl.setAttribute('data-tip', t('options.cnlCountdownUp'));
  cnlCountdownDownEl.setAttribute('aria-label', t('options.cnlCountdownDown'));
  cnlCountdownDownEl.setAttribute('data-tip', t('options.cnlCountdownDown'));
  markCountdownEnds();
}

/** Disables a stepper at the end of its range, read from the input's min and
 *  max. */
function markCountdownEnds() {
  const v = Number(cnlCountdownEl.value);
  cnlCountdownDownEl.disabled = v <= Number(cnlCountdownEl.min);
  cnlCountdownUpEl.disabled = v >= Number(cnlCountdownEl.max);
}

/**
 * The field's own stepper arrows replace the native ones (GlimStone 1.7.0), so
 * the value can be nudged without typing. stepUp and stepDown honour the
 * element's min, max and step, and the dispatched 'change' saves the value.
 */
function stepCountdown(by) {
  try {
    if (by > 0) cnlCountdownEl.stepUp();
    else cnlCountdownEl.stepDown();
  } catch {
    // stepUp throws when the field is empty or out of range. Land somewhere
    // valid rather than doing nothing.
    cnlCountdownEl.value = String(CNL_COUNTDOWN_DEFAULT);
  }
  cnlCountdownEl.dispatchEvent(new Event('change'));
}

cnlCountdownUpEl.addEventListener('click', () => stepCountdown(1));
cnlCountdownDownEl.addEventListener('click', () => stepCountdown(-1));

/**
 * The wheel nudges the field, but only while it has focus (GlimStone 1.8.0), so
 * scrolling past it cannot change the value. preventDefault comes before the
 * range check because while focused this handler is the scroll. Only the sign
 * of the delta counts, since trackpads report fractions; the steppers' disabled
 * state marks the ends.
 */
cnlCountdownEl.addEventListener(
  'wheel',
  (event) => {
    if (document.activeElement !== cnlCountdownEl) return;
    if (event.deltaY === 0) return;
    const up = event.deltaY < 0;
    event.preventDefault();
    if (up ? cnlCountdownUpEl.disabled : cnlCountdownDownEl.disabled) return;
    stepCountdown(up ? 1 : -1);
  },
  { passive: false },
);

// Saved on 'change' rather than 'input', which would store 3 on the way to 30.
// writeCnlCountdown clamps the value.
cnlCountdownEl.addEventListener('change', async () => {
  cnlCountdownEl.value = String(await writeCnlCountdown(cnlCountdownEl.value));
  markCountdownEnds();
});

cnlEnabledEl.addEventListener('click', async () => {
  const on = cnlEnabledEl.getAttribute('aria-checked') !== 'true';
  cnlEnabledEl.setAttribute('aria-checked', String(on));
  // The countdown row follows the switch at once.
  cnlCountdownRow.hidden = !on;
  await chrome.storage.local.set({ cnlEnabled: on });
  await chrome.runtime.sendMessage({ type: 'knightloader-cnl-scripts', on }).catch(() => {});
  // No status message: the switch is the feedback, and say() writes into the
  // group card.
});

// Appearance: theme, corners, accent and the rainbow. appearance.js applies
// them at the top of every page, and the look can also be taken from the
// default instance (adoptFromInstance in appearance.js).

/** Wires a rainbow switch: write, apply, redraw. */
function wireRainbowSwitch(el, key) {
  el.addEventListener('click', async () => {
    const on = el.getAttribute('aria-checked') !== 'true';
    const patch = { [key]: on };
    // A fresh offset makes turning rotation on visible, as on the web UI's
    // Look page.
    if (key === 'rainbowRotate' && on) {
      patch.rainbowSeed = 1 + Math.floor(Math.random() * (RAINBOW.length - 1));
    }
    await writeAppearance(patch);
    const next = await readAppearance();
    applyRainbow(next.rainbow);
    await renderAppearance();
  });
}
wireRainbowSwitch(rainbowOnEl, 'rainbow');
wireRainbowSwitch(rainbowReactiveEl, 'rainbowReactive');
wireRainbowSwitch(rainbowRotateEl, 'rainbowRotate');

/**
 * Takes the look from the default instance, or goes back to the local one.
 * The look is fetched once and stored, so no page waits on the relay before
 * painting; Refresh in the group card picks up later changes.
 */
followInstanceEl.addEventListener('click', async () => {
  const on = followInstanceEl.getAttribute('aria-checked') !== 'true';
  if (on) {
    followInstanceEl.setAttribute('aria-checked', 'true');
    // A snapshot first, so switching off restores the local look.
    await stashLocalLook();
    const ok = await adoptFromInstance().catch(() => false);
    if (!ok) {
      followInstanceEl.setAttribute('aria-checked', 'false');
      say(t('options.followFailed'), false);
      // The switch snaps back visibly.
      shake(followInstanceEl);
      return;
    }
  } else {
    await restoreLocalLook();
  }
  await writeAppearance({ followInstance: on });
  const next = await readAppearance();
  applyShape(next.shape);
  applyAccent(next.accent);
  applyRainbow(next.rainbow);
  await renderAppearance();
});

/**
 * The keys adopting the instance's look replaces or clears, and so the ones
 * the stash keeps. The theme is never adopted.
 */
const ADOPTED_KEYS = [
  'accent', 'accentSlotChosen', 'accentCustoms',
  'shape', 'rainbow', 'rainbowReactive', 'rainbowRotate', 'rainbowSeed', 'rainbowPalette',
];

async function stashLocalLook() {
  const mine = await chrome.storage.local.get(ADOPTED_KEYS);
  await chrome.storage.local.set({ lookBeforeFollow: mine });
}

/**
 * restoreLocalLook puts back the look from before adopting and drops the
 * snapshot. Keys that were absent are removed, since chrome.storage would keep
 * an undefined as a value.
 */
async function restoreLocalLook() {
  const { lookBeforeFollow } = await chrome.storage.local.get('lookBeforeFollow');
  if (!lookBeforeFollow) return;
  const put = {};
  const drop = [];
  for (const k of ADOPTED_KEYS) {
    if (k in lookBeforeFollow) put[k] = lookBeforeFollow[k];
    else drop.push(k);
  }
  if (drop.length) await chrome.storage.local.remove(drop);
  if (Object.keys(put).length) await chrome.storage.local.set(put);
  await chrome.storage.local.remove('lookBeforeFollow');
}

const themeSeg = document.getElementById('themeSeg');
const shapeSeg = document.getElementById('shapeSeg');
const accentSwatches = document.getElementById('accentSwatches');

/**
 * paintHues gives every card a palette position and the three rainbow rows
 * their own run. Rerun after every appearance change, since rotation changes
 * what each position resolves to.
 */
function paintHues() {
  // Positions go on the card, so everything inside that uses --accent follows.
  setHues([...document.querySelectorAll('.glim-card')]);
  // The rainbow rows are a set of their own, as on the web UI's Look page.
  setHues([rainbowRow, rainbowReactiveRow, rainbowRotateRow]);
}

/**
 * swatch builds one round colour button. A click selects a swatch that is not
 * in force and opens the picker on the one that is; without `onPick`, as in
 * the palette row where all colours are in force, a click always edits.
 */
function swatch(hex, { label: name, pressed, onPick, onEdit, onEditClose }) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'glim-swatch';
  b.style.backgroundColor = hex;
  b.setAttribute('data-tip', name);
  b.setAttribute('aria-label', name);
  if (pressed !== undefined) b.setAttribute('aria-pressed', String(pressed));
  b.addEventListener('click', () => {
    if (onPick && !pressed) {
      onPick();
      return;
    }
    if (onEdit) openColorPickerPopover(b, hex, onEdit, onEditClose);
    else onPick?.();
  });
  return b;
}

/** The reset badge, at the badge size the swatches follow. */
function resetBadge(onClick) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'glim-reset';
  // Half the badge's box.
  b.appendChild(glyph(D_RETRY, 16));
  b.setAttribute('data-tip', t('options.accentReset'));
  b.setAttribute('aria-label', t('options.accentReset'));
  b.addEventListener('click', onClick);
  return b;
}

/**
 * segment builds a "well" selector: equal segments on a shared track, as the
 * web UI's Corners picker. aria-pressed marks the active one for screen readers
 * and the stylesheet alike.
 */
function segment(host, options, current, onPick) {
  host.innerHTML = '';
  for (const o of options) {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = o.label;
    b.setAttribute('aria-pressed', String(o.value === current));
    b.addEventListener('click', () => onPick(o.value));
    host.appendChild(b);
  }
}

/**
 * The mixed colour of each slot, updated on every drag frame, like the app's
 * liveOverride (mobile/src/theme/AppearanceContext.tsx). Reading the map back
 * from storage each frame could miss the previous frame's write and lose a
 * mix. renderAppearance reseeds it on every render.
 */
let liveAccentCustoms = {};

async function renderAppearance() {
  const a = await readAppearance();
  liveAccentCustoms = { ...a.accentCustoms };

  // Light and dark only; readAppearance selects the machine's setting until
  // the user picks one.
  segment(
    themeSeg,
    [
      { value: 'light', label: t('options.themeLight') },
      { value: 'dark', label: t('options.themeDark') },
    ],
    a.theme,
    async (v) => {
      await writeAppearance({ theme: v });
      applyTheme(v);
      await renderAppearance();
    },
  );

  segment(
    shapeSeg,
    [
      { value: 'round', label: t('options.shapeRound') },
      { value: 'soft', label: t('options.shapeSoft') },
      { value: 'square', label: t('options.shapeSquare') },
    ],
    a.shape,
    async (v) => {
      await writeAppearance({ shape: v });
      applyShape(v);
      await renderAppearance();
    },
  );

  const live = (a.accent || DEFAULT_ACCENT).toLowerCase();
  // The stored choice wins; the nearest preset is only the fresh-install
  // fallback, since once two slots hold the same colour the choice cannot be
  // derived. The app does the same (mobile/src/screens/SettingsScreen.tsx).
  const liveSlot = a.accentChosen !== undefined ? a.accentChosen : accentSlot(live);
  const customs = a.accentCustoms;

  accentSwatches.innerHTML = '';
  const pick = async (v) => {
    await writeAppearance({ accent: v });
    applyAccent(v);
    await renderAppearance();
  };
  /**
   * chooseSlot makes a slot's remembered colour the accent and stores the slot
   * as chosen.
   */
  const chooseSlot = async (i, hex) => {
    await writeAppearance({ accent: hex, accentSlotChosen: i });
    applyAccent(hex);
    await renderAppearance();
  };
  ACCENTS.forEach((x, i) => {
    // Each slot shows the colour it was last mixed to, so several slots can
    // hold mixes at once.
    const shown = (customs[String(i)] ?? x.hex).toLowerCase();
    const mine = i === liveSlot;
    // A ring marks the chosen slot; a tick would need its own ink on all eight
    // colours.
    const b = swatch(shown, {
      // A mixed slot is labelled with its value rather than the preset name.
      label: shown !== x.hex.toLowerCase() ? shown.toUpperCase() : x.name,
      pressed: mine,
      onPick: () => void chooseSlot(i, shown),
      onEdit: async (next) => {
        // Editing a slot also chooses it.
        const cust = { ...liveAccentCustoms, [String(i)]: next };
        liveAccentCustoms = cust;
        await writeAppearance({ accent: next, accentSlotChosen: i, accentCustoms: cust });
        applyAccent(next);
        // The swatch follows the drag directly, since redrawing the row would
        // replace the button the popover is anchored to.
        b.style.backgroundColor = next;
      },
      // The row is redrawn once the popover closes.
      onEditClose: () => void renderAppearance(),
    });
    accentSwatches.appendChild(b);
  });
  // The reset is always shown, like the palette's, so it can be found before
  // experimenting.
  accentSwatches.appendChild(
    resetBadge(async () => {
      // Removed rather than set to undefined, which chrome.storage would keep.
      await chrome.storage.local.remove(['accentSlotChosen', 'accentCustoms']);
      await pick('');
    }),
  );

  // The rainbow, with the same three switches in the same order as the web
  // UI's Look page.
  rainbowOnEl.setAttribute('aria-checked', String(a.rainbow.on));
  rainbowReactiveEl.setAttribute('aria-checked', String(a.rainbow.reactive));
  rainbowRotateEl.setAttribute('aria-checked', String(a.rainbow.rotate));
  // The dimming for these rows and for the follow switch happens together at
  // the end, so two passes cannot overwrite each other.

  paletteSwatches.innerHTML = '';
  a.rainbow.palette.forEach((hex, i) => {
    // A click edits that position through the accent's popover.
    const b = swatch(hex, {
      label: t('options.palettePosition', { position: i + 1 }),
      onEdit: async (next) => {
        const palette = (await readAppearance()).rainbow.palette.slice();
        palette[i] = next;
        await writeAppearance({ rainbowPalette: palette });
        applyRainbow((await readAppearance()).rainbow);
        b.style.backgroundColor = next;
        paintHues();
      },
      onEditClose: () => void renderAppearance(),
    });
    // Each swatch also wears its own position, so the row shows the mode.
    setHue(b, i);
    paletteSwatches.appendChild(b);
  });
  // Back to the shipped palette. Always shown, since an unreadable palette is
  // when the way back is hardest to find.
  paletteSwatches.appendChild(
    resetBadge(async () => {
      await writeAppearance({ rainbowPalette: null });
      applyRainbow((await readAppearance()).rainbow);
      await renderAppearance();
    }),
  );

  followInstanceEl.setAttribute('aria-checked', String(a.followInstance));

  // Without a group the follow switch gives way to a notice (GlimStone
  // 1.15.0). `joined` comes from the phrase, not the roster, so an instance
  // that is briefly offline does not make the switch come and go.
  const joined = (await readPhrase()) !== '';
  followInstanceRow.hidden = !joined;
  followUnavailableEl.hidden = joined;

  // Reactive, rotation and the palette are hidden rather than dimmed while the
  // rainbow is off (GlimStone 1.10.0); the mode's own switch stays.
  const rainbowIsOff = !a.rainbow.on;
  rainbowReactiveRow.hidden = rainbowIsOff;
  rainbowRotateRow.hidden = rainbowIsOff;
  paletteRow.hidden = rainbowIsOff;

  // While the instance decides the look, its settings are shown but refused
  // through the controls' own `disabled`; the theme stays local. The labels'
  // words are dimmed so the "(i)" stays readable. `&& joined` covers a stored
  // followInstance left over without a group.
  const off = a.followInstance && joined;
  setRefused([shapeSeg], off);
  setRefused([rainbowOnEl, rainbowReactiveEl, rainbowRotateEl], off);
  setRefused([paletteSwatches], off);
  setDimmed([labelWordsOf(rainbowLabelEl)], off);
  setDimmed([labelWordsOf(rainbowReactiveLabelEl)], off);
  setDimmed([labelWordsOf(rainbowRotateLabelEl)], off);
  setDimmed([labelWordsOf(paletteLabelEl)], off);

  // Under the rainbow the accent still paints the popup's tab strip and
  // collector buttons, so the row is dimmed but stays usable, and its bubble
  // says the rainbow is in charge (GlimStone 1.16.0), with the web UI's
  // sentence. Following the instance refuses it outright.
  setDimmed([labelWordsOf(accentLabelEl), accentSwatches], off || a.rainbow.on);
  setRefused([accentSwatches], off);
  glimSetInfo(
    'accentLabel',
    a.rainbow.on ? `${t('options.accentHint')} ${t('options.accentRainbowOwns')}` : t('options.accentHint'),
  );

  // The hues last, since the palette decides what each position resolves to;
  // the report names the look too.
  paintHues();
  void renderReport();
}

// Problems: a report to paste into an issue, so it arrives with the version
// and the shape of the setup. It leaves out anything private: no instance
// address, no token, no relay key.

/**
 * The GlimStone release this extension implements, kept by hand since there is
 * no package to import it from. Each surface is lifted separately, so the
 * number is per surface. The extension has no motion setting; its fixed
 * motion uses the top level's numbers.
 */
const GLIMSTONE_VERSION = '1.17.0';

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
const CONTACT_MAIL = 'hello@halleluja.design';
const COFFEE_URL = 'https://buymeacoffee.com/junkerderprovinz';

/** A version number linking to its release, opened in a new tab. */
function versionLink(href, label) {
  const a = document.createElement('a');
  a.href = href;
  a.target = '_blank';
  a.rel = 'noreferrer noopener';
  a.className = 'versionLink';
  a.textContent = label;
  return a;
}

/**
 * The About card: the versions, a thank-you and the ways to report something.
 * The extension's version comes from the manifest.
 */
function renderAbout() {
  const versions = document.getElementById('aboutVersions');
  const text = document.getElementById('aboutText');
  const gh = document.getElementById('aboutGithub');
  const mail = document.getElementById('aboutMail');
  if (!versions) return;
  // Both versions link to their release. Extension tags carry an "extension/"
  // prefix because the repository ships three products.
  versions.replaceChildren(
    document.createTextNode(`${t('options.aboutVersion')} `),
    versionLink(`${REPO_URL}/releases/tag/extension/v${chrome.runtime.getManifest().version}`, chrome.runtime.getManifest().version),
    document.createTextNode(' · GlimStone '),
    versionLink(`${GLIMSTONE_URL}/releases/tag/v${GLIMSTONE_VERSION}`, GLIMSTONE_VERSION),
  );
  text.textContent = t('options.aboutText');
  const coffeeText = document.getElementById('aboutCoffee');
  const coffeeBtn = document.getElementById('aboutCoffeeBtn');
  if (coffeeText) coffeeText.textContent = t('options.aboutCoffee');
  if (coffeeBtn) {
    coffeeBtn.href = COFFEE_URL;
    coffeeBtn.replaceChildren(glyph(D_COFFEE, 14), document.createTextNode(t('options.aboutCoffeeButton')));
  }
  const reportText = document.getElementById('aboutReport');
  if (reportText) reportText.textContent = t('options.aboutReport');
  gh.href = REPO_URL;
  gh.replaceChildren(glyph(D_GITHUB, 15), document.createTextNode(t('options.aboutGithub')));
  // The subject names the product; a prefilled body would read like a form.
  mail.href = `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent('KnightLoader ' + t('options.aboutMailSubject'))}`;
  mail.replaceChildren(glyph(D_MAIL, 14), document.createTextNode(t('options.aboutMail')));
}

const reportEl = document.getElementById('report');
const copyReportBtn = document.getElementById('copyReport');

async function buildReport() {
  // The relay is asked, so the report tells an unreachable group from a
  // healthy one.
  const joined = (await readPhrase()) !== '';
  let reachable = 'not joined';
  if (joined) {
    try {
      reachable = `${(await groupInstances()).length} online`;
    } catch {
      reachable = 'the relay could not be reached';
    }
  }
  const { cnlEnabled } = await chrome.storage.local.get('cnlEnabled');
  // A switched-off feature, missing or stale scripts and an unloaded redirect
  // rule all look the same from outside, so the report lists each script with
  // its world and fallback, and the enabled rulesets.
  let registered = '?';
  try {
    const s = await chrome.scripting.getRegisteredContentScripts();
    registered = s.length
      ? s.map((x) => `${x.id}/${x.world || 'ISOLATED'}${x.matchOriginAsFallback ? '+fallback' : ''}`).join(' ')
      : 'none';
  } catch (e) {
    registered = `unreadable (${e instanceof Error ? e.message : String(e)})`;
  }
  let rules = '?';
  try {
    const en = await chrome.declarativeNetRequest.getEnabledRulesets();
    rules = en.length ? en.join(' ') : 'none';
  } catch (e) {
    rules = `unavailable (${e instanceof Error ? e.message : String(e)})`;
  }
  const a = await readAppearance();
  const m = chrome.runtime.getManifest();
  return [
    `extension: ${m.version}`,
    `browser:   ${navigator.userAgent}`,
    `language:  ${currentLanguage()} (browser: ${navigator.language})`,
    `appearance: theme=${a.theme || 'system'} shape=${a.shape} accent=${a.accent || 'default'} rainbow=${a.rainbow?.on ? 'on' : 'off'}`,
    `group:     ${joined ? 'joined' : 'no phrase stored'} (${reachable})`,
    `default:   ${(await readDefaultTarget()) ? 'chosen' : 'first in the group'}`,
    `clicknload: ${cnlEnabled !== false ? 'on' : 'off'}`,
    `  scripts: ${registered}`,
    `  rules:   ${rules}`,
  ].join('\n');
}

async function renderReport() {
  const text = await buildReport();
  reportEl.textContent = text;
  copyReportBtn.replaceChildren(glyph(D_COPY, 14), document.createTextNode(t('options.problemsCopy')));
}

copyReportBtn.addEventListener('click', async () => {
  const text = await buildReport();
  try {
    await navigator.clipboard.writeText(text);
    say(t('options.problemsCopied'), true);
  } catch {
    // Not every browser allows the clipboard here; the report is on screen to
    // copy by hand.
    say(t('options.problemsCopyFailed'), false);
    shake(copyReportBtn);
  }
});

(async () => {
  await applyAppearance();
  await loadLanguage();
  // Before any data-tip is written, so the first hover finds the bubble.
  wireTooltips();
  applyStaticText();
  await renderLanguagePicker();
  await renderCnl();
  await renderAppearance();
  renderAbout();
  await renderPinHint();
  void renderReport();
  // Not awaited: the page stays usable while the relay answers.
  void renderGroup();
})();
