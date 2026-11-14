// The popup's word on passing sign-ins on (signin.js): which sites KnightLoader
// has a sign-in for from this browser, or a notice while the browser has not
// allowed reading them yet. Its button opens the settings page, which asks:
// a permission prompt closes the popup and the request with it.

const signInsEl = document.getElementById('signIns');

async function renderSignIns() {
  const nodes = await signInNodes();
  signInsEl.replaceChildren(...nodes);
  signInsEl.hidden = nodes.length === 0;
}

/** The notice, or a line for each site KnightLoader has a sign-in for, or nothing. */
async function signInNodes() {
  if (!(await readPhrase())) return [];
  const state = await signinState();
  if (!state.wanted || state.sites.length === 0) return [];
  if (!state.on) return [askNotice(state.sites)];
  const { signinHanded } = await chrome.storage.local.get('signinHanded');
  return state.sites.flatMap((site) => {
    const had = Object.values(signinHanded ?? {})
      .map((bySite) => bySite[site.id])
      .filter((h) => h && !h.refused);
    if (had.length === 0) return [];
    const line = document.createElement('p');
    line.className = 'signInLine';
    const account = had.find((h) => h.account)?.account;
    line.textContent = account ? t('signin.passedOnAs', { site: site.name, account }) : t('signin.passedOn', { site: site.name });
    return [line];
  });
}

function askNotice(sites) {
  const box = document.createElement('div');
  box.className = 'glim-unavailable';
  const title = document.createElement('span');
  title.className = 'glim-unavailable-title';
  title.textContent = t('signin.askTitle');
  const reason = document.createElement('span');
  reason.className = 'glim-unavailable-reason';
  reason.textContent = t('signin.askBody', { sites: siteNames(sites) });
  const allow = document.createElement('button');
  allow.type = 'button';
  allow.className = 'primary';
  allow.textContent = t('signin.allow');
  allow.addEventListener('click', () => chrome.runtime.openOptionsPage());
  box.append(title, reason, allow);
  return box;
}

chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && (changes.signinSites || changes.signinHanded)) void renderSignIns();
});

(async () => {
  await loadLanguage();
  await renderSignIns();
  await learnSignInSites();
})();
