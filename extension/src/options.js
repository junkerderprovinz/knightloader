// The options page: the group this browser belongs to, the language, the look,
// Click'n'Load, and a report for when something is wrong.
//
// The instance registry that used to live here is gone. It asked for a name and
// an address per instance, which was the pre-phrase model still standing in a
// product that had moved on (jdp, 2026-08-28: "Wieso muss man eine Instanz per
// Name & Adresse hinzufügen? Das soll doch jetzt alles ausschliesslich via
// Phrase laufen."). One phrase now replaces the list, the peer sync, the add
// form and the per-origin permission prompts they needed — see group.js.
//
// Every explanation on this page is an info bubble on a heading badge, never a
// grey paragraph under a control (jdp, same message: "Alle infotexte in i
// infobubbles!"), and the one switch is a real toggle rather than a native
// checkbox ("Nie checkboxen sondern immer toggles!"). Both rules are in the
// GlimStone guide now, so this page is following the language rather than
// carrying a local exception to it.

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
// The four labels whose rows get dimmed. Held here rather than looked up at the
// moment of dimming, because renderAppearance() runs on every appearance change
// and a querySelector per row per render is four lookups for a value that never
// moves.
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
 * The three glyphs this page draws itself. Filled shapes, not outlines, like
 * every other glyph in the language.
 *
 * Built with createElementNS rather than assigned as innerHTML, and that is
 * not fussiness: Mozilla's own linter fails the package on an innerHTML
 * assignment whose right-hand side is a variable (UNSAFE_VAR_ASSIGNMENT), and
 * that linter is a release gate here. Building the nodes is also the honest
 * version - there is no markup to parse and nothing that could ever be handed
 * a string from somewhere else.
 *
 * fill="currentColor" is right for a glyph WE drew and wrong for a vendor's
 * logo, and this one function makes both. It stays, because the About card's
 * brand buttons override it from the stylesheet rather than here: an author
 * rule beats a presentation attribute, so `.glim-brand-btn svg path` wins
 * (glimstone.css, the brand section). Deciding it per call site would put a
 * colour question inside a shape builder and leave the next glyph to get it
 * wrong again.
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
 * The one glyph on this page that is not a single path: the web UI's own
 * IconInstances (web/src/lib/icons.tsx), copied shape for shape rather than
 * redrawn, because which glyph means "other instances" is a contract across the
 * surfaces of one product and not a choice a page makes for itself. Two stacked
 * units, each with its own lamp - a shape nobody has to be taught.
 *
 * It exists for the group list's empty state, which is the one place on this
 * page that has nothing of its own to show.
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
// The cancelling half of the confirmation window. Every button in this family
// carries a glyph, and a words-only cancel beside a bin reads as unfinished -
// reported in exactly those terms on a sibling app ("löschen hat kein Glyph").
// The same path popup.js draws for its own cancel, so one mark means "no".
const D_CROSS =
  'M4.2 2.8 8 6.6l3.8-3.8 1.4 1.4L9.4 8l3.8 3.8-1.4 1.4L8 9.4l-3.8 3.8-1.4-1.4L6.6 8 2.8 4.2z';
// A cup with a handle and a saucer, for the thank-you in the About card.
const D_COFFEE =
  'M2.5 3h8.2v5.2a3.6 3.6 0 0 1-3.6 3.6H6.1A3.6 3.6 0 0 1 2.5 8.2V3z' +
  'M11.6 4.1h1.2a2.1 2.1 0 0 1 0 4.2h-1.2V6.9h1.2a.8.8 0 0 0 0-1.6h-1.2V4.1z' +
  'M1.6 13h10.8v1.5H1.6z';
// GitHub's own mark, on a 16-viewBox, for the button that goes there (jdp,
// 2026-09-01: "der Githubutton soll das github logo als glyph bekomen und nut
// GitHub heißen"). A button that carries a site's own logo and its own name
// needs no verb: it is obvious where it goes, and the sentence above it already
// said why.
//
// This is the ONE place in this family where a third party's mark is drawn
// rather than a glyph of our own, and it is deliberate: a logo is recognised or
// it is not, and an approximation of one is worse than the drawn-here glyph it
// replaced. Reproduced from GitHub's published Octicon "mark-github", which
// they offer for exactly this use.
const D_GITHUB =
  'M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49' +
  '-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82' +
  '.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15' +
  '-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27' +
  '1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95' +
  '.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.012 8.012 0 0 0 16 8c0-4.42-3.58-8-8-8z';
// Two offset sheets: the "copy" mark everywhere else, so nobody has to learn a
// second vocabulary for it. The back sheet is drawn first and the front one over
// it, and they are separate subpaths rather than one shape so the overlap reads
// as depth at 14px instead of as a blob.
const D_COPY =
  'M4 1.5h6.5a1 1 0 0 1 1 1V4H6a1.5 1.5 0 0 0-1.5 1.5V11H3a1 1 0 0 1-1-1V2.5a1 1 0 0 1 1-1z' +
  'M6.5 5.5h7a1 1 0 0 1 1 1v7a1 1 0 0 1-1 1h-7a1 1 0 0 1-1-1v-7a1 1 0 0 1 1-1z';
// An envelope: the body, with the flap cut out of it by fillRule so the V is
// the ground showing through rather than a shape painted in a guessed colour.
const D_MAIL =
  'M1.5 3.5h13v9h-13v-9zm1.6 1.4L8 8.4l4.9-3.5H3.1z';
// A filled bin: lid, handle, and a solid body with two slots carved by
// fillRule rather than layered over - the same technique the icon rule names
// for a gap inside a solid shape, and the reason this is one path and not three.
const D_TRASH =
  'M6.5 1h3a1 1 0 0 1 1 1v1H13v1.5H3V3h2.5V2a1 1 0 0 1 1-1zm.5 2h2v-.5H7V3z' +
  'M4 5.5h8l-.6 8.1a1.4 1.4 0 0 1-1.4 1.4H6a1.4 1.4 0 0 1-1.4-1.4L4 5.5z';
// A clipboard: the board, with the clip overlapping its top edge. Two filled
// subpaths in one path, the same shape the app draws from boxes on its own
// paste button, so the family has one mark for "the clipboard" and nobody has
// to learn a second. The board is solid rather than a frame, because an
// outlined mark among filled ones reads as borrowed from another icon set.
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
 * The page's one status line — and it clears itself.
 *
 * GlimStone's "Failure feedback" objects to a permanent sentence for one named
 * reason: it never clears itself, so a failure from ten minutes ago looks
 * exactly as current as one from a second ago until the next click overwrites
 * it. The language's own answer is a toast, which is a surface this extension
 * does not have; what it can have is the property the toast was wanted FOR. Four
 * seconds is the toast's own duration, so the two surfaces time out alike.
 *
 * The timer is cleared before it is set, or two messages in quick succession
 * leave the first one's clock running and it wipes the second mid-read.
 *
 * `hold` is for a PROGRESS message rather than a result — "connecting…", which
 * describes something still running and is always replaced by its own outcome.
 * That is not the case the rule is about: a sentence that ages badly is one
 * reporting something that already finished, and a progress line that vanished
 * on its own clock while the work was still in flight would be the opposite
 * defect.
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
 * labelWords puts a row label's WORDS in an element of their own.
 *
 * The label used to be a bare text node, written with `el.textContent = text`,
 * with the "(i)" appended after it. That shape cannot be dimmed correctly:
 * `opacity` composites a subtree, so the only element available to carry the
 * dimming was the label itself, and the trigger inside it went with it.
 * GlimStone 1.9.0 calls that a trap rather than a preference, and it is - the
 * one element that has to stay readable while the row recedes is the one nobody
 * can read, and it is invisible in the markup.
 *
 * Idempotent, like glimSetInfo(): applyStaticText() re-runs on every language
 * change, and this must not leave a second span behind or drop the icon that
 * follows it. prepend rather than append, so the words stay ahead of the "(i)".
 */
