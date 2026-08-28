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
const followInstanceRow = followInstanceEl.closest('.glim-row');
const accentLabelEl = document.getElementById('accentLabel');
const problemsHeadingEl = document.getElementById('problemsHeading');
const phraseEye = document.getElementById('phraseEye');

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
const D_RETRY = 'M8 3V1L5 3.5 8 6V4a3.5 3.5 0 1 1-3.5 3.5H3A5 5 0 1 0 8 3z';
const D_EYE =
  'M8 3C4.4 3 1.5 6.1.7 7.6a.8.8 0 0 0 0 .8C1.5 9.9 4.4 13 8 13s6.5-3.1 7.3-4.6a.8.8 0 0 0 0-.8' +
  'C14.5 6.1 11.6 3 8 3zm0 8a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm0-1.6a1.4 1.4 0 1 0 0-2.8 1.4 1.4 0 0 0 0 2.8z';
const D_EYE_OFF =
  'M2.3 1.3 1.2 2.4l2 2C1.9 5.4.9 6.7.7 7.6a.8.8 0 0 0 0 .8C1.5 9.9 4.4 13 8 13c1.2 0 2.3-.4 3.3-.9' +
  'l2.3 2.3 1.1-1.1L2.3 1.3zM8 11a3 3 0 0 1-2.6-4.5l1.2 1.2A1.4 1.4 0 0 0 8 9.4l1.2 1.2A3 3 0 0 1 8 11z' +
  'm7.3-2.6C14.7 9.4 13 11 11 12l-1.4-1.4A3 3 0 0 0 5.4 6.4L4 5a7.6 7.6 0 0 1 4-2c3.6 0 6.5 3.1 7.3 4.6a.8.8 0 0 1 0 .8z';

function say(text, ok) {
  status.textContent = text;
  status.className = ok ? 'ok' : '';
}

