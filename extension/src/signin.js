// Handing the session of a site the owner signs in to over to KnightLoader
// (internal/sitelogin). The instance waits for it from its accounts page; the
// popup offers it (popup-signin.js) and signin.html does it, in a tab, since a
// permission prompt closes the popup and the request with it. Only that one
// site's cookies are read, under access asked for that site at that moment
// and given back once they are read.
//
// Plain functions, so check-signin.mjs can run them without a browser.

const SIGNIN_LIST = '/api/sitelogins/handover';

/** What reading one site's cookies needs: the cookies, on its own domains alone. */
function siteAccess(site) {
  return { permissions: ['cookies'], origins: site.domains.flatMap((d) => [`https://${d}/*`, `https://*.${d}/*`]) };
}

/** Whether a cookie's domain is one of the site's or below one of them. */
function siteOwns(site, domain) {
  const d = String(domain ?? '').toLowerCase().replace(/^\./, '');
  return site.domains.some((own) => d === own || d.endsWith(`.${own}`));
}

/**
 * Whether an instance's entry is a site this browser may read: a name and
 * domains of at least two labels each, so no entry reaches every site of a
 * top-level domain or more.
 */
function readableSite(site) {
  return (
    !!site &&
    typeof site.id === 'string' &&
    typeof site.name === 'string' &&
    Array.isArray(site.domains) &&
    site.domains.length > 0 &&
    site.domains.every((d) => typeof d === 'string' && /^[a-z0-9-]+(\.[a-z0-9-]+)+$/.test(d))
  );
}

/** The sites the instances of the group wait for a sign-in to, each as { instance, site }. */
async function waitingSignIns(siblings, call) {
  const answers = await Promise.all(siblings.map(async (instance) => ({ instance, list: await readJSON(call, instance.instanceId, SIGNIN_LIST) })));
  return answers.flatMap(({ instance, list }) =>
    (Array.isArray(list) ? list : []).filter((site) => readableSite(site) && site.waiting).map((site) => ({ instance, site })),
  );
}

/**
 * The site's cookies in the cookie store the sign-in came from, with the
 * fields the instance builds a cookies.txt from and nothing else.
 */
async function readSiteCookies(site, storeId) {
  const found = new Map();
  for (const domain of site.domains) {
    for (const c of await chrome.cookies.getAll(storeId ? { domain, storeId } : { domain })) {
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

/**
 * Gives back what reading a site took. The cookies stay while taking
 * downloads over or finding media still reads cookies on every site. The
 * site's own domains always go: Firefox keeps them apart from the
 * <all_urls> of the features that hold all websites, so they would outlive
 * that access.
 */
async function releaseSiteAccess(site) {
  const [takeover, media] = await Promise.all([takeoverState(), mediaState()]);
  const permissions = takeover.on || media.on ? [] : ['cookies'];
  await chrome.permissions.remove({ permissions, origins: siteAccess(site).origins }).catch(() => false);
}

/**
 * Hands one site's session to an instance through `send(method, path, body)`.
 * The access is asked for before anything is awaited, inside the click.
 * Resolves with the outcome, one of 'done', 'refused', 'noSession', 'gone' and
 * 'failed', and the account the instance found signed in.
 */
async function signInWith(site, { storeId, send }) {
  const granted = await chrome.permissions.request(siteAccess(site)).catch(() => false);
  if (!granted) return { outcome: 'refused' };
  let cookies;
  try {
    cookies = await readSiteCookies(site, storeId);
  } finally {
    await releaseSiteAccess(site);
  }
  if (cookies.length === 0) return { outcome: 'noSession' };
  const res = await send('POST', `/api/sitelogins/${encodeURIComponent(site.id)}/handover`, JSON.stringify({ cookies })).catch(() => null);
  if (!res) return { outcome: 'failed' };
  if (res.status === 422) return { outcome: 'noSession' };
  if (res.status === 404) return { outcome: 'gone' };
  if (res.status < 200 || res.status >= 300) return { outcome: 'failed' };
  let answer = null;
  try {
    answer = JSON.parse(res.body);
  } catch {
    // Stored all the same; only the name is missing.
  }
  return { outcome: 'done', account: typeof answer?.account === 'string' ? answer.account : '' };
}
