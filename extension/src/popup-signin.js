// The popup's notice for a site sign-in an instance waits for, which the
// owner started on its accounts page. The button opens signin.html in a tab,
// in the cookie store of the tab the popup was opened over, and that page asks
// for the site's cookies and hands the session over.

const signInsEl = document.getElementById('signIns');

async function renderSignIns() {
  if (!(await readPhrase())) return;
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  let waiting;
  try {
    waiting = await withGroup(({ siblings, call }) => waitingSignIns(siblings, call));
  } catch {
    return;
  }
  if (waiting.length === 0) return;
  signInsEl.replaceChildren(...waiting.map((w) => signInNotice(w, tab)));
  signInsEl.hidden = false;
}

function signInNotice({ instance, site }, tab) {
  const box = document.createElement('div');
  box.className = 'glim-unavailable';
  const title = document.createElement('span');
  title.className = 'glim-unavailable-title';
  title.textContent = t('signin.popupTitle', { site: site.name });
  const reason = document.createElement('span');
  reason.className = 'glim-unavailable-reason';
  reason.textContent = t('signin.popupReason', { site: site.name, instance: instanceLabel(instance) });
  const open = document.createElement('button');
  open.type = 'button';
  open.className = 'glim-btn-subtle';
  open.textContent = t('signin.popupOpen');
  open.addEventListener('click', async () => {
    const query = new URLSearchParams({ site: site.id, instance: instance.instanceId });
    if (tab?.cookieStoreId) query.set('store', tab.cookieStoreId);
    await chrome.tabs.create({ url: chrome.runtime.getURL(`signin.html?${query}`) });
    window.close();
  });
  box.append(title, reason, open);
  return box;
}

(async () => {
  await loadLanguage();
  await renderSignIns();
})();
