// The three axes that belong to the user, applied here the same way GlimStone
// applies them everywhere: an attribute or a custom property on <html>, and
// nothing below has to know which setting produced a value.
//
// Ported from the shared reference (github.com/junkerderprovinz/glimstone,
// reference/appearance.ts) with its types stripped, because this extension
// deliberately has no build step - src/ ships as plain files so "load
// unpacked" works straight from a checkout (see ../embed.go). The VALUES are
// copied verbatim: re-picking a colour by eye is how two apps that both claim
// one design language stop looking like one product.
//
// The rainbow used to be deliberately absent here, on the reasoning that it
// colours ACTIVITY in a list by position and none of these small pages was a
// list of anything. That reasoning expired: the popup, the send-to window and
// the options page all draw the group as a list of instance cards now, which is
// exactly what the mode is for. jdp, 2026-08-28: "Der Regenbogenmodus fehlt
// komplett in der Erweiterung!" - and an app carrying half a design language is
// not carrying it.

const SHAPES = ['round', 'soft', 'square'];

/** The accent before anyone touches the picker: every app in the family opens
 *  in this colour, so a fresh install already looks like the same product. */
const DEFAULT_ACCENT = '#FCC419';

const ACCENTS = [
  { name: 'Sunflower', hex: '#FCC419' },
  { name: 'Blue', hex: '#1D99F3' },
  { name: 'Green', hex: '#6FDC8C' },
  { name: 'Red', hex: '#FF8389' },
  { name: 'Purple', hex: '#BE95FF' },
  { name: 'Orange', hex: '#FF832B' },
  { name: 'Teal', hex: '#3DDBD9' },
  { name: 'Pink', hex: '#FF7EB6' },
];

function validHex(hex) {
  return typeof hex === 'string' && /^#[0-9a-fA-F]{6}$/.test(hex);
}

