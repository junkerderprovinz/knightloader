const list = document.getElementById('list');
const status = document.getElementById('status');
const addForm = document.getElementById('addForm');
const addName = document.getElementById('addName');
const addUrl = document.getElementById('addUrl');
const subtitleEl = document.getElementById('subtitle');
const languageHeadingEl = document.getElementById('languageHeading');
const languageSubEl = document.getElementById('languageSub');
const languageSelect = document.getElementById('languageSelect');
const instancesHeadingEl = document.getElementById('instancesHeading');
const addHeadingEl = document.getElementById('addHeading');
const addButton = document.getElementById('add');
const noteEl = document.getElementById('note');
const syncPeersBtn = document.getElementById('syncPeers');
const appearanceHeadingEl = document.getElementById('appearanceHeading');
const appearanceSubEl = document.getElementById('appearanceSub');
const themeLabelEl = document.getElementById('themeLabel');
const shapeLabelEl = document.getElementById('shapeLabel');
const accentLabelEl = document.getElementById('accentLabel');
const problemsHeadingEl = document.getElementById('problemsHeading');

function say(text, ok) {
  status.textContent = text;
  status.className = ok ? 'ok' : '';
}

/**
 * applyStaticText fills in every fixed label on the page from the current
 * language — called once on load and again whenever the language picker
 * below changes it, so nothing needs a full page reload to update (jdp:
 * "Die sprache soll eingestellt werden können und soll die sprache die im
 * Bowser eingestellt ist standardmäßig übernehmen"). "KnightLoader" itself
 * (the <h1>) is left alone — a product name, not translated.
 */
function applyStaticText() {
  subtitleEl.textContent = t('options.subtitle');
  languageHeadingEl.textContent = t('options.languageHeading');
  languageSubEl.textContent = t('options.languageSub');
  instancesHeadingEl.textContent = t('options.instancesHeading');
  addHeadingEl.textContent = t('options.addHeading');
  addName.placeholder = t('options.addNamePlaceholder');
  addUrl.placeholder = t('options.addUrlPlaceholder');
  addButton.textContent = t('options.addButton');
  noteEl.textContent = t('options.note');
  syncPeersBtn.textContent = t('options.syncButton');
  // The appearance axes and the Problems heading. They were translated into
  // every language and then never read: the markup carried English text and
  // nothing overwrote it, so the page rendered half in the reader's language
  // and half in mine. check-locales.mjs could not see it - a parity gate asks
  // whether a key EXISTS everywhere, not whether anything ever renders it.
  appearanceHeadingEl.textContent = t('options.appearanceHeading');
  appearanceSubEl.textContent = t('options.appearanceSub');
  themeLabelEl.textContent = t('options.themeLabel');
  shapeLabelEl.textContent = t('options.shapeLabel');
  accentLabelEl.textContent = t('options.accentLabel');
  problemsHeadingEl.textContent = t('options.problemsHeading');
}

/** buildLanguageSelect fills the dropdown once; renderLanguageSelect (below) only updates which option is selected. */
function buildLanguageSelect() {
  languageSelect.innerHTML = '';
  const auto = document.createElement('option');
  auto.value = '';
  languageSelect.appendChild(auto);
  for (const lang of LANGUAGES) {
    const opt = document.createElement('option');
    opt.value = lang.code;
    opt.textContent = lang.label;
    languageSelect.appendChild(opt);
  }
}

async function renderLanguageSelect() {
  languageSelect.firstElementChild.textContent = t('options.languageAuto');
  const stored = await chrome.storage.local.get('language');
  languageSelect.value = typeof stored.language === 'string' ? stored.language : '';
}

languageSelect.addEventListener('change', async () => {
  await setLanguage(languageSelect.value || null);
  await loadLanguage();
  applyStaticText();
  await renderLanguageSelect();
  render();
});