/**
 * applyStaticText fills in every fixed label from the current language — called
 * once on load and again whenever the language picker changes it, so nothing
 * needs a page reload to update. "KnightLoader" itself (the <h1>) is left
 * alone: a product name, not translated.
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
  joinBtn.textContent = t('options.join');
  leaveBtn.textContent = t('options.leave');
  refreshBtn.textContent = t('options.refresh');

  languageHeadingEl.textContent = t('options.languageHeading');
  glimSetInfo('languageHeading', t('options.languageSub'));

  cnlToggleEl.textContent = t('options.cnlToggle');
  glimSetInfo('cnlHeading', t('options.cnlSub'));

  // The look, now one card per axis. Each heading carries what the card is
  // for; each row label carries what that one control does. Two different
  // facts, so neither bubble repeats the other.
  appearanceHeadingEl.textContent = t('options.appearanceHeading');
  glimSetInfo('appearanceHeading', t('options.appearanceSub'));
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
  const label = (id, text, tip) => {
    document.getElementById(id).textContent = text;
    glimSetInfo(id, tip);
  };
  label('accentLabel', t('options.accentLabel'), t('options.accentHint'));
  label('rainbowLabel', t('options.rainbow'), t('options.rainbowHint'));
  label('rainbowReactiveLabel', t('options.rainbowReactive'), t('options.rainbowReactiveHint'));
  label('rainbowRotateLabel', t('options.rainbowRotate'), t('options.rainbowRotateHint'));
  label('paletteLabel', t('options.paletteLabel'), t('options.paletteHint'));
  label('followInstanceLabel', t('options.followInstance'), t('options.followInstanceHint'));

  problemsHeadingEl.textContent = t('options.problemsHeading');
  glimSetInfo('problemsHeading', t('options.problemsSub'));
  // The button and the link get their text from renderReport(), which also
  // fills the report itself and the prefilled issue URL — setting them here as
  // well would be two places saying the same thing, and the second one to run
  // would silently win.
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
  phraseEye.replaceChildren(glyph(shown ? D_EYE_OFF : D_EYE, 16));
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
    siblings = await groupInstances();
  } catch {
    loading.textContent = t('options.groupUnreachable');
    return;
  }
  list.innerHTML = '';
  if (siblings.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = t('options.groupEmpty');
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
        onSetDefault: async (picked) => {
          await writeDefaultTarget(picked.instanceId);
          await renderGroup();
          // The report names which instance is the default, so it goes stale
          // the moment that changes - and a report that says "none" under a
          // card wearing the badge is worse than no report.
          void renderReport();
        },
      }),
    );
  });
}

joinForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  joinBtn.disabled = true;
  say(t('options.joining'), true);
  try {
    await writePhrase(phraseInput.value);
  } catch (err) {
    joinBtn.disabled = false;
    say(phraseProblemText(err), false);
    return;
  }
  // Checked against the relay before it is called a success: a phrase that
  // decodes is not the same as a phrase that reaches anybody, and reporting
  // "connected" for the first would be a lie the user only finds out about on
  // their next send.
  try {
    const siblings = await groupInstances();
    say(siblings.length ? t('options.joined', { count: siblings.length }) : t('options.joinedEmpty'), true);
  } catch {
    say(t('options.groupUnreachable'), false);
  }
  joinBtn.disabled = false;
  await renderGroup();
  // The report names whether this browser is in a group and how many instances
  // answered, so it has to be redrawn when that changes - a report still saying
  // "no phrase stored" under a list of two instances is worse than no report.
  void renderReport();
});

leaveBtn.addEventListener('click', async () => {
  await forgetGroup();
  phraseInput.value = '';
  // Back to masked: leaving and re-joining is the one moment somebody is most
  // likely to walk away from an open options page.
  phraseInput.type = 'password';
  renderPhraseEye();
  say(t('options.left'), true);
  await renderGroup();
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
  cnlEnabledEl.setAttribute('aria-checked', String(stored.cnlEnabled !== false));
}

cnlEnabledEl.addEventListener('click', async () => {
  const on = cnlEnabledEl.getAttribute('aria-checked') !== 'true';
  cnlEnabledEl.setAttribute('aria-checked', String(on));
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
    const ok = await adoptFromInstance().catch(() => false);
    if (!ok) {
      followInstanceEl.setAttribute('aria-checked', 'false');
      say(t('options.followFailed'), false);
      return;
    }
  }
  await writeAppearance({ followInstance: on });
  const next = await readAppearance();
  applyShape(next.shape);
  applyAccent(next.accent);
  applyRainbow(next.rainbow);
  await renderAppearance();
});

const themeSeg = document.getElementById('themeSeg');
const shapeSeg = document.getElementById('shapeSeg');
const accentSwatches = document.getElementById('accentSwatches');
const accentNow = document.getElementById('accentNow');

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
  const badges = [...document.querySelectorAll('.glim-section-badge')];
  badges.forEach((el, i) => setHue(el, i));
  // Their own sequence, restarting at 0: this set of three rows is its own
  // equal-member set, exactly as the web UI's Look page treats it.
  [rainbowRow, rainbowReactiveRow, rainbowRotateRow].forEach((el, i) => setHue(el, i));
}

/**
 * swatch builds one round colour button. `onPick` is a plain click (a preset);
 * `onEdit` opens the page's own picker popover for a colour that can be
 * changed - the design language rules out a native <input type="color">, and
 * jdp asked for these to be editable (2026-08-28: "Die Farbfelder sollen ganz
 * rechts angeordnet sein und bearbeitbar sein").
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
    if (onEdit) openColorPickerPopover(b, hex, onEdit, onEditClose);
    else onPick?.();
  });
  return b;
}

/** The reset badge: an icon, not a text link, and the same circle as the
 *  swatches it stands beside. */
function resetBadge(onClick) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'glim-reset';
  b.appendChild(glyph(D_RETRY, 13));
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

