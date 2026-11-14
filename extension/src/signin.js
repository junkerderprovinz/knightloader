// Passing the browser's sign-ins on to KnightLoader (internal/sitelogin). Each
// instance lists the sites whose session it borrows from the browser. Once the
// browser lets the extension read those sites' cookies, the background hands a
// site's session to every instance that lists it: at start, after pairing and
// whenever a sign-in cookie changes, and an empty one once the browser signs
// out. Cookies of private windows are never read.
//
// Plain functions, so check-signin.mjs can run them without a browser.

const SIGNIN_LIST = '/api/sitelogins/handover';

/** What reading the sites needs: the cookies, on their own domains alone. */
function signinAccess(sites) {
  return { permissions: ['cookies'], origins: sites.flatMap((s) => s.domains.flatMap((d) => [`https://${d}/*`, `https://*.${d}/*`])) };
}

/** Whether a cookie's domain is one of the site's or below one of them. */
function siteOwns(site, domain) {
  const d = String(domain ?? '').toLowerCase().replace(/^\./, '');
  return site.domains.some((own) => d === own || d.endsWith(`.${own}`));
}

/**
 * Whether an instance's entry is a site this browser may read: a name, the
 * names of its sign-in cookies, and domains of at least two labels each, so
 * no entry reaches every site of a top-level domain or more.
 */
function readableSite(site) {
  return (
    !!site &&
    typeof site.id === 'string' &&
    typeof site.name === 'string' &&
    Array.isArray(site.signIn) &&
    site.signIn.length > 0 &&
    site.signIn.every((n) => typeof n === 'string' && n !== '') &&
    Array.isArray(site.domains) &&
    site.domains.length > 0 &&
    site.domains.every((d) => typeof d === 'string' && /^[a-z0-9-]+(\.[a-z0-9-]+)+$/.test(d))
  );
}

/** The sites each instance of the group lists, as { instance, sites }; an instance that did not answer lists none. */
async function listedSites(siblings, call) {
  return Promise.all(
    siblings.map(async (instance) => {
      const list = await readJSON(call, instance.instanceId, SIGNIN_LIST);
      return { instance, answered: Array.isArray(list), sites: (Array.isArray(list) ? list : []).filter(readableSite) };
    }),
  );
}

/** Every site any instance lists, once, with what the permission prompt and the watcher need. */
function allSites(lists) {
  const byId = new Map();
  for (const { sites } of lists) {
    for (const s of sites) {
      if (!byId.has(s.id)) byId.set(s.id, { id: s.id, name: s.name, domains: s.domains, signIn: s.signIn });
    }
  }
  return [...byId.values()];
}

/** The site names as a list in the page's language, such as "Patreon and YouTube". */
function siteNames(sites) {
  const names = sites.map((s) => s.name);
  try {
    return new Intl.ListFormat(currentLanguage(), { type: 'conjunction' }).format(names);
  } catch {
    return names.join(', ');
  }
}

/** The sites the group listed last, kept so a click can ask for them before anything is awaited. */
async function storedSites() {
  const { signinSites } = await chrome.storage.local.get('signinSites');
  return Array.isArray(signinSites) ? signinSites.filter(readableSite) : [];
}

/**
 * Asks the background for the group's sites while none are known, as when the
 * phrase went in before an instance let this browser into the group.
 */
async function learnSignInSites() {
  if ((await storedSites()).length > 0 || !(await readPhrase())) return;
  chrome.runtime.sendMessage({ type: 'knightloader-signins' }).catch(() => {});
}

/**
 * Whether sign-ins are passed on. `wanted` is the settings switch, on unless
 * someone turned it off; `on` also needs the access to the sites, which
 * taking downloads over may already hold.
 */
async function signinState(sites) {
  const { signinEnabled } = await chrome.storage.local.get('signinEnabled');
  const wanted = signinEnabled !== false;
  const list = sites ?? (await storedSites());
  let on = false;
  if (wanted && list.length > 0) {
    try {
      on = await chrome.permissions.contains(signinAccess(list));
    } catch {
      // A browser that cannot say has not granted it.
    }
  }
  return { wanted, on, sites: list };
}