async function render() {
  const { instances, defaultName } = await readInstances();
  list.innerHTML = '';
  if (instances.length === 0) {
    const p = document.createElement('p');
    p.className = 'empty';
    p.textContent = t('options.empty');
    list.appendChild(p);
    return;
  }
  for (const inst of instances) {
    const row = document.createElement('div');
    row.className = 'row' + (inst.name === defaultName ? ' isDefault' : '');

    const info = document.createElement('div');
    info.className = 'info';
    const name = document.createElement('div');
    name.className = 'name';
    name.textContent = entryLabel(inst);
    if (inst.name === defaultName) {
      const badge = document.createElement('span');
      badge.className = 'badge';
      badge.textContent = t('options.defaultBadge');
      name.appendChild(badge);
    }
    const url = document.createElement('div');
    url.className = 'url';
    // An entry with no address says WHAT it is instead of showing a blank
    // line: reached through another instance, or not reachable at all.
    url.textContent = inst.url || (inst.via ? t('options.viaPeer', { via: inst.via }) : t('options.unreachable'));
    info.append(name, url);
    row.appendChild(info);

    if (inst.name !== defaultName) {
      const makeDefault = document.createElement('button');
      makeDefault.className = 'secondary';
      makeDefault.type = 'button';
      makeDefault.textContent = t('options.makeDefault');
      makeDefault.addEventListener('click', async () => {
        await writeInstances(instances, inst.name);
        say(t('options.setDefault', { name: entryLabel(inst) }), true);
        render();
      });
      row.appendChild(makeDefault);
    }

    const remove = document.createElement('button');
    remove.className = 'danger';
    remove.type = 'button';
    remove.textContent = t('options.remove');
    remove.addEventListener('click', async () => {
      const next = instances.filter((i) => i.name !== inst.name);
      await writeInstances(next, defaultName);
      say(t('options.removed', { name: entryLabel(inst) }), true);
      render();
    });
    row.appendChild(remove);

    list.appendChild(row);
  }
}

// --- Sync from paired instances --------------------------------------------
//
// jdp: "Wenn ich die Extension installiert habe und danach instanzen
// verbinde, werden die dann automatisch auch in der extension angezeigt?" —
// approved as "#3". Two app instances paired via a pairing code (Settings →
// Access, internal/api/routes_pairing.go) each already know about the other
// through GET /api/instances (internal/api/routes_federation.go) — a
// federation registry this extension's own {name,url}[] list (readInstances,
// shared.js) has never talked to. This section is what closes that gap: ask
// every instance ALREADY in this extension's list what peers IT knows about,
// and fold in whatever this list does not have yet.
//
// Two things stand between "ask" and "fold in":
//
// 1. Permission. manifest.json declares no host_permissions at all (see its
//    own git history / shared.js's readInstances doc comment on why a
//    federation-style fetch was skipped when multi-instance support first
//    landed) — every instance URL here was typed by a user, so there is no
//    fixed set of origins to ask for upfront, and shipping `<all_urls>` in
//    the STORE-LISTED permissions would be exactly the broad grant this repo
//    wants to avoid before a Chrome/Edge/Firefox Web Store submission.
//    optional_host_permissions carries `<all_urls>` instead, and
//    chrome.permissions.request() below asks for the exact origins this
//    extension already has typed addresses for, nothing wider — and only
//    ever from the "Sync paired instances" button's own click handler, since
//    request() only works from a real user gesture (Chrome silently refuses
//    it from a page-load handler, Firefox is stricter still).
//
// 2. Session. GET /api/instances needs a session once the target instance
//    has a password (routes.go's reg.Add doc comment) — there is no separate
//    login step here, credentials:'include' below just rides whatever
//    session cookie this BROWSER already holds for that origin, the exact
//    same one a tab open to that instance would use. No password, no
//    session, or the wrong browser profile and the fetch below simply comes
//    back non-OK; treated the same as an offline instance.
//
// Given (1), the check that runs automatically every time Options opens
// (the bottom IIFE) only ever looks at origins that ALREADY hold a granted
// permission — from an earlier click of the button — so opening this page
// can never itself pop a permission prompt. That keeps the "safest, most
// predictable" page-load sync jdp asked for without a silent request for
// a NEW grant nobody clicked anything to approve.

/** peerOrigin turns a stored instance URL into the match pattern chrome.permissions wants ("https://host:port/*"); null for a URL too broken to parse (readInstances' add-form already validates on the way in, this is just defense against a hand-edited storage blob). */
function peerOrigin(url) {
  try {
    return new URL(url).origin + '/*';
  } catch {
    return null;
  }
}

/**
 * fetchPeers asks one already-configured instance's own GET /api/instances
 * for the peers IT is federated with — same host, same session cookie, just
 * a different path than the /quickadd popup.js's quickAddUrl builds. Never
 * throws: an unreachable instance, an old KnightLoader without the pairing
 * feature, a non-JSON response, or a request that outlives the 8s timeout
 * all resolve to an empty list, exactly like a peer that offered nothing new.
 */