async function renderAppearance() {
  const a = await readAppearance();

  // "" is the honest default for two of the three: follow the browser, and use
  // the theme's own gold. Neither is a fourth value to invent.
  segment(
    themeSeg,
    [
      { value: '', label: t('options.themeSystem') },
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

  // The live accent, which is the stored one or the default when nothing is
  // stored. The circle at the trailing edge opens the picker; the eight
  // presets beside it are the shortcut.
  const live = (a.accent || DEFAULT_ACCENT).toLowerCase();
  accentNow.style.backgroundColor = live;
  accentNow.setAttribute('data-tip', t('options.accentLabel'));
  accentNow.setAttribute('aria-label', t('options.accentLabel'));
  accentNow.onclick = () =>
    openColorPickerPopover(
      accentNow,
      live,
      async (hex) => {
        await writeAppearance({ accent: hex });
        applyAccent(hex);
        accentNow.style.backgroundColor = hex;
      },
      // Only once it is closed: the row cannot be redrawn while the popover is
      // open, because that would replace the very button the popover is
      // anchored to. This is what brings the reset badge in and puts the new
      // colour into the report.
      () => void renderAppearance(),
    );

  accentSwatches.innerHTML = '';
  // The presets are a shortcut, not the whole choice, and KnightLoader's own
  // row says so with this label, so this one does too.
  const presets = document.createElement('span');
  presets.className = 'glim-eyebrow';
  presets.style.marginInlineEnd = '2px';
  presets.textContent = t('options.accentPresets');
  accentSwatches.appendChild(presets);
  const pick = async (v) => {
    await writeAppearance({ accent: v });
    applyAccent(v);
    await renderAppearance();
  };
  for (const x of ACCENTS) {
    // A ring marks the current one, not a tick: a glyph would have to stay
    // legible on all eight, which means computing an ink colour for a
    // decoration.
    accentSwatches.appendChild(
      swatch(x.hex, { label: x.name, pressed: x.hex.toLowerCase() === live, onPick: () => pick(x.hex) }),
    );
  }
  // The way back, and only once there is something to go back from (jdp,
  // 2026-08-28: "Die resetbuttons fehlen bei den farben"). It was dropped in
  // an earlier round for appearing and disappearing beside eight fixed
  // circles; what brings it back is that the accent is now editable to any
  // colour at all, so "pick Sunflower again" is no longer a complete way home
  // - a custom colour has no swatch to return to.
  if (live !== DEFAULT_ACCENT.toLowerCase()) {
    accentSwatches.appendChild(resetBadge(() => pick('')));
  }

  // --- The rainbow ---------------------------------------------------------
  // The same three axes the web UI's Look page offers, in the same order, so
  // somebody who set this up there recognises it here.
  rainbowOnEl.setAttribute('aria-checked', String(a.rainbow.on));
  rainbowReactiveEl.setAttribute('aria-checked', String(a.rainbow.reactive));
  rainbowRotateEl.setAttribute('aria-checked', String(a.rainbow.rotate));
  // Reactive, rotation and the palette only mean anything while the mode is on.
  // Dimmed rather than hidden: a control that disappears never teaches anyone
  // what the mode does.
  for (const el of [rainbowReactiveRow, rainbowRotateRow, paletteRow]) {
    el.style.opacity = a.rainbow.on ? '1' : '.5';
    el.style.pointerEvents = a.rainbow.on ? '' : 'none';
  }

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
  // Everything the instance decides is shown but not editable while the switch
  // is on — an accent you can click that snaps back on the next refresh is
  // worse than one you cannot click. Theme is not in the list: it stays local
  // on purpose (see adoptFromInstance).
  for (const el of [accentSwatches, accentNow, shapeSeg, rainbowRow, rainbowReactiveRow, rainbowRotateRow, paletteRow]) {
    el.style.pointerEvents = a.followInstance ? 'none' : el.style.pointerEvents || '';
    el.style.opacity = a.followInstance ? '.5' : el.style.opacity || '';
  }

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

const REPORT_URL = 'https://github.com/junkerderprovinz/knightloader/issues/new?template=extension.yml';

const reportEl = document.getElementById('report');
const copyReportBtn = document.getElementById('copyReport');
const reportLink = document.getElementById('reportLink');

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
  let registered = '?';
  try {
    registered = String((await chrome.scripting.getRegisteredContentScripts()).length);
  } catch {
    /* no scripting permission: leave it unknown rather than claim zero */
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
    `clicknload: ${cnlEnabled !== false ? 'on' : 'off'} (${registered} content scripts registered)`,
  ].join('\n');
}

async function renderReport() {
  const text = await buildReport();
  reportEl.textContent = text;
  // Prefilled, so the form opens with the report already in it rather than
  // asking somebody to paste something they have to go back for.
  reportLink.href = `${REPORT_URL}&report=${encodeURIComponent(text)}`;
  reportLink.textContent = t('options.problemsReport');
  copyReportBtn.textContent = t('options.problemsCopy');
}

copyReportBtn.addEventListener('click', async () => {
  const text = await buildReport();
  try {
    await navigator.clipboard.writeText(text);
    say(t('options.problemsCopied'), true);
  } catch {
    // Clipboard permission is not guaranteed on an extension page in every
    // browser. The report is already on screen, so the fallback is to say so
    // rather than to fail silently.
    say(t('options.problemsCopyFailed'), false);
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
  void renderReport();
  // Last, and not awaited by the rest: it opens a relay connection, and the
  // page should be usable while that is in flight rather than blank.
  void renderGroup();
})();
