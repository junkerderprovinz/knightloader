// The settings page's part of passing sign-ins on (signin.js): the notice at
// the top while the browser has not allowed reading the listed sites, and the
// switch in the browser card that asks for the access or gives it back.

const signinAccessEl = document.getElementById('signinAccess');
const signinAllowEl = document.getElementById('signinAccessAllow');
const signinKeepOffEl = document.getElementById('signinAccessKeepOff');
const signinRowEl = document.getElementById('signinRow');
const signinEnabledEl = document.getElementById('signinEnabled');
const signinRefusedEl = document.getElementById('signinRefused');

/** The sites last rendered, held so a click asks for them before anything is awaited. */
let signinSites = [];
let signinRefused = false;

async function renderSignin() {
  const [state, phrase] = await Promise.all([signinState(), readPhrase()]);
  signinSites = state.sites;
  const names = siteNames(state.sites);
  const known = !!phrase && state.sites.length > 0;

  document.getElementById('signinAccessHeading').textContent = t('signin.askTitle');
  document.getElementById('signinAccessBody').textContent = t('signin.askBody', { sites: names });
  signinAllowEl.textContent = t('signin.allow');
  signinKeepOffEl.textContent = t('signin.keepOff');
  rowLabel('signinLabel', t('signin.toggle'), t('signin.toggleInfo'), signinEnabledEl);
  document.getElementById('signinRefusedTitle').textContent = t('signin.refusedTitle');
  document.getElementById('signinRefusedReason').textContent = t('signin.refused', { sites: names });

  signinAccessEl.hidden = !known || !state.wanted || state.on;
  signinRowEl.hidden = !known;
  signinEnabledEl.setAttribute('aria-checked', String(state.on));
  signinRefusedEl.hidden = state.on || !signinRefused;
}

/** Nothing may be awaited before requestSignins, or the click stops counting as a gesture. */
async function turnSigninOn() {
  const granted = await requestSignins(signinSites);
  signinRefused = !granted;
  await renderSignin();
  if (!granted) {
    shake(signinEnabledEl);
    signinRefusedEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }
}

signinAllowEl.addEventListener('click', () => void turnSigninOn());
signinKeepOffEl.addEventListener('click', async () => {
  await releaseSignins();
  await renderSignin();
});
signinEnabledEl.addEventListener('click', async () => {
  if (signinEnabledEl.getAttribute('aria-checked') !== 'true') {
    await turnSigninOn();
    return;
  }
  signinEnabledEl.setAttribute('aria-checked', 'false');
  signinRefused = false;
  await releaseSignins();
  await renderSignin();
});

chrome.permissions.onAdded.addListener(() => void renderSignin());
chrome.permissions.onRemoved.addListener(() => void renderSignin());
chrome.storage.onChanged.addListener(async (changes, area) => {
  if (area !== 'local') return;
  if (changes.language) await loadLanguage();
  if (changes.language || changes.signinSites || changes.signinEnabled) await renderSignin();
});

(async () => {
  await loadLanguage();
  await renderSignin();
  await learnSignInSites();
})();