async function fetchPeers(baseUrl) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 8000);
  try {
    const res = await fetch(normalizeOrigin(baseUrl) + '/api/instances', {
      credentials: 'include',
      signal: ctrl.signal,
    });
    // Issue #27: the reason travels with the failure now. It used to be
    // thrown away here and every caller was told "No new instances found" -
    // the same sentence for an empty list, a 401, a dead host and a timeout.
    // Four different problems with four different fixes, and one message that
    // pointed at none of them.
    if (res.status === 401 || res.status === 403) return { peers: [], failure: 'signedOut' };
    if (!res.ok) return { peers: [], failure: 'error' };
    const data = await res.json();
    return { peers: Array.isArray(data) ? data : [], failure: null };
  } catch (e) {
    return { peers: [], failure: e?.name === 'AbortError' ? 'timeout' : 'unreachable' };
  } finally {
    clearTimeout(timer);
  }
}

/**
 * isKnownPeer reuses the addForm submit handler's own duplicate rule below
 * (same name = already have it) and adds the one check that only matters for
 * an AUTOMATIC add: the same URL under a DIFFERENT name must not get a
 * second entry either. A person is free to do that by hand through the form
 * below — this function only guards the path nobody explicitly asked for,
 * so a peer discovered through federation can never silently duplicate or
 * shadow an entry the user already named themselves.
 */
function isKnownPeer(instances, candidate) {
  const url = candidate.url ? normalizeOrigin(candidate.url) : '';
  return instances.some((i) => i.name === candidate.name || (url !== '' && normalizeOrigin(i.url ?? '') === url));
}

/**
 * syncPairedInstances is the one function both call sites below share.
 * requestPermission is false for the page-load call (never prompts, only
 * ever touches origins already granted) and true for the button's click
 * handler (may prompt, once, for every configured origin that still needs
 * it — chrome.permissions.request on an origin already granted just resolves
 * true with no dialog, so this never re-asks for one already held).
 *
 * Returns { added, permissionDenied } rather than throwing on any failure
 * path — jdp's brief: "fail silently/gracefully - this is a convenience
 * sync, not a required step" — the caller decides what, if anything, to
 * show for each shape of "nothing happened".
 */
async function syncPairedInstances({ requestPermission }) {
  const { instances, defaultName } = await readInstances();
  const empty = { added: 0, viaRelay: 0, permissionDenied: false, failures: [], asked: 0 };
  if (instances.length === 0) return empty;

  // Only entries with an address of their own can be ASKED - a relay-only
  // peer has no API this browser can open. It is still a perfectly good
  // sync TARGET (that is what `via` is for); it just cannot be a source.
  const withOrigin = instances.map((i) => ({ ...i, origin: peerOrigin(i.url ?? '') })).filter((i) => i.origin);
  if (withOrigin.length === 0) return empty;

  let reachable;
  if (requestPermission) {
    let granted = false;
    try {
      granted = await chrome.permissions.request({ origins: withOrigin.map((i) => i.origin) });
    } catch {
      granted = false;
    }
    if (!granted) return { ...empty, permissionDenied: true };
    reachable = withOrigin;
  } else {
    reachable = [];
    for (const i of withOrigin) {
      try {
        if (await chrome.permissions.contains({ origins: [i.origin] })) reachable.push(i);
      } catch {
        /* chrome.permissions unavailable for some reason — treat like "not granted", never throw */
      }
    }
  }
  if (reachable.length === 0) return empty;

  const results = await Promise.allSettled(reachable.map(async (i) => ({ from: i, ...(await fetchPeers(i.url)) })));
  let current = instances;
  let added = 0;
  let viaRelay = 0;
  const failures = [];
  for (const r of results) {
    if (r.status !== 'fulfilled') {
      failures.push('error');
      continue;
    }
    const { from, peers, failure } = r.value;
    if (failure) failures.push(failure);
    for (const peer of peers) {
      if (typeof peer?.name !== 'string' || !peer.name) continue;
      if (isKnownPeer(current, peer)) continue;
      // Issue #27: a peer with no URL is not junk to drop - it is a desktop
      // build or a relay-only peer, exactly the case the extension could
      // never reach. It is kept with `via` set to the instance that told us
      // about it, which forwards on its behalf (entryTarget in shared.js).
      // Dropping it is what produced "No new instances found" for a sync that
      // had in fact found something.
      // name is the ADDRESS (federation addresses a relay peer by its
      // instance id); label is the name it announced for people to read.
      // Kept apart deliberately - collapsing them would either show an id or
      // send to a name nothing answers to.
      const label = typeof peer.displayName === 'string' && peer.displayName ? peer.displayName : peer.name;
      const entry =
        typeof peer.url === 'string' && peer.url
          ? { name: peer.name, label, url: peer.url }
          : { name: peer.name, label, url: '', via: normalizeOrigin(from.url) };
      if (!entry.url) viaRelay++;
      // The peer's own federation name IS the name it gets here - no second
      // naming scheme invented on top of internal/federation's.
      current = [...current, entry];
      added++;
    }
  }
  if (added > 0) await writeInstances(current, defaultName);
  return { added, viaRelay, permissionDenied: false, failures, asked: reachable.length };
}

