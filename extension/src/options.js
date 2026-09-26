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
const rainbowDiscoEl = document.getElementById('rainbowDisco');
const rainbowRow = document.getElementById('rainbowRow');
const rainbowReactiveRow = document.getElementById('rainbowReactiveRow');
const rainbowRotateRow = document.getElementById('rainbowRotateRow');
const rainbowDiscoRow = document.getElementById('rainbowDiscoRow');
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
 * A brand mark keeps fill="currentColor" and takes its colour from the class
 * on its box (`.glim-paypal-mark` and the others in glimstone.css). A coin
 * paints its disc with var(--mark-ink, currentColor), which a lit brand tile
 * sets to its ink.
 */
const NS = 'http://www.w3.org/2000/svg';
function glyph(d, size, box = '0 0 16 16', fill = 'currentColor') {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', box);
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS(NS, 'path');
  path.setAttribute('fill', fill);
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
// PayPal from Simple Icons, CC0 1.0, on a 24-unit grid, the mark the web UI's
// About card draws (web/src/components/donateMarks.tsx).
const D_PAYPAL =
  'M15.607 4.653H8.941L6.645 19.251H1.82L4.862 0h7.995c3.754 0 6.375 2.294 6.473 5.513-.648-.478-2.105-.86-3.722-.86' +
  'm6.57 5.546c0 3.41-3.01 6.853-6.958 6.853h-2.493L11.595 24H6.74l1.845-11.538h3.592c4.208 0 7.346-3.634 7.153-6.949' +
  'a5.24 5.24 0 0 1 2.848 4.686M9.653 5.546h6.408c.907 0 1.942.222 2.363.541-.195 2.741-2.655 5.483-6.441 5.483H8.714Z';
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
// A filled bin in one path.
const D_TRASH =
  'M6.5 1h3a1 1 0 0 1 1 1v1H13v1.5H3V3h2.5V2a1 1 0 0 1 1-1zm.5 2h2v-.5H7V3z' +
  'M4 5.5h8l-.6 8.1a1.4 1.4 0 0 1-1.4 1.4H6a1.4 1.4 0 0 1-1.4-1.4L4 5.5z';
// A solid clipboard with its clip, the same shape the app's paste button uses.
const D_PASTE =
  'M4.1 2.8h7.8a1.6 1.6 0 0 1 1.6 1.6v8.8a1.6 1.6 0 0 1-1.6 1.6H4.1a1.6 1.6 0 0 1-1.6-1.6V4.4a1.6 1.6 0 0 1 1.6-1.6z' +
  'M5.8 1.2h4.4a.8.8 0 0 1 .8.8v1a.8.8 0 0 1-.8.8H5.8a.8.8 0 0 1-.8-.8V2a.8.8 0 0 1 .8-.8z';
/**
 * The web UI's IconEye and IconEyeOff (web/src/lib/icons.tsx), copied shape for
 * shape so the reveal eye looks the same everywhere: an almond with the iris
 * cut out, and for the shown phrase the same almond at .55 behind a bar.
 */
function eyeGlyph(off, size) {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 20 20');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('fill', 'currentColor');
  svg.setAttribute('aria-hidden', 'true');
  const eye = document.createElementNS(NS, 'path');
  eye.setAttribute('fill-rule', 'evenodd');
  eye.setAttribute(
    'd',
    'M2.5 10C2.5 10 6 4.3 10 4.3C14 4.3 17.5 10 17.5 10C17.5 10 14 15.7 10 15.7C6 15.7 2.5 10 2.5 10Z' +
      'M12.6 10a2.6 2.6 0 1 1 -5.2 0 2.6 2.6 0 0 1 5.2 0Z',
  );
  svg.appendChild(eye);
  if (off) {
    eye.setAttribute('opacity', '.55');
    const bar = document.createElementNS(NS, 'rect');
    bar.setAttribute('x', '9.1');
    bar.setAttribute('width', '1.8');
    bar.setAttribute('height', '20');
    bar.setAttribute('rx', '0.9');
    bar.setAttribute('transform', 'rotate(45 10 10)');
    svg.appendChild(bar);
  }
  return svg;
}

/** Buy Me a Coffee's button artwork (appMarks.js), cup and lettering. */
function coffeeArt() {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', COFFEE_BUTTON_ART.box);
  svg.setAttribute('aria-hidden', 'true');
  for (const p of COFFEE_BUTTON_ART.paths) {
    const path = document.createElementNS(NS, 'path');
    path.setAttribute('fill', p.fill);
    if (p.evenodd) path.setAttribute('fill-rule', 'evenodd');
    path.setAttribute('d', p.d);
    svg.appendChild(path);
  }
  return svg;
}

/**
 * The mail button's envelope, closed and open in one box. The open one takes
 * over under the pointer through GlimStone's .glim-mark-rest and
 * .glim-mark-hover.
 */
function mailMarks() {
  return [
    ['glim-mark-rest', MAIL_MARK.rest],
    ['glim-mark-hover', MAIL_MARK.hover],
  ].map(([cls, d]) => {
    const svg = glyph(d, 14, MAIL_MARK.box);
    svg.setAttribute('class', cls);
    return svg;
  });
}

/**
 * The check a copy button shows once the copy landed, one stroke that
 * GlimStone's glim-check-draw traces. pathLength="1" lets the dash offset run
 * from 1 to 0 whatever the path's real length.
 */
function checkGlyph(size) {
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 16 16');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS(NS, 'path');
  path.setAttribute('class', 'glim-check-draw');
  path.setAttribute('pathLength', '1');
  // As wide as the copy glyph it replaces, so the label does not shift.
  path.setAttribute('d', 'M2.5 8.5l3.5 3.5 7.5-8');
  path.setAttribute('fill', 'none');
  path.setAttribute('stroke', 'currentColor');
  path.setAttribute('stroke-width', '2');
  path.setAttribute('stroke-linecap', 'round');
  path.setAttribute('stroke-linejoin', 'round');
  svg.appendChild(path);
  return svg;
}

/**
 * readmeFace fills one of the About card's README buttons (GlimStone's
 * .glim-readme-btn): its mark, painted at rest by `markClass`, and its name on
 * one line, as on the README's give buttons, so nothing moves up under the
 * pointer. `art` is a vendor's own button artwork in place of both.
 */
function readmeFace(el, { name, mark, markClass, art }) {
  el.setAttribute('aria-label', name);
  if (art) {
    const box = document.createElement('span');
    box.className = 'glim-readme-btn-art';
    box.setAttribute('aria-hidden', 'true');
    box.appendChild(art);
    el.replaceChildren(box);
    return;
  }
  const markBox = document.createElement('span');
  markBox.className = `glim-readme-btn-mark ${markClass}`;
  markBox.setAttribute('aria-hidden', 'true');
  markBox.append(...[mark].flat());
  const text = document.createElement('span');
  text.className = 'glim-readme-btn-text';
  const label = document.createElement('span');
  label.className = 'glim-readme-btn-name';
  label.textContent = name;
  text.appendChild(label);
  el.replaceChildren(markBox, text);
}

/**
 * Shrinks a translation longer than its README button instead of cutting it
 * off. A line measures nothing before it is laid out, so the rows are fitted
 * again whenever they resize and once the fonts are in.
 */
function fitReadmeText(root) {
  for (const line of root.querySelectorAll('.glim-readme-btn-name')) {
    line.style.fontSize = '';
    if (line.clientWidth === 0) continue;
    const over = line.scrollWidth / line.clientWidth;
    if (over > 1) line.style.fontSize = `${parseFloat(getComputedStyle(line).fontSize) / over}px`;
  }
}

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
  label('rainbowDiscoLabel', t('options.disco'), t('options.discoGlideHint'));
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
  phraseEye.replaceChildren(eyeGlyph(shown, 14));
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
    empty.className = 'emptyState glim-content-fade';
    empty.appendChild(instancesGlyph(28));
    const title = document.createElement('span');
    title.textContent = t('options.groupEmpty');
    empty.appendChild(title);
    list.appendChild(empty);
    return;
  }

  // The same card the popup draws (shared.js), without its queue and open
  // actions: this page sets the group up and the popup operates it (GlimStone
  // rule 22), so a settings card never carries buttons that act on a running
  // instance. The popup keeps all three. The default is changed by
  // right-clicking another card.
  const preferred = defaultOf(siblings, await readDefaultTarget());
  const cards = siblings.map((inst, i) => [
    inst.instanceId,
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
    }),
  ]);
  staggerRows(rosterArrivals, cards);
  list.append(...cards.map(([, card]) => card));
}

