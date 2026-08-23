// Shared by background.js (importScripts), popup.js and options.js (<script>
// tag) — one copy of "where is the instance and how do we reach it" rather
// than three that can each drift.

/**
 * readInstanceUrl resolves the configured origin: whatever was saved in
 * chrome.storage.local, or — the first time the extension runs, before
 * anyone has opened Options — the origin the zip was generated from.
 *
 * That default is baked into config.default.json at download time by
 * internal/api/routes_browsertools.go, from the request that fetched the
 * zip. Loading it from a bundled file rather than hardcoding it at build
 * time is what lets `git clone` + "load unpacked" also work: that copy's
 * config.default.json ships with an empty instanceUrl, and readInstanceUrl
 * returns '' for it exactly like an unconfigured install, rather than
 * silently pointing a developer's checkout at somebody else's server.
 */
async function readInstanceUrl() {
  const stored = await chrome.storage.local.get('instanceUrl');
  if (typeof stored.instanceUrl === 'string' && stored.instanceUrl !== '') {
    return stored.instanceUrl;
  }
  try {
    const res = await fetch(chrome.runtime.getURL('config.default.json'));
    const cfg = await res.json();
    return typeof cfg.instanceUrl === 'string' ? cfg.instanceUrl : '';
  } catch {
    return '';
  }
}

async function writeInstanceUrl(url) {
  await chrome.storage.local.set({ instanceUrl: url });
}

/**
 * Multi-instance registry (jdp, 2026-08-23: "wenn mehrere Instanzen
 * vorhanden/verbunden sind soll man z.B. bei Click'n'Load auswählen können
 * zu welcher instanz es hinzugefügt werden soll. man soll eine instanz auch
 * als standard auswählen können.") - a small, extension-local {name, url}[]
 * plus one default name, independent of the app's own /api/instances
 * federation registry (Instances.tsx). Kept separate on purpose: the
 * extension has no session cookie for an arbitrary configured origin until
 * the user actually visits it in that tab, so fetching a remote instance's
 * own API from the background script would need host_permissions per origin
 * for no real benefit - the user already knows the addresses of instances
 * they run, the same way they already typed the one URL Options asked for
 * before this change.
 */
async function readInstances() {
  const stored = await chrome.storage.local.get(['instances', 'defaultInstance']);
  if (Array.isArray(stored.instances) && stored.instances.length > 0) {
    const names = stored.instances.map((i) => i.name);
    const defaultName = names.includes(stored.defaultInstance) ? stored.defaultInstance : names[0];
    return { instances: stored.instances, defaultName };
  }
  // First run since this became multi-instance: fold the single URL this
  // install already had (typed by hand, or baked into config.default.json)
  // into a one-entry list, so nobody who already configured the extension
  // loses that configuration.
  const legacyUrl = await readInstanceUrl();
  if (legacyUrl) {
    const instances = [{ name: 'Default', url: legacyUrl }];
    await writeInstances(instances, 'Default');
    return { instances, defaultName: 'Default' };
  }
  return { instances: [], defaultName: null };
}

async function writeInstances(instances, defaultName) {
  const names = instances.map((i) => i.name);
  const resolved = names.includes(defaultName) ? defaultName : (names[0] ?? null);
  await chrome.storage.local.set({ instances, defaultInstance: resolved });
}

/** normalizeOrigin strips a trailing slash so string concatenation below never doubles one up. */
function normalizeOrigin(url) {
  return url.replace(/\/+$/, '');
}

/**
 * quickAddUrl builds the address /quickadd resolves — the same page the
 * bookmarklet and the PWA share target land on, so all three entrances share
 * one implementation of "stage this and say what happened" (web/src/pages/QuickAdd.tsx).
 */
function quickAddUrl(origin, { url, text, title } = {}) {
  const u = new URL(normalizeOrigin(origin) + '/quickadd');
  if (url) u.searchParams.set('url', url);
  if (text) u.searchParams.set('text', text);
  if (title) u.searchParams.set('title', title);
  return u.toString();
}