/**
 * syncOutcome turns one sync result into the sentence that is actually true
 * for it. Issue #27's first half: five different outcomes used to share the
 * string "No new instances found", which is a lie for four of them and
 * points at a fix for none.
 */
function syncOutcome({ added, viaRelay, permissionDenied, failures, asked }) {
  if (permissionDenied) return { msg: t('options.syncPermissionDenied'), ok: false };
  if (asked === 0) return { msg: t('options.syncNothingConfigured'), ok: false };
  if (added > 0) {
    const base = t('options.syncAdded', { count: added });
    // Named separately, because those entries behave differently: they have
    // no address of their own and are reached through the instance that knows
    // them. Somebody who sees one appear with no URL deserves to know why.
    return { msg: viaRelay > 0 ? base + ' ' + t('options.syncViaRelay', { count: viaRelay }) : base, ok: true };
  }
  // Nothing added, and at least one instance could not be asked at all: that
  // is the answer, not "no new instances".
  if (failures.length > 0) {
    const [worst, key] = failures.includes('signedOut')
      ? ['signedOut', 'options.syncSignedOut']
      : failures.includes('timeout')
        ? ['timeout', 'options.syncTimeout']
        : failures.includes('unreachable')
          ? ['unreachable', 'options.syncUnreachable']
          : ['error', 'options.syncError'];
    // Count the instances that failed THIS way, not every instance that
    // failed. Three configured, one not signed in and two switched off, and
    // the total would read "not signed in to 3 of the configured instances" -
    // sending somebody to open three tabs and sign in to two machines that
    // were never asking. Which is the same shape of wrong answer this whole
    // function exists to stop giving.
    const count = failures.filter((f) => f === worst).length;
    return { msg: t(key, { count }), ok: false };
  }
  return { msg: t('options.syncNoNew'), ok: true };
}

syncPeersBtn.addEventListener('click', async () => {
  syncPeersBtn.disabled = true;
  say(t('options.syncing'), true);
  const result = await syncPairedInstances({ requestPermission: true });
  syncPeersBtn.disabled = false;
  const { msg, ok } = syncOutcome(result);
  say(msg, ok);
  if (result.added > 0) render();
});

addForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  const name = addName.value.trim();
  const urlValue = addUrl.value.trim();
  if (!name) {
    say(t('options.needName'), false);
    return;
  }
  try {
    new URL(urlValue);
  } catch {
    say(t('options.badUrl'), false);
    return;
  }
  const { instances, defaultName } = await readInstances();
  if (instances.some((i) => i.name === name)) {
    say(t('options.duplicate', { name }), false);
    return;
  }
  const next = [...instances, { name, url: urlValue }];
  // The first instance ever added becomes the default automatically; later
  // ones keep whichever default was already set.
  await writeInstances(next, defaultName ?? name);
  addName.value = '';
  addUrl.value = '';
  say(t('options.added', { name }), true);
  render();
});

/**
 * A fresh install starts with the add form already filled in for the case
 * that is true for most people: KnightLoader running on this same machine,
 * on its own default port. It is a SUGGESTION, not a silent default - the
 * form still has to be submitted, so nothing ever gets pointed at an address
 * nobody looked at (which is exactly why config.default.json ships empty for
 * a `git clone` checkout, see readInstanceUrl in shared.js).
 */
const LOCAL_DEFAULT = 'http://localhost:8749';