/** When each instance's card first came in, kept across redraws (staggerRows). */
const rosterArrivals = new Map();

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
    confirmPulse(joinBtn);
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
  // A group joined later arrives afresh, even with the same instances in it.
  rosterArrivals.clear();
  // Leaving stops following and restores the local look. Without a group the
  // follow switch is removed, so nothing could turn it off later.
  const { followInstance } = await chrome.storage.local.get('followInstance');
  if (followInstance === true) {
    await restoreLocalLook();
    await writeAppearance({ followInstance: false });
    const back = await readAppearance();
    applyShape(back.shape);
    wipeColours();
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

/**
 * Whether disco has been found while this page is open. Page state and never
 * storage: once somebody leaves with disco off, the switch is gone until the
 * gesture is made again. The turn-ons are counted here too.
 */
let discoFound = false;
const discoTaps = { taps: 0, last: 0 };

/** The same for the leaf, the shape found by tapping a chosen square five more
 *  times (leafTap in appearance.js). */
let leafFound = false;
const leafTaps = { taps: 0 };

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
    // The fifth quick turn-on of the rainbow unlocks disco and starts it, so
    // the gesture ends on a palette that is already walking.
    if (key === 'rainbow' && discoTap(discoTaps, on, Date.now())) {
      patch.rainbowDisco = true;
      discoFound = true;
    }
    await writeAppearance(patch);
    const next = await readAppearance();
    // Disco walks the palette on its own clock, and a wipe would only drag
    // behind it.
    if (key !== 'rainbowDisco') wipeColours();
    applyRainbow(next.rainbow);
    await renderAppearance();
  });
}