/**
 * Asks for the access and keeps the switch on once it is granted. Nothing may
 * be awaited before this in a click handler, or the browser stops counting
 * the click as the user's.
 */
async function requestSignins(sites) {
  const granted = await chrome.permissions.request(signinAccess(sites)).catch(() => false);
  if (granted) await chrome.storage.local.set({ signinEnabled: true });
  return granted;
}

/** Switches passing sign-ins on off and gives back the sites' domains, and the cookies unless another feature reads them. */
async function releaseSignins() {
  await chrome.storage.local.set({ signinEnabled: false });
  const [takeover, media, sites] = await Promise.all([takeoverState(), mediaState(), storedSites()]);
  const permissions = takeover.on || media.on ? [] : ['cookies'];
  await chrome.permissions.remove({ permissions, origins: signinAccess(sites).origins }).catch(() => false);
}

/** Chrome keeps incognito cookies in store "1", Firefox in "firefox-private". */
const privateStoreId = (id) => id === '1' || id === 'firefox-private';

/** Whether a cookie change is one of a listed site's sign-in cookies outside a private window. */
function signInCookieChanged(sites, change) {
  const c = change?.cookie;
  if (!c || privateStoreId(c.storeId)) return false;
  return sites.some((s) => s.signIn.includes(c.name) && siteOwns(s, c.domain));
}

/** Sign-in cookie changes since the last sync, by store and domain, so the store changed last can win. */
const signinChanges = new Map();

function noteSignInChange(change, at = Date.now()) {
  const c = change.cookie;
  signinChanges.set(`${c.storeId}\t${String(c.domain).toLowerCase().replace(/^\./, '')}`, at);
}

/** Folds the noted changes into the stored ones, as { siteId: { storeId: ms } }. */
function mergeChanges(stored, sites) {
  const out = {};
  for (const [id, byStore] of Object.entries(stored && typeof stored === 'object' ? stored : {})) out[id] = { ...byStore };
  for (const [key, at] of signinChanges) {
    const [store, domain] = key.split('\t');
    for (const s of sites) {
      if (!siteOwns(s, domain)) continue;
      out[s.id] ??= {};
      out[s.id][store] = Math.max(out[s.id][store] ?? 0, at);
    }
  }
  signinChanges.clear();
  return out;
}

/** How many Firefox container ids are tried; ids count up from 1 as containers are made. */
const CONTAINER_PROBE = 32;

/**
 * The cookie stores to read: those the browser lists outside private windows,
 * and those a sign-in came from before. Firefox lists a container only while
 * one of its tabs is open, and listing them all takes a permission of its
 * own, so in Firefox each container id is tried, and one that does not exist
 * is refused.
 */
async function cookieStores(handed, changed) {
  const ids = new Set();
  for (const s of await chrome.cookies.getAllCookieStores()) {
    if (!s.incognito && !privateStoreId(s.id)) ids.add(s.id);
  }
  if (ids.has('firefox-default')) {
    const tries = Array.from({ length: CONTAINER_PROBE }, (_, i) => `firefox-container-${i + 1}`);
    const found = await Promise.all(
      tries.map((storeId) =>
        chrome.cookies.getAll({ storeId, domain: 'knightloader.invalid' }).then(
          () => storeId,
          () => '',
        ),
      ),
    );
    found.filter(Boolean).forEach((id) => ids.add(id));
  }
  for (const bySite of Object.values(handed)) for (const h of Object.values(bySite)) if (h.store) ids.add(h.store);
  for (const byStore of Object.values(changed)) for (const id of Object.keys(byStore)) ids.add(id);
  return [...ids].filter((id) => !privateStoreId(id));
}

/** The site's cookies in one store, with the fields the instance builds a cookies.txt from and nothing else. */
async function readSiteCookies(site, storeId) {
  const found = new Map();
  for (const domain of site.domains) {
    for (const c of await chrome.cookies.getAll({ domain, storeId })) {
      if (!siteOwns(site, c.domain)) continue;
      found.set(`${c.domain}\t${c.path}\t${c.name}`, {
        domain: c.domain,
        hostOnly: !!c.hostOnly,
        path: c.path,
        secure: !!c.secure,
        httpOnly: !!c.httpOnly,
        session: !!c.session,
        expirationDate: c.expirationDate ?? 0,
        name: c.name,
        value: c.value,
      });
    }
  }
  return [...found.values()];
}