function parseHex(hex) {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/**
 * accentSlot is which of the eight preset positions a colour belongs to.
 *
 * An exact preset answers with itself; anything else answers with the nearest,
 * and that is what gives a hand-mixed accent a home. The row shows the eight
 * presets and marks the one in force - so a colour that is no preset used to be
 * in force and shown nowhere, with no way back to the picker that made it. Now
 * the slot it was nudged out of keeps it, wears it, and re-opens the picker on
 * it. Eight circles, no ninth, and nothing invisible.
 *
 * Plain squared RGB distance, deliberately not a perceptual metric: it only has
 * to be stable and unsurprising for eight widely separated hues, and every
 * fancier answer here is a colour-science argument nobody can check by looking.
 */
function accentSlot(hex) {
  if (!validHex(hex)) return -1;
  const c = parseHex(hex);
  let best = 0;
  let bestD = Infinity;
  for (let i = 0; i < ACCENTS.length; i++) {
    const p = parseHex(ACCENTS[i].hex);
    const d = (c.r - p.r) ** 2 + (c.g - p.g) ** 2 + (c.b - p.b) ** 2;
    if (d < bestD) {
      bestD = d;
      best = i;
    }
  }
  return best;
}

/**
 * sanitiseCustoms is what a stored map of hand-mixed slot colours is allowed to
 * say: a real hex against a real slot index, and nothing else.
 *
 * Read on the way IN, so nothing downstream has to defend itself against a
 * value the picker could not have produced. Keyed by slot index as a string,
 * because that is what an object round-tripped through storage gives back, and
 * because the app stores the identical shape (mobile/src/theme/
 * AppearanceContext.tsx sanitiseCustoms): one defect, one fix, one shape in
 * both clients.
 *
 * An empty map rather than a missing one, because "no slot has been mixed" and
 * "nothing valid was stored" are the same answer to every caller here.
 */
function sanitiseCustoms(raw) {
  if (!raw || typeof raw !== 'object') return {};
  const out = {};
  for (const [k, v] of Object.entries(raw)) {
    const i = Number(k);
    if (!Number.isInteger(i) || i < 0 || i >= ACCENTS.length) continue;
    if (validHex(v)) out[String(i)] = v;
  }
  return out;
}

/**
 * luminance is the perceptual brightness used to decide black or white on top.
 * The sRGB channels are linearised first, because the raw values overstate how
 * bright blue is and understate green - which is exactly the case that
 * produces an unreadable button.
 */
function luminance(r, g, b) {
  const lin = (c) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** contrastOn is the ink to put ON a colour, computed rather than configured:
 *  asking for a second colour to make the first one readable is not a setting,
 *  it is a trap. */
function contrastOn(hex) {
  if (!validHex(hex)) return '#FFFFFF';
  const { r, g, b } = parseHex(hex);
  // Carbon's own ink, not a warm near-black: on a yellow accent a
  // brown-tinted black reads as a smudge.
  return luminance(r, g, b) > 0.55 ? '#161616' : '#FFFFFF';
}

/**
 * systemTheme is what the machine is set to, right now.
 *
 * It is what the picker SHOWS when nobody has chosen yet (jdp, 2026-08-29: "es
 * soll nur hell und dunkel zur auswahl geben und es soll automatisch der im
 * system eingestelle modus standardmäßig ausgewählt werden"). Exactly the rule
 * the language picker already follows: resolve the machine's answer and select
 * it, rather than offering a third entry that names the act of resolving.
 * "Follow the browser" looked like a choice and was an excuse - it could not
 * answer the only question anyone asks the control, which is which of the two
 * is running.
 */
function systemTheme() {
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(theme) {
  const root = document.documentElement;
  // Always one of the two now. Nothing stored still means "what the machine
  // says", resolved at read time rather than written down - so a machine that
  // switches to dark in the evening takes the extension with it, right up
  // until somebody picks a side, and from then on their choice holds.
  root.setAttribute('data-theme', theme === 'light' || theme === 'dark' ? theme : systemTheme());
}

function applyShape(shape) {
  document.documentElement.setAttribute('data-shape', SHAPES.includes(shape) ? shape : 'round');
}

/**
 * applyAccent overrides the accent tokens, or clears the override so the
 * theme's own gold comes back - including the darkened one the light theme
 * uses, which is why clearing has to remove the properties rather than write
 * the dark default over them.
 */
function applyAccent(hex) {
  const root = document.documentElement.style;
  if (!validHex(hex)) {
    root.removeProperty('--accent');
    root.removeProperty('--accent-contrast');
    root.removeProperty('--accent-soft');
    return;
  }
  const { r, g, b } = parseHex(hex);
  root.setProperty('--accent', hex);
  root.setProperty('--accent-contrast', contrastOn(hex));
  root.setProperty('--accent-soft', `rgba(${r}, ${g}, ${b}, 0.14)`);
}

/**
 * readAppearance is what this extension has chosen locally.
 *
 * Local, and not read from a configured instance, for a reason worth stating:
 * fetching it would need a host permission for that origin, and this extension
 * is careful never to ask for one outside a real click on the sync button (see
 * options.js). Paying a permission prompt for a colour would be a bad trade -
 * so the extension carries its own three settings, defaulting to the same
 * values every other app in the family starts from.
 */
async function readAppearance() {
  const s = await chrome.storage.local.get([
    'theme', 'accent', 'shape',
    'accentSlotChosen', 'accentCustoms',
    'rainbow', 'rainbowReactive', 'rainbowRotate', 'rainbowSeed', 'rainbowPalette',
    'followInstance',
  ]);

  const accent = validHex(s.accent) ? s.accent : '';

  /* Two facts about the swatch row are STORED here rather than worked out from
     the accent, because neither one is recoverable from a colour.

     jdp, 2026-09-02: "Der farbpicker funktionniert nicht bzw. speichert das
     farbfeld die ausgewählte farbe nicht", and on the app a day earlier, the
     same defect stated from the other side: "wenn ich zb. alle farbfelder rot
     machen will geht das nicht. nicht alle farbfelder speichern dann die
     farbe".

     The whole custom-colour state used to be the single `accent` string. WHICH
     slot was chosen came from accentSlot(), and WHAT a slot had been mixed to
     was never kept at all: the one live colour was simply painted over its
     nearest preset. So the row could hold exactly one hand-mixed colour, and
     choosing any other swatch overwrote it out of existence (jdp, 2026-09-01:
     "wenn man ein Farbfeld bearbeitet setzt es die farbe wieder zurück, sobald
     man ein anderes farbfeld auswählt"). Deriving the choice fails the moment
     two slots hold the same colour, which is exactly what mixing them all to
     one red does: the arithmetic marks one slot and the other seven fall back
     to their factory hex.

     Note the key is accentSlotChosen and not accentSlot: options.html loads
     these files as plain scripts into one shared global (this extension has no
     build step), so a constant named accentSlot would shadow the function. */
  const accentChosen =
    Number.isInteger(s.accentSlotChosen) && s.accentSlotChosen >= 0 && s.accentSlotChosen < ACCENTS.length
      ? s.accentSlotChosen
      : undefined;
  const accentCustoms = sanitiseCustoms(s.accentCustoms);
  // One-time migration for an install that predates the map: a colour already
  // mixed lives only in `accent`, so hand it to the slot that was wearing it.
  // Without this, shipping the fix would snap somebody's hand-mixed accent back
  // to a factory preset, which is the very complaint being fixed.
  if (Object.keys(accentCustoms).length === 0 && accent) {
    const slot = accentChosen !== undefined ? accentChosen : accentSlot(accent);
    if (slot >= 0) accentCustoms[String(slot)] = accent;
  }

  return {
    // Resolved, never blank: the picker offers two values and has to be able
    // to show which one is in force. systemTheme() is the answer while nobody
    // has chosen, and it is a live reading, so it follows the machine.
    theme: s.theme === 'light' || s.theme === 'dark' ? s.theme : systemTheme(),
    accent,
    // What each of the eight preset slots was last mixed to, by slot index.
    accentCustoms,
    // Which slot is chosen, or undefined on a fresh install where nobody has
    // chosen yet. The caller falls back to accentSlot() there, which is still
    // the right answer while every slot wears a different colour.
    accentChosen,
    shape: SHAPES.includes(s.shape) ? s.shape : 'round',
    rainbow: {
      on: s.rainbow === true,
      reactive: s.rainbowReactive === true,
      rotate: s.rainbowRotate === true,
      seed: Number.isFinite(s.rainbowSeed) ? s.rainbowSeed : 0,
      palette: usablePalette(s.rainbowPalette),
    },
    // Whether the look is taken from the default instance instead of being
    // chosen here. Theme is never part of it - see applyAppearance.
    followInstance: s.followInstance === true,
  };
}

async function writeAppearance(next) {
  await chrome.storage.local.set(next);
}

/** applyAppearance puts the stored choices on <html>. Called first thing on
 *  every page, before anything is drawn, so nothing is ever painted in one
 *  look and repainted in another. */
async function applyAppearance() {
  const a = await readAppearance();
  applyTheme(a.theme);
  applyShape(a.shape);
  applyAccent(a.accent);
  applyRainbow(a.rainbow);
  return a;
}

/**
 * adoptFromInstance takes the look from the default instance and stores it here.
 *
 * GET /api/appearance exists precisely so this is not a licence to read
 * /api/settings, and it is on the list a group member may reach through the
 * relay - so this costs no permission at all. The older comment above said
 * fetching it would need a host permission for that origin, which was true of
 * the direct fetch this extension used to do and is not true of a relayed call.
 *
 * Theme is deliberately NOT taken. Light or dark is a property of the screen
 * somebody is sitting at, not of a server: a headless container has no opinion
 * about whether this browser is in a dark room, and overwriting the local
 * choice with the instance's would be the one part of the look that gets worse.
 *
 * Returns false when there is nothing to take it from, so the caller can say so
 * rather than silently leaving the switch on with nothing behind it.
 */
async function adoptFromInstance() {
  const target = await readDefaultTarget();
  const res = await withGroup(async ({ siblings, call }) => {
    const pick = siblings.find((s) => s.instanceId === target) ?? siblings[0];
    if (!pick) return null;
    return call(pick.instanceId, 'GET', '/api/appearance');
  });
  if (!res || res.status < 200 || res.status >= 300) return false;
  let a;
  try {
    a = JSON.parse(res.body);
  } catch {
    return false;
  }
  // The row's own two facts are dropped, not overwritten: the instance sends a
  // single accent and has no opinion about which of the eight slots it belongs
  // to or what the other seven were mixed to. Leaving them would paint the
  // local mixes over a colour that came from the server, so the row would
  // disagree with the look actually in force. They are REMOVED rather than set
  // to undefined, because chrome.storage keeps an undefined as a present value
  // and readAppearance would then read it instead of falling through. The
  // stashed copy in options.js (ADOPTED_KEYS) is what brings them back when the
  // follow switch goes off again.
  await chrome.storage.local.remove(['accentSlotChosen', 'accentCustoms']);
  await writeAppearance({
    accent: validHex(a.accent) ? a.accent : '',
    shape: SHAPES.includes(a.shape) ? a.shape : 'round',
    rainbow: a.rainbow === true,
    rainbowReactive: a.rainbowReactive === true,
    rainbowRotate: a.rainbowRotate === true,
    rainbowSeed: Number.isFinite(a.rainbowSeed) ? a.rainbowSeed : 0,
    rainbowPalette: Array.isArray(a.rainbowPalette) ? a.rainbowPalette : null,
  });
  return true;
}

/* ---------------------------------------------------------------------------
   The rainbow: the accent in the plural.

   Ported from the shared reference (glimstone/reference/appearance.ts and the
   web UI's own lib/appearance.ts) with the VALUES copied verbatim. Re-picking
   these by eye is how two apps that both claim one design language stop looking
   like one product.
   --------------------------------------------------------------------------- */

/** Eight positions. Sunflower is the default accent, so one row always matches
 *  the single-accent look somebody is coming from. */
const RAINBOW = [
  '#FF8389', // red 30
  '#FF832B', // orange 40
  '#FCC419', // sunflower
  '#6FDC8C', // green 30
  '#3DDBD9', // teal 30
  '#1D99F3', // blue
  '#BE95FF', // purple 30
  '#FF7EB6', // magenta 30
];

const RAINBOW_OFF = { on: false, reactive: false, rotate: false, seed: 0, palette: RAINBOW };

let rainbowNow = RAINBOW_OFF;

/** A palette is taken only in full: one unusable entry is not a palette with
 *  seven good colours, it is a palette that turns one row invisible. */
function usablePalette(p) {
  if (!Array.isArray(p) || p.length !== RAINBOW.length) return RAINBOW;
  return p.every(validHex) ? p : RAINBOW;
}

/** The colour for one position, with the rotation applied. */
function rainbowAt(i) {
  const p = rainbowNow.palette;
  const off = rainbowNow.rotate ? rainbowNow.seed : 0;
  const n = ((Math.trunc(i) % p.length) + p.length) % p.length;
  return p[(n + off) % p.length];
}

/**
 * hueVars are the inline custom properties an element carrying a palette
 * position sets on itself. The `.glim-hue` rules in glimstone.css decide
 * whether that hue shows at rest or waits for a hover, so a component only ever
 * says which colour it owns, never which mode is active.
 *
 * The class and these properties always travel together: `.glim-hue` with no
 * `--item-hue` under it resolves the accent to nothing.
 */
function hueVars(hex) {
  if (!validHex(hex)) return {};
  const { r, g, b } = parseHex(hex);
  return {
    '--item-hue': hex,
    '--item-hue-ink': contrastOn(hex),
    '--item-hue-soft': `rgba(${r}, ${g}, ${b}, 0.22)`,
    '--item-hue-wash': `rgba(${r}, ${g}, ${b}, 0.16)`,
    '--item-hue-badge': `rgba(${r}, ${g}, ${b}, 0.5)`,
    '--item-hue-ring': `rgba(${r}, ${g}, ${b}, 0.55)`,
  };
}

/** setHue puts a palette position on one element: the class and the properties
 *  in one call, so no caller can hand out one without the other. */
function setHue(el, i) {
  el.classList.add('glim-hue');
  const vars = hueVars(rainbowAt(i));
  for (const [k, v] of Object.entries(vars)) el.style.setProperty(k, v);
}

/**
 * setHues hands a whole set its positions, 0-based, in the order given.
 *
 * Every surface in this extension calls it once per render with the blocks
 * that are an equal-member set on that surface — the cards on the options
 * page, the header and the send button in the popup. Positions are assigned to
 * the CONTAINER rather than to the one badge inside it, because
 * `[data-rainbow] .glim-hue` rebinds --accent for the whole subtree: give a
 * card its position and its badge, its switch track, its buttons and its focus
 * ring all follow, with no list of exceptions to keep in step.
 *
 * That is what was missing (jdp, 2026-08-29: "der Regenbogenmodus funktioniert
 * erweiterungs-weit nicht überall"). Only the badges and the instance cards
 * owned a position, so the mode was on and three quarters of the extension went
 * on wearing the single accent.
 */
function setHues(elements) {
  elements.filter(Boolean).forEach((el, i) => setHue(el, i));
}

function applyRainbow(next) {
  const merged = { ...RAINBOW_OFF, ...next };
  merged.palette = usablePalette(merged.palette);
  merged.seed = Number.isFinite(merged.seed) ? Math.abs(Math.trunc(merged.seed)) % RAINBOW.length : 0;
  rainbowNow = merged;

  const root = document.documentElement;
  for (let i = 0; i < RAINBOW.length; i++) root.style.setProperty(`--rb-${i}`, rainbowAt(i));
  const mode = !merged.on ? 'off' : merged.reactive ? 'reactive' : 'on';
  if (mode === 'off') root.removeAttribute('data-rainbow');
  else root.setAttribute('data-rainbow', mode);
}