let wipeTimer = 0;

/**
 * wipeColours lets the next change of the rainbow, or of the whole look, cross
 * the page together instead of every hued element snapping on its own
 * (GlimStone's colour wipe). The class stays only as long as the wipe, so
 * ordinary hovers keep their own timing. Not for a colour being dragged in the
 * picker, which has to follow the pointer.
 */
function wipeColours() {
  const root = document.documentElement;
  root.classList.add('glim-colour-wipe');
  clearTimeout(wipeTimer);
  wipeTimer = setTimeout(() => root.classList.remove('glim-colour-wipe'), motionMs('--motion-wipe-dur') + 50);
}
wireRainbowSwitch(rainbowOnEl, 'rainbow');
wireRainbowSwitch(rainbowReactiveEl, 'rainbowReactive');
wireRainbowSwitch(rainbowRotateEl, 'rainbowRotate');
wireRainbowSwitch(rainbowDiscoEl, 'rainbowDisco');

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
    // The instance answered, which is the part that could have failed.
    confirmPulse(followInstanceEl);
  } else {
    await restoreLocalLook();
  }
  await writeAppearance({ followInstance: on });
  const next = await readAppearance();
  applyShape(next.shape);
  wipeColours();
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
 * paintHues gives every card a palette position and the rainbow rows their own
 * run. A position points at the root's colours, so a palette change or a
 * rotation needs no second pass.
 */
