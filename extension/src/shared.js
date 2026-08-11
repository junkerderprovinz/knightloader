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