const signInCookies = (site, cookies) => cookies.filter((c) => site.signIn.includes(c.name) && c.value !== '');

/** A digest of the sign-in cookies, so a new session is told from a tracking cookie changing without keeping the session. */
async function sessionPrint(site, cookies) {
  const lines = signInCookies(site, cookies)
    .map((c) => `${c.domain}\t${c.path}\t${c.name}\t${c.value}`)
    .sort();
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(lines.join('\n')));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

const defaultStoreId = (id) => id === '0' || id === 'firefox-default';

/**
 * The signed-in store to hand over: the one whose sign-in changed last, then
 * the one handed over before, then the default store.
 */
function pickReading(readings, changed, lastStore) {
  const rank = (r) => [changed[r.store] ?? 0, r.store === lastStore ? 1 : 0, defaultStoreId(r.store) ? 1 : 0];
  const better = (a, b) => {
    const [x, y] = [rank(a), rank(b)];
    const i = x.findIndex((v, n) => v !== y[n]);
    return i >= 0 && x[i] > y[i];
  };
  return readings.filter((r) => r.signedIn).reduce((best, r) => (!best || better(r, best) ? r : best), null);
}

/**
 * Hands each listed site's session to the instances that list it, through
 * `call` of a group session. What went where is kept as a digest, so an
 * unchanged session is not sent again and a sign-out reaches only the
 * instances that had one. Resolves with whether an instance did not answer,
 * so the background tries again a little later.
 */
async function syncSignIns(siblings, call) {
  const { signinEnabled } = await chrome.storage.local.get('signinEnabled');
  if (signinEnabled === false) return { retry: false };
  const lists = await listedSites(siblings, call);
  const sites = allSites(lists);
  if (lists.some((l) => l.answered)) await chrome.storage.local.set({ signinSites: sites });
  // A site an instance has just begun to list waits for its own access, and
  // the others go on meanwhile.
  const readable = [];
  for (const site of sites) if ((await signinState([site])).on) readable.push(site);
  if (readable.length === 0) return { retry: false };
  let retry = lists.some((l) => !l.answered);

  const stored = await chrome.storage.local.get(['signinHanded', 'signinChanged']);
  const handed = stored.signinHanded && typeof stored.signinHanded === 'object' ? stored.signinHanded : {};
  const changed = mergeChanges(stored.signinChanged, sites);
  const stores = await cookieStores(handed, changed);

  for (const site of readable) {
    const readings = await Promise.all(
      stores.map(async (store) => {
        const cookies = await readSiteCookies(site, store);
        return { store, cookies, signedIn: signInCookies(site, cookies).length > 0 };
      }),
    );
    const lastStore = Object.values(handed).find((bySite) => bySite[site.id])?.[site.id]?.store;
    const best = pickReading(readings, changed[site.id] ?? {}, lastStore);
    const print = best ? await sessionPrint(site, best.cookies) : '';

    for (const { instance, sites: listed } of lists) {
      if (!listed.some((s) => s.id === site.id)) continue;
      const id = instance.instanceId;
      const last = handed[id]?.[site.id];
      if (best ? last?.print === print : !last) continue;
      if (!best && last.refused) {
        delete handed[id][site.id];
        continue;
      }
      const path = `/api/sitelogins/${encodeURIComponent(site.id)}/handover`;
      const res = await call(id, 'POST', path, JSON.stringify({ cookies: best ? best.cookies : [] })).catch(() => null);
      if (!res || res.status >= 500) {
        retry = true;
        continue;
      }
      if (res.status !== 422 && (res.status < 200 || res.status >= 300)) continue;
      if (!best) {
        delete handed[id][site.id];
        continue;
      }
      let account = '';
      try {
        account = String(JSON.parse(res.body)?.account ?? '');
      } catch {
        // Taken all the same; only the name is missing.
      }
      handed[id] ??= {};
      handed[id][site.id] = { print, store: best.store, name: instanceLabel(instance), account, refused: res.status === 422 };
    }
  }
  await chrome.storage.local.set({ signinHanded: handed, signinChanged: changed });
  return { retry };
}