function paintHues() {
  // Positions go on the card, so everything inside that uses --accent follows.
  setHues([...document.querySelectorAll('.glim-card')]);
  // The rainbow rows are a set of their own, as on the web UI's Look page.
  setHues([rainbowRow, rainbowReactiveRow, rainbowRotateRow, rainbowDiscoRow]);
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

/**
 * The rainbow palette as the swatches show it, updated on every drag frame for
 * the reason liveAccentCustoms is: a frame that read the palette back from
 * storage could miss the frame before it and write that colour back over its
 * edit. renderAppearance reseeds it.
 */
let livePalette = [];

/**
 * editPaletteColour stores one position of the rainbow palette and paints it
 * while the picker is still open. A walking disco keeps a loop built from the
 * palette it started with, and its next frame would paint those colours back,
 * so it is applied again too.
 */
async function editPaletteColour(i, hex) {
  const palette = livePalette.slice();
  palette[i] = hex;
  livePalette = palette;
  await writeAppearance({ rainbowPalette: palette });
  const a = await readAppearance();
  applyRainbow(a.rainbow);
  applyDisco(a.disco);
}

async function renderAppearance() {
  const a = await readAppearance();
  liveAccentCustoms = { ...a.accentCustoms };
  livePalette = a.rainbow.palette.slice();

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

  // A leaf in force counts as found, so its segment stays until the page is
  // left, as disco's switch does.
  if (a.shape === 'leaf') leafFound = true;
  segment(
    shapeSeg,
    [
      { value: 'round', label: t('options.shapeRound') },
      { value: 'soft', label: t('options.shapeSlightlyRounded') },
      { value: 'square', label: t('options.shapeSquare') },
      ...(leafFound ? [{ value: 'leaf', label: t('options.shapeLeaf') }] : []),
    ],
    a.shape,
    async (v) => {
      // Taps on a chosen square count toward the hidden shape.
      const found = leafTap(leafTaps, v, a.shape);
      if (found) leafFound = true;
      const next = found ?? v;
      await writeAppearance({ shape: next });
      applyShape(next);
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
  rainbowDiscoEl.setAttribute('aria-checked', String(a.disco));
  // A switch that hid the value it is showing would be lying, so disco in
  // force counts as found, and the switch stays until the page is left.
  if (a.disco) discoFound = true;
  // The dimming for these rows and for the follow switch happens together at
  // the end, so two passes cannot overwrite each other.

  paletteSwatches.innerHTML = '';
  a.rainbow.palette.forEach((hex, i) => {
    // A click edits that position through the accent's popover.
    const b = swatch(hex, {
      label: t('options.palettePosition', { position: i + 1 }),
      onEdit: async (next) => {
        await editPaletteColour(i, next);
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
      const back = await readAppearance();
      wipeColours();
      applyRainbow(back.rainbow);
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
  rainbowDiscoRow.hidden = rainbowIsOff || !discoFound;
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

  // The report names the look, so it comes last. Disco before it: it walks on
  // from the stored palette, which the calls above have just applied.
  paintHues();
  applyDisco(a.disco);
  void renderReport();
}

// Problems: a report to paste into an issue, so it arrives with the version
// and the shape of the setup. It leaves out anything private: no instance
// address, no token, no relay key.

// The GlimStone version comes from appearance.js, beside the ports it
// describes, rather than from a number kept next to this card.
const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
const CONTACT_MAIL = 'hello@halleluja.design';
// The widget page for the coffee handle in the README's donate row.
const COFFEE_WIDGET_URL = 'https://buymeacoffee.com/widget/page/junkerderprovinz?color=%23FFDD00';
// PayPal's hosted donation button, the same address the web UI's About card
// and the README's donate row use. GlimStone's PayPal window needs a PayPal
// app's client id and a plan per interval, which this project does not have.
const PAYPAL_URL = 'https://www.paypal.com/donate/?hosted_button_id=76FVV52TKXTUS';

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
  if (coffeeText) coffeeText.textContent = t('options.aboutCoffee');
  // The artwork carries the words, which are the button's name as well.
  readmeFace(coffeeBtn, { name: t('options.aboutCoffeeButton'), art: coffeeArt() });
  const paypalBtn = document.getElementById('aboutPaypalBtn');
  if (paypalBtn) {
    paypalBtn.href = PAYPAL_URL;
    readmeFace(paypalBtn, {
      name: t('options.aboutPaypal'),
      mark: glyph(D_PAYPAL, 14, '0 0 24 24'),
      markClass: 'glim-paypal-mark',
    });
  }
  // Bitcoin's letterform reads as "crypto" to somebody who has never held any;
  // the window then shows every coin on offer, so nobody takes it for the only
  // one.
  readmeFace(cryptoBtn, {
    name: t('options.aboutCrypto'),
    mark: glyph(BTC_LETTER.d, 14, BTC_LETTER.box),
    markClass: 'glim-bitcoin-mark',
  });
  const reportText = document.getElementById('aboutReport');
  if (reportText) reportText.textContent = t('options.aboutReport');
  gh.href = REPO_URL;
  readmeFace(gh, { name: t('options.aboutGithub'), mark: glyph(D_GITHUB, 14), markClass: 'glim-github-mark' });
  // The subject names the product; a prefilled body would read like a form.
  mail.href = `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent('KnightLoader ' + t('options.aboutMailSubject'))}`;
  readmeFace(mail, { name: t('options.aboutMail'), mark: mailMarks(), markClass: 'glim-house-mark' });
  for (const row of aboutRows) fitReadmeText(row);
}

const aboutRows = [document.getElementById('aboutGive'), document.getElementById('aboutReportRow')];
for (const row of aboutRows) new ResizeObserver(() => fitReadmeText(row)).observe(row);
void document.fonts?.ready.then(() => aboutRows.forEach(fitReadmeText));

// The coffee window: Buy Me a Coffee's widget in a window of our own, so a donor
// pays without leaving the page. The frame exists only while the window is open.

const coffeeBtn = document.getElementById('aboutCoffeeBtn');
const coffeeEl = document.getElementById('coffeeDonate');
const coffeeFrameEl = document.getElementById('coffeeFrame');
const coffeeCloseEl = document.getElementById('coffeeClose');

function openCoffee() {
  document.getElementById('coffeeTitle').textContent = t('options.aboutCoffeeButton');
  glimSetInfo('coffeeHeading', t('options.coffeeIntro'));
  document.getElementById('coffeeAppeal').textContent = t('options.donateAppeal');
  coffeeCloseEl.replaceChildren(glyph(D_CROSS, 14), document.createTextNode(t('common.close')));
  const frame = document.createElement('iframe');
  frame.src = COFFEE_WIDGET_URL;
  frame.title = t('options.aboutCoffeeButton');
  frame.allow = 'payment';
  coffeeFrameEl.replaceChildren(frame);
  coffeeEl.hidden = false;
  coffeeCloseEl.focus();
  document.addEventListener('keydown', onCoffeeKey);
}

function closeCoffee() {
  coffeeEl.hidden = true;
  coffeeFrameEl.replaceChildren();
  document.removeEventListener('keydown', onCoffeeKey);
  coffeeBtn.focus();
}

// Tab is left alone: the frame's own fields are stops a trap would skip.
function onCoffeeKey(event) {
  if (event.key !== 'Escape') return;
  event.preventDefault();
  closeCoffee();
}

coffeeBtn.addEventListener('click', openCoffee);
coffeeCloseEl.addEventListener('click', closeCoffee);
// Only a press on the backdrop itself closes it.
coffeeEl.addEventListener('click', (event) => {
  if (event.target === coffeeEl) closeCoffee();
});

// The crypto window: pick a coin, then its chain, and get the address as a code,
// as text and through a copy button. Nothing leaves the browser, no account is
// needed at either end, and every chain on offer carries its own address
// (donate.js), so a coin cannot be sent where nobody receives it.

const cryptoBtn = document.getElementById('aboutCryptoBtn');
const cryptoEl = document.getElementById('cryptoDonate');
const cryptoTitleEl = document.getElementById('cryptoTitle');
const cryptoQrEl = document.getElementById('cryptoQr');
const cryptoAddressEl = document.getElementById('cryptoAddress');
const cryptoChainsEl = document.getElementById('cryptoChains');
const cryptoNoteEl = document.getElementById('cryptoNote');
const cryptoCopyEl = document.getElementById('cryptoCopy');
const cryptoCoinsEl = document.getElementById('cryptoCoins');
const cryptoCloseEl = document.getElementById('cryptoClose');

let cryptoCoin = CRYPTO_COINS[0];
let cryptoNetwork = cryptoCoin.networks[0];
let cryptoCopiedTimer = 0;

/**
 * qrSvg draws an address as a QR code, the module grid qrcode-generator makes
 * with the web UI's settings: the smallest version that fits, at level M, which
 * suits a screen. One path for all modules keeps the node count down, and
 * crispEdges keeps the modules square at any size.
 */
function qrSvg(value, size) {
  const qr = qrcode(0, 'M');
  qr.addData(value);
  qr.make();
  const n = qr.getModuleCount();
  let d = '';
  for (let y = 0; y < n; y++) {
    for (let x = 0; x < n; x++) if (qr.isDark(y, x)) d += `M${x} ${y}h1v1h-1z`;
  }
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', `0 0 ${n} ${n}`);
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('shape-rendering', 'crispEdges');
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', value);
  const path = document.createElementNS(NS, 'path');
  path.setAttribute('fill', '#000000');
  path.setAttribute('d', d);
  svg.appendChild(path);
  return svg;
}

/** Everything that depends on the chosen coin and chain, drawn again on each
 *  pick. The positions follow each set's own order, so rainbow mode tells the
 *  tiles and the chips apart. */
function renderCrypto() {
  cryptoTitleEl.textContent = t('options.cryptoTitle');
  glimSetInfo('cryptoHeading', t('options.cryptoIntro'));
  document.getElementById('cryptoAppeal').textContent = t('options.donateAppeal');
  cryptoQrEl.replaceChildren(qrSvg(cryptoNetwork.address, 168));
  cryptoAddressEl.textContent = cryptoNetwork.address;

  // Shown for a coin with one chain as well: it also says which network the
  // address belongs to, which must not come and go with the tile that is lit.
  cryptoChainsEl.setAttribute('aria-label', t('options.cryptoNetworks'));
  cryptoChainsEl.replaceChildren(
    ...cryptoCoin.networks.map((n, i) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'glim-crypto-chip';
      b.setAttribute('role', 'option');
      const on = n.id === cryptoNetwork.id;
      b.setAttribute('aria-selected', String(on));
      b.classList.toggle('glim-active', on);
      b.textContent = n.name;
      setHue(b, i);
      // The row is drawn again, so focus goes to the chip that replaced this one.
      b.addEventListener('click', () => {
        cryptoNetwork = n;
        renderCrypto();
        cryptoChainsEl.querySelectorAll('button')[i]?.focus();
      });
      return b;
    }),
  );

  cryptoNoteEl.hidden = !cryptoNetwork.noteKey;
  cryptoNoteEl.textContent = cryptoNetwork.noteKey ? t(cryptoNetwork.noteKey) : '';

  // The copy button takes the coin's position, so in rainbow mode it matches
  // the tile the address came from.
  setHue(cryptoCopyEl, CRYPTO_COINS.indexOf(cryptoCoin));
  paintCryptoCopy(false);

  cryptoCoinsEl.setAttribute('aria-label', t('options.cryptoTitle'));
  cryptoCoinsEl.replaceChildren(
    ...CRYPTO_COINS.map((c, i) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'glim-crypto-tile glim-hue-icon';
      b.setAttribute('role', 'option');
      const on = c.id === cryptoCoin.id;
      b.setAttribute('aria-selected', String(on));
      b.classList.toggle('glim-active', on);
      b.setAttribute('aria-label', `${c.name} (${c.symbol})`);
      b.setAttribute('data-tip', c.name);
      const mark = COIN_MARKS[c.id];
      const ticker = document.createElement('span');
      ticker.textContent = c.symbol;
      b.append(glyph(mark.d, 24, mark.box, 'var(--mark-ink, currentColor)'), ticker);
      setHue(b, i);
      b.style.setProperty('--tile', c.tile.color);
      b.style.setProperty('--tile-ink', c.tile.ink);
      b.addEventListener('click', () => {
        // Always the new coin's first chain, never one carried over from the
        // last coin without being checked against this one.
        cryptoCoin = c;
        cryptoNetwork = c.networks[0];
        renderCrypto();
        cryptoCoinsEl.querySelectorAll('button')[i]?.focus();
      });
      return b;
    }),
  );
}

function paintCryptoCopy(copied) {
  cryptoCopyEl.replaceChildren(
    copied ? checkGlyph(14) : glyph(D_COPY, 14),
    document.createTextNode(t(copied ? 'common.copied' : 'common.copy')),
  );
}

function openCrypto() {
  renderCrypto();
  cryptoCloseEl.replaceChildren(glyph(D_CROSS, 14), document.createTextNode(t('common.close')));
  cryptoEl.hidden = false;
  cryptoCopyEl.focus();
  document.addEventListener('keydown', onCryptoKey);
}

function closeCrypto() {
  cryptoEl.hidden = true;
  clearTimeout(cryptoCopiedTimer);
  document.removeEventListener('keydown', onCryptoKey);
  cryptoBtn.focus();
}

/**
 * Escape closes the window, and Tab stays inside it, as aria-modal promises.
 * The stops include the (i) on the heading, which holds the window's
 * introduction and would otherwise open only under a mouse.
 */
function onCryptoKey(event) {
  if (event.key === 'Escape') {
    event.preventDefault();
    closeCrypto();
    return;
  }
  if (event.key !== 'Tab') return;
  const stops = [...cryptoEl.querySelectorAll('button, [tabindex="0"]')];
  const at = stops.indexOf(document.activeElement);
  event.preventDefault();
  const next = event.shiftKey ? (at <= 0 ? stops.length - 1 : at - 1) : at === stops.length - 1 ? 0 : at + 1;
  stops[next].focus();
}

cryptoBtn.addEventListener('click', openCrypto);
cryptoCloseEl.addEventListener('click', closeCrypto);
// Only a press on the backdrop itself closes it.
cryptoEl.addEventListener('click', (event) => {
  if (event.target === cryptoEl) closeCrypto();
});
cryptoCopyEl.addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(cryptoNetwork.address);
  } catch {
    // The address stays on screen whole, to be selected by hand.
    shake(cryptoCopyEl);
    return;
  }
  // The label says it landed: a clipboard write is otherwise invisible, and the
  // page's status line is behind the window.
  paintCryptoCopy(true);
  confirmPulse(cryptoCopyEl);
  clearTimeout(cryptoCopiedTimer);
  cryptoCopiedTimer = setTimeout(() => paintCryptoCopy(false), 1500);
});

const reportEl = document.getElementById('report');
const copyReportBtn = document.getElementById('copyReport');
let reportCopiedTimer = 0;

/** The copy button says it landed, as the crypto window's does. */
function paintReportCopy(copied) {
  copyReportBtn.replaceChildren(
    copied ? checkGlyph(14) : glyph(D_COPY, 14),
    document.createTextNode(t(copied ? 'common.copied' : 'options.problemsCopy')),
  );
}

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
  // The report waits on the relay, so its first text fades in; the class stays
  // and later rewrites land without it.
  if (!reportEl.textContent) reportEl.classList.add('glim-content-fade');
  reportEl.textContent = text;
  paintReportCopy(false);
}

copyReportBtn.addEventListener('click', async () => {
  const text = await buildReport();
  try {
    await navigator.clipboard.writeText(text);
    // Said at the button: the page's status line sits in the group card,
    // usually out of view from here.
    paintReportCopy(true);
    confirmPulse(copyReportBtn);
    clearTimeout(reportCopiedTimer);
    reportCopiedTimer = setTimeout(() => paintReportCopy(false), 1500);
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
  // Two frames after the stored shape is painted, so the first radii never
  // animate and every later change of shape does.
  requestAnimationFrame(() =>
    requestAnimationFrame(() => document.documentElement.classList.add('glim-shape-transitions')),
  );
})();
