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
    name.textContent = inst.name;
    if (inst.name === defaultName) {
      const badge = document.createElement('span');
      badge.className = 'badge';
      badge.textContent = t('options.defaultBadge');
      name.appendChild(badge);
    }
    const url = document.createElement('div');
    url.className = 'url';
    url.textContent = inst.url;
    info.append(name, url);
    row.appendChild(info);

    if (inst.name !== defaultName) {
      const makeDefault = document.createElement('button');
      makeDefault.className = 'secondary';
      makeDefault.type = 'button';
      makeDefault.textContent = t('options.makeDefault');
      makeDefault.addEventListener('click', async () => {
        await writeInstances(instances, inst.name);
        say(t('options.setDefault', { name: inst.name }), true);
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
      say(t('options.removed', { name: inst.name }), true);
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
    if (!res.ok) return [];
    const data = await res.json();
    return Array.isArray(data) ? data : [];
  } catch {
    return [];
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
  const url = normalizeOrigin(candidate.url);
  return instances.some((i) => i.name === candidate.name || normalizeOrigin(i.url) === url);
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
  if (instances.length === 0) return { added: 0, permissionDenied: false };

  const withOrigin = instances.map((i) => ({ ...i, origin: peerOrigin(i.url) })).filter((i) => i.origin);
  if (withOrigin.length === 0) return { added: 0, permissionDenied: false };

  let reachable;
  if (requestPermission) {
    let granted = false;
    try {
      granted = await chrome.permissions.request({ origins: withOrigin.map((i) => i.origin) });
    } catch {
      granted = false;
    }
    if (!granted) return { added: 0, permissionDenied: true };
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
  if (reachable.length === 0) return { added: 0, permissionDenied: false };

  const results = await Promise.allSettled(reachable.map((i) => fetchPeers(i.url)));
  let current = instances;
  let added = 0;
  for (const r of results) {
    if (r.status !== 'fulfilled') continue;
    for (const peer of r.value) {
      if (typeof peer?.name !== 'string' || typeof peer?.url !== 'string' || !peer.name || !peer.url) continue;
      if (isKnownPeer(current, peer)) continue;
      // The peer's own federation name IS the name it gets here — no second
      // naming scheme invented on top of internal/federation's.
      current = [...current, { name: peer.name, url: peer.url }];
      added++;
    }
  }
  if (added > 0) await writeInstances(current, defaultName);
  return { added, permissionDenied: false };
}

syncPeersBtn.addEventListener('click', async () => {
  syncPeersBtn.disabled = true;
  say(t('options.syncing'), true);
  const { added, permissionDenied } = await syncPairedInstances({ requestPermission: true });
  syncPeersBtn.disabled = false;
  if (permissionDenied) {
    say(t('options.syncPermissionDenied'), false);
  } else if (added > 0) {
    say(t('options.syncAdded', { count: added }), true);
    render();
  } else {
    say(t('options.syncNoNew'), true);
  }
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

(async () => {
  await loadLanguage();
  buildLanguageSelect();
  applyStaticText();
  await renderLanguageSelect();
  render();

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
