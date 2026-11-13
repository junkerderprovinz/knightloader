// The tab the popup opens to hand one site's session over (signin.js). It
// names the site and the instance, asks the browser for that site's cookies on
// the click and says how it went.

const signInQuery = new URLSearchParams(location.search);
const signInTitleEl = document.getElementById('title');
const signInReasonEl = document.getElementById('reason');
const signInButton = document.getElementById('handOver');
const signInResultEl = document.getElementById('result');

let signInTarget = null;

/** The site and instance the popup named, as the instance lists them. */
function findSignIn() {
  return withGroup(async ({ siblings, call }) => {
    const instance = siblings.find((s) => s.instanceId === signInQuery.get('instance'));
    if (!instance) return null;
    const list = await readJSON(call, instance.instanceId, SIGNIN_LIST);
    const site = (Array.isArray(list) ? list : []).find((s) => readableSite(s) && s.id === signInQuery.get('site'));
    return site ? { instance, site } : null;
  });
}

function showResult(text, ok) {
  signInResultEl.textContent = text;
  signInResultEl.className = ok ? 'ok' : 'warn';
  signInResultEl.hidden = false;
}

const OUTCOME_TEXT = {
  done: ({ account }) => (account ? t('signin.doneAs', { account }) : t('signin.done')),
  refused: ({ site }) => t('signin.refused', { site: site.name }),
  noSession: ({ site }) => t('signin.noSession', { site: site.name }),
  gone: () => t('signin.gone'),
  failed: ({ instance }) => t('signin.failed', { instance: instanceLabel(instance) }),
};

signInButton.addEventListener('click', async () => {
  if (!signInTarget) return;
  const { site, instance } = signInTarget;
  signInButton.disabled = true;
  // Nothing is awaited before signInWith, whose first step is the browser's
  // permission prompt, so the click still counts as the user's.
  const res = await signInWith(site, {
    storeId: signInQuery.get('store') || undefined,
    send: (method, path, body) => withGroup(({ call }) => call(instance.instanceId, method, path, body)),
  });
  showResult(OUTCOME_TEXT[res.outcome]({ ...res, site, instance }), res.outcome === 'done');
  signInButton.disabled = res.outcome === 'done';
});

(async () => {
  await applyAppearance();
  await loadLanguage();
  signInTarget = await findSignIn().catch(() => null);
  if (!signInTarget) {
    signInTitleEl.textContent = 'KnightLoader';
    showResult(t('signin.gone'), false);
    return;
  }
  const { site, instance } = signInTarget;
  document.title = t('signin.pageTitle', { site: site.name });
  signInTitleEl.textContent = document.title;
  signInReasonEl.textContent = t('signin.pageReason', { site: site.name, instance: instanceLabel(instance) });
  signInButton.textContent = t('signin.pageButton');
  signInButton.hidden = false;
})();