function labelWords(id, text) {
  const host = document.getElementById(id);
  if (!host) return;
  let span = host.querySelector('.glim-label-words');
  if (!span) {
    // The English placeholder the markup ships with ("Accent", "Palette") is a
    // loose text node, and `textContent = text` used to be what removed it.
    // Prepending a span next to it would have printed the word twice, in two
    // languages, on the first render. Only text nodes go: the "(i)" is an
    // element and has to survive a language change.
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
 * setDimmed MARKS a setting, on its PARTS rather than on its row. It no longer
 * deadens it, and that split is GlimStone 1.16.0.
 *
 * IT USED TO DO BOTH, and the doctrine behind that was the language's own until
 * 1.16.0 made the two halves answer separate questions. The old comment here
 * read: "Both halves together, always: dimming alone still lets a click land and
 * a picker open on a value nothing will read." That is true of a control with
 * nothing behind it, and 1.16.0's answer for that control is not to deaden it -
 * it is to take it off the page.
 *
 * THE TEST IS WHETHER THE CONTROL STILL DOES ANYTHING. No state behind it,
 * remove it; state that still shows somewhere, dim it and say who is in charge.
 * Dimmed AND switched off is the one shape both halves rule out, because
 * whichever half is true, that pair is the wrong answer to it.
 *
 * AND THE PAIR CARRIES A SECOND FAULT, which is why it is worth banning rather
 * than arguing case by case: `pointer-events: none` is invisible to the
 * KEYBOARD. Measured on this page, not reasoned about - with rainbow mode on,
 * the accent row rendered at opacity .5 with pointer-events none, and focusing
 * the second swatch and pressing it still set --accent to #1d99f3 and wrote it
 * to storage. The row was unreachable by mouse and perfectly reachable by Tab,
 * which is worse than either honest answer.
 *
 * So a control that genuinely refuses uses setRefused() below, which sets the
 * button's own `disabled` - a state a screen reader reads, the keyboard cannot
 * fire, and the bubble beside it can still explain.
 *
 * Each caller passes the label's words and the control group, never the flex
 * container that holds them, so the "(i)" between them keeps full strength -
 * which is the whole reason this is a function and not two inline assignments,
 * and it matters most here, because the bubble is the conditional kind that
 * exists FOR the state doing the dimming.
 *
 * The empty string rather than '1': that clears the inline property and lets the
 * stylesheet decide again. Writing a value back is how a lock gets set and never
 * cleared - the unlock here once read `el.style.pointerEvents || ''`, which
 * consults the very value it is trying to clear.
 */
function setDimmed(parts, off) {
  for (const el of parts) {
    if (!el) continue;
    el.style.opacity = off ? '.5' : '';
  }
}

/**
 * setRefused switches controls off through their OWN disabled state.
 *
 * The other half of the split above, and the only one that may take a control's
 * answer away. Every element handed to it is a real <button> - the swatches, the
 * reset badges, the segment buttons and the switches all are - so `disabled` is
 * available, carries into the accessibility tree, and takes the keyboard with
 * it. The greying comes from the stylesheet's own `:disabled` rules rather than
 * from an inline opacity, so there is exactly one place that decides what a dead
 * control looks like.
 *
 * `groups` are containers; each one's buttons are switched, which keeps the call
 * sites reading like the rows they describe rather than like DOM queries.
 */
function setRefused(groups, off) {
  for (const el of groups) {
    if (!el) continue;
    const buttons = el.tagName === 'BUTTON' ? [el] : [...el.querySelectorAll('button')];
    for (const b of buttons) b.disabled = off;
  }
}

/** The words inside a row label, which is the half of it that may be dimmed.
 *  Null before applyStaticText() has run, which setDimmed() skips. */
function labelWordsOf(labelEl) {
  return labelEl?.querySelector('.glim-label-words') ?? null;
}

/**
 * applyStaticText fills in every fixed label from the current language — called
 * once on load and again whenever the language picker changes it, so nothing
 * needs a page reload to update.
 *
 * The four glimSetInfo() calls are where this page's explanations live now.
 * They are idempotent, which matters precisely because this function re-runs on
 * every language change — otherwise each switch would leave another icon behind.
 */
function applyStaticText() {
  groupHeadingEl.textContent = t('options.groupHeading');
  glimSetInfo('groupHeading', t('options.groupInfo'));
  phraseInput.placeholder = t('options.phrasePlaceholder');
  renderPhraseEye();
  renderPhrasePaste();
  joinBtn.textContent = t('options.join');
  // The badge carries no text: its name lives on the element for a screen
  // reader and in the tooltip for everyone else.
  leaveBtn.textContent = '';
  leaveBtn.setAttribute('aria-label', t('options.leave'));
  leaveBtn.setAttribute('data-tip', t('options.leave'));
  // 16 in the --btn-h badge: a glyph standing ALONE in a square is half its box
  // (GlimStone 1.8.0). It was 18 in 36, which is the same half - the box is
  // what moved. There is no word beside it to set the size against, so the only
  // proportion left is how much of the frame the ink takes.
  leaveBtn.replaceChildren(glyph(D_TRASH, 16));
  refreshBtn.textContent = t('options.refresh');

  // The confirmation window. Its title is a badge, like every other window and
  // every other card heading (rule 15), so it is filled the same way.
  //
  // Both buttons are `.secondary`, which is this page's neutral: since 1.12.0 a
  // destructive control takes the colour its siblings take, and since 1.13.0
  // there is no tone lever left to pass. Cancel and commit look alike on
  // purpose - neither is recommended, the sentence above them decides. The
  // committing one names its action rather than saying "OK", which is what
  // makes a glanced-at window still answerable.
  leaveConfirmTitleEl.textContent = t('options.leaveConfirmTitle');
  leaveConfirmCancelEl.replaceChildren(glyph(D_CROSS, 14), document.createTextNode(t('common.cancel')));
  leaveConfirmCommitEl.replaceChildren(glyph(D_TRASH, 14), document.createTextNode(t('options.leave')));

  languageHeadingEl.textContent = t('options.languageHeading');
  glimSetInfo('languageHeading', t('options.languageSub'));

  cnlToggleEl.textContent = t('options.cnlToggle');
  glimSetInfo('cnlHeading', t('options.cnlSub'));

  // The look, one card per axis. A heading carries a bubble only where the
  // card holds more than one control and the bubble has something to say that
  // no single row does. Look and Colours have no bubble on purpose: their rows
  // already explain themselves, and a second bubble repeating the first is the
  // padding the rule exists to remove, not an application of it.
  appearanceHeadingEl.textContent = t('options.appearanceHeading');
  themeHeadingEl.textContent = t('options.themeHeading');
  glimSetInfo('themeHeading', t('options.themeHint'));
  shapeHeadingEl.textContent = t('options.shapeHeading');
  glimSetInfo('shapeHeading', t('options.shapeHint'));
  coloursHeadingEl.textContent = t('options.coloursHeading');

  // Every one of these labels gets its own bubble (jdp, 2026-08-28: "Aussehen
  // card. Infotexte fehlen. auch beim Aussehen übernehmen toggle."). The icon
  // is focusable, which is why the captions live outside their switches now -
  // inside a <button> it would be invalid markup and a click on it would flip
  // the switch somebody was only trying to read about.
  //
  // The words go into a span of their own rather than straight onto the label,
  // and that is not tidiness: it is what lets the dimming sit on the PARTS of a
  // row instead of on the row (GlimStone 1.9.0). `opacity` composites a whole
  // subtree and a child cannot be less transparent than its parent, so a "(i)"
  // inside the element carrying the dimming renders dimmed too - and the bubble
  // is the conditional kind, which means it exists precisely for the state that
  // hides it. A text node cannot carry a class, so there has to be an element
  // around the words for the dimming to land on. See setDimmed() below.
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

  // The refusal notice that stands where the follow switch would be with no
  // group (GlimStone 1.15.0). Its TITLE is the switch's own label, deliberately:
  // the box is there in place of that control, and somebody looking for the
  // setting should find the words they were looking for rather than a second
  // paraphrase of them. The reason names the card that fixes it - "what is wrong
  // AND what would change it" is both halves of the rule, and a notice that only
  // states the problem leaves somebody hunting for the cure.
  //
  // The reason is UI copy in the reader's own language, never a diagnostic: the
  // relay's own failure strings are English and belong in the group card's
  // status line, not in the paragraph that explains a feature.
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
  // The button and the link get their text from renderReport(), which also
  // fills the report itself and the prefilled issue URL — setting them here as
  // well would be two places saying the same thing, and the second one to run
  // would silently win.
}

/**
 * The "it IS installed, it is behind the puzzle piece" card.
 *
 * Two conditions, both required: background.js saw an unpinned toolbar at
 * install time AND the toolbar is STILL unpinned now. The second check is what
 * keeps this honest across a reload - somebody who pinned it and then reopened
 * this page must not be told again about something they have already done.
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

// --- The group -------------------------------------------------------------

/**
 * The phrase is masked (jdp, 2026-08-28: "Die Phrase wird offen angezeigt in
 * der erweiterung"), because it is a credential and not a setting: twelve
 * words open every instance in the group, not this one browser, and an options
 * page is exactly the surface somebody opens while another person is looking
 * at the screen. The eye is the way to check what was pasted.
 *
 * renderPhraseEye is called on load, on every language change and on every
 * toggle, so the glyph and its accessible name can never disagree with the
 * field's own type.
 */
function renderPhraseEye() {
  const shown = phraseInput.type === 'text';
  // Half of .glim-eye's 28px box. It carries no ground at rest but takes one on
  // hover, so the frame the rule measures against is real.
  phraseEye.replaceChildren(glyph(shown ? D_EYE_OFF : D_EYE, 14));
  const name = shown ? t('options.phraseHide') : t('options.phraseShow');
  phraseEye.setAttribute('aria-label', name);
  phraseEye.setAttribute('data-tip', name);
  // The bubble is open while this runs - the press hid it and the focus that
  // followed re-showed it with the OLD text, because the click handler that
  // changes the text runs after both.
  glimRefreshTip(phraseEye);
}

phraseEye.addEventListener('click', () => {
  phraseInput.type = phraseInput.type === 'password' ? 'text' : 'password';
  renderPhraseEye();
});

/** Same treatment as the eye: glyph and accessible name drawn together, so a
 *  language change cannot leave one of them behind. */
function renderPhrasePaste() {
  phrasePaste.replaceChildren(glyph(D_PASTE, 14));
  const name = t('options.phrasePaste');
  phrasePaste.setAttribute('aria-label', name);
  phrasePaste.setAttribute('data-tip', name);
  glimRefreshTip(phrasePaste);
}

/**
 * Paste, then normalise, then let the ordinary validation say the rest.
 *
 * clipboardRead is OPTIONAL rather than required, and the request goes first
 * in the handler: a permission added to the manifest of an extension somebody
 * already runs disables it until they approve it again, which is a heavy price
 * for a convenience button - and asking at the moment of the click is also the
 * honest place to ask. permissions.request resolves true without a prompt once
 * it has been granted, so this costs one dialogue in the lifetime of an
 * install. It also has to be the FIRST await in the handler: both browsers
 * require a user gesture, and an await before it spends the gesture.
 *
 * The words are lowercased and their whitespace collapsed here rather than in
 * writePhrase alone, because a phrase copied out of a chat window arrives with
 * a capital first word and sometimes a line break in the middle, and a field
 * that shows a phrase it will silently repair reads as a field that rejected it.
 */
phrasePaste.addEventListener('click', async () => {
  try {
    await chrome.permissions.request({ permissions: ['clipboardRead'] });
  } catch {
    // Firefox rejects the request outright when the page has lost the gesture,
    // and Chrome throws when the permission is not listed as optional. Both
    // land in the same place as a refusal: try the read anyway, because a
    // browser that already granted it needs no request at all.
  }
  // The answer is deliberately not kept. What the request RESOLVED with says
  // nothing useful here - a browser that granted the permission long ago
  // resolves true without a prompt, and one that has it already needs no
  // request at all - so the only honest test is whether the read itself works.
  let text = '';
  try {
    text = await navigator.clipboard.readText();
  } catch {
    // A FAILED READ IS REPORTED WHATEVER THE PERMISSION SAID, and the `erlaubt`
    // guard that used to sit here got that backwards. It reported only when the
    // request had been refused and stayed silent when it had been GRANTED - so
    // the one case where somebody has every reason to expect the button to work
    // (they approved it, and the read still threw: a dismissed paste prompt on
    // Firefox, a clipboard holding something that is not text) fell through with
    // an empty string, hit the `if (!worte) return` below, and did nothing at
    // all. A button that does nothing and says nothing is the shape "Failure
    // feedback" exists to remove: reported once, loudly, in the one place built
    // for it, with the control that caused it visibly saying so.
    say(t('options.phrasePasteBlocked'), false);
    // The control that was clicked says so itself. Every action that can fail
    // reports through the same two channels and the button plays glim-shake -
    // it is not a rule about toggles, it covers a plain action button that
    // just called something and has to report what came back.
    shake(phrasePaste);
    return;
  }
  const worte = String(text).trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
  if (!worte) return;
  phraseInput.value = worte;
  // Masked it was and masked it stays: the mask is there because an options
  // page gets opened while somebody else is looking at the screen, and a paste
  // is not a reason to overrule that. The eye is right beside it for the one
  // person who does want to check what landed.
  phraseInput.focus();
});

/**
 * phraseProblemText turns a PhraseError into a sentence in the reader's own
 * language. The word and its position are named on purpose: bisecting a
 * twelve-word phrase by hand is not a thing to ask of anybody.
 */
function phraseProblemText(err) {
  const p = err?.problem;
  if (!p) return t('options.phraseBad');
  if (p.reason === 'word_count') return t('options.phraseWordCount', { count: p.count });
  if (p.reason === 'unknown_word') return t('options.phraseUnknownWord', { word: p.word, position: p.position });
  return t('options.phraseChecksum');
}

/**
 * renderGroup shows who is in the group RIGHT NOW, asked of the relay rather
 * than read from storage.
 *
 * That is the difference the whole rework buys and it is worth showing rather
 * than describing: an instance that is switched off is not listed, and one that
 * came online a minute ago is, without this browser having been told anything.
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
    // The status version: the roster plus what each instance is doing, in one
    // relay session (jdp, 2026-08-29: "können wir auf der card sinnvolle infos
    // anzeigen?"). The popup deliberately still uses the plain roster - it has
    // one job and should not wait on three reads per instance to do it.
    siblings = await groupStatus();
  } catch {
    loading.textContent = t('options.groupUnreachable');
    return;
  }
  list.innerHTML = '';
  if (siblings.length === 0) {
    // A deliberate empty state, not a grey line where a list should be: the
    // shared shape is a muted icon at reduced opacity over a muted title. It
    // needs no .glim-card of its own - it already sits inside the Group card,
    // and a card inside a card is one elevation too many. The optional action
    // button is left off because the two actions this state has (connect
    // somewhere else, refresh) are the controls directly above it, and a third
    // copy of a button already on screen answers nothing.
    const empty = document.createElement('div');
    empty.className = 'emptyState';
    empty.appendChild(instancesGlyph(28));
    const title = document.createElement('span');
    title.textContent = t('options.groupEmpty');
    empty.appendChild(title);
    list.appendChild(empty);
    return;
  }

  // The same card the popup and the send-to window draw (shared.js), so the
  // group looks like one thing wherever it appears. The default carries a badge
  // and is reassigned by right-clicking another card — there is no "set as
  // default" button on every row any more, which was both a permanent control
  // for a once-in-a-lifetime decision and, as jdp pointed out, not shaped like a
  // button at all.
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
          // The report names which instance is the default, so it goes stale
          // the moment that changes - and a report that says "none" under a
          // card wearing the badge is worse than no report.
          void renderReport();
        },
        onQueue: async (picked, halted, el) => {
          const ok = await setQueueHalted(picked.instanceId, halted).catch(() => false);
          if (!ok) {
            // The same sentence the adopt-the-look switch uses when an
            // instance stays silent, because it is the same fact. A second,
            // freshly written way of saying "it did not answer" would be a
            // 42-language translation for no new information.
            say(t('options.followFailed'), false);
            // And the control that was pressed says so itself: the message alone
            // is half the standard, and it is the half somebody reading the card
            // rather than the line under it never sees.
            shake(el);
            return;
          }
          // Re-read rather than assume: the instance decides what its queue
          // does, and a card that shows what we asked for instead of what
          // happened is a card that lies on the one occasion it matters.
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
    // The button says it, not a sentence under it. The reason still exists -
    // it is the button's tooltip now, so it is one hover away for anybody who
    // wants it - but nothing permanent is left on the page (GlimStone,
    // "Failure feedback": the inline sentence is removed outright, because
    // unlike a transient it never clears itself).
    joinBtn.setAttribute('data-tip', phraseProblemText(err));
    say('', false);
    shake(joinBtn);
    return;
  }
  // Checked against the relay before it is called a success: a phrase that
  // decodes is not the same as a phrase that reaches anybody, and reporting
  // "connected" for the first would be a lie the user only finds out about on
  // their next send.
  try {
    const siblings = await groupInstances();
    // [282] No count line for a group that HAS members (jdp, 2026-08-30: "2
    // Instanzen in der Gruppe. text kann weg"). The cards underneath are the
    // count, one per instance, each with its name and what it is doing - a
    // sentence saying "2" above two visible cards is the same fact twice. An
    // EMPTY group still says so, because there are no cards to say it instead.
    say(siblings.length ? '' : t('options.joinedEmpty'), true);
    joinBtn.removeAttribute('data-tip');
  } catch {
    // Same treatment as a rejected phrase: the control says so, the page keeps
    // no sentence.
    joinBtn.setAttribute('data-tip', t('options.groupUnreachable'));
    say('', false);
    shake(joinBtn);
  }
  joinBtn.disabled = false;
  await renderGroup();
  // Joining gives the Look card its follow switch back, in place of the notice
  // explaining that there was nothing to follow. Same reason as the report
  // below: a card that only catches up on the next page load is a card that
  // disagrees with the one directly above it.
  await renderAppearance();
  // The report names whether this browser is in a group and how many instances
  // answered, so it has to be redrawn when that changes - a report still saying
  // "no phrase stored" under a list of two instances is worse than no report.
  void renderReport();
});

/**
 * The question that stands between the bin and the deletion.
 *
 * GlimStone 1.12.0 took status-red off every destructive control, and the whole
 * argument rests on this window existing: "what warns is the QUESTION - an
 * irreversible action opens a window that states the stakes in words and
 * counts, and somebody who has read it and reached for the button has already
 * been told." This page had the conclusion without the premise. The bin was
 * neutral, which was right, and it deleted the phrase and emptied the group on
 * the press, which made the neutral colour the worst of both - and the comment
 * beside it in options.html said out loud that a confirmation was there.
 *
 * The count is read off the list that is already on screen rather than asked
 * for: a confirmation that opens after a relay round trip is a confirmation
 * somebody presses twice. It is honest about what it is - how many instances
 * are showing right now - and that is the number somebody is looking at.
 *
 * Escape and a press on the ground behind both cancel, which is where a second
 * way out belongs; the window itself has no corner X (1.8.0 makes it optional,
 * and two controls doing one thing read as a choice).
 */
function openLeaveConfirm() {
  leaveConfirmMessageEl.textContent = t('options.leaveConfirmBody', {
    count: String(list.querySelectorAll('.glim-instance').length),
  });
  leaveConfirmEl.hidden = false;
  // The cancelling half takes focus, the same way the reference dialog does:
  // the window opens on the answer that changes nothing.
  leaveConfirmCancelEl.focus();
  document.addEventListener('keydown', onLeaveConfirmKey);
}

/** `back` is the control focus belongs to afterwards. It is not always the bin:
 *  once the group is gone the bin is hidden, and focusing a hidden element
 *  quietly drops focus to the body. */
function closeLeaveConfirm(back) {
  leaveConfirmEl.hidden = true;
  document.removeEventListener('keydown', onLeaveConfirmKey);
  back?.focus();
}

/**
 * Escape closes it, and Tab cannot leave it.
 *
 * The trap is short because the window holds exactly two controls: forward from
 * the committing one and backward from the cancelling one both land on the
 * other. Without it, Tab walks straight out of a window that says aria-modal
 * and onto the page behind, where every control is still reachable - which is
 * the one thing "modal" is supposed to mean.
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
  // -1 means focus is somewhere on the page behind, which Tab must pull back
  // in: forwards to the first stop, backwards to the last.
  const next = event.shiftKey ? (at <= 0 ? stops.length - 1 : at - 1) : at === stops.length - 1 ? 0 : at + 1;
  stops[next].focus();
}

leaveBtn.addEventListener('click', () => openLeaveConfirm());
leaveConfirmCancelEl.addEventListener('click', () => closeLeaveConfirm(leaveBtn));
// A press on the ground, not on the window: without the target check, a press
// that starts inside the card and ends on the backdrop would cancel it.
leaveConfirmEl.addEventListener('click', (event) => {
  if (event.target === leaveConfirmEl) closeLeaveConfirm(leaveBtn);
});

leaveConfirmCommitEl.addEventListener('click', async () => {
  // Closed first, and focus handed to Connect rather than to the bin: the bin
  // is about to be hidden by renderGroup(), and the button that is left is the
  // one that gets you back in.
  closeLeaveConfirm(joinBtn);
  await forgetGroup();
  // Leaving un-follows, and puts the local look back with it.
  //
  // Without this, "follow the instance" would be stuck ON with no instance to
  // follow and, since 1.15.0 removes the switch where it cannot act, no control
  // left to turn it off - a lock that can be set and never cleared, which is the
  // same defect this page already fixed once in setDimmed's own history.
  // Restoring rather than merely flipping the flag is the other half: a switch
  // is a claim that it can be undone, and undone means the thing that was put
  // away comes back.
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
  // Back to masked: leaving and re-joining is the one moment somebody is most
  // likely to walk away from an open options page.
  phraseInput.type = 'password';
  renderPhraseEye();
  say(t('options.left'), true);
  await renderGroup();
  // The Look card changes with the group: the follow switch goes and the notice
  // takes its place. Without this the swap would wait for the next page load.
  await renderAppearance();
  void renderReport();
});

refreshBtn.addEventListener('click', async () => {
  refreshBtn.disabled = true;
  await renderGroup();
  refreshBtn.disabled = false;
});

// --- Language --------------------------------------------------------------

/**
 * The language picker: a listbox with flags, and no "Automatic" entry.
 *
 * jdp, 2026-08-28: "Im Sprachen-dropdown soll nicht stehen Automatisch. Es soll
 * einfach die Sprache auswählen die im Browser eingestellt ist und diese quasi
 * selbst im dropdown auswählen." He is right that "Automatic" was a worse
 * answer than it looked: it is a fourth kind of value in a list of real
 * languages, and it makes the control unable to answer the only question anyone
 * asks it — which language am I actually reading? Resolving the browser's own
 * language and SELECTING it says that outright, and changes nothing about what
 * is displayed.
 *
 * currentLanguage() is the resolved code either way, so nothing here has to
 * know whether it came from storage or from the browser.
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
      // The Click'n'Load card is the one whose text does NOT all live in
      // applyStaticText: the countdown's label, its unit and the two steppers'
      // names are written by renderCnl, which ran once at boot and was never
      // asked again. Measured on the rendered page while checking something
      // else: switching to German turned the switch's own caption into
      // "Click'n'Load-Knöpfe abfangen" and left "Countdown before sending" and
      // "seconds" standing underneath it, in English, for good.
      await renderCnl();
      await renderAppearance();
      await renderGroup();
      void renderReport();
    },
  );
}

// --- Click'n'Load ----------------------------------------------------------

/**
 * ON by default, because it is the reason most people install this at all
 * (jdp, 2026-08-28: "Der CnL Toggle soll standardmäßig aktiviert sein! Das ist
 * ja das Hauptfeature warum man sich die Erweiterung installiert!").
 *
 * That decision is what moved <all_urls> into the manifest rather than behind a
 * prompt, and the reasoning is worth keeping because the previous version's is
 * still readable in the git history and looks equally sound: a feature that is
 * on by default has to work on a fresh install, and asking for the permission
 * later would leave a switch reading "on" while doing nothing. The install
 * dialog names the access instead — the honest place for it, and what
 * JDownloader's own extension does too.
 *
 * Switching it OFF still unregisters the content scripts, so off is really off:
 * no code in any page, and the site's own button goes back to reaching for
 * 127.0.0.1:9666 the way it always did.
 */
async function renderCnl() {
  const stored = await chrome.storage.local.get('cnlEnabled');
  // Absent means on: background.js sets it on install, and a storage read that
  // lost the flag should not silently turn the main feature off.
  const on = stored.cnlEnabled !== false;
  cnlEnabledEl.setAttribute('aria-checked', String(on));

  // THE COUNTDOWN IS A SUB-CONTROL, AND IT GOES WITH ITS PARENT (GlimStone
  // 1.10.0, and 1.16.0 for the test that decides it).
  //
  // The question 1.16.0 settles is whether the control still DOES anything, and
  // this one's answer is measurable rather than arguable: the number is read in
  // exactly one place, popup.js's startCountdown(), which only ever runs for a
  // parked send whose origin is 'cnl'. With this switch off, background.js's
  // handleCnl() returns before parking anything and syncCnlScripts() has
  // unregistered the catcher in every page, so no such send can exist. There is
  // no state behind the field at all, so leaving it on screen offers a decision
  // nobody can make, with the reason sitting one row up, which is precisely
  // where nobody looks once they have decided this row is the interesting one.
  //
  // Absent rather than dimmed, and never dimmed AND switched off: see
  // setDimmed() for why that pair is the one shape both halves of the rule
  // forbid.
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

/** Greys out whichever stepper has nowhere left to go. min and max are read
 *  from the input rather than repeated here: two places holding the same range
 *  is one place holding it wrong. */
function markCountdownEnds() {
  const v = Number(cnlCountdownEl.value);
  cnlCountdownDownEl.disabled = v <= Number(cnlCountdownEl.min);
  cnlCountdownUpEl.disabled = v >= Number(cnlCountdownEl.max);
}

/**
 * The two steppers, drawn by us (jdp, 2026-08-31: "#297 jetzt sind keine
 * pfeiltasten mehr da").
 *
 * Dropping the OS widget was right; dropping the AFFORDANCE with it was not. A
 * number field wants a way to nudge it without typing, and on a touch screen
 * that is the only comfortable way to reach 5 from 4. So the control keeps its
 * two arrows - ours now, in the page's own tokens - and the design language
 * gains the second half of the rule it was missing (GlimStone 1.7.0).
 *
 * stepUp/stepDown rather than arithmetic: they already honour min, max and step
 * from the element, so the range lives in exactly one place, and they fire the
 * same 'change' the keyboard and typing do - which is what persists the value,
 * so nothing here has to know how the value is stored.
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
 * The wheel is the third way to nudge the field, and it answers only while the
 * field has FOCUS.
 *
 * That condition is the design and not a caution (GlimStone 1.8.0). A number
 * input that answers the wheel whenever a pointer happens to pass over it is a
 * well-known way to change a value somebody was only scrolling past, and
 * browsers removed the behaviour from the native widget for exactly that
 * reason. Requiring focus means the field has been deliberately entered first -
 * the same gesture that already enables the arrow keys - so the wheel becomes a
 * second way to do what the keyboard and the two arrows do rather than a new
 * hazard. This page is one long column, and this field sits in the middle of
 * it.
 *
 * A real listener with `passive: false`, because preventDefault is half the
 * feature: without it the page scrolls while the value changes and the field
 * slides out from under the pointer mid-adjustment. It is called before the
 * end-of-range check on purpose - while the field has focus this handler IS the
 * scroll, whether or not the number moves.
 *
 * Up is more, matching the upper arrow and the up key. A trackpad reports
 * fractional deltas, so only the sign is read. The two steppers already know
 * where the range ends (markCountdownEnds keeps them current), so there is no
 * second copy of min and max here.
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

// Written on 'change', not on every keystroke: a number field fires 'input' for
// each digit, so typing "30" would pass through 3 on the way - and a value of 3
// briefly saved is a value that survives if the field then loses focus. The
// clamp lives in writeCnlCountdown so the stored value can never be a shape the
// popup has to defend against.
cnlCountdownEl.addEventListener('change', async () => {
  cnlCountdownEl.value = String(await writeCnlCountdown(cnlCountdownEl.value));
  markCountdownEnds();
});

cnlEnabledEl.addEventListener('click', async () => {
  const on = cnlEnabledEl.getAttribute('aria-checked') !== 'true';
  cnlEnabledEl.setAttribute('aria-checked', String(on));
  // The countdown row appears and disappears WITH the switch, not on the next
  // page load: a sub-control that only goes away after a reload is a dimmed one
  // wearing a delay.
  cnlCountdownRow.hidden = !on;
  await chrome.storage.local.set({ cnlEnabled: on });
  await chrome.runtime.sendMessage({ type: 'knightloader-cnl-scripts', on }).catch(() => {});
  // Deliberately silent. The switch itself is the feedback, and say() writes
  // into the group card's own status line — a sentence about Click'n'Load
  // appearing under the phrase field is a message in the wrong room (jdp: "Die
  // Meldung CnL ist an in der Gruppen-card kann weg").
});

// --- Appearance ------------------------------------------------------------
//
// The axes GlimStone gives the user: theme, corners, accent, and the rainbow.
// Applied at the top of every page (appearance.js) rather than by the page that
// edits them - a page that paints itself leaves every other page on the old
// value, and this one is the last place anyone would notice.
//
// Local by default, and now optionally taken from the instance instead. The old
// note here said reading it from a configured instance would cost a host
// permission for that origin, so it was not worth it for a colour. Through the
// relay it costs nothing: GET /api/appearance is on the list a group member may
// reach, and that route exists precisely so this is not a licence to read
// /api/settings. See adoptFromInstance() in appearance.js.

/** The switches wired once. Each writes, re-applies and redraws, so the page
 *  never shows a value that is not also on <html>. */
function wireRainbowSwitch(el, key) {
  el.addEventListener('click', async () => {
    const on = el.getAttribute('aria-checked') !== 'true';
    const patch = { [key]: on };
    // Turning rotation on draws a fresh offset, so the switch does something
    // visible rather than re-applying the rotation the palette already had -
    // the same call the web UI's own Look page makes.
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
 * Take the look from the default instance, or go back to choosing it here.
 *
 * Switching it on fetches once and stores the result, rather than fetching on
 * every page load: a popup that opens a relay connection before it can paint
 * itself is a popup that flashes. The Refresh button in the group card is the
 * way to pick up a change made on the instance since.
 */
followInstanceEl.addEventListener('click', async () => {
  const on = followInstanceEl.getAttribute('aria-checked') !== 'true';
  if (on) {
    followInstanceEl.setAttribute('aria-checked', 'true');
    // Snapshot BEFORE adopting, so switching back off can put things where
    // they were (jdp, 2026-08-29: "wenn ich den toggle aktiviere und
    // deaktiviere ... die optionen resetten sich nicht"). Adopting overwrote
    // the local look and there was nothing left to go back to: the switch was
    // one-way in everything but appearance.
    await stashLocalLook();
    const ok = await adoptFromInstance().catch(() => false);
    if (!ok) {
      followInstanceEl.setAttribute('aria-checked', 'false');
      say(t('options.followFailed'), false);
      // The switch was shown on optimistically and is being snapped back, which
      // is the exact case glim-shake was named for: a toggle that quietly
      // reverts leaves the reader to notice the state changed twice, while the
      // reason sits in a line the eye may not be on. Reverting silently is the
      // defect; reverting visibly is the rule.
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
 * Everything adopting the instance's look replaces, so everything the stash has
 * to carry. Theme is not among them and never was (see adoptFromInstance).
 *
 * accentSlotChosen and accentCustoms are on the list for the second reason
 * rather than the first: the instance sends neither, and adoptFromInstance
 * CLEARS them (an accent from the server with the local mixes still painted
 * over it is a row that disagrees with itself). A switch has to be reversible,
 * so what following puts away has to be what un-following brings back.
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
 * restoreLocalLook puts back what was there before the instance's look was
 * adopted, and then forgets the snapshot.
 *
 * A key that was ABSENT before has to be removed rather than written back as
 * undefined: chrome.storage stores undefined as a value, and readAppearance
 * would then see a present-but-meaningless field instead of falling through to
 * its default.
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
 * paintHues hands every card heading its own palette position, and the three
 * rainbow rows their own 0-based run.
 *
 * This is what "rainbow mode" MEANS on a page like this, and it was missing:
 * the mode was stored, applied to <html> and visible in the report, while
 * every badge on the page went on taking the single accent (jdp, 2026-08-28:
 * "Die Farbmodi sind nicht verdrahtet. Regenbogenmodus geht nicht."). Setting
 * data-rainbow only decides HOW a position is shown; something still has to
 * own one. The instance cards did, and nothing else did.
 *
 * Re-run after every appearance change, because rotating the palette changes
 * which colour each position resolves to.
 */
function paintHues() {
  // The CARD, not its badge. Everything inside a card that reaches for
  // --accent — the badge, the switch track, the buttons, the focus ring — then
  // follows the card's position without anybody keeping a list of them.
  setHues([...document.querySelectorAll('.glim-card')]);
  // Their own sequence, restarting at 0: this set of three rows is its own
  // equal-member set, exactly as the web UI's Look page treats it, and a
  // nested position overrides the card's for its own subtree.
  setHues([rainbowRow, rainbowReactiveRow, rainbowRotateRow]);
}

/**
 * swatch builds one round colour button.
 *
 * Both jobs on one control, decided by whether this swatch is the one in force:
 *
 *   - not selected -> a click SELECTS it (`onPick`), and nothing else happens;
 *   - already selected -> a click opens the picker on it (`onEdit`);
 *   - no `onPick` at all -> a click always edits. That is the palette row,
 *     where all eight colours are in force at once and "select" means nothing.
 *
 * The previous version opened the picker on every click, on the stated grounds
 * that "the popover applies live on open, so the colour lands immediately". It
 * does not: colorPicker only calls back on interaction, so choosing a preset
 * set nothing at all and left a picker standing over the row instead (jdp,
 * 2026-09-01: "es nimmt die neu eingestellte farb nicht an und ich kann die
 * farbfelder nicht auswählen. es kommt immer der farbpicker"). The claim was
 * mine and it was wrong; this is the shape that actually delivers what it was
 * meant to - one click to choose any of the eight, and every one of them
 * editable, with no ninth control to do it.
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

/** The reset badge: an icon, not a text link, at the one badge size these pages
 *  use — and the swatches beside it follow that size rather than setting it. */
function resetBadge(onClick) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'glim-reset';
  // 16 in the --btn-h circle - half its box, the same ratio every other badge
  // on these pages carries. It was 11 in 22, which is the same half: the box is
  // what moved, to the one badge size the page now takes.
  b.appendChild(glyph(D_RETRY, 16));
  b.setAttribute('data-tip', t('options.accentReset'));
  b.setAttribute('aria-label', t('options.accentReset'));
  b.addEventListener('click', onClick);
  return b;
}

/**
 * segment builds one "well" selector: a shared padded track with equal
 * segments and no per-item glyph, which is the variant KnightLoader's own
 * Corners picker uses. Three native <select>s stood here before, and a native
 * dropdown next to token-styled controls reads as another application's
 * widget sitting inside this one.
 *
 * aria-pressed rather than a class carries which one is on, so the state is
 * the button's own and a screen reader gets it for free; the stylesheet
 * selects on the same attribute.
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
 * What each slot is mixed to, mirrored in a variable that is updated the moment
 * a drag frame writes it.
 *
 * This is the extension's copy of the app's liveOverride ref (mobile/src/theme/
 * AppearanceContext.tsx): the picker calls back on EVERY drag frame, so a
 * handler that read the map back out of storage each time would be building on
 * a read that had not returned when the previous frame wrote. Two mixes in
 * quick succession are exactly that case, and the second one would write the
 * first back out of existence with nothing looking wrong until you returned to
 * the earlier swatch and found the preset there (jdp, 2026-09-02: "nicht alle
 * farbfelder speichern dann die farbe").
 *
 * renderAppearance re-seeds it from storage on every render, and every path
 * that changes the map from outside the row (reset, follow the instance) ends
 * in a render, so it cannot drift.
 */
let liveAccentCustoms = {};

async function renderAppearance() {
  const a = await readAppearance();
  liveAccentCustoms = { ...a.accentCustoms };

  // Two values, and the machine's own answer is already the selected one
  // (jdp, 2026-08-29). The third entry, "follow the browser", is gone: it read
  // as a choice and was an excuse, because it could not answer the only
  // question anyone asks this control - which of the two am I looking at. Same
  // ruling as the language picker's missing "Automatic", and the same fix:
  // resolve it and select it. readAppearance does the resolving, live, so
  // nothing is written down until somebody actually picks a side.
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

  // The applied accent, which is the stored one or the default when nothing is
  // stored. It no longer decides what any circle LOOKS like; there is no
  // separate circle for it either (see below). Its one remaining job here is to
  // seed the fallback on a fresh install.
  const live = (a.accent || DEFAULT_ACCENT).toLowerCase();
  // Which of the eight is chosen: the STORED fact first, the arithmetic only as
  // the fresh-install fallback.
  //
  // It used to be accentSlot(live) and nothing else, which works only while
  // every swatch holds a different colour. Mix two of them to the same red and
  // the nearest-preset match marks one slot and abandons the other, so the
  // seven that are not marked fall back to their factory hex and the row looks
  // like it forgot them (jdp, 2026-09-02: "Der farbpicker funktionniert nicht
  // bzw. speichert das farbfeld die ausgewählte farbe nicht"). A choice is not
  // recoverable from a value once two values are equal. Same fix, same shape,
  // as the app's own (mobile/src/screens/SettingsScreen.tsx).
  const liveSlot = a.accentChosen !== undefined ? a.accentChosen : accentSlot(live);
  // What each slot was last mixed to. A slot nobody has touched answers with
  // its preset.
  const customs = a.accentCustoms;

  accentSwatches.innerHTML = '';
  // No "Voreinstellungen" caption any more (jdp, 2026-09-01: "der text
  // Voreinstellungen soll weg"). Eight colour circles in a row labelled
  // "Accent" do not need a second word telling you they are colours to choose
  // from, and it was the only thing making this row look different from the
  // palette row four lines below it.
  const pick = async (v) => {
    await writeAppearance({ accent: v });
    applyAccent(v);
    await renderAppearance();
  };
  /**
   * chooseSlot puts one slot in force: its remembered colour becomes the
   * accent, and the slot itself is written down as the chosen one. Both facts
   * get stored, because a colour cannot say which of eight circles was clicked
   * once two of them hold it.
   */
  const chooseSlot = async (i, hex) => {
    await writeAppearance({ accent: hex, accentSlotChosen: i });
    applyAccent(hex);
    await renderAppearance();
  };
  ACCENTS.forEach((x, i) => {
    // Each slot wears whatever IT was last mixed to, and keeps it.
    //
    // The slot in force used to wear the LIVE accent, drawn over whichever
    // preset it sat nearest, and no other slot remembered anything. So the row
    // could hold exactly one hand-mixed colour: choosing any other swatch
    // overwrote `accent`, the mix was stored nowhere else, and the circle fell
    // back to its factory hex (jdp, 2026-09-01: "wenn man ein Farbfeld
    // bearbeitet setzt es die farbe wieder zurück, sobald man ein anderes
    // farbfeld auswählt"). The same derivation also made an edit JUMP slots
    // when a drag crossed a nearest-preset boundary: nudge Red towards orange
    // and the ORANGE circle ended up wearing it while Red snapped back. Slot
    // memory ends both, because a colour no longer has to imply where it lives.
    const shown = (customs[String(i)] ?? x.hex).toLowerCase();
    const mine = i === liveSlot;
    // One click chooses; a second click on the one already chosen edits it
    // (jdp, 2026-09-01: "alle farbfelder sollen sich editieren lassen ... es
    // kommt immer der farbpicker"). Both of his asks land on the same control,
    // which is why the ninth circle with the pencil on it could go: every
    // colour here is editable, and reaching the editor costs the click that
    // selects it - which is a click somebody about to change a colour was
    // always going to make.
    //
    // The palette row two lines down keeps click-to-edit, and the difference is
    // in what the rows MEAN rather than an inconsistency: this is a choice
    // among eight, and there all eight are in force at once, so there is no
    // "the selected one" to click twice.
    //
    // A ring marks the current one, not a tick: a glyph would have to stay
    // legible on all eight, which means computing an ink colour for a
    // decoration.
    const b = swatch(shown, {
      // Named while it wears its preset; its own value once it does not,
      // because "Sunflower" on a circle that is no longer Sunflower is the
      // one label worse than no label. No longer conditional on being the
      // chosen one: a slot holding a mix is holding it whether or not it is in
      // force, so it has to say so.
      label: shown !== x.hex.toLowerCase() ? shown.toUpperCase() : x.name,
      pressed: mine,
      // One click chooses THIS slot and wears what it remembers, which is why
      // the mix survives a trip round the other seven.
      onPick: () => void chooseSlot(i, shown),
      onEdit: async (next) => {
        // Three facts per drag frame: the colour that is applied, the slot's
        // own memory of it, and the fact that this slot is now the chosen one.
        // Editing a swatch is also choosing it, which is what a picker standing
        // open over it already means.
        const cust = { ...liveAccentCustoms, [String(i)]: next };
        liveAccentCustoms = cust;
        await writeAppearance({ accent: next, accentSlotChosen: i, accentCustoms: cust });
        applyAccent(next);
        // The circle being edited has to change colour DURING the drag, the way
        // the palette row four blocks down already does. It did not, so the one
        // control the eye is on while dragging was the only thing on the page
        // not following the picker. The row cannot be redrawn here (that would
        // replace the very button the popover is anchored to), so this is a
        // direct mutation of the live element, not a render.
        b.style.backgroundColor = next;
      },
      // The row cannot be redrawn while the popover is open - that would
      // replace the very button it is anchored to - so the redraw happens
      // once, on close. See openColorPickerPopover's own doc comment.
      onEditClose: () => void renderAppearance(),
    });
    accentSwatches.appendChild(b);
  });
  // The way back, always rendered (jdp, 2026-08-31: "die akzentfarbe hat keinen
  // resetbutton"). It used to appear only once the accent had actually moved,
  // which is defensible and turned out to be wrong for the same reason the
  // palette's own reset was already unconditional two rows below: a control
  // that is sometimes there is a control nobody learns the position of, and the
  // moment somebody goes looking for it is exactly the moment it is missing -
  // they check whether a reset exists BEFORE deciding to experiment, not after.
  // Consistency inside one card decides it too: two colour rows, one reset each,
  // both always in the same place.
  // "Any colour at all" is the swatch itself now, not a ninth circle beside the
  // eight (jdp, 2026-08-31: "der farbpicker bei den akzentfarbe-voreinstellungen
  // fehlt"). A click on the one already in force opens the picker on it, which
  // is what the onEdit branch above does - so the reset badge has something to
  // reset FROM, which was the half that was actually missing. The separate
  // control that briefly stood here is gone, and its stylesheet block went with
  // it rather than being left as a size nothing draws.
  accentSwatches.appendChild(
    resetBadge(async () => {
      // The two row facts are REMOVED, never set to undefined: chrome.storage
      // keeps an undefined as a present value, so readAppearance would find a
      // present-but-meaningless field instead of falling through to the factory
      // presets, and the reset would silently do nothing. Same trap as the one
      // restoreLocalLook documents above.
      await chrome.storage.local.remove(['accentSlotChosen', 'accentCustoms']);
      await pick('');
    }),
  );

  // --- The rainbow ---------------------------------------------------------
  // The same three axes the web UI's Look page offers, in the same order, so
  // somebody who set this up there recognises it here.
  rainbowOnEl.setAttribute('aria-checked', String(a.rainbow.on));
  rainbowReactiveEl.setAttribute('aria-checked', String(a.rainbow.reactive));
  rainbowRotateEl.setAttribute('aria-checked', String(a.rainbow.rotate));
  // Reactive, rotation and the palette only mean anything while the mode is on;
  // the dimming for that, and for the follow switch, is applied together at the
  // end of this function, because two loops setting the same property in turn
  // is how the second one silently wins.

  paletteSwatches.innerHTML = '';
  a.rainbow.palette.forEach((hex, i) => {
    // Editable, one position at a time, through the same popover the accent
    // uses (jdp, 2026-08-28: "bearbeitbar"). Clicking used to ROTATE the
    // palette so the clicked colour started the run; that is still available,
    // as the Rotate switch above, which draws a fresh offset each time it is
    // switched on. A click here now does what a click on a colour looks like
    // it should do: change that colour.
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
    // Each swatch also wears its own position, so in rainbow mode the row is
    // the mode demonstrating itself rather than eight circles describing it.
    setHue(b, i);
    paletteSwatches.appendChild(b);
  });
  // Back to the eight the language ships with. Always rendered, unlike the
  // accent's: a palette that has been edited into something unreadable is
  // exactly when the way back is hardest to find by clicking.
  paletteSwatches.appendChild(
    resetBadge(async () => {
      await writeAppearance({ rainbowPalette: null });
      applyRainbow((await readAppearance()).rainbow);
      await renderAppearance();
    }),
  );

  followInstanceEl.setAttribute('aria-checked', String(a.followInstance));

  // THE ENVIRONMENT REFUSAL (GlimStone 1.15.0): with no group, there is no
  // instance to take a look from, so the switch is not offered at all and a
  // paragraph says why. See the markup in options.html for the argument; here is
  // only the swap. `joined` is read from the phrase rather than from the roster,
  // deliberately: a group that is joined and momentarily offline is a REACHABILITY
  // problem the switch's own failure path already reports, and taking the control
  // away every time an instance is asleep would be a refusal that comes and goes.
  const joined = (await readPhrase()) !== '';
  followInstanceRow.hidden = !joined;
  followUnavailableEl.hidden = joined;

  // Reactive, rotation and the palette are ABSENT while the rainbow is off,
  // never dimmed (GlimStone 1.10.0). The language reversed itself on this and
  // said why: a dimmed sub-switch is something somebody can see, read and reach
  // for that answers nothing, and the reason it is dead sits one row up, which
  // is exactly where nobody looks once they have decided this row is the
  // interesting one. Reaching it and getting nothing teaches less than its
  // absence does. The palette editor is named in the reversal by hand - eight
  // swatches nobody can open beside a reset nobody can press.
  //
  // The MODE's own switch stays, and that is the other half of the same rule:
  // it is the control somebody is looking for. What goes is only what has
  // meaning underneath it.
  const rainbowIsOff = !a.rainbow.on;
  rainbowReactiveRow.hidden = rainbowIsOff;
  rainbowRotateRow.hidden = rainbowIsOff;
  paletteRow.hidden = rainbowIsOff;

  // Everything the instance decides is shown but not editable while the switch
  // is on — an accent you can click that snaps back on the next refresh is
  // worse than one you cannot click. Theme is not in the list: it stays local
  // on purpose (see adoptFromInstance).
  //
  // The refusal is now the CONTROL's own `disabled` rather than a wrapper's
  // pointer-events (GlimStone 1.16.0, and setRefused() carries the argument):
  // the old wrapper was invisible to the keyboard, so every row below could be
  // Tabbed into and fired while it looked dead. The dimming still rides along,
  // from the stylesheet's `:disabled` rules, so there is one place that decides
  // what a dead control looks like.
  //
  // The unlock used to read `el.style.pointerEvents || ''`, which is the bug
  // jdp hit (2026-08-29: "bleiben viele Einstellungen gesperrt"): that reads
  // back the 'none' this very loop wrote a moment ago and hands it straight to
  // itself again, so the lock could be set and never cleared. A fallback that
  // consults the value you are trying to clear is not a fallback. The lock is a
  // boolean written outright now, which cannot get into that state at all.
  //
  // Every call names the label's WORDS and the control group, never the row
  // that holds them: the dimming goes on the parts, so the "(i)" between them
  // stays readable in exactly the state it exists to explain (1.9.0). The
  // heading badges are not in any list - a card heading carries its own bubble
  // and is not what the switch takes over.
  //
  // `&& joined` is the belt: an install that was following when its group went
  // away still carries followInstance: true in storage, and the switch that
  // would clear it is no longer on the page. The leave button clears both, so
  // this only ever catches a state written by an older build.
  const off = a.followInstance && joined;
  setRefused([shapeSeg], off);
  setRefused([rainbowOnEl, rainbowReactiveEl, rainbowRotateEl], off);
  setRefused([paletteSwatches], off);
  setDimmed([labelWordsOf(rainbowLabelEl)], off);
  setDimmed([labelWordsOf(rainbowReactiveLabelEl)], off);
  setDimmed([labelWordsOf(rainbowRotateLabelEl)], off);
  setDimmed([labelWordsOf(paletteLabelEl)], off);

  // THE ACCENT ROW, AND THE CASE GLIMSTONE 1.16.0 WAS WRITTEN FOR.
  //
  // Two rules in that document had been contradicting each other for six
  // releases - a control hanging off another mode should be ABSENT, and this
  // exact row should stay with "the dimmed controls the signal that something
  // changed" - and 1.16.0 settles it with one question: DOES THE CONTROL STILL
  // DO ANYTHING?
  //
  // The comment that used to stand here answered "no", on the grounds that
  // "every card on this page carries a palette position". That is true of THIS
  // page and it is not true of the extension, which is the surface the setting
  // belongs to: paintHues() in popup.js hands positions to the header and the
  // send button only, so the tab strip's own pressed segment and both collector
  // buttons (.primary, background: var(--accent)) go on painting the picked
  // accent with the rainbow running. The value is still doing work; it is
  // simply not in charge of everything any more, and removing the row would
  // hide a setting that is still in effect.
  //
  // SO IT DIMS AND STAYS PRESSABLE. "Dim it and say who is in charge" is what
  // the rule asks for; making it inert as well would mean somebody cannot
  // change the colour of the controls it still paints without switching the
  // rainbow off first, which is a capability taken away to signal a state - and
  // it is what this page did until now, with a wrapper the keyboard walked
  // straight through anyway.
  //
  // While the instance decides the look, the row is genuinely refused instead,
  // because then the value really is not this browser's to set.
  setDimmed([labelWordsOf(accentLabelEl), accentSwatches], off || a.rainbow.on);
  setRefused([accentSwatches], off);
  // AND THE "(i)" GAINS A SENTENCE rather than a second glyph beside it: the
  // conditional bubble the language prescribes for exactly this row, appearing
  // while the state holds and saying who is in charge (GlimStone 1.8.0's
  // conditional-bubble rule, pointed at from 1.16.0). Same sentence the web
  // UI's own Look page appends, word for word in all 42 catalogues.
  glimSetInfo(
    'accentLabel',
    a.rainbow.on ? `${t('options.accentHint')} ${t('options.accentRainbowOwns')}` : t('options.accentHint'),
  );

  // The hues last, because rotating or editing the palette changes what every
  // position resolves to, and the report because it names theme, accent and
  // the rainbow — it used to keep whatever it said at page load, which meant
  // it disagreed with the switches directly above it.
  paintHues();
  void renderReport();
}

// --- Problems? -------------------------------------------------------------
//
// A report first, a link second, and in that order on purpose: an issue that
// arrives with no version and no idea how the extension is configured costs a
// round trip before anyone can even start, and the person who filed it has
// usually moved on by then.
//
// What it does NOT collect is as deliberate as what it does. No instance
// address (that is someone's home network), no token, no relay key. What is
// left is what is actually needed: which build, which browser, and the SHAPE
// of the configuration - how many instances, and how many of them are reached
// through a forwarder rather than directly.


/**
 * Which GlimStone this page implements. A plain constant, kept in step by
 * hand, because there is nothing to import it from: this extension has no
 * build step, and the design language is a document plus a stylesheet rather
 * than a package.
 *
 * THE NUMBER IS PER SURFACE, and it has to be, because the three of them are
 * lifted separately. It went 1.6.0 to 1.14.0 in one step once every release
 * between had actually been checked against these files: the brand marks carry
 * their own colours instead of the button's ink, the scrim is a token, deleting
 * a group asks a real question, the control that goes ahead sits at the end of
 * its row, and the three right-to-left languages this extension ships now set
 * `dir` rather than being laid out backwards.
 *
 * 1.14.0 TO 1.17.0, and what each of the three actually cost here:
 *
 *   1.15.0 "The second way in". Two of its three cards are about a LOGIN, and
 *     this extension has none: it joins a phrase group and membership IS the
 *     credential, so it holds no password, sends no Authorization header and
 *     calls /api/auth/login from nowhere. What DOES apply is the release's
 *     third refusal case - a capability the environment forbids is refused with
 *     the reason rather than offered as a button that fails - and this page had
 *     one: "take the look from the default instance", offered with no group to
 *     take it from, which turned on optimistically and snapped back. It is
 *     removed now, with the paragraph the rule owes standing in its place, on
 *     --status-warn-bg-soft, the token 1.15.0 added for exactly that box.
 *
 *   1.16.0 "a control whose value still ACTS stays, dimmed; one that does
 *     nothing goes". Two rows answered it wrongly in opposite directions. The
 *     Click'n'Load countdown stood fully live while the feature was switched
 *     off, where nothing can read it; it is absent now. The accent row was
 *     dimmed AND made inert under rainbow mode, which is the one shape both
 *     halves of the rule forbid - and its value still paints the popup's tab
 *     strip and both collector buttons, so it dims, keeps working, and says who
 *     is in charge through the conditional bubble the language prescribes.
 *
 *   1.17.0 "storm", the hidden fourth motion level. NOTHING TO ADOPT, and that
 *     is a measurement rather than a shrug: this extension has no motion axis
 *     at all - no data-motion attribute, no picker, no stored level, not one
 *     occurrence of "full" or "wild" in any of these files or any of the 42
 *     catalogues - so there is no floor for a fourth step to sit below and no
 *     picker for it to stay out of. What the release DOES ask of a surface with
 *     one fixed level is that the numbers be the top level's numbers, and they
 *     are: glim-shake runs 360ms with the decaying +-4/+-4/+-2/+-2 swing, the
 *     bubble fades in 110ms, and both match the web UI's own top-level
 *     --motion-shake-dur and --motion-fade-dur exactly. The reduced-motion
 *     substitution the language insists on is there too, as glim-shake-quiet.
 *     The same measurement settles the "wild, not full" rename that has been
 *     outstanding since 1.10.0: there is no name here to rename.
 *
 * The phone app still says 1.6.0. That disagreement is deliberate too - a card
 * claiming a release its own files do not speak is worse than one that is
 * behind.
 */
const GLIMSTONE_VERSION = '1.17.0';

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
const CONTACT_MAIL = 'hello@halleluja.design';
// The handle from .github/FUNDING.yml, so there is one place that knows it.
const COFFEE_URL = 'https://buymeacoffee.com/junkerderprovinz';

/** A version number that goes where the version number should go. New tab, so
 *  reading a changelog does not cost somebody the settings page they were in
 *  the middle of. */
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
 * The About card (jdp, 2026-08-31), replacing the quiet centred line that used
 * to sit under the last card: "in der App und der Erweiterung und im KL soll
 * eine neue Card rein ... Die vversionsnummer sollen dann nicht nochmal unter
 * den card im hintergrund angeziegt werden".
 *
 * The footer was the design language's own answer for a while and it had a real
 * weakness: page chrome reads as something nobody put there on purpose, and it
 * has nowhere to hang the thing a person reading a version number usually wants
 * next, which is how to report what they just found. A card carries both, and
 * the two buttons make "report it" a click rather than a search.
 *
 * Versions read from the manifest, never typed here: a number written down
 * twice is a number that disagrees with itself the day one of them is bumped.
 */
function renderAbout() {
  const versions = document.getElementById('aboutVersions');
  const text = document.getElementById('aboutText');
  const gh = document.getElementById('aboutGithub');
  const mail = document.getElementById('aboutMail');
  if (!versions) return;
  // Both numbers are LINKS to their own release page (jdp, 2026-08-31: "Die
  // Versionsnummer (auch von Glimstone) soll immer auf deren release auf github
  // zeigen ... Das soll immmer und überall gelten"). A version answers "which
  // build is this"; the question straight after is always "and what changed",
  // and a number nobody can follow makes somebody search a repository for a tag
  // they then retype by hand. Now GlimStone 1.6.0 for the whole family.
  //
  // Built from the version, never a hand-kept list: that list is wrong the first
  // time somebody forgets it. The tag shape is the repository's own - this one
  // ships three artefacts, so the extension's tag carries a prefix.
  versions.replaceChildren(
    document.createTextNode(`${t('options.aboutVersion')} `),
    versionLink(`${REPO_URL}/releases/tag/extension/v${chrome.runtime.getManifest().version}`, chrome.runtime.getManifest().version),
    document.createTextNode(' · GlimStone '),
    versionLink(`${GLIMSTONE_URL}/releases/tag/v${GLIMSTONE_VERSION}`, GLIMSTONE_VERSION),
  );
  text.textContent = t('options.aboutText');
  // The coffee, with its own sentence and its own button. The handle comes from
  // .github/FUNDING.yml, so one place knows it.
  const kaffee = document.getElementById('aboutCoffee');
  const kaffeeBtn = document.getElementById('aboutCoffeeBtn');
  if (kaffee) kaffee.textContent = t('options.aboutCoffee');
  if (kaffeeBtn) {
    kaffeeBtn.href = COFFEE_URL;
    kaffeeBtn.replaceChildren(glyph(D_COFFEE, 14), document.createTextNode(t('options.aboutCoffeeButton')));
  }
  const melden = document.getElementById('aboutReport');
  if (melden) melden.textContent = t('options.aboutReport');
  gh.href = REPO_URL;
  gh.replaceChildren(glyph(D_GITHUB, 15), document.createTextNode(t('options.aboutGithub')));
  // A plain mailto, with the subject prefilled so a mail that arrives already
  // says which product it is about. No body: a prefilled body reads as a form
  // to fill in, and this is meant to be a message somebody writes.
  mail.href = `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent('KnightLoader ' + t('options.aboutMailSubject'))}`;
  mail.replaceChildren(glyph(D_MAIL, 14), document.createTextNode(t('options.aboutMail')));
}

const reportEl = document.getElementById('report');
const copyReportBtn = document.getElementById('copyReport');

async function buildReport() {
  // The group is asked for rather than read from storage, which makes this
  // report worth pasting: "joined, three instances, none of them answering"
  // and "joined, three instances, all there" are different problems and used
  // to look identical here.
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
  // The switch and the registered scripts are two different facts, and a report
  // that only carries the switch cannot tell "he turned it off" apart from "the
  // registration failed" — the two causes of the one complaint this feature
  // ever produces.
  //
  // The DETAIL matters, not just the count, and that is a lesson from a live
  // failure rather than a preference. jdp reported Click'n'Load dead in Brave
  // with "gar nichts sichtbar"; every candidate cause - the switch off, the
  // scripts unregistered, the registration stale from before a fix, the
  // redirect ruleset not loaded - produces that exact same nothing, and a
  // report saying "on (2 content scripts registered)" tells the four apart not
  // at all. So the report names each script with the two properties that have
  // actually gone wrong (which world, and whether the blank-document fallback
  // is on), plus which rulesets the browser has enabled.
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
  // Prefilled, so the form opens with the report already in it rather than
  // asking somebody to paste something they have to go back for.
  // replaceChildren, not textContent: textContent wipes every child node, which
  // is the reason this button carried no glyph while its two neighbours in the
  // About card did (jdp, 2026-09-02: "Der Bericht teilen button soll bericht
  // kopieren button heißen (mit glyph), in allen instanzen"). It already copied
  // and already said so; the mark was the missing half.
  copyReportBtn.replaceChildren(glyph(D_COPY, 14), document.createTextNode(t('options.problemsCopy')));
}

copyReportBtn.addEventListener('click', async () => {
  const text = await buildReport();
  try {
    await navigator.clipboard.writeText(text);
    say(t('options.problemsCopied'), true);
  } catch {
    // Clipboard permission is not guaranteed on an extension page in every
    // browser. The report is already on screen, so the fallback is to say so
    // rather than to fail silently - and the button that was pressed shakes,
    // because a copy that did not happen looks exactly like one that did.
    say(t('options.problemsCopyFailed'), false);
    shake(copyReportBtn);
  }
});

(async () => {
  // Before anything is drawn: the look goes on <html> first, so no page is
  // ever painted in one look and repainted in another.
  await applyAppearance();
  await loadLanguage();
  // Wired before anything writes a data-tip, so the very first hover already
  // finds a listener rather than the browser's own native balloon.
  wireTooltips();
  applyStaticText();
  await renderLanguagePicker();
  await renderCnl();
  await renderAppearance();
  renderAbout();
  await renderPinHint();
  void renderReport();
  // Last, and not awaited by the rest: it opens a relay connection, and the
  // page should be usable while that is in flight rather than blank.
  void renderGroup();
})();