async function suggestLocalDefault() {
  const { instances } = await readInstances();
  if (instances.length > 0) return;
  if (addUrl.value === '') addUrl.value = LOCAL_DEFAULT;
  if (addName.value === '') addName.value = t('options.localName');
}


// --- Appearance ------------------------------------------------------------
//
// The three axes GlimStone gives the user: theme, corners, accent. Applied at
// the top of every page (appearance.js) rather than by the page that edits
// them - a page that paints itself leaves every other page on the old value,
// and this one is the last place anyone would notice.
//
// Local rather than read from a configured instance, and worth stating why:
// fetching it would need a host permission for that origin, and this extension
// asks for one only from a real click on the sync button. Paying a permission
// prompt for a colour is a bad trade.

const themeSeg = document.getElementById('themeSeg');
const shapeSeg = document.getElementById('shapeSeg');
const accentSwatches = document.getElementById('accentSwatches');
const accentNow = document.getElementById('accentNow');
const accentInput = document.getElementById('accentInput');

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
  // stored - the swatch row marks whichever of the eight matches it, and the
  // colour input beside them is the way to any other colour, exactly as on
  // KnightLoader's own Look page.
  const live = (a.accent || DEFAULT_ACCENT).toLowerCase();
  accentNow.style.backgroundColor = live;
  accentInput.value = live;
  accentInput.setAttribute('aria-label', t('options.accentLabel'));

  accentSwatches.innerHTML = '';
  // The presets are a shortcut, not the whole choice - the colour field to
  // their left is any other colour - and KnightLoader's own row says so with
  // this label, so this one does too.
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
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'glim-swatch';
    b.style.backgroundColor = x.hex;
    b.title = x.name;
    b.setAttribute('aria-label', x.name);
    // A ring, not a tick: a glyph would have to stay legible on all eight,
    // which means computing an ink colour for a decoration.
    b.setAttribute('aria-pressed', String(x.hex.toLowerCase() === live));
    b.addEventListener('click', () => pick(x.hex));
    accentSwatches.appendChild(b);
  }
  // No reset control here (jdp: "Der standard button der bei der akzentfarbe
  // erschient in der erweiterung kannst du entfernen"). The way back is the
  // first swatch: Sunflower IS the default, so picking it lands on the same
  // colour, and a button that appears and disappears beside eight fixed
  // circles was the only thing in the row that moved.
}

accentInput.addEventListener('change', async () => {
  await writeAppearance({ accent: accentInput.value });
  applyAccent(accentInput.value);
  await renderAppearance();
});

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
const problemsSubEl = document.getElementById('problemsSub');
const copyReportBtn = document.getElementById('copyReport');
const reportLink = document.getElementById('reportLink');

async function buildReport() {
  const { instances, defaultName } = await readInstances();
  const viaForwarder = instances.filter((i) => !i.url && i.via).length;
  const unreachable = instances.filter((i) => !entryTarget(i)).length;
  const a = await readAppearance();
  const m = chrome.runtime.getManifest();
  return [
    `extension: ${m.version}`,
    `browser:   ${navigator.userAgent}`,
    `language:  ${currentLanguage()} (browser: ${navigator.language})`,
    `appearance: theme=${a.theme || 'system'} shape=${a.shape} accent=${a.accent || 'default'}`,
    `instances: ${instances.length} configured, ${viaForwarder} via a forwarder, ${unreachable} with no way in`,
    `default:   ${defaultName ? 'set' : 'none'}`,
  ].join('\n');
}

async function renderReport() {
  const text = await buildReport();
  reportEl.textContent = text;
  problemsSubEl.textContent = t('options.problemsSub');
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
  buildLanguageSelect();
  applyStaticText();
  await renderLanguageSelect();
  await renderAppearance();
  render();
  suggestLocalDefault();
  void renderReport();

  // Silent half of the sync: only origins a PRIOR click of the button
  // already got permission for, so simply opening this page can never pop
  // a permission prompt on its own — see syncPairedInstances' own doc
  // comment above for the full reasoning. Fire-and-forget: the list already
  // rendered above with whatever this extension already had; this only
  // repaints it if something new actually turned up.
  syncPairedInstances({ requestPermission: false }).then(({ added }) => {
    if (added > 0) {
      say(t('options.syncAdded', { count: added }), true);
      render();
    }
  });
})();
